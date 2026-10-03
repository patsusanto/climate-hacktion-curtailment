package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"climate-hacktion-curtailment/backend/internal/wire"
)

const (
	maxBodyBytes = 16 << 10
	streamTick   = 50 * time.Millisecond
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"message": msg})
}

// POST /v1/playground/run
//
// The request is checked here, for every kind of client. Then:
//   - a house that is exactly one of the precomputed examples is served from memory;
//   - the page (it asks for a stream) gets a live run of any other house, if a model worker is
//     set up;
//   - anyone else, or everyone when there is no worker, gets the closest example.
func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
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
	format := wire.StreamFormat(r.Header.Get("Accept"))

	run := s.exactRun(p)
	if run == nil && format != "" && s.live != nil {
		s.liveRun(w, r, p, format)
		return
	}
	if run == nil {
		run = pickRun(s.runs, p.Window, p.PvKwAc, p.BatteryKwh)
	}
	if run == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("no example run available for window %q", p.Window))
		return
	}

	// The address only changes the label; the series itself is precomputed.
	meta := run.Meta
	meta.Assumptions.AddressLabel = p.Address

	// The page sends one request and reads the stream from the response: each
	// line is {"type":"meta"|"step"|"done"|"error", ...}. Other clients get the
	// run id and metadata as JSON and read /events separately.
	if format != "" {
		s.streamRun(w, r, run, meta, format)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run_id": run.ID, "meta": meta})
}

// exactRun is the precomputed example for exactly this house and window, or nil.
func (s *Server) exactRun(p wire.Params) *Run {
	for _, run := range s.runs {
		if run.params() == p.House() {
			return run
		}
	}
	return nil
}

// Aliases, so the names the rest of the package uses keep working.
const (
	formatNDJSON = wire.FormatNDJSON
	formatSSE    = wire.FormatSSE
)

func streamFormat(accept string) string { return wire.StreamFormat(accept) }

// streamRun answers the start request with the whole run as a stream: the
// metadata, then every step, then the summary. The run id travels in the
// X-Run-Id header so the page can ask for step detail afterwards.
//
// In NDJSON each event is one line of JSON. In SSE each is one "data:" line
// with nothing else, because the page parses every line it receives.
func (s *Server) streamRun(w http.ResponseWriter, r *http.Request, run *Run, meta wire.Meta, format string) {
	f, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	metaLine := wire.MetaLine(meta)

	h := w.Header()
	h.Set("Content-Type", wire.ContentType(format))
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // stop nginx buffering the stream
	h.Set("X-Run-Id", run.ID)
	w.WriteHeader(http.StatusOK)

	emit := func(line []byte) bool { _, err := w.Write(wire.Encode(format, line)); return err == nil }

	if !emit(metaLine) {
		return
	}
	f.Flush() // the page draws its axes as soon as the metadata arrives

	n := len(run.lines)
	batch, pause := s.pacing(r, n)
	for i := 0; i < n; {
		for end := min(i+batch, n); i < end; i++ {
			if !emit(run.lines[i]) {
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
	if !emit(run.doneLine) {
		return
	}
	f.Flush()
}

// pacing is how many steps to write per flush and how long to wait between
// flushes, so a full run takes about streamFor. ?speed=max skips the wait.
func (s *Server) pacing(r *http.Request, n int) (batch int, pause time.Duration) {
	if r.URL.Query().Get("speed") == "max" {
		return n, 0
	}
	ticks := max(int(s.streamFor/streamTick), 1)
	return (n + ticks - 1) / ticks, streamTick
}

func sseError(w http.ResponseWriter, f http.Flusher, msg string) {
	data, _ := json.Marshal(map[string]string{"message": msg})
	fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
	f.Flush()
}

// GET /v1/playground/run/{id}/events
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // stop nginx buffering the stream

	run := s.runs[r.PathValue("id")]
	if run == nil {
		sseError(w, f, "unknown run_id")
		return
	}
	n := len(run.frames)

	// On reconnect the browser sends the last id it saw; continue after it.
	start := 0
	if h := r.Header.Get("Last-Event-ID"); h != "" {
		last, err := strconv.Atoi(h)
		if err != nil || last < 0 || last >= n {
			sseError(w, f, "invalid Last-Event-ID")
			return
		}
		start = last + 1
	}

	batch, pause := s.pacing(r, n)

	for i := start; i < n; {
		for end := min(i+batch, n); i < end; i++ {
			if _, err := w.Write(run.frames[i]); err != nil {
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
	if _, err := w.Write(run.done); err != nil {
		return
	}
	f.Flush()
}

// GET /v1/playground/run/{id}/steps/{i}
//
// A step with stored detail is answered from memory. Any other step of an example, and the
// steps of a live run (the id comes from the X-Run-Id header of the live stream), are asked of
// the model worker, which can explain any step.
func (s *Server) step(w http.ResponseWriter, r *http.Request) {
	id, stepParam := r.PathValue("id"), r.PathValue("i")
	run := s.runs[id]
	if run == nil {
		if s.live != nil {
			if p, err := wire.ParseID(id); err == nil {
				s.liveStep(w, r, p, stepParam)
				return
			}
		}
		writeError(w, http.StatusNotFound, "unknown run_id")
		return
	}
	i, err := strconv.Atoi(stepParam)
	if err != nil || i < 0 || i >= len(run.frames) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("step must be an integer from 0 to %d", len(run.frames)-1))
		return
	}
	if detail, ok := run.steps[i]; ok {
		w.Header().Set("Content-Type", "application/json")
		w.Write(detail)
		return
	}
	if s.live != nil {
		s.liveStep(w, r, run.params(), stepParam)
		return
	}
	writeError(w, http.StatusNotFound, "no forecast detail stored for this step")
}
