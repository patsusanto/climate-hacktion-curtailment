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

// daySteps is one day of 5-minute steps.
const daySteps = 288

// Options are how the house is billed and how the planner plans.
type Options struct {
	Tariff  battery.Tariff
	Curtail planner.Curtail
	// WearAUDPerKWh is what the planner charges itself per kWh the battery moves (AC), so it cycles
	// only when the price difference pays for the wear. It is not on the bill.
	WearAUDPerKWh float64
	// HorizonSteps is how far the planner looks ahead. The forecasts cover 8 hours (96 steps);
	// beyond that the planner assumes tomorrow repeats today (yesterday's price, solar and demand
	// at the same time), which is enough to see that tomorrow's sun will refill the battery.
	HorizonSteps int
	// FallbackAUD: when the plan expects to beat self-consumption over the horizon by less than
	// this, the battery runs self-consumption for the step instead. Forecasts are uncertain, so a
	// small expected gain is not worth acting on. Zero turns the fallback off.
	FallbackAUD float64
	// Load, if set, is the site's demand (kW) on the data's clock, used instead of the household
	// profile (which is what the trained demand model forecasts). The planner then forecasts it as
	// the same time last week, which suits a business's weekly routine. All zeros is a site with
	// no demand, such as a solar farm.
	Load []float64
}

// weekSteps is one week of 5-minute steps.
const weekSteps = 7 * daySteps

// DefaultOptions are what the playground uses.
func DefaultOptions() Options {
	// The fallback is off: on the validation and test windows it cost slightly more than it saved.
	return Options{Tariff: battery.Ausgrid, Curtail: planner.Economic, WearAUDPerKWh: 0.05, HorizonSteps: daySteps}
}

// Step is one settled interval.
type Step struct {
	Time      time.Time
	Price     float64        // spot, $/MWh
	Prices    battery.Prices // what importing cost and exporting earned, $/kWh
	PVkW      float64        // what the roof made
	LoadkW    float64        // what the house used
	SOCBefore float64        // kWh
	Planner   battery.Step
	Self      battery.Step // self-consumption baseline
	// Battery wear this step at the options' rate ($), each way of running the battery.
	PlannerWearAUD, SelfWearAUD float64
	// SupplyAUDPerDay is the tariff's daily supply charge, the same for every step.
	SupplyAUDPerDay float64
	Reason          string // why the planner did what it did, in plain words
	Fallback        bool   // the plan's expected gain was too small, so it ran self-consumption
	// The 1-hour-ahead forecasts made an hour before this interval.
	EstPrice  [3]float64 // P10/P50/P90 $/MWh
	EstPVkW   float64
	EstLoadkW float64
}

// Result is a finished replay.
type Result struct {
	Spec    battery.Spec
	Options Options
	Steps   []Step
	Planner battery.Bill
	Self    battery.Bill
	// Annual is the same house over a year, for the payback figures. Run leaves it nil; the
	// caller sets it from RunAnnual when it has the year's data.
	Annual *Annual

	in    *forecast.Inputs
	fc    *forecast.Forecasts
	bands map[string][2]float64
	first int
}

