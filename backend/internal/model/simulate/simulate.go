// Package simulate replays a house over a window: every 5 minutes the planner chooses the
// battery's next move from the forecasts, and the meter settles it against what happened.
// Self-consumption (store spare solar, cover the house from the battery) runs alongside as the
// baseline.
package simulate

import (
	"fmt"
	"math"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/battery"
	"climate-hacktion-curtailment/backend/internal/model/features"
	"climate-hacktion-curtailment/backend/internal/model/forecast"
	"climate-hacktion-curtailment/backend/internal/model/house"
	"climate-hacktion-curtailment/backend/internal/model/planner"
)

// AimSteps: a step's "estimate" is the 1-hour-ahead forecast made 12 steps earlier.
const AimSteps = 12

// Step is one settled interval.
type Step struct {
	Time      time.Time
	Price     float64 // $/MWh
	PVkW      float64 // what the roof made
	LoadkW    float64 // what the house used
	SOCBefore float64 // kWh
	Planner   battery.Step
	Self      battery.Step // self-consumption baseline
	// The 1-hour-ahead forecasts made an hour before this interval.
	EstPrice  [3]float64 // P10/P50/P90 $/MWh
	EstPVkW   float64
	EstLoadkW float64
}

// Result is a finished replay.
type Result struct {
	Spec    battery.Spec
	Steps   []Step
	Planner battery.Bill
	Self    battery.Bill

	in    *forecast.Inputs
	fc    *forecast.Forecasts
	bands map[string][2]float64
	first int
}

// Run replays the house over the intervals ending at start..end (NEM time). onStep, if not nil,
// is called as each interval settles; returning an error stops the replay.
func Run(m *forecast.Models, in *forecast.Inputs, spec battery.Spec, start, end time.Time, curtail planner.Curtail, onStep func(Step) error) (*Result, error) {
	first, last := in.Index(start), in.Index(end)
	if first < 0 || last < 0 || last < first {
		return nil, fmt.Errorf("the data does not cover %s to %s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	fc, err := m.Run(in, first-AimSteps, last)
	if err != nil {
		return nil, err
	}
	r := &Result{Spec: spec, in: in, fc: fc, bands: m.Meta.Units.Bands, first: first}
	loadScale := spec.DailyLoadKWh / house.LoadUnitDailyKWh
	plannerMeter, selfMeter := battery.NewMeter(spec), battery.NewMeter(spec)
	soc, socSelf := spec.InitialSOC(), spec.InitialSOC()
	for k := first; k <= last; k++ {
		t, price := in.Time(k), in.Price[k]
		pv, load := in.PV[k]*spec.PVkWAC, in.Load[k]*loadScale
		pricePath, pvPath, loadPath := r.paths(k)
		flows, err := planner.Plan(soc, pricePath, pvPath, loadPath, spec, curtail)
		if err != nil {
			return nil, err
		}
		step, err := battery.ApplyFlows(soc, flows, pv, load, spec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.Format(time.RFC3339), err)
		}
		action := battery.DischargeLoad
		if pv > load {
			action = battery.ChargeSurplus
		}
		self, err := battery.Apply(socSelf, action, pv, load, spec)
		if err != nil {
			return nil, err
		}
		plannerMeter.Record(t, step, price)
		selfMeter.Record(t, self, price)
		aimed, aimedPV, aimedLoad := fc.At(k - AimSteps)
		s := Step{Time: t, Price: price, PVkW: pv, LoadkW: load, SOCBefore: soc, Planner: step, Self: self,
			EstPrice: aimed[0], EstPVkW: aimedPV[0] * spec.PVkWAC, EstLoadkW: aimedLoad[0] * loadScale}
		soc, socSelf = step.SOC, self.SOC
		r.Steps = append(r.Steps, s)
		if onStep != nil {
			if err := onStep(s); err != nil {
				return nil, err
			}
		}
	}
	r.Planner, r.Self = plannerMeter.Finish(), selfMeter.Finish()
	return r, nil
}

// paths are the 8-hour price, PV and load paths the planner plans on at interval k: the price
// now (published at the interval's start) and the last measured PV and load, then the forecasts.
func (r *Result) paths(k int) (price, pv, load []float64) {
	q, fpv, fload := r.fc.At(k)
	loadScale := r.Spec.DailyLoadKWh / house.LoadUnitDailyKWh
	p50, pvLeads, loadLeads := make([]float64, len(q)), make([]float64, len(q)), make([]float64, len(q))
	for j := range q {
		p50[j] = q[j][1]
		pvLeads[j] = fpv[j] * r.Spec.PVkWAC
		loadLeads[j] = fload[j] * loadScale
	}
	lastPV, lastLoad := r.in.PV[k-1]*r.Spec.PVkWAC, r.in.Load[k-1]*loadScale
	return planner.Path(r.in.Price[k], features.Leads, p50), planner.Path(lastPV, features.Leads, pvLeads), planner.Path(lastLoad, features.Leads, loadLeads)
}

// Lead is the forecast at one lead, made at a step.
type Lead struct {
	Steps                     int        // 5-minute steps ahead
	Price                     [3]float64 // P10/P50/P90 $/MWh
	PVkW, PVLow, PVHigh       float64    // point forecast, and its 10th-90th percentile error range
	LoadkW, LoadLow, LoadHigh float64
}

// Detail is what the planner saw at step i of the window.
type Detail struct {
	Leads []Lead
	// The 96-step (8-hour) paths it planned on; [0] is what was known at the decision.
	PricePath, PVPath, LoadPath []float64
}

// Detail explains step i.
func (r *Result) Detail(i int) (Detail, error) {
	if i < 0 || i >= len(r.Steps) {
		return Detail{}, fmt.Errorf("no step %d", i)
	}
	k := r.first + i
	q, fpv, fload := r.fc.At(k)
	loadScale := r.Spec.DailyLoadKWh / house.LoadUnitDailyKWh
	var d Detail
	for j, lead := range features.Leads {
		pvBand, loadBand := r.bands[fmt.Sprintf("pv_%d", lead)], r.bands[fmt.Sprintf("load_%d", lead)]
		d.Leads = append(d.Leads, Lead{
			Steps:    lead,
			Price:    q[j],
			PVkW:     fpv[j] * r.Spec.PVkWAC,
			PVLow:    math.Max(fpv[j]+pvBand[0], 0) * r.Spec.PVkWAC,
			PVHigh:   math.Max(fpv[j]+pvBand[1], 0) * r.Spec.PVkWAC,
			LoadkW:   fload[j] * loadScale,
			LoadLow:  math.Max(fload[j]+loadBand[0], 0) * loadScale,
			LoadHigh: math.Max(fload[j]+loadBand[1], 0) * loadScale,
		})
	}
	d.PricePath, d.PVPath, d.LoadPath = r.paths(k)
	return d, nil
}
