// Package golden checks the Go port against reference answers from the original Python model.
//
// The tests read backend/export/golden.json, a file of those answers that comes with an
// export. Without that file every test here is skipped, so a plain `go test ./...` still
// passes. Set GOLDEN_DIR to read the export from somewhere else.
package golden

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/data"
	"climate-hacktion-curtailment/backend/internal/engine"
	"climate-hacktion-curtailment/backend/internal/forecast"
	"climate-hacktion-curtailment/backend/internal/house"
	"climate-hacktion-curtailment/backend/internal/planner"
	"climate-hacktion-curtailment/backend/internal/split"
)

type lead struct {
	LeadSteps int     `json:"lead_steps"`
	PriceP10  float64 `json:"price_p10"`
	PriceP50  float64 `json:"price_p50"`
	PriceP90  float64 `json:"price_p90"`
	PvKw      float64 `json:"pv_kw"`
	PvLo      float64 `json:"pv_lo"`
	PvHi      float64 `json:"pv_hi"`
	LoadKw    float64 `json:"load_kw"`
	LoadLo    float64 `json:"load_lo"`
	LoadHi    float64 `json:"load_hi"`
}

type point struct {
	Time          string                         `json:"time"`
	Row           int                            `json:"row"`
	Price         float64                        `json:"price"`
	PvKw          float64                        `json:"pv_kw"`
	LoadKw        float64                        `json:"load_kw"`
	Curve         []lead                         `json:"curve"`
	PriceFeatures map[string]map[string]*float64 `json:"price_features"`
	UnitFeatures  map[string]*float64            `json:"unit_features"`
	Actions       map[string]string              `json:"actions"` // soc -> first action
}

type bill struct {
	BillAud         float64 `json:"bill_aud"`
	SpotMtdAud      float64 `json:"spot_mtd_aud"`
	ThroughputAcKwh float64 `json:"throughput_ac_kwh"`
}

type replay struct {
	StartRow        int       `json:"start_row"`
	Steps           int       `json:"steps"`
	Actions         []string  `json:"actions"`
	SocAfter        []float64 `json:"soc_after"`
	Planner         bill      `json:"planner"`
	SelfConsumption bill      `json:"self_consumption"`
	FallbackSteps   int       `json:"fallback_steps"`
}

type goldenFile struct {
	Points []point `json:"points"`
	Replay replay  `json:"replay"`
}

type fixture struct {
	golden goldenFile
	frame  *data.Frame
	models *forecast.Models
}

func load(t *testing.T) *fixture {
	t.Helper()
	dir := os.Getenv("GOLDEN_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "export")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Skipf("no golden vectors (%v); they come with an export, see the README", err)
	}
	var fx fixture
	if err := json.Unmarshal(raw, &fx.golden); err != nil {
		t.Fatal(err)
	}
	if fx.frame, err = data.LoadFrame(filepath.Join(dir, "frame.csv.gz")); err != nil {
		t.Fatal(err)
	}
	if fx.models, err = forecast.LoadModels(dir); err != nil {
		t.Fatal(err)
	}
	return &fx
}