// Run replays the house over the intervals ending at start..end (NEM time). onStep, if not nil,
// is called as each interval settles; returning an error stops the replay.
func Run(m *forecast.Models, in *forecast.Inputs, spec battery.Spec, start, end time.Time, opt Options, onStep func(Step) error) (*Result, error) {
	first, last := in.Index(start), in.Index(end)
	if first < 0 || last < 0 || last < first {
		return nil, fmt.Errorf("the data does not cover %s to %s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	if opt.HorizonSteps <= 0 {
		opt.HorizonSteps = planner.ForecastSteps
	}
	if first < daySteps {
		return nil, fmt.Errorf("the data needs a day of history before %s", start.Format(time.RFC3339))
	}
	fc, err := m.Run(in, first-AimSteps, last)
	if err != nil {
		return nil, err
	}
	spec.WearAUDPerKWh = opt.WearAUDPerKWh
	r := &Result{Spec: spec, Options: opt, in: in, fc: fc, bands: m.Meta.Units.Bands, first: first}
	loadScale := spec.DailyLoadKWh / house.LoadUnitDailyKWh
	plannerMeter, selfMeter := battery.NewMeter(opt.Tariff), battery.NewMeter(opt.Tariff)
	soc, socSelf := spec.InitialSOC(), spec.InitialSOC()
	for k := first; k <= last; k++ {
		t, price := in.Time(k), in.Price[k]
		prices := opt.Tariff.At(t, price)
		pv, load := in.PV[k]*spec.PVkWAC, r.load(k)
		h := r.horizon(k)
		plan, err := planner.Solve(soc, h, spec, opt.Curtail)
		if err != nil {
			return nil, err
		}
		intent, fallback := plan.First, false
		if opt.FallbackAUD > 0 {
			if gain := selfCost(soc, h, spec, opt.Curtail) - plan.Cost; gain < opt.FallbackAUD {
				intent, fallback = selfIntent(h.PV[0], h.Load[0]), true
			}
		}
		step, err := battery.Settle(soc, intent, pv, load, prices, spec, opt.Curtail)
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
		plannerMeter.Record(t, step, prices)
		selfMeter.Record(t, self, prices)
		aimed, aimedPV, aimedLoad := fc.At(k - AimSteps)
		s := Step{Time: t, Price: price, Prices: prices, PVkW: pv, LoadkW: load, SOCBefore: soc, Planner: step, Self: self,
			Fallback: fallback, EstPrice: aimed[0], EstPVkW: aimedPV[0] * spec.PVkWAC, EstLoadkW: aimedLoad[0] * loadScale}
		if opt.Load != nil {
			s.EstLoadkW = r.load(k - weekSteps)
		}
		s.SupplyAUDPerDay = opt.Tariff.SupplyAUDPerDay
		s.PlannerWearAUD = opt.WearAUDPerKWh * (step.ChargeAC + step.DischargeAC)
		s.SelfWearAUD = opt.WearAUDPerKWh * (self.ChargeAC + self.DischargeAC)
		s.Reason = reason(s, plan, h, t, spec)
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

// selfIntent is the self-consumption rule as a plan: store spare solar, cover the house from
// the battery. Settle fits it to what the interval brings.
func selfIntent(pvKW, loadKW float64) battery.Flows {
	surplus := (pvKW - loadKW) * battery.IntervalHours
	if surplus > 0 {
		return battery.Flows{PVToBattery: surplus}
	}
	return battery.Flows{BatteryToLoad: -surplus}
}

// selfCost is what self-consumption would cost over the horizon if the forecasts came true,
// priced the way the planner prices its own plan (wear included), so the two compare.
func selfCost(soc float64, h planner.Horizon, spec battery.Spec, curtail planner.Curtail) float64 {
	leg := spec.Leg()
	total := 0.0
	for k := range h.Price {
		dt := battery.IntervalHours * float64(h.Intervals[k])
		pv, load := math.Max(h.PV[k], 0)*dt, math.Max(h.Load[k], 0)*dt
		maxAC := spec.MaxPowerKW * dt
		cap := spec.ExportCapKW * dt
		var b float64
		if pv > load {
			b = math.Min(pv-load, math.Min(maxAC, math.Max(0, (spec.SOCMax()-soc)/leg)))
			soc += b * leg
		} else {
			b = -math.Min(load-pv, math.Min(maxAC, math.Max(0, (soc-spec.SOCMin())*leg)))
			soc += b / leg
		}
		_, c := battery.GridNet(b, pv, load, cap, h.Price[k], curtail)
		total += c + spec.WearAUDPerKWh*math.Abs(b)
	}
	return total
}

// paths are the 8-hour spot price ($/MWh), PV and load (kW) paths the forecasts give at
// interval k: the price now (published at the interval's start) and the last measured PV and
// load, then the forecasts at each lead, linear between.
func (r *Result) paths(k int) (price, pv, load []float64) {
	q, fpv, fload := r.fc.At(k)
	loadScale := r.Spec.DailyLoadKWh / house.LoadUnitDailyKWh
	p50, pvLeads, loadLeads := make([]float64, len(q)), make([]float64, len(q)), make([]float64, len(q))
	for j := range q {
		p50[j] = q[j][1]
		pvLeads[j] = fpv[j] * r.Spec.PVkWAC
		loadLeads[j] = fload[j] * loadScale
	}
	if r.Options.Load != nil { // a site's own demand: the same time last week, from the last reading
		for j, lead := range features.Leads {
			loadLeads[j] = r.load(k + lead - weekSteps)
		}
	}
	lastPV, lastLoad := r.in.PV[k-1]*r.Spec.PVkWAC, r.load(k-1)
	return planner.Path(r.in.Price[k], features.Leads, p50), planner.Path(lastPV, features.Leads, pvLeads), planner.Path(lastLoad, features.Leads, loadLeads)
}

// horizon is what the planner plans on at interval k: the forecast paths for 8 hours in
// 5-minute steps, then yesterday's values at the same time of day (all known at the decision)
// in hourly steps out to HorizonSteps, priced by the tariff.
func (r *Result) horizon(k int) planner.Horizon {
	const block = 12 // intervals per step beyond the forecasts
	spot, pv, load := r.paths(k)
	var h planner.Horizon
	for j := 0; j < len(spot) && j < r.Options.HorizonSteps; j++ {
		h.Price = append(h.Price, r.Options.Tariff.At(r.in.Time(k+j), spot[j]))
		h.PV, h.Load, h.Intervals = append(h.PV, pv[j]), append(h.Load, load[j]), append(h.Intervals, 1)
	}
	for j := len(spot); j < r.Options.HorizonSteps; j += block {
		m := min(block, r.Options.HorizonSteps-j)
		var p battery.Prices
		var sumPV, sumLoad float64
		for i := j; i < j+m; i++ {
			y := k + i - daySteps*(i/daySteps+1) // the same time of day, on the last day already seen
			at := r.Options.Tariff.At(r.in.Time(k+i), r.in.Price[y])
			p.Import += at.Import / float64(m)
			p.Export += at.Export / float64(m)
			sumPV += r.in.PV[y] * r.Spec.PVkWAC
			sumLoad += r.load(y)
		}
		h.Price = append(h.Price, p)
		h.PV, h.Load, h.Intervals = append(h.PV, sumPV/float64(m)), append(h.Load, sumLoad/float64(m)), append(h.Intervals, m)
	}
	return h
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
		if r.Options.Load != nil { // last week's demand, without a range
			l := r.load(k + lead - weekSteps)
			last := &d.Leads[len(d.Leads)-1]
			last.LoadkW, last.LoadLow, last.LoadHigh = l, l, l
		}
	}
	d.PricePath, d.PVPath, d.LoadPath = r.paths(k)
	return d, nil
}

// load is the site's demand (kW) in interval k: its own profile, or the household's scaled to
// the spec's daily use.
func (r *Result) load(k int) float64 {
	if r.Options.Load != nil {
		return r.Options.Load[k]
	}
	return r.in.Load[k] * (r.Spec.DailyLoadKWh / house.LoadUnitDailyKWh) // this order, as the scale is computed elsewhere
}
