package engine

import (
	"math"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/data"
	"climate-hacktion-curtailment/backend/internal/forecast"
	"climate-hacktion-curtailment/backend/internal/house"
	"climate-hacktion-curtailment/backend/internal/planner"
	"climate-hacktion-curtailment/backend/internal/policy"
	"climate-hacktion-curtailment/backend/internal/sim"
	"climate-hacktion-curtailment/backend/internal/split"
)

var t0 = time.Date(2026, 7, 1, 0, 0, 0, 0, split.NEM)

// synthFrame is a few weeks of days with a cheap sunny midday, an expensive
// evening and an occasional spike, so a battery has something to arbitrage.
func synthFrame(days int) *data.Frame {
	f := &data.Frame{}
	for i := 0; i < days*split.DaySteps; i++ {
		ts := t0.Add(time.Duration(i*split.StepMinutes) * time.Minute)
		hour := float64(ts.Hour()) + float64(ts.Minute())/60
		price := 80.0
		switch {
		case hour >= 10 && hour < 15:
			price = 20
		case hour >= 17 && hour < 21:
			price = 260
			if ts.YearDay()%3 == 0 && hour < 18 {
				price = 1500
			}
		}
		pv := 0.0
		if hour > 7 && hour < 17 {
			pv = 0.8 * math.Sin(math.Pi*(hour-7)/10)
		}
		load := 0.15
		if hour >= 17 && hour < 22 {
			load = 0.45
		}
		f.Times = append(f.Times, ts)
		f.Price = append(f.Price, price)
		f.PvUnit = append(f.PvUnit, pv)
		f.LoadUnit = append(f.LoadUnit, load)
	}
	return f
}

// perfect knows the future: its forecast at row i is what actually happens at
// i+lead, with a band around it.
type perfect struct{}

func (perfect) Curve(f *data.Frame, i int, spec house.HouseSpec, h int) (planner.ForecastCurve, bool, error) {
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
		pvKw, loadKw := house.ScaleUnits(f.PvUnit[i+lead], f.LoadUnit[i+lead], spec)
		c.Leads = append(c.Leads, planner.LeadForecast{
			LeadSteps: lead,
			PriceP10:  p - 10, PriceP50: p, PriceP90: p + 10,
			PvKw: pvKw, PvLo: pvKw * 0.9, PvHi: pvKw * 1.1,
			LoadKw: loadKw, LoadLo: loadKw * 0.9, LoadHi: loadKw * 1.1,
		})
	}
	return c, true, nil
}

