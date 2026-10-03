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
		plan, err := Solve(c.SOC, spotOnly(c.Price, c.PV, c.Load), spec, c.Curtail)
		if err != nil {
			t.Fatal(err)
		}
		cost := plan.Cost
		if math.Abs(cost-c.LPCost) > 1e-6*math.Max(1, math.Abs(c.LPCost)) {
			t.Errorf("case %d (%s): DP cost %.8f, LP cost %.8f", i, c.Curtail, cost, c.LPCost)
		}
	}
}

// spotOnly is a horizon priced at the spot price both ways ($/MWh in), as in the training LP.
func spotOnly(price, pv, load []float64) Horizon {
	h := Horizon{PV: pv, Load: load}
	for _, p := range price {
		h.Price = append(h.Price, battery.Prices{Import: p / 1000, Export: p / 1000})
	}
	return h
}

// first is the first step of the plan for a spot-only horizon.
func first(t *testing.T, soc float64, price, pv, load []float64, spec battery.Spec, curtail Curtail) battery.Flows {
	t.Helper()
	plan, err := Solve(soc, spotOnly(price, pv, load), spec, curtail)
	if err != nil {
		t.Fatal(err)
	}
	return plan.First
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
	f := first(t, mid, cheapThenDear, flat(96, 0), flat(96, 0.5), spec, Economic)
	if f.GridToBattery <= 0 {
		t.Errorf("should buy to charge before a price rise: %+v", f)
	}
	dearThenCheap := append(flat(2, 400), flat(94, 20)...)
	f = first(t, mid, dearThenCheap, flat(96, 0), flat(96, 0.5), spec, Economic)
	if f.BatteryToLoad+f.BatteryToExport <= 0 {
		t.Errorf("should discharge before a price fall: %+v", f)
	}
	negative := flat(96, -50)
	f = first(t, mid, negative, flat(96, 4), flat(96, 0.5), spec, Economic)
	if f.BatteryToLoad+f.BatteryToExport > 0 || f.PVToExport > 0 {
		t.Errorf("at a negative price: no discharge, no export: %+v", f)
	}
	f = first(t, mid, negative, flat(96, 4), flat(96, 0.5), spec, ForcedOnly)
	if f.PVClipped > 1e-9 && f.PVToExport < spec.ExportCapKW*battery.IntervalHours-1e-9 {
		t.Errorf("forced_only clips only what the cap and battery force: %+v", f)
	}
}

// At a solar farm's size (here 1000 times a house's) the plan must still be found, and since
// the problem scales linearly, cost exactly 1000 times as much.
func TestLargeSitesScale(t *testing.T) {
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
	const k = 1000
	scale := func(v []float64) []float64 {
		out := make([]float64, len(v))
		for i, x := range v {
			out[i] = x * k
		}
		return out
	}
	for i, c := range file.Cases {
		spec, _ := battery.NewSpec(6.6*k, c.Spec.BatteryKWh*k, c.Spec.BatteryKW*k, c.Spec.ExportCapKW*k, 15*k, c.Spec.Wear)
		plan, err := Solve(c.SOC*k, spotOnly(c.Price, scale(c.PV), scale(c.Load)), spec, c.Curtail)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(plan.Cost-k*c.LPCost) > 1e-6*math.Max(1, math.Abs(k*c.LPCost)) {
			t.Errorf("case %d at %dx: cost %.6f, want %.6f", i, k, plan.Cost, k*c.LPCost)
		}
	}
}
