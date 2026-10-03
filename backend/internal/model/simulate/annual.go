package simulate

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/battery"
	"climate-hacktion-curtailment/backend/internal/model/forecast"
)

// AnnualWeeks is how many weeks the payback estimate replays: one every three weeks of
// data.Year, so every season in it is sampled.
const AnnualWeeks = 14

// Annual is a year of bills for one house, scaled up from sample weeks spread over the year.
type Annual struct {
	From, To    time.Time // the year the weeks were drawn from
	Weeks       int       // weeks replayed
	SampledDays int
	// Bills for 365 days (energy at the tariff plus the daily supply charge), AUD. Battery wear
	// is not in them.
	NoSystemAUD, SelfAUD, PlannerAUD float64
	// Battery throughput for 365 days (kWh, AC), to price wear.
	SelfThroughputKWh, PlannerThroughputKWh float64
}

// RunAnnual replays weeks evenly spaced over [from, to] (one every 52/weeks weeks) and scales
// the bills to a year. "No system" is the same house buying all its demand from the grid.
// Weeks run in parallel; each starts with the battery half full.
func RunAnnual(m *forecast.Models, in *forecast.Inputs, spec battery.Spec, from, to time.Time, weeks int, opt Options) (Annual, error) {
	span := int(to.Sub(from).Hours()/24/7) + 1
	if weeks <= 0 || weeks > span {
		weeks = span
	}
	stride := span / weeks
	type part struct {
		days                 int
		noSystem, self, plan float64
		selfThru, planThru   float64
		err                  error
	}
	parts := make([]part, weeks)
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for w := 0; w < weeks; w++ {
		start := from.AddDate(0, 0, 7*w*stride)
		end := start.AddDate(0, 0, 7).Add(-5 * time.Minute)
		if end.After(to) {
			end = to
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := Run(m, in, spec, start, end, opt, nil)
			if err != nil {
				parts[w].err = fmt.Errorf("week from %s: %w", start.Format("2 Jan 2006"), err)
				return
			}
			p := part{days: res.Planner.Days, self: res.Self.BillAUD, plan: res.Planner.BillAUD,
				selfThru: res.Self.Throughput(), planThru: res.Planner.Throughput()}
			p.noSystem = float64(res.Planner.Days) * opt.Tariff.SupplyAUDPerDay
			for _, s := range res.Steps {
				p.noSystem += s.LoadkW * battery.IntervalHours * s.Prices.Import
			}
			parts[w] = p
		}()
	}
	wg.Wait()
	a := Annual{From: from, To: to, Weeks: weeks}
	for _, p := range parts {
		if p.err != nil {
			return Annual{}, p.err
		}
		a.SampledDays += p.days
		a.NoSystemAUD += p.noSystem
		a.SelfAUD += p.self
		a.PlannerAUD += p.plan
		a.SelfThroughputKWh += p.selfThru
		a.PlannerThroughputKWh += p.planThru
	}
	if a.SampledDays == 0 {
		return Annual{}, fmt.Errorf("no days replayed")
	}
	scale := 365 / float64(a.SampledDays)
	a.NoSystemAUD *= scale
	a.SelfAUD *= scale
	a.PlannerAUD *= scale
	a.SelfThroughputKWh *= scale
	a.PlannerThroughputKWh *= scale
	return a, nil
}
