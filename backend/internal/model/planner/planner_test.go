package planner

import (
	"math"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/house"
	"climate-hacktion-curtailment/backend/internal/model/sim"
	"climate-hacktion-curtailment/backend/internal/model/split"
)

func TestStageEnds(t *testing.T) {
	for hours, want := range map[int]int{8: 19, 2: 13, 1: 12} {
		got, err := StageEnds(hours)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != want {
			t.Errorf("horizon %dh: %d stages, want %d", hours, len(got), want)
		}
	}
	got, _ := StageEnds(8)
	if got[0] != 0 || got[11] != 11 || got[12] != 12 || got[18] != 84 {
		t.Errorf("stage offsets %v", got)
	}
	if _, err := StageEnds(0); err == nil {
		t.Error("horizon 0 should fail")
	}
}

func TestLinspaceAndInterp(t *testing.T) {
	g := linspace(1, 9, 17)
	if g[0] != 1 || g[16] != 9 || math.Abs(g[1]-1.5) > 1e-15 {
		t.Errorf("grid %v", g)
	}
	cont := make([]float64, 17)
	for i := range cont {
		cont[i] = float64(i) * 2
	}
	// Halfway between the first two bins is halfway between their values.
	if v := interp(1.25, g, cont); math.Abs(v-1.0) > 1e-12 {
		t.Errorf("interp %v", v)
	}
	// Outside the band clamps to the ends.
	if interp(0, g, cont) != 0 || interp(100, g, cont) != 32 {
		t.Error("interp should clamp")
	}
}

func TestNpInterp(t *testing.T) {
	xp := []float64{0, 1, 2}
	fp := []float64{0, 10, 14}
	cases := []struct{ x, want float64 }{
		{-1, 0}, {0, 0}, {0.5, 5}, {1, 10}, {1.5, 12}, {2, 14}, {3, 14},
	}
	for _, c := range cases {
		if got := NpInterp(c.x, xp, fp); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("interp(%v) = %v, want %v", c.x, got, c.want)
		}
	}
}

func fiveLeads() ForecastCurve {
	var c ForecastCurve
	for i, lead := range split.Leads {
		base := float64(100 * (i + 1))
		c.Leads = append(c.Leads, LeadForecast{
			LeadSteps: lead,
			PriceP10:  base - 10, PriceP50: base, PriceP90: base + 50,
			PvKw: 2, PvLo: 1, PvHi: 3,
			LoadKw: 1, LoadLo: 0.5, LoadHi: 1.5,
		})
	}
	return c
}

func TestStoriesStagesGrid(t *testing.T) {
	stories, offsets, err := Stories(fiveLeads(), 80, 0.5, 0.3, 8, "stages")
	if err != nil {
		t.Fatal(err)
	}
	if len(offsets) != 19 || len(stories) != 4 {
		t.Fatalf("%d offsets, %d stories", len(offsets), len(stories))
	}
	counts := 0
	for _, s := range stories {
		counts += s.Count
		if s.PriceAudMwh[0] != 80 || s.PvKw[0] != 0.5 || s.LoadKw[0] != 0.3 {
			t.Errorf("%s column 0 must be the measurement", s.Name)
		}
	}
	if counts != split.NScenarios {
		t.Errorf("counts sum to %d", counts)
	}
	byName := map[string]Story{}
	for _, s := range stories {
		byName[s.Name] = s
	}
	// offset 12 is hour 1, which is exactly the 1h knot (lead 12).
	if v := byName["mid"].PriceAudMwh[12]; v != 100 {
		t.Errorf("mid price at 1h = %v", v)
	}
	if v := byName["bright"].PriceAudMwh[12]; v != 90 {
		t.Errorf("bright price at 1h uses P10: %v", v)
	}
	if v := byName["dull"].PvKw[12]; v != 1 {
		t.Errorf("dull pv at 1h uses the low band: %v", v)
	}
	if v := byName["dull"].LoadKw[12]; v != 1.5 {
		t.Errorf("dull load at 1h uses the high band: %v", v)
	}
	// offset 72 (index 17) is the 6h knot (lead 72): the spike story uses P90 from 6h on.
	if offsets[17] != 72 {
		t.Fatalf("offsets[17] = %d", offsets[17])
	}
	if v := byName["spike"].PriceAudMwh[17]; v != 450 { // lead 72 is the 4th lead: base 400, P90 = 450
		t.Errorf("spike price at 6h = %v", v)
	}
	if v := byName["mid"].PriceAudMwh[17]; v != 400 {
		t.Errorf("mid price at 6h = %v", v)
	}
	// Before 6h the spike story follows the middle path.
	if byName["spike"].PriceAudMwh[12] != byName["mid"].PriceAudMwh[12] {
		t.Error("spike story should equal mid before six hours")
	}
}

