package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/data"
	"climate-hacktion-curtailment/backend/internal/engine"
	"climate-hacktion-curtailment/backend/internal/forecast"
	"climate-hacktion-curtailment/backend/internal/house"
	"climate-hacktion-curtailment/backend/internal/planner"
	"climate-hacktion-curtailment/backend/internal/split"
)

// stand-in forecaster: the curve at row i is what happens at i+lead
type lookahead struct{}

func (lookahead) Curve(f *data.Frame, i int, spec house.HouseSpec, h int) (planner.ForecastCurve, bool, error) {
	if i < forecast.MaxLookbackV2 {
		return planner.ForecastCurve{}, false, nil
	}
	var c planner.ForecastCurve
	for _, lead := range split.Leads {
		if lead > h*split.StepsPerHour {
			continue
		}
		if i+lead >= f.Len() {
			return planner.ForecastCurve{}, false, nil
		}
		p := f.Price[i+lead]
		pv, load := house.ScaleUnits(f.PvUnit[i+lead], f.LoadUnit[i+lead], spec)
		c.Leads = append(c.Leads, planner.LeadForecast{
			LeadSteps: lead, PriceP10: p - 5, PriceP50: p, PriceP90: p + 5,
			PvKw: pv, PvLo: pv * 0.9, PvHi: pv * 1.1, LoadKw: load, LoadLo: load * 0.9, LoadHi: load * 1.1,
		})
	}
	return c, true, nil
}

// A run written by the engine must load through the service's own loader and
// pass its checks (ticks match meta.window.n, i is in order, steps are in range),
// so that genrun output can simply be dropped into runs/.
func TestEngineRunFileLoadsInTheService(t *testing.T) {
	f := &data.Frame{}
	t0 := time.Date(2026, 7, 1, 0, 0, 0, 0, split.NEM)
	for i := 0; i < 12*split.DaySteps; i++ {
		ts := t0.Add(time.Duration(i*split.StepMinutes) * time.Minute)
		h := float64(ts.Hour())
		price := 90.0
		if h >= 17 && h < 21 {
			price = 300
		}
		pv := 0.0
		if h > 7 && h < 17 {
			pv = 0.7 * math.Sin(math.Pi*(h-7)/10)
		}
		f.Times = append(f.Times, ts)
		f.Price = append(f.Price, price)
		f.PvUnit = append(f.PvUnit, pv)
		f.LoadUnit = append(f.LoadUnit, 0.2)
	}

	cfg := engine.DefaultConfig()
	start := forecast.MaxLookbackV2 + split.DaySteps
	res, err := engine.Replay(f, lookahead{}, cfg, start, start+split.DaySteps)
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "runs") // the service loads from a subdirectory (runs/)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.WriteRunFile(dir, engine.BuildRunFile("gen-test", "validation", cfg, res)); err != nil {
		t.Fatal(err)
	}

	runs, err := loadRuns(os.DirFS(parent), "runs")
	if err != nil {
		t.Fatalf("the service rejected the engine's run file: %v", err)
	}
	run := runs["gen-test"]
	if run == nil || len(run.frames) != split.DaySteps || run.WindowName != "validation" {
		t.Fatalf("loaded run: %+v", run)
	}
	if len(run.steps) != split.DaySteps/cfg.StepEvery {
		t.Errorf("%d step details loaded, want %d", len(run.steps), split.DaySteps/cfg.StepEvery)
	}
	if run.Meta.Spec.BatteryKwh != 10 || run.Meta.Window.N != split.DaySteps {
		t.Errorf("meta: %+v", run.Meta)
	}
}
