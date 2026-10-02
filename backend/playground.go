package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxBodyBytes   = 16 << 10
	maxAddressLen  = 200
	streamTick     = 50 * time.Millisecond
	defaultStreamS = 25
)

type server struct {
	runs map[string]*Run
	// streamFor is how long a full run takes to stream. ?speed=max skips pacing.
	streamFor time.Duration
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"message": msg})
}

// POST /v1/playground/run
func (s *server) startRun(w http.ResponseWriter, r *http.Request) {
	var req PlaygroundRequest
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
	writeJSON(w, http.StatusOK, map[string]any{"run_id": run.ID, "meta": meta})
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
func (s *server) events(w http.ResponseWriter, r *http.Request) {
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

	batch, pause := n, time.Duration(0)
	if r.URL.Query().Get("speed") != "max" {
		ticks := max(int(s.streamFor/streamTick), 1)
		batch, pause = (n+ticks-1)/ticks, streamTick
	}

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
func (s *server) step(w http.ResponseWriter, r *http.Request) {
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
