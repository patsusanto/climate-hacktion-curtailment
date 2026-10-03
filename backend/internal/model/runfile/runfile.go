// Package runfile turns a replay (simulate.Result) into the run file the playground service
// serves: backend/runs/<run_id>.json, in the shapes of internal/wire.
//
// Build packages a finished replay. The pieces it is made of (Meta, Ticker, StepDecision and
// Summary) are exported so a live run can send them one at a time as the replay goes.
package runfile

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/battery"
	"climate-hacktion-curtailment/backend/internal/model/house"
	"climate-hacktion-curtailment/backend/internal/model/payback"
	"climate-hacktion-curtailment/backend/internal/model/simulate"
	"climate-hacktion-curtailment/backend/internal/wire"
)

// Options are the run's labels and what to keep.
type Options struct {
	ID            string
	WindowName    string  // "validation" or "test"
	DetailEvery   int     // keep the forecast detail for every Nth step (the file grows ~3 KB per kept step)
	TrainedBefore string  // shown in the note, e.g. "19 Aug 2026"
}

// Meta is the run's metadata, which a live run sends before the first step. start and end are
// the first and last intervals of the window, and n the number of 5-minute steps between them.
func Meta(spec battery.Spec, sim simulate.Options, start, end time.Time, n int, opt Options) wire.Meta {
	return wire.Meta{
		Assumptions: wire.Assumptions{
			PriceRegion: "NSW1",
			PriceSource: "historical_spot",
			Roof:        "sydney_observed_weather_scaled_by_pv_kw_ac",
			Load:        "evening_peak_synthetic",
			Lat:         house.Lat,
			Lon:         house.Lon,
			Note: "The house runs on Sydney's observed weather and NSW1 spot prices whatever the address. " +
				"Prices and solar are forecast by models trained on data before " + opt.TrainedBefore + ".",
			Tariff: sim.Tariff.Describe(),
		},
		Spec: wire.Spec{
			PvKwAc:               spec.PVkWAC,
			ExportCapKw:          spec.ExportCapKW,
			BatteryKwh:           spec.CapacityKWh,
			BatteryKw:            spec.MaxPowerKW,
			UsableKwh:            round(spec.Usable(), 4),
			DailyLoadKwh:         spec.DailyLoadKWh,
			DegradationAudPerKwh: sim.WearAUDPerKWh,
		},
		Window: wire.Window{
			Start:       stamp(start),
			End:         stamp(end),
			StepMinutes: 5,
			N:           n,
		},
	}
}

// Ticker turns settled steps into ticks, in order, keeping the running cost of each strategy:
// energy plus battery wear, so the two compare like for like.
type Ticker struct{ cumSelf, cumPlanner float64 }

// Tick is step i's tick.
func (t *Ticker) Tick(i int, s simulate.Step) wire.Tick {
	cash := s.Planner.Cash(s.Prices)
	t.cumPlanner += cash + s.PlannerWearAUD
	t.cumSelf += s.Self.Cash(s.Prices) + s.SelfWearAUD
	return wire.Tick{
		I:                    i,
		T:                    stamp(s.Time),
		PriceAudMwh:          round(s.Price, 3),
		PriceEstP10:          round(s.EstPrice[0], 3),
		PriceEstP50:          round(s.EstPrice[1], 3),
		PriceEstP90:          round(s.EstPrice[2], 3),
		PvKw:                 round(s.PVkW, 4),
		PvEstKw:              round(s.EstPVkW, 4),
		LoadKw:               round(s.LoadkW, 4),
		LoadEstKw:            round(s.EstLoadkW, 4),
		Action:               string(s.Planner.Action),
		SocKwh:               round(s.Planner.SOC, 4),
		GridImportKwh:        round(s.Planner.GridImport, 5),
		GridExportKwh:        round(s.Planner.GridExport, 5),
		EnergyCashAud:        round(cash, 5),
		CumulativeSelfAud:    round(t.cumSelf, 4),
		CumulativeSavingsAud: round(t.cumSelf-t.cumPlanner, 4),
		ImportAudKwh:         round(s.Prices.Import, 4),
		ExportAudKwh:         round(s.Prices.Export, 4),
		Reason:               s.Reason,
	}
}

