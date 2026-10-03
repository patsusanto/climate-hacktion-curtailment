// Package worker is the live model service. It runs the trained model on the house in a request
// and streams the result, in the same events the public API sends to the page.
//
// It is the only server that imports the model, so the public API (internal/api) stays small.
// The model is loaded once at startup, together with the data of each window it can replay;
// a run is then about a second of computing.
package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/battery"
	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/forecast"
	"climate-hacktion-curtailment/backend/internal/model/runfile"
	"climate-hacktion-curtailment/backend/internal/model/simulate"
	"climate-hacktion-curtailment/backend/internal/wire"
)

const (
	maxBodyBytes = 16 << 10
	streamTick   = 50 * time.Millisecond
)

// Config is how the worker is set up.
type Config struct {
	// DataDir holds one folder per window (DataDir/validation, ...), as written by cmd/genrun.
	DataDir string
	// Windows are the replay windows by name. It defaults to data.Windows. A window with no
	// folder in DataDir is left out.
	Windows map[string][2]time.Time
	// StreamFor is how long a full run takes to stream, so the page animates. Zero streams as
	// fast as the run is computed.
	StreamFor time.Duration
	MaxRuns   int           // model runs at once (the work is CPU-bound); default 2
	CacheSize int           // finished runs kept for step detail; default 8
	CacheTTL  time.Duration // default 15 minutes
	Logger    *log.Logger
}

type window struct {
	name        string
	start, end  time.Time
	in          *forecast.Inputs
	first, last int // interval indices of start and end
}

// Server runs the model.
type Server struct {
	cfg     Config
	models  *forecast.Models
	windows map[string]*window
	sem     chan struct{}
	results *cache
	log     *log.Logger
	year    *forecast.Inputs // a year of data for the payback figures; nil without it
}

// New loads the models and the data of every window that has data.
func New(cfg Config) (*Server, error) {
	if cfg.Windows == nil {
		cfg.Windows = data.Windows
	}
	if cfg.MaxRuns <= 0 {
		cfg.MaxRuns = 2
	}
	if cfg.CacheSize <= 0 {
		cfg.CacheSize = 8
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 15 * time.Minute
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	t0 := time.Now()
	models, err := forecast.Load()
	if err != nil {
		return nil, fmt.Errorf("loading the models: %w", err)
	}
	s := &Server{
		cfg:     cfg,
		models:  models,
		windows: map[string]*window{},
		sem:     make(chan struct{}, cfg.MaxRuns),
		results: newCache(cfg.CacheSize, cfg.CacheTTL),
		log:     cfg.Logger,
	}
	for name, bounds := range cfg.Windows {
		dir, ok := windowDir(cfg.DataDir, name)
		if !ok {
			s.log.Printf("window %q: no data in %s, leaving it out", name, filepath.Join(cfg.DataDir, name))
			continue
		}
		d, err := data.Load(dir)
		if err != nil {
			return nil, fmt.Errorf("window %q: %w", name, err)
		}
		in, err := models.Prepare(d)
		if err != nil {
			return nil, fmt.Errorf("window %q: %w", name, err)
		}
		w := &window{name: name, start: bounds[0], end: bounds[1], in: in, first: in.Index(bounds[0]), last: in.Index(bounds[1])}
		if w.first < 0 || w.last < w.first {
			return nil, fmt.Errorf("window %q: the data in %s does not cover %s to %s", name, dir,
				bounds[0].Format(time.RFC3339), bounds[1].Format(time.RFC3339))
		}
		s.windows[name] = w
		s.log.Printf("window %q ready: %d steps from %s", name, w.last-w.first+1, dir)
	}
	if dir, ok := windowDir(cfg.DataDir, "year"); ok {
		d, err := data.Load(dir)
		if err != nil {
			return nil, fmt.Errorf("year: %w", err)
		}
		if s.year, err = models.Prepare(d); err != nil {
			return nil, fmt.Errorf("year: %w", err)
		}
		// Forecast the whole year now, once; every house's payback replay then reuses it.
		if _, err := models.Run(s.year, s.year.Index(data.Year[0]), s.year.Index(data.Year[1])); err != nil {
			return nil, fmt.Errorf("year: %w", err)
		}
		s.log.Printf("year data ready from %s: runs include payback figures", dir)
	} else {
		s.log.Printf("no year data in %s: runs leave out the payback figures", filepath.Join(cfg.DataDir, "year"))
	}
	if len(s.windows) == 0 {
		return nil, fmt.Errorf("no window has data in %s; run cmd/genrun once for a window, or point DATA_DIR at its download", cfg.DataDir)
	}
	s.log.Printf("worker ready in %s", time.Since(t0).Round(time.Millisecond))
	return s, nil
}

// windowDir finds a window's data: DataDir/<name>, or DataDir/<name>-nopd, which is what genrun
// writes when it skips AEMO pre-dispatch.
func windowDir(base, name string) (string, bool) {
	for _, dir := range []string{filepath.Join(base, name), filepath.Join(base, name+"-nopd")} {
		if data.Exists(filepath.Join(dir, "prices.csv")) {
			return dir, true
		}
	}
	return "", false
}

// Windows lists the windows that can be run.
func (s *Server) Windows() []string {
	var names []string
	for name := range s.windows {
		names = append(names, name)
	}
	return names
}

// Routes is the worker's HTTP handler.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/playground/run", s.run)
	mux.HandleFunc("GET /v1/playground/run/{id}/steps/{i}", s.step)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"message": msg})
}

var errBusy = errors.New("the model is busy with other runs; try again in a moment")

