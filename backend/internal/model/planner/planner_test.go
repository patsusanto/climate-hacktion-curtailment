package planner

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"climate-hacktion-curtailment/backend/internal/model/battery"
)

// The dynamic program must reach the same optimum as the training code's LP (scipy/HiGHS).
// testdata/lp_cases.json holds random 8-hour problems solved by that LP.
func TestMatchesLP(t *testing.T) {
	raw, err := os.ReadFile("testdata/lp_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []struct {
			SOC     float64   `json:"soc"`
			Price   []float64 `json:"price"`
			PV      []float64 `json:"pv_kw"`
			Load    []float64 `json:"load_kw"`
			Curtail Curtail   `json:"curtail"`
			Spec    struct {
				BatteryKWh  float64 `json:"battery_kwh"`
				BatteryKW   float64 `json:"battery_kw"`
				ExportCapKW float64 `json:"export_cap_kw"`
				Wear        float64 `json:"wear"`
			} `json:"spec"`
			LPCost float64 `json:"lp_cost"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	for i, c := range file.Cases {
		spec, err := battery.NewSpec(6.6, c.Spec.BatteryKWh, c.Spec.BatteryKW, c.Spec.ExportCapKW, 15, c.Spec.Wear)
		if err != nil {
			t.Fatal(err)
		}
		_, cost, err := solve(c.SOC, c.Price, c.PV, c.Load, spec, c.Curtail)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(cost-c.LPCost) > 1e-6*math.Max(1, math.Abs(c.LPCost)) {
			t.Errorf("case %d (%s): DP cost %.8f, LP cost %.8f", i, c.Curtail, cost, c.LPCost)
		}
	}
}

func flat(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestDecisions(t *testing.T) {
	spec, _ := battery.NewSpec(6.6, 10, 5, 5, 15, 0)
	mid := (spec.SOCMin() + spec.SOCMax()) / 2
	cheapThenDear := append(flat(2, 20), flat(94, 400)...) // too short to fill later
	f, _ := Plan(mid, cheapThenDear, flat(96, 0), flat(96, 0.5), spec, Economic)
	if f.GridToBattery <= 0 {
		t.Errorf("should buy to charge before a price rise: %+v", f)
	}
	dearThenCheap := append(flat(2, 400), flat(94, 20)...)
	f, _ = Plan(mid, dearThenCheap, flat(96, 0), flat(96, 0.5), spec, Economic)
	if f.BatteryToLoad+f.BatteryToExport <= 0 {
		t.Errorf("should discharge before a price fall: %+v", f)
	}
	negative := flat(96, -50)
	f, _ = Plan(mid, negative, flat(96, 4), flat(96, 0.5), spec, Economic)
	if f.BatteryToLoad+f.BatteryToExport > 0 || f.PVToExport > 0 {
		t.Errorf("at a negative price: no discharge, no export: %+v", f)
	}
	f, _ = Plan(mid, negative, flat(96, 4), flat(96, 0.5), spec, ForcedOnly)
	if f.PVClipped > 1e-9 && f.PVToExport < spec.ExportCapKW*battery.IntervalHours-1e-9 {
		t.Errorf("forced_only clips only what the cap and battery force: %+v", f)
	}
}