func nanOr(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

func close(a, b, tol float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	return math.Abs(a-b) <= tol*(1+math.Abs(b))
}

func (fx *fixture) check(t *testing.T, p point) {
	t.Helper()
	got := fx.frame.Times[p.Row]
	want, err := time.Parse(time.RFC3339, p.Time)
	if err != nil || !got.Equal(want) {
		t.Fatalf("row %d is %s in Go but %s in Python: the frames differ", p.Row, got.Format(time.RFC3339), p.Time)
	}
}

// The features must match Python's at every recorded timestamp.
func TestFeaturesMatchPython(t *testing.T) {
	fx := load(t)
	bad := 0
	for _, p := range fx.golden.Points {
		fx.check(t, p)
		lo := p.Row - forecast.MaxLookbackV2

		unit := forecast.UnitFeatures(fx.frame, lo, p.Row)
		for name, want := range p.UnitFeatures {
			if got, ok := unit[name]; !ok || !close(got, nanOr(want), 1e-6) {
				t.Errorf("%s unit feature %s = %v, Python %v", p.Time, name, got, nanOr(want))
				bad++
			}
		}
		for leadKey, features := range p.PriceFeatures {
			steps, _ := strconv.Atoi(leadKey)
			got, err := forecast.PriceFeatures(fx.frame, lo, p.Row, steps, fx.models.Weather)
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range features {
				if g, ok := got[name]; !ok || !close(g, nanOr(want), 1e-6) {
					t.Errorf("%s price feature %s (lead %s) = %v, Python %v", p.Time, name, leadKey, g, nanOr(want))
					bad++
				}
			}
		}
		if bad > 20 {
			t.Fatal("too many mismatches; stopping")
		}
	}
}

// The forecast curve (XGBoost quantiles and the net) must match.
func TestCurveMatchesPython(t *testing.T) {
	fx := load(t)
	spec := house.DefaultHouse()
	for _, p := range fx.golden.Points {
		fx.check(t, p)
		curve, ok, err := fx.models.Curve(fx.frame, p.Row, spec, 8)
		if err != nil {
			t.Fatal(err)
		}
		if p.Curve == nil {
			if ok {
				t.Errorf("%s: Python had no forecast but Go did", p.Time)
			}
			continue
		}
		if !ok || len(curve.Leads) != len(p.Curve) {
			t.Errorf("%s: Go curve ok=%v with %d leads, Python %d", p.Time, ok, len(curve.Leads), len(p.Curve))
			continue
		}
		for i, want := range p.Curve {
			got := curve.Leads[i]
			if got.LeadSteps != want.LeadSteps {
				t.Errorf("%s: lead %d vs %d", p.Time, got.LeadSteps, want.LeadSteps)
				continue
			}
			pairs := []struct {
				name      string
				got, want float64
				tol       float64
			}{
				{"price_p10", got.PriceP10, want.PriceP10, 5e-4}, {"price_p50", got.PriceP50, want.PriceP50, 5e-4},
				{"price_p90", got.PriceP90, want.PriceP90, 5e-4},
				{"pv_kw", got.PvKw, want.PvKw, 1e-4}, {"pv_lo", got.PvLo, want.PvLo, 1e-4}, {"pv_hi", got.PvHi, want.PvHi, 1e-4},
				{"load_kw", got.LoadKw, want.LoadKw, 1e-4}, {"load_lo", got.LoadLo, want.LoadLo, 1e-4}, {"load_hi", got.LoadHi, want.LoadHi, 1e-4},
			}
			for _, c := range pairs {
				if !close(c.got, c.want, c.tol) {
					t.Errorf("%s lead %d %s = %v, Python %v", p.Time, want.LeadSteps, c.name, c.got, c.want)
				}
			}
		}
	}
}

// Fed Python's own forecast curve, the Go planner must choose Python's action.
// This isolates the planner from any difference in the models.
func TestPlannerMatchesPythonGivenTheSameForecast(t *testing.T) {
	fx := load(t)
	spec := house.DefaultHouse()
	tariff, _ := house.GetTariff(spec.TariffID)
	clock, _ := planner.StageEnds(8)
	checked := 0
	for _, p := range fx.golden.Points {
		if p.Curve == nil {
			continue
		}
		fx.check(t, p)
		var curve planner.ForecastCurve
		for _, l := range p.Curve {
			curve.Leads = append(curve.Leads, planner.LeadForecast{
				LeadSteps: l.LeadSteps, PriceP10: l.PriceP10, PriceP50: l.PriceP50, PriceP90: l.PriceP90,
				PvKw: l.PvKw, PvLo: l.PvLo, PvHi: l.PvHi, LoadKw: l.LoadKw, LoadLo: l.LoadLo, LoadHi: l.LoadHi,
			})
		}
		now := fx.frame.Times[p.Row]
		batch, err := planner.BuildPointPath(curve, p.Price, p.PvKw, p.LoadKw, now, 8, "intervals")
		if err != nil {
			t.Fatal(err)
		}
		for socKey, want := range p.Actions {
			soc, _ := strconv.ParseFloat(socKey, 64)
			got, err := planner.PlanFirstAction(soc, batch, spec, tariff, clock, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != want {
				t.Errorf("%s soc %s: Go chose %s, Python %s", p.Time, socKey, got, want)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Skip("the golden file has no planner cases")
	}
}

// A full replay with the real models. Acceptance from the plan: at least 99.5%
// of actions identical and each bill within 0.02 AUD.
func TestReplayMatchesPython(t *testing.T) {
	fx := load(t)
	r := fx.golden.Replay
	if r.Steps == 0 {
		t.Skip("the golden file has no replay")
	}
	cfg := engine.DefaultConfig()
	cfg.StepEvery = split.DaySteps
	res, err := engine.Replay(fx.frame, fx.models, cfg, r.StartRow, r.StartRow+r.Steps)
	if err != nil {
		t.Fatal(err)
	}
	mismatches := 0
	for i, want := range r.Actions {
		if res.Ticks[i].Action != want {
			if mismatches < 10 {
				t.Logf("step %d (%s): Go %s, Python %s", i, res.Ticks[i].T, res.Ticks[i].Action, want)
			}
			mismatches++
		}
	}
	agree := 1 - float64(mismatches)/float64(len(r.Actions))
	t.Logf("%d of %d actions differ (%.2f%% identical)", mismatches, len(r.Actions), 100*agree)
	if agree < 0.995 {
		t.Errorf("only %.2f%% of actions match Python; the plan requires 99.5%%", 100*agree)
	}
	if d := math.Abs(res.Summary.Planner.BillAud - r.Planner.BillAud); d > 0.02 {
		t.Errorf("planner bill %v, Python %v (off by %v)", res.Summary.Planner.BillAud, r.Planner.BillAud, d)
	}
	if d := math.Abs(res.Summary.SelfConsumption.BillAud - r.SelfConsumption.BillAud); d > 0.02 {
		t.Errorf("baseline bill %v, Python %v (off by %v)", res.Summary.SelfConsumption.BillAud, r.SelfConsumption.BillAud, d)
	}
	if res.FallbackSteps != r.FallbackSteps {
		t.Errorf("%d fallback steps, Python %d", res.FallbackSteps, r.FallbackSteps)
	}
}