// acquire takes a model slot without waiting. A busy worker says so rather than queueing, so the
// caller can tell the user straight away.
func (s *Server) acquire() (release func(), ok bool) {
	select {
	case s.sem <- struct{}{}:
		return func() { <-s.sem }, true
	default:
		return nil, false
	}
}

func (s *Server) options(p wire.Params) runfile.Options {
	return runfile.Options{ID: p.ID(), WindowName: p.Window, DetailEvery: 1, TrainedBefore: s.models.TrainedBefore()}
}

func spec(p wire.Params) (battery.Spec, error) {
	return battery.NewSpec(p.PvKwAc, p.BatteryKwh, p.BatteryKw, p.ExportCapKw, p.DailyLoadKwh, 0)
}

// result is the finished replay for a request: from the cache if it is there, otherwise computed.
// Computing needs a free slot; stop is polled during the replay so a client that has gone stops it.
func (s *Server) result(p wire.Params, sp battery.Spec, win *window, stop func() error) (*simulate.Result, error) {
	id := p.ID()
	if res, ok := s.results.get(id); ok {
		return res, nil
	}
	release, ok := s.acquire()
	if !ok {
		return nil, errBusy
	}
	defer release()
	t0 := time.Now()
	opt := simulate.DefaultOptions()
	// The year for the payback runs alongside the window.
	var annual simulate.Annual
	annualErr := make(chan error, 1)
	if s.year != nil {
		go func() {
			var err error
			annual, err = simulate.RunAnnual(s.models, s.year, sp, data.Year[0], data.Year[1], simulate.AnnualWeeks, opt)
			annualErr <- err
		}()
	} else {
		annualErr <- nil
	}
	res, err := simulate.Run(s.models, win.in, sp, win.start, win.end, opt, func(simulate.Step) error { return stop() })
	if aerr := <-annualErr; err == nil && aerr != nil {
		err = aerr
	}
	if err != nil {
		return nil, err
	}
	if s.year != nil {
		res.Annual = &annual
	}
	s.results.put(id, res)
	s.log.Printf("ran %s: %d steps in %s", id, len(res.Steps), time.Since(t0).Round(time.Millisecond))
	return res, nil
}

// POST /v1/playground/run
func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	var req wire.PlaygroundRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	p, err := req.Resolve()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	win := s.windows[p.Window]
	if win == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("no data is loaded for window %q", p.Window))
		return
	}
	sp, err := spec(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	// Compute first, so that a busy or failed run is an ordinary error status and not a stream
	// that stops. The result is cached before anything is sent, so the page can ask for the
	// detail of a step while the stream is still going.
	res, err := s.result(p, sp, win, r.Context().Err)
	switch {
	case errors.Is(err, errBusy):
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	case r.Context().Err() != nil:
		return // the client has gone
	case err != nil:
		s.log.Printf("run %s failed: %v", p.ID(), err)
		writeError(w, http.StatusInternalServerError, "the model could not run this house")
		return
	}

	format := wire.StreamFormat(r.Header.Get("Accept"))
	if format == "" {
		format = wire.FormatNDJSON
	}
	h := w.Header()
	h.Set("Content-Type", wire.ContentType(format))
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // stop nginx buffering the stream
	h.Set("X-Run-Id", p.ID())
	w.WriteHeader(http.StatusOK)

	emit := func(line []byte) bool { _, err := w.Write(wire.Encode(format, line)); return err == nil }

	opts := s.options(p)
	n := len(res.Steps)
	meta := runfile.Meta(res.Spec, res.Options, res.Steps[0].Time, res.Steps[n-1].Time, n, opts)
	meta.Assumptions.AddressLabel = p.Address
	if !emit(wire.MetaLine(meta)) {
		return
	}
	f.Flush() // the page draws its axes as soon as the metadata arrives

	batch, pause := s.pacing(r, n)
	var ticker runfile.Ticker
	for i := 0; i < n; {
		for end := min(i+batch, n); i < end; i++ {
			if !emit(wire.StepLine(ticker.Tick(i, res.Steps[i]))) {
				return
			}
		}
		f.Flush()
		if pause > 0 && i < n {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(pause):
			}
		}
	}
	if emit(wire.DoneLine(runfile.Summary(res, opts))) {
		f.Flush()
	}
}

// pacing is how many steps to write per flush and how long to wait between flushes, so a full
// run takes about StreamFor. ?speed=max skips the wait.
func (s *Server) pacing(r *http.Request, n int) (batch int, pause time.Duration) {
	if s.cfg.StreamFor <= 0 || r.URL.Query().Get("speed") == "max" {
		return n, 0
	}
	ticks := max(int(s.cfg.StreamFor/streamTick), 1)
	return (n + ticks - 1) / ticks, streamTick
}

// GET /v1/playground/run/{id}/steps/{i}: what the planner saw at any step of a run.
func (s *Server) step(w http.ResponseWriter, r *http.Request) {
	p, err := wire.ParseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	win := s.windows[p.Window]
	if win == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no data is loaded for window %q", p.Window))
		return
	}
	i, err := strconv.Atoi(r.PathValue("i"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "step must be an integer")
		return
	}
	sp, err := spec(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.result(p, sp, win, r.Context().Err)
	switch {
	case errors.Is(err, errBusy):
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	case r.Context().Err() != nil:
		return
	case err != nil:
		s.log.Printf("step %s/%d failed: %v", p.ID(), i, err)
		writeError(w, http.StatusInternalServerError, "the model could not run this house")
		return
	}
	if i < 0 || i >= len(res.Steps) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("step must be an integer from 0 to %d", len(res.Steps)-1))
		return
	}
	d, err := runfile.StepDecision(res, i)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}