// StepDecision is the forecast detail for step i of a finished replay.
func StepDecision(res *simulate.Result, i int) (wire.StepDecision, error) {
	d, err := res.Detail(i)
	if err != nil {
		return wire.StepDecision{}, err
	}
	s := res.Steps[i]
	decision := wire.StepDecision{
		T:      stamp(s.Time),
		Action: string(s.Planner.Action),
		Measured: wire.Measured{
			PriceAudMwh: round(s.Price, 3),
			PvKw:        round(d.PVPath[0], 4), // the last measured interval: what the planner knew
			LoadKw:      round(d.LoadPath[0], 4),
			SocKwh:      round(s.SOCBefore, 4),
		},
		EnergyCashAud: round(s.Planner.Cash(s.Prices), 5),
		Stories: []wire.Story{{
			ID: "mid", Count: 1,
			PriceAudMwh: roundAll(d.PricePath, 2),
			PvKw:        roundAll(d.PVPath, 3),
			LoadKw:      roundAll(d.LoadPath, 3),
		}},
	}
	for _, l := range d.Leads {
		decision.Leads = append(decision.Leads, wire.Lead{
			LeadSteps: l.Steps,
			PriceP10:  round(l.Price[0], 3), PriceP50: round(l.Price[1], 3), PriceP90: round(l.Price[2], 3),
			PvKw: round(l.PVkW, 4), PvLo: round(l.PVLow, 4), PvHi: round(l.PVHigh, 4),
			LoadKw: round(l.LoadkW, 4), LoadLo: round(l.LoadLow, 4), LoadHi: round(l.LoadHigh, 4),
		})
	}
	return decision, nil
}

// Summary is the last event of a run: both bills, the savings, warnings and the payback. Each
// bill includes battery wear at the run's rate, so the savings compare like for like.
func Summary(res *simulate.Result, opt Options) wire.Summary {
	wear := res.Options.WearAUDPerKWh
	p, sc := res.Planner, res.Self
	saved := (sc.BillAUD + wear*sc.Throughput()) - (p.BillAUD + wear*p.Throughput())
	out := wire.Summary{
		SelfConsumption:    bill(sc, wear),
		Planner:            bill(p, wear),
		SavingsAud:         round(saved, 4),
		SavingsWithWearAud: round(saved, 4),
		SupplyAud:          round(p.SupplyAUD(), 4),
	}
	if saved < 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("For this house, the battery's own self-consumption mode did better than the planner "+
			"over this window, by %s once battery wear is counted. Planning has little to work with when the house "+
			"can't export or the battery is small for its solar and demand.", dollars(-saved)))
	}
	if res.Annual != nil {
		pb := Payback(res.Spec, *res.Annual, wear)
		out.Payback = &pb
		const warranty = 10 // years; the usual battery warranty
		switch {
		case pb.PaybackYearsPlanner == 0:
			out.Warnings = append(out.Warnings, "At these prices the system's savings never repay what it costs.")
		case pb.PaybackYearsPlanner > warranty:
			out.Warnings = append(out.Warnings, fmt.Sprintf("Payback takes %.1f years, longer than the usual 10-year battery warranty.", pb.PaybackYearsPlanner))
		}
	}
	return out
}

