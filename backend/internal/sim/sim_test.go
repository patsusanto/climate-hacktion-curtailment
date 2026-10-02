package sim

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/house"
	"climate-hacktion-curtailment/backend/internal/split"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.9f, want %.9f", name, got, want)
	}
}

var allActions = []Action{Hold, ChargeSurplus, Charge, DischargeLoad, Discharge}

// Hand-checked: 1.2 kW load for 5 minutes is 0.1 kWh, no solar.
func TestApplyDischargeLoadByHand(t *testing.T) {
	spec := house.DefaultHouse() // 10 kWh, 5 kW, 90% round trip, SOC band 1-9 kWh
	leg := math.Sqrt(0.9)
	res, err := Apply(5.0, DischargeLoad, 0, 1.2, spec)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "discharge_ac", res.BatteryDischargeAcKwh, 0.1, 1e-12)
	near(t, "grid_import", res.GridImportKwh, 0, 1e-12)
	near(t, "soc", res.SocKwh, 5.0-0.1/leg, 1e-12)
}

func TestApplyEachActionByHand(t *testing.T) {
	spec := house.DefaultHouse()
	leg := math.Sqrt(0.9)
	maxAc := 5.0 / 12.0
	capKwh := 5.0 / 12.0

	// Hold with surplus solar: 8 kW solar, 1.2 kW load. The 0.567 kWh surplus
	// is capped at the export limit and the rest is clipped.
	res, _ := Apply(5.0, Hold, 8.0, 1.2, spec)
	near(t, "hold export", res.GridExportKwh, capKwh, 1e-12)
	near(t, "hold clipped", res.ClippedKwh, 8.0/12-0.1-capKwh, 1e-12)
	near(t, "hold soc", res.SocKwh, 5.0, 0)

	// Charge from surplus: all of the 0.4 kWh surplus goes in.
	res, _ = Apply(5.0, ChargeSurplus, 6.0, 1.2, spec)
	near(t, "surplus charge_ac", res.BatteryChargeAcKwh, 0.4, 1e-12)
	near(t, "surplus soc", res.SocKwh, 5.0+0.4*leg, 1e-12)
	near(t, "surplus export", res.GridExportKwh, 0, 1e-12)

	// Grid charge with no solar: charge_ac is the power limit, all imported.
	res, _ = Apply(5.0, Charge, 0, 0, spec)
	near(t, "grid charge_ac", res.BatteryChargeAcKwh, maxAc, 1e-12)
	near(t, "grid charge import", res.GridImportKwh, maxAc, 1e-12)
	near(t, "grid charge soc", res.SocKwh, 5.0+maxAc*leg, 1e-12)

	// Discharge to the grid with no load: limited by power and the export cap.
	res, _ = Apply(5.0, Discharge, 0, 0, spec)
	near(t, "export discharge", res.BatteryDischargeAcKwh, math.Min(maxAc, capKwh), 1e-12)
	near(t, "export discharge export", res.GridExportKwh, math.Min(maxAc, capKwh), 1e-12)
	if res.GridImportKwh != 0 {
		t.Errorf("exporting step imported %v", res.GridImportKwh)
	}
}

func TestApplyRespectsSocBand(t *testing.T) {
	spec := house.DefaultHouse()
	// At the floor, discharge does nothing.
	res, err := Apply(spec.Battery.SocMinKwh(), DischargeLoad, 0, 3, spec)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "floor discharge", res.BatteryDischargeAcKwh, 0, 0)
	// At the ceiling, charge does nothing.
	res, err = Apply(spec.Battery.SocMaxKwh(), ChargeSurplus, 8, 0, spec)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "ceiling charge", res.BatteryChargeAcKwh, 0, 0)
}

func TestApplyUnknownAction(t *testing.T) {
	if _, err := Apply(5, Action("fly"), 0, 0, house.DefaultHouse()); err == nil {
		t.Error("expected an error for an unknown action")
	}
}

