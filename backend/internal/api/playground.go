package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"climate-hacktion-curtailment/backend/internal/wire"
)

const (
	maxBodyBytes  = 16 << 10
	maxAddressLen = 200
	streamTick    = 50 * time.Millisecond
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
func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	var req wire.PlaygroundRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	address := strings.TrimSpace(req.Address)
	switch {
	case address == "":
		writeError(w, http.StatusBadRequest, "address is required")
		return
	case utf8.RuneCountInString(address) > maxAddressLen:
		writeError(w, http.StatusBadRequest, "address is too long")
		return
	case req.PvKwAc <= 0:
		writeError(w, http.StatusBadRequest, "pv_kw_ac must be greater than 0")
		return
	case req.BatteryKwh < 0:
		writeError(w, http.StatusBadRequest, "battery_kwh must not be negative")
		return
	}
	for name, v := range map[string]*float64{
		"battery_kw": req.BatteryKw, "export_cap_kw": req.ExportCapKw, "daily_load_kwh": req.DailyLoadKwh,
	} {
		if v != nil && *v < 0 {
			writeError(w, http.StatusBadRequest, name+" must not be negative")
			return
		}
	}

	window, err := windowName(req.Window)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	run := pickRun(s.runs, window, req.PvKwAc, req.BatteryKwh)
	if run == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("no example run available for window %q", window))
		return
	}

	// The address only changes the label; the series itself is precomputed.
	meta := run.Meta
	meta.Assumptions.AddressLabel = address

	// The page sends one request and reads the stream from the response: each
	// line is {"type":"meta"|"step"|"done"|"error", ...}. Other clients get the
	// run id and metadata as JSON and read /events separately.
	if format := streamFormat(r.Header.Get("Accept")); format != "" {
		s.streamRun(w, r, run, meta, format)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run_id": run.ID, "meta": meta})
}

const (
	formatNDJSON = "ndjson"
	formatSSE    = "sse"
)

// streamFormat says how to stream to a client, from its Accept header: NDJSON
// if it asks for it, otherwise SSE if it asks for that, otherwise "" (no stream).
func streamFormat(accept string) string {
	accept = strings.ToLower(accept)
	switch {
	case strings.Contains(accept, "application/x-ndjson"):
		return formatNDJSON
	case strings.Contains(accept, "text/event-stream"):
		return formatSSE
	}
	return ""
}

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
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not encode the run metadata")
		return
	}

	h := w.Header()
	if format == formatSSE {
		h.Set("Content-Type", "text/event-stream")
	} else {
		h.Set("Content-Type", "application/x-ndjson")
	}
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // stop nginx buffering the stream
	h.Set("X-Run-Id", run.ID)
	w.WriteHeader(http.StatusOK)

	// emit writes one event; line is JSON followed by a newline.
	emit := func(line []byte) error {
		if format == formatSSE {
			if _, err := w.Write([]byte("data: ")); err != nil {
				return err
			}
			if _, err := w.Write(line); err != nil {
				return err
			}
			_, err := w.Write([]byte("\n")) // the blank line that ends an SSE event
			return err
		}
		_, err := w.Write(line)
		return err
	}

	metaLine := append(append([]byte(`{"type":"meta","meta":`), metaJSON...), "}\n"...)
	if emit(metaLine) != nil {
		return
	}
	f.Flush() // the page draws its axes as soon as the metadata arrives

	n := len(run.lines)
	batch, pause := s.pacing(r, n)
	for i := 0; i < n; {
		for end := min(i+batch, n); i < end; i++ {
			if emit(run.lines[i]) != nil {
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
	if emit(run.doneLine) != nil {
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

// windowName accepts "validation" or "test". Custom {start, end} windows are
// not supported yet because runs are precomputed.
func windowName(raw json.RawMessage) (string, error) {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		if name == "validation" || name == "test" {
			return name, nil
		}
		return "", fmt.Errorf(`window must be "validation" or "test"`)
	}
	var custom struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	if json.Unmarshal(raw, &custom) == nil && custom.Start != "" && custom.End != "" {
		return "", fmt.Errorf("custom windows are not supported yet; use \"validation\" or \"test\"")
	}
	return "", fmt.Errorf(`window must be "validation", "test" or {"start","end"}`)
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
func (s *Server) step(w http.ResponseWriter, r *http.Request) {
	run := s.runs[r.PathValue("id")]
	if run == nil {
		writeError(w, http.StatusNotFound, "unknown run_id")
		return
	}
	i, err := strconv.Atoi(r.PathValue("i"))
	if err != nil || i < 0 || i >= len(run.frames) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("step must be an integer from 0 to %d", len(run.frames)-1))
		return
	}
	detail, ok := run.steps[i]
	if !ok {
		writeError(w, http.StatusNotFound, "no forecast detail stored for this step")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(detail)
}
