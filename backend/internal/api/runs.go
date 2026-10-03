package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"strconv"

	"climate-hacktion-curtailment/backend/internal/wire"
)

// Run is one precomputed example, ready to stream.
type Run struct {
	ID         string
	WindowName string // one of wire.Windows
	Meta       wire.Meta
	frames     [][]byte       // complete SSE "step" events (id, event, data), indexed by i
	done       []byte         // complete SSE "done" event
	lines      [][]byte       // {"type":"step","tick":{...}} plus a newline, indexed by i
	doneLine   []byte         // {"type":"done","summary":{...}} plus a newline
	steps      map[int][]byte // StepDecision JSON by step index (may be sparse)
}

// runFile is the on-disk format of runs/<run_id>.json.
type runFile struct {
	RunID      string                     `json:"run_id"`
	WindowName string                     `json:"window_name"`
	Meta       wire.Meta                  `json:"meta"`
	Ticks      []json.RawMessage          `json:"ticks"`
	Summary    json.RawMessage            `json:"summary"`
	Steps      map[string]json.RawMessage `json:"steps"`
}

// loadRuns reads every *.json file in dir and validates it.
func loadRuns(fsys fs.FS, dir string) (map[string]*Run, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	runs := map[string]*Run{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := dir + "/" + e.Name()
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		run, err := parseRun(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if _, dup := runs[run.ID]; dup {
			return nil, fmt.Errorf("%s: duplicate run_id %q", name, run.ID)
		}
		runs[run.ID] = run
	}
	if len(runs) == 0 {
		return nil, fmt.Errorf("no runs found in %s", dir)
	}
	return runs, nil
}

func parseRun(data []byte) (*Run, error) {
	var f runFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	n := f.Meta.Window.N
	switch {
	case f.RunID == "":
		return nil, fmt.Errorf("missing run_id")
	case !wire.KnownWindow(f.WindowName):
		return nil, fmt.Errorf("window_name must be one of %v, got %q", wire.Windows, f.WindowName)
	case n <= 0 || len(f.Ticks) != n:
		return nil, fmt.Errorf("meta.window.n is %d but there are %d ticks", n, len(f.Ticks))
	case len(f.Summary) == 0:
		return nil, fmt.Errorf("missing summary")
	}

	run := &Run{
		ID:         f.RunID,
		WindowName: f.WindowName,
		Meta:       f.Meta,
		frames:     make([][]byte, n),
		lines:      make([][]byte, n),
		steps:      make(map[int][]byte, len(f.Steps)),
	}

	for i, raw := range f.Ticks {
		var idx struct {
			I    int      `json:"i"`
			Self *float64 `json:"cumulative_self_aud"`
		}
		if err := json.Unmarshal(raw, &idx); err != nil || idx.I != i {
			return nil, fmt.Errorf("tick at position %d has i=%d", i, idx.I)
		}
		// The page draws the self-consumption cost from this field; without it
		// the chart fills with blanks, so refuse a run that predates it.
		if idx.Self == nil {
			return nil, fmt.Errorf("tick %d has no cumulative_self_aud; regenerate the run", i)
		}
		// data must be one JSON object with no line breaks inside it
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw); err != nil {
			return nil, err
		}
		run.frames[i] = fmt.Appendf(nil, "id: %d\nevent: step\ndata: %s\n\n", i, buf.Bytes())
		run.lines[i] = fmt.Appendf(nil, `{"type":"step","tick":%s}`+"\n", buf.Bytes())
	}

	var summary bytes.Buffer
	if err := json.Compact(&summary, f.Summary); err != nil {
		return nil, err
	}
	run.done = fmt.Appendf(nil, "id: %d\nevent: done\ndata: %s\n\n", n-1, summary.Bytes())
	run.doneLine = fmt.Appendf(nil, `{"type":"done","summary":%s}`+"\n", summary.Bytes())

	for k, raw := range f.Steps {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 || i >= n {
			return nil, fmt.Errorf("steps key %q is not a valid step index", k)
		}
		run.steps[i] = bytes.Clone(raw)
	}
	return run, nil
}

// pickRun returns the run for the window whose solar and battery sizes are
// closest to the request, or nil if there is no run for that window.
func pickRun(runs map[string]*Run, window string, pvKw, batteryKwh float64) *Run {
	var best *Run
	bestDist := math.Inf(1)
	for _, r := range runs {
		if r.WindowName != window {
			continue
		}
		d := math.Abs(r.Meta.Spec.PvKwAc-pvKw)/math.Max(r.Meta.Spec.PvKwAc, 1) +
			math.Abs(r.Meta.Spec.BatteryKwh-batteryKwh)/math.Max(r.Meta.Spec.BatteryKwh, 1)
		// map order is random, so break ties by id to stay deterministic
		if d < bestDist || (d == bestDist && best != nil && r.ID < best.ID) {
			best, bestDist = r, d
		}
	}
	return best
}

// params is the house and window this run was made for, as the model worker would be asked for it.
func (r *Run) params() wire.Params {
	sp := r.Meta.Spec
	return wire.Params{
		PvKwAc: sp.PvKwAc, BatteryKwh: sp.BatteryKwh, BatteryKw: sp.BatteryKw,
		ExportCapKw: sp.ExportCapKw, DailyLoadKwh: sp.DailyLoadKwh, Window: r.WindowName,
	}
}