// The invariants hold for every action across random states, so Apply never
// reports an unbalanced step.
func TestApplyInvariantsRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	spec := house.DefaultHouse()
	b := spec.Battery
	for i := 0; i < 20000; i++ {
		soc := b.SocMinKwh() + rng.Float64()*(b.SocMaxKwh()-b.SocMinKwh())
		pv := rng.Float64() * 12
		load := rng.Float64() * 6
		for _, a := range allActions {
			if _, err := Apply(soc, a, pv, load, spec); err != nil {
				t.Fatalf("soc=%v pv=%v load=%v action=%s: %v", soc, pv, load, a, err)
			}
		}
	}
}

func at(day, hour, minute int) time.Time {
	return time.Date(2026, 7, day, hour, minute, 0, 0, split.NEM)
}

func TestEpisodeBillByHand(t *testing.T) {
	spec := house.DefaultHouse()
	steps := []StepResult{
		{GridImportKwh: 0.1, PvAvailKwh: 0},
		{GridExportKwh: 0.2, PvAvailKwh: 0.2, PvToExportKwh: 0.2},
	}
	stamps := []time.Time{at(16, 18, 0), at(16, 12, 0)}
	prices := []float64{200, 50} // AUD/MWh
	res, err := Episode(steps, stamps, spec, prices)
	if err != nil {
		t.Fatal(err)
	}
	// energy: 0.1*200/1000 - 0.2*50/1000 = 0.02 - 0.01; one day of supply: 1.00 * 1.10
	near(t, "spot", res.SpotMtdAud, 0.01, 1e-12)
	near(t, "bill", res.BillAud, 0.01+1.10, 1e-12)
	near(t, "import", res.GridImportKwh, 0.1, 0)
}

func TestEpisodeSupplyChargesPerDay(t *testing.T) {
	spec := house.DefaultHouse()
	steps := make([]StepResult, 3)
	stamps := []time.Time{at(16, 0, 0), at(16, 0, 5), at(17, 0, 0)}
	res, _ := Episode(steps, stamps, spec, nil)
	near(t, "two days of supply", res.BillAud, 2*1.10, 1e-12)
}

func TestEpisodeTwoWayTariffAndDegradation(t *testing.T) {
	spec := house.DefaultHouse()
	spec.TariffID = "tou_fit_twoway"
	spec.DegradationAudPerKwh = 0.01
	steps := []StepResult{
		{GridExportKwh: 8.0, BatteryChargeAcKwh: 1, BatteryDischargeAcKwh: 2}, // midday
		{GridExportKwh: 3.0}, // evening
	}
	stamps := []time.Time{at(16, 12, 0), at(16, 17, 0)}
	res, err := Episode(steps, stamps, spec, []float64{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	charge := (8.0 - 6.85) * 0.012320
	credit := 3.0 * 0.038551
	degradation := 3.0 * 0.01
	near(t, "bill", res.BillAud, 1.10+charge-credit+degradation, 1e-12)
	near(t, "throughput", res.ThroughputAcKwh, 3.0, 0)
}

func TestEpisodeValidation(t *testing.T) {
	spec := house.DefaultHouse()
	if _, err := Episode(make([]StepResult, 2), []time.Time{at(1, 0, 0)}, spec, nil); err == nil {
		t.Error("length mismatch should fail")
	}
	if _, err := Episode(make([]StepResult, 1), []time.Time{at(1, 0, 0)}, spec, []float64{1, 2}); err == nil {
		t.Error("price mismatch should fail")
	}
	spec.TariffID = "nope"
	if _, err := Episode(nil, nil, spec, nil); err == nil {
		t.Error("unknown tariff should fail")
	}
}

func TestMeterMatchesEpisode(t *testing.T) {
	spec := house.DefaultHouse()
	m := NewMeter(spec)
	if m.SocKwh != 5.0 {
		t.Fatalf("initial soc %v", m.SocKwh)
	}
	for i := 0; i < 12; i++ {
		if _, err := m.Step(at(16, 12, 5*i), ChargeSurplus, 6, 1, 80); err != nil {
			t.Fatal(err)
		}
	}
	res, err := m.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if res.BatteryChargeAcKwh <= 0 || m.SocKwh <= 5.0 {
		t.Errorf("battery should have charged: %+v soc=%v", res, m.SocKwh)
	}
	m.Reset()
	if m.SocKwh != 5.0 {
		t.Errorf("reset soc %v", m.SocKwh)
	}
}