func replay(t *testing.T, cfg Config, f *data.Frame, start, n int) *Result {
	t.Helper()
	res, err := Replay(f, perfect{}, cfg, start, start+n)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestReplayShapesAndConsistency(t *testing.T) {
	f := synthFrame(14)
	cfg := DefaultConfig()
	start := forecast.MaxLookbackV2 + 288 // a day after the warm-up
	n := 288 * 2
	res := replay(t, cfg, f, start, n)

	if len(res.Ticks) != n {
		t.Fatalf("%d ticks, want %d", len(res.Ticks), n)
	}
	for k, tk := range res.Ticks {
		if tk.I != k {
			t.Fatalf("tick %d has i=%d", k, tk.I)
		}
		if tk.T != stamp(f.Times[start+k]) {
			t.Fatalf("tick %d time %s", k, tk.T)
		}
	}

	// the stream's last tick agrees with the summary
	last := res.Ticks[n-1]
	if math.Abs(last.CumulativeSavingsAu-res.Summary.SavingsAud) > 0.005 {
		t.Errorf("last cumulative %v vs summary savings %v", last.CumulativeSavingsAu, res.Summary.SavingsAud)
	}

	// supply is one charge per calendar day, plus GST
	if math.Abs(res.Summary.SupplyAud-2*1.10) > 1e-9 {
		t.Errorf("two days of supply = %v", res.Summary.SupplyAud)
	}

	// each bill is its energy cash plus supply (no wear, one tariff)
	for name, b := range map[string]Bill{"planner": res.Summary.Planner, "baseline": res.Summary.SelfConsumption} {
		if math.Abs(b.BillAud-(b.EnergyCashAud+res.Summary.SupplyAud)) > 0.01 {
			t.Errorf("%s bill %v != energy %v + supply %v", name, b.BillAud, b.EnergyCashAud, res.Summary.SupplyAud)
		}
	}

	// sum of per-tick energy cash is the planner's energy cash
	sum := 0.0
	for _, tk := range res.Ticks {
		sum += tk.EnergyCashAud
	}
	if math.Abs(sum-res.Summary.Planner.EnergyCashAud) > 0.01 {
		t.Errorf("tick cash %v vs planner energy cash %v", sum, res.Summary.Planner.EnergyCashAud)
	}

	// the SOC stays inside the band and the actions are real ones
	b := cfg.Spec.Battery
	valid := map[string]bool{"hold": true, "charge_surplus": true, "charge": true, "discharge_load": true, "discharge": true}
	for _, tk := range res.Ticks {
		if tk.SocKwh < b.SocMinKwh()-1e-6 || tk.SocKwh > b.SocMaxKwh()+1e-6 {
			t.Fatalf("tick %d soc %v outside the band", tk.I, tk.SocKwh)
		}
		if !valid[tk.Action] {
			t.Fatalf("tick %d action %q", tk.I, tk.Action)
		}
		if tk.GridImportKwh > 1e-6 && tk.GridExportKwh > 1e-6 {
			t.Fatalf("tick %d imports and exports", tk.I)
		}
	}
	if res.FallbackSteps != 0 {
		t.Errorf("%d fallback steps with a forecast available", res.FallbackSteps)
	}
}

// With a forecast that sees the evening price, the planner beats self-consumption.
func TestReplayPlannerBeatsBaselineWithGoodForecasts(t *testing.T) {
	f := synthFrame(14)
	res := replay(t, DefaultConfig(), f, forecast.MaxLookbackV2+288, 288*3)
	if res.Summary.SavingsAud <= 0 {
		t.Errorf("savings %v: the planner should beat self-consumption here", res.Summary.SavingsAud)
	}
	if res.Summary.SavingsAud != res.Summary.SelfConsumption.BillAud-res.Summary.Planner.BillAud &&
		math.Abs(res.Summary.SavingsAud-(res.Summary.SelfConsumption.BillAud-res.Summary.Planner.BillAud)) > 1e-3 {
		t.Errorf("savings %v is not baseline minus planner", res.Summary.SavingsAud)
	}
}

// The baseline in the replay is the self-consumption rule run on the same data.
func TestReplayBaselineMatchesPolicyLoop(t *testing.T) {
	f := synthFrame(14)
	cfg := DefaultConfig()
	start := forecast.MaxLookbackV2 + 288
	n := 288
	res := replay(t, cfg, f, start, n)

	m := sim.NewMeter(cfg.Spec)
	for i := start; i < start+n; i++ {
		pv, load := house.ScaleUnits(f.PvUnit[i], f.LoadUnit[i], cfg.Spec)
		a := policy.ChooseSelfConsumption(pv, load, m.SocKwh, cfg.Spec)
		if _, err := m.Step(f.Times[i], a, pv, load, f.Price[i]); err != nil {
			t.Fatal(err)
		}
	}
	want, _ := m.Finish()
	if math.Abs(want.BillAud-res.Summary.SelfConsumption.BillAud) > 1e-3 {
		t.Errorf("baseline bill %v, independent loop %v", res.Summary.SelfConsumption.BillAud, want.BillAud)
	}
}

func TestReplayEstimatesAreTheForecastIssuedAnHourEarlier(t *testing.T) {
	f := synthFrame(14)
	start := forecast.MaxLookbackV2 + 288
	res := replay(t, DefaultConfig(), f, start, 100)
	for _, k := range []int{0, 1, 50, 99} {
		i := start + k
		// the perfect forecaster's 1h-ahead price issued at i-12 is the price at i
		if math.Abs(res.Ticks[k].PriceEstP50-f.Price[i]) > 0.005 {
			t.Errorf("tick %d: estimate %v, price %v", k, res.Ticks[k].PriceEstP50, f.Price[i])
		}
		if math.Abs(res.Ticks[k].PriceEstP10-(f.Price[i]-10)) > 0.005 || math.Abs(res.Ticks[k].PriceEstP90-(f.Price[i]+10)) > 0.005 {
			t.Errorf("tick %d: band %v..%v", k, res.Ticks[k].PriceEstP10, res.Ticks[k].PriceEstP90)
		}
	}
}

func TestReplayStepDetail(t *testing.T) {
	f := synthFrame(14)
	cfg := DefaultConfig()
	cfg.StepEvery = 12
	res := replay(t, cfg, f, forecast.MaxLookbackV2+288, 288)
	if len(res.Steps) != 288/12 {
		t.Fatalf("%d detail steps, want %d", len(res.Steps), 288/12)
	}
	d, ok := res.Steps[24]
	if !ok {
		t.Fatal("step 24 has no detail")
	}
	if len(d.Leads) != 5 || len(d.Stories) != 4 {
		t.Fatalf("%d leads, %d stories", len(d.Leads), len(d.Stories))
	}
	counts := 0
	for _, s := range d.Stories {
		counts += s.Count
		if len(s.PriceAudMwh) != 19 { // one column per planner stage for 8 hours
			t.Errorf("story %s has %d columns", s.ID, len(s.PriceAudMwh))
		}
		if s.PriceAudMwh[0] != d.Measured.PriceAudMwh {
			t.Errorf("story %s column 0 %v is not the measured price %v", s.ID, s.PriceAudMwh[0], d.Measured.PriceAudMwh)
		}
	}
	if counts != 21 {
		t.Errorf("story counts sum to %d", counts)
	}
	if d.T != res.Ticks[24].T || d.Action != res.Ticks[24].Action {
		t.Errorf("detail %s/%s does not match tick %s/%s", d.T, d.Action, res.Ticks[24].T, res.Ticks[24].Action)
	}
	if d.Leads[0].LeadSteps != 12 || d.Leads[4].LeadSteps != 96 {
		t.Errorf("leads %+v", d.Leads)
	}
}

func TestReplayWearOnlyAffectsTheWearFigure(t *testing.T) {
	f := synthFrame(14)
	start := forecast.MaxLookbackV2 + 288
	free := DefaultConfig()
	free.WearAudPerKwh = 0
	a := replay(t, free, f, start, 288*2)
	worn := DefaultConfig()
	worn.WearAudPerKwh = 0.10
	b := replay(t, worn, f, start, 288*2)
	if a.Summary.SavingsAud != b.Summary.SavingsAud {
		t.Errorf("wear must not change decisions: %v vs %v", a.Summary.SavingsAud, b.Summary.SavingsAud)
	}
	if a.Summary.SavingsWithWearAud != a.Summary.SavingsAud {
		t.Errorf("with zero wear the two savings must agree: %v vs %v", a.Summary.SavingsWithWearAud, a.Summary.SavingsAud)
	}
	extra := b.Summary.Planner.ThroughputAcKwh - b.Summary.SelfConsumption.ThroughputAcKwh
	want := b.Summary.SavingsAud - 0.10*extra
	if math.Abs(b.Summary.SavingsWithWearAud-want) > 0.01 {
		t.Errorf("savings with wear %v, want %v", b.Summary.SavingsWithWearAud, want)
	}
}

func TestReplayIsDeterministic(t *testing.T) {
	f := synthFrame(14)
	start := forecast.MaxLookbackV2 + 288
	a := replay(t, DefaultConfig(), f, start, 288)
	b := replay(t, DefaultConfig(), f, start, 288)
	for i := range a.Ticks {
		if a.Ticks[i] != b.Ticks[i] {
			t.Fatalf("tick %d differs between runs", i)
		}
	}
}

func TestReplayRejectsBadWindows(t *testing.T) {
	f := synthFrame(14)
	cfg := DefaultConfig()
	if _, err := Replay(f, perfect{}, cfg, 100, 200); err == nil {
		t.Error("a window without a week of history must fail")
	}
	if _, err := Replay(f, perfect{}, cfg, forecast.MaxLookbackV2+288, f.Len()+1); err == nil {
		t.Error("a window past the end must fail")
	}
	if _, err := Replay(f, perfect{}, cfg, forecast.MaxLookbackV2+300, forecast.MaxLookbackV2+300); err == nil {
		t.Error("an empty window must fail")
	}
	bad := cfg
	bad.Spec.Battery.CapacityKwh = 0
	if _, err := Replay(f, perfect{}, bad, forecast.MaxLookbackV2+288, forecast.MaxLookbackV2+388); err == nil {
		t.Error("an invalid house must fail")
	}
}

// When the forecaster cannot produce a curve the controller falls back to the
// self-consumption rule and says so.
type blind struct{}

func (blind) Curve(*data.Frame, int, house.HouseSpec, int) (planner.ForecastCurve, bool, error) {
	return planner.ForecastCurve{}, false, nil
}

func TestReplayWithoutForecastsErrorsRatherThanInventingEstimates(t *testing.T) {
	f := synthFrame(14)
	_, err := Replay(f, blind{}, DefaultConfig(), forecast.MaxLookbackV2+288, forecast.MaxLookbackV2+388)
	if err == nil {
		t.Error("ticks need a 1-hour-ahead estimate; with no forecast the replay must fail loudly")
	}
}

func TestRunFileRoundTrip(t *testing.T) {
	f := synthFrame(14)
	cfg := DefaultConfig()
	res := replay(t, cfg, f, forecast.MaxLookbackV2+288, 288)
	run := BuildRunFile("test-run", "validation", cfg, res)
	if run.Meta.Window.N != 288 || run.Meta.Spec.UsableKwh != 8 || run.Meta.Spec.BatteryKwh != 10 {
		t.Errorf("meta %+v", run.Meta)
	}
	if len(run.Steps) != len(res.Steps) {
		t.Errorf("%d steps in the file, %d in the result", len(run.Steps), len(res.Steps))
	}
	dir := t.TempDir()
	path, err := WriteRunFile(dir, run)
	if err != nil || path == "" {
		t.Fatalf("write: %v", err)
	}
	if DefaultRunID(cfg.Spec) != "10.5kw-10kwh" {
		t.Errorf("default id %q", DefaultRunID(cfg.Spec))
	}
	if _, err := WriteRunFile(dir, RunFile{}); err == nil {
		t.Error("a run without an id must not be written")
	}
}