// Payback is the year of bills and what the system costs.
func Payback(spec battery.Spec, a simulate.Annual, wear float64) wire.Payback {
	cost, rebate := payback.Cost(spec.PVkWAC, spec.CapacityKWh)
	return wire.Payback{
		Basis: fmt.Sprintf("%d weeks spread over %s - %s, scaled to a year. The models were trained on data before %s, so most of this year is data they have seen.",
			a.Weeks, a.From.Format("2 Jan 2006"), a.To.Format("2 Jan 2006"), "19 Aug 2026"),
		AnnualBillNoSystemAud:        round(a.NoSystemAUD, 2),
		AnnualBillSelfConsumptionAud: round(a.SelfAUD+wear*a.SelfThroughputKWh, 2),
		AnnualBillPlannerAud:         round(a.PlannerAUD+wear*a.PlannerThroughputKWh, 2),
		AnnualWearSelfConsumptionAud: round(wear*a.SelfThroughputKWh, 2),
		AnnualWearPlannerAud:         round(wear*a.PlannerThroughputKWh, 2),
		WearAudPerKwh:                wear,
		SolarAudPerKw:                payback.SolarAUDPerKW,
		BatteryAudPerKwh:             payback.BatteryAUDPerKWh,
		BatteryRebateAud:             round(rebate, 2),
		SystemCostAud:                round(cost, 2),
		CostSources:                  payback.Sources,
		PaybackYearsSelfConsumption:  round(payback.Years(cost, a.NoSystemAUD-a.SelfAUD-wear*a.SelfThroughputKWh), 2),
		PaybackYearsPlanner:          round(payback.Years(cost, a.NoSystemAUD-a.PlannerAUD-wear*a.PlannerThroughputKWh), 2),
	}
}

func dollars(v float64) string { return fmt.Sprintf("$%.2f", v) }

// Build packages a finished replay.
func Build(res *simulate.Result, opt Options) (wire.RunFile, error) {
	if len(res.Steps) == 0 {
		return wire.RunFile{}, fmt.Errorf("runfile: the replay has no steps")
	}
	if opt.DetailEvery <= 0 {
		opt.DetailEvery = 1
	}
	last := len(res.Steps) - 1
	run := wire.RunFile{
		RunID:      opt.ID,
		WindowName: opt.WindowName,
		Meta:       Meta(res.Spec, res.Options, res.Steps[0].Time, res.Steps[last].Time, len(res.Steps), opt),
		Steps:      map[string]wire.StepDecision{},
	}
	var ticker Ticker
	for i, s := range res.Steps {
		run.Ticks = append(run.Ticks, ticker.Tick(i, s))
		if i%opt.DetailEvery != 0 {
			continue
		}
		decision, err := StepDecision(res, i)
		if err != nil {
			return wire.RunFile{}, err
		}
		run.Steps[strconv.Itoa(i)] = decision
	}
	run.Summary = Summary(res, opt)
	return run, nil
}

func bill(b battery.Bill, wear float64) wire.Bill {
	w := wear * b.Throughput()
	return wire.Bill{BillAud: round(b.BillAUD+w, 4), EnergyCashAud: round(b.EnergyCashAUD, 4), ClippedKwh: round(b.Clipped, 4),
		ThroughputAcKwh: round(b.Throughput(), 4), GridImportKwh: round(b.GridImport, 4), GridExportKwh: round(b.GridExport, 4),
		WearAud: round(w, 4)}
}

// Write writes the run as compact JSON to dir/<run_id>.json, replacing any existing file only
// once the new one is complete.
func Write(dir string, run wire.RunFile) (string, error) {
	if run.RunID == "" {
		return "", fmt.Errorf("runfile: the run needs an id")
	}
	raw, err := json.Marshal(run)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, run.RunID+".json")
	if err := os.WriteFile(path+".tmp", raw, 0o644); err != nil {
		return "", err
	}
	return path, os.Rename(path+".tmp", path)
}

func stamp(t time.Time) string { return t.Format("2006-01-02T15:04:05-07:00") }

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	r := math.Round(v*p) / p
	if r == 0 {
		return 0 // no "-0"
	}
	return r
}

func roundAll(v []float64, places int) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = round(x, places)
	}
	return out
}