func TestBuildScenariosShapes(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, split.NEM)
	full, err := BuildScenarios(fiveLeads(), 80, 0.5, 0.3, now, 8, "intervals", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.PvKw) != 21 || len(full.PvKw[0]) != 96 || len(full.Timestamps) != 96 {
		t.Errorf("21 scenarios x 96 steps expected, got %d x %d", len(full.PvKw), len(full.PvKw[0]))
	}
	if err := full.Validate(); err != nil {
		t.Error(err)
	}
	if !full.Timestamps[12].Equal(now.Add(time.Hour)) {
		t.Errorf("timestamp 12 = %v", full.Timestamps[12])
	}
	dedup, _ := BuildScenarios(fiveLeads(), 80, 0.5, 0.3, now, 8, "intervals", true)
	if len(dedup.PvKw) != 4 || dedup.Weights[0] != 7 || dedup.Weights[3] != 4 {
		t.Errorf("dedup %d rows, weights %v", len(dedup.PvKw), dedup.Weights)
	}
	point, _ := BuildPointPath(fiveLeads(), 80, 0.5, 0.3, now, 8, "intervals")
	if len(point.PvKw) != 1 {
		t.Errorf("point path rows %d", len(point.PvKw))
	}
	if _, err := BuildScenarios(fiveLeads(), 80, 0.5, 0.3, now, 8, "weird", false); err == nil {
		t.Error("bad grid should fail")
	}
	missing := fiveLeads()
	missing.Leads[0].PriceP50 = math.NaN()
	if _, err := BuildScenarios(missing, 80, 0.5, 0.3, now, 8, "stages", false); err == nil {
		t.Error("a lead without a price should fail")
	}
}

// flat builds a one-scenario batch from per-step prices with no solar.
func flat(prices []float64, loadKw float64) ScenarioBatch {
	n := len(prices)
	b := ScenarioBatch{
		Timestamps:  make([]time.Time, n),
		PvKw:        [][]float64{make([]float64, n)},
		LoadKw:      [][]float64{make([]float64, n)},
		PriceAudMwh: [][]float64{prices},
	}
	for i := range prices {
		b.LoadKw[0][i] = loadKw
	}
	return b
}

