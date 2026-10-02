// Package compat checks that the two halves of the system agree on the run
// file: what the engine writes (internal/model/engine) is accepted and served
// by the API (internal/api). It lives apart from both so that neither has to
// import the other.
package compat

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/api"
	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/engine"
	"climate-hacktion-curtailment/backend/internal/model/forecast"
	"climate-hacktion-curtailment/backend/internal/model/house"
	"climate-hacktion-curtailment/backend/internal/model/planner"
	"climate-hacktion-curtailment/backend/internal/model/split"
	"climate-hacktion-curtailment/backend/internal/wire"
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

func syntheticFrame(days int) *data.Frame {
	f := &data.Frame{}
	t0 := time.Date(2026, 7, 1, 0, 0, 0, 0, split.NEM)
	for i := 0; i < days*split.DaySteps; i++ {
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
	return f
}

// A run written by the engine must load through the service's own loader and
// stream correctly, so that genrun output can simply be dropped into runs/.
func TestEngineRunFileIsServedByTheAPI(t *testing.T) {
	cfg := engine.DefaultConfig()
	start := forecast.MaxLookbackV2 + split.DaySteps
	res, err := engine.Replay(syntheticFrame(12), lookahead{}, cfg, start, start+split.DaySteps)
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

	srv, err := api.New(os.DirFS(parent), "runs", time.Second)
	if err != nil {
		t.Fatalf("the service rejected the engine's run file: %v", err)
	}
	if srv.RunCount() != 1 {
		t.Fatalf("%d runs loaded", srv.RunCount())
	}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// start: the meta the engine wrote comes back, with the address swapped in
	resp, err := http.Post(ts.URL+"/v1/playground/run", "application/json", strings.NewReader(
		`{"address":"1 Example St","pv_kw_ac":10.5,"battery_kwh":10,"window":"validation"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		RunID string    `json:"run_id"`
		Meta  wire.Meta `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if started.RunID != "gen-test" || started.Meta.Window.N != split.DaySteps || started.Meta.Spec.BatteryKwh != 10 ||
		started.Meta.Assumptions.AddressLabel != "1 Example St" {
		t.Errorf("start response: %+v", started)
	}

	// stream: every tick arrives and the last event is the summary
	resp, err = http.Get(ts.URL + "/v1/playground/run/gen-test/events?speed=max")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if n := strings.Count(string(body), "event: step\n"); n != split.DaySteps {
		t.Errorf("%d step events, want %d", n, split.DaySteps)
	}
	if !strings.Contains(string(body), "event: done\n") {
		t.Error("the stream has no done event")
	}

	// detail: only the steps the engine kept (every StepEvery-th) are served
	for step, want := range map[string]int{"0": 200, "12": 200, "1": 404} {
		resp, err := http.Get(ts.URL + "/v1/playground/run/gen-test/steps/" + step)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("step %s: status %d, want %d", step, resp.StatusCode, want)
		}
	}
}