func plan(t *testing.T, prices []float64, soc float64) sim.Action {
	t.Helper()
	spec := house.DefaultHouse()
	tariff, _ := house.GetTariff(spec.TariffID)
	clock, _ := StageEnds(8)
	a, err := PlanFirstAction(soc, flat(prices, 0.3), spec, tariff, clock, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPlannerGridChargesWhenNowIsCheapestBeforeASpike(t *testing.T) {
	// 50 AUD/MWh for the first hour, 300 after, and a 1,500 spike four hours
	// out. Buying now is much cheaper than buying later, and the battery has
	// less than the spike can absorb, so it should grid-charge now.
	prices := make([]float64, 96)
	for i := range prices {
		prices[i] = 300
	}
	for i := 0; i < 12; i++ {
		prices[i] = 50
	}
	for i := 48; i < 60; i++ {
		prices[i] = 1500
	}
	if a := plan(t, prices, 3.0); a != sim.Charge {
		t.Errorf("expected grid charge while cheap before a spike, got %s", a)
	}
}

func TestPlannerTakesEqualCashNow(t *testing.T) {
	// A flat price makes buying now and buying later the same cash. Ties are
	// taken now, so with a positive price the plan serves the load from the
	// battery rather than waiting.
	prices := make([]float64, 96)
	for i := range prices {
		prices[i] = 50
	}
	for i := 48; i < 60; i++ {
		prices[i] = 1500
	}
	if a := plan(t, prices, 3.0); a != sim.DischargeLoad {
		t.Errorf("expected the equal-cash tie to be taken now, got %s", a)
	}
}

func TestPlannerSellsIntoASpikeNow(t *testing.T) {
	prices := make([]float64, 96)
	for i := range prices {
		prices[i] = 60
	}
	prices[0] = 2000 // right now
	if a := plan(t, prices, 7.0); a != sim.Discharge {
		t.Errorf("a spike now should be sold into, got %s", a)
	}
}

func TestPlannerChargesBeforeAPriceSpike(t *testing.T) {
	// A negative price now followed by a high one later: charge from the grid.
	prices := make([]float64, 96)
	for i := range prices {
		prices[i] = 100
	}
	for i := 0; i < 12; i++ {
		prices[i] = -50
	}
	for i := 24; i < 36; i++ {
		prices[i] = 1200
	}
	if a := plan(t, prices, 2.0); a != sim.Charge {
		t.Errorf("paid to import before a spike should grid-charge, got %s", a)
	}
}

func TestPlannerIsDeterministic(t *testing.T) {
	prices := make([]float64, 96)
	for i := range prices {
		prices[i] = 80 + float64(i%7)*15
	}
	first := plan(t, prices, 5.0)
	for i := 0; i < 5; i++ {
		if got := plan(t, prices, 5.0); got != first {
			t.Fatalf("run %d chose %s, first chose %s", i, got, first)
		}
	}
}

func TestPlannerRejectsShortScenarios(t *testing.T) {
	spec := house.DefaultHouse()
	tariff, _ := house.GetTariff(spec.TariffID)
	clock, _ := StageEnds(8)
	if _, err := PlanFirstAction(5, flat(make([]float64, 50), 0.3), spec, tariff, clock, 0, 0); err == nil {
		t.Error("scenarios shorter than the clock should fail")
	}
	if _, err := PlanFirstAction(5, flat(make([]float64, 96), 0.3), spec, tariff, []int{3, 4}, 0, 0); err == nil {
		t.Error("a clock that does not start at 0 should fail")
	}
}

func TestScenarioBatchValidate(t *testing.T) {
	b := flat(make([]float64, 4), 1)
	b.PvKw = [][]float64{make([]float64, 3)}
	if b.Validate() == nil {
		t.Error("ragged rows should fail")
	}
	b = flat(make([]float64, 4), 1)
	b.Weights = []float64{-1}
	if b.Validate() == nil {
		t.Error("negative weights should fail")
	}
}

// The slow clip-bonus path and the vectorised path agree when the bonus is zero.
func TestRollVecMatchesApply(t *testing.T) {
	spec := house.DefaultHouse()
	pv := []float64{0, 2, 6, 9, 3, 0}
	load := []float64{0.4, 0.4, 1.0, 1.0, 2.0, 3.0}
	price := []float64{80, 40, -5, 20, 300, 900}
	for _, action := range []sim.Action{sim.Hold, sim.ChargeSurplus, sim.Charge, sim.DischargeLoad, sim.Discharge} {
		soc0 := 5.0
		state := make([]float64, 1)
		cash := make([]float64, 1)
		rollVec([]float64{soc0}, action, 0, len(pv), pv, load, price, spec, 0, state, cash)

		soc, want := soc0, 0.0
		for i := range pv {
			step, err := sim.Apply(soc, action, pv[i], load[i], spec)
			if err != nil {
				t.Fatal(err)
			}
			want += (step.GridExportKwh - step.GridImportKwh) * price[i] / 1000.0
			soc = step.SocKwh
		}
		if math.Abs(state[0]-soc) > 1e-12 || math.Abs(cash[0]-want) > 1e-12 {
			t.Errorf("%s: soc %v vs %v, cash %v vs %v", action, state[0], soc, cash[0], want)
		}
	}
}

func BenchmarkPlanFirstAction(b *testing.B) {
	spec := house.DefaultHouse()
	tariff, _ := house.GetTariff(spec.TariffID)
	clock, _ := StageEnds(8)
	prices := make([]float64, 96)
	for i := range prices {
		prices[i] = 80 + float64(i%7)*15
	}
	sc := flat(prices, 0.3)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := PlanFirstAction(5.0, sc, spec, tariff, clock, 0, 0); err != nil {
			b.Fatal(err)
		}
	}
}
