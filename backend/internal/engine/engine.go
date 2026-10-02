// Package engine replays a window of history with the frozen controller and
// the self-consumption baseline side by side, and produces what the playground
// streams: one tick per 5-minute step, a summary, and forecast detail for
// selected steps. It combines app/eval/run_house.py and app/policies/mpc.py.
package engine

import (
	"fmt"
	"math"
	"time"

	"climate-hacktion-curtailment/backend/internal/data"
	"climate-hacktion-curtailment/backend/internal/forecast"
	"climate-hacktion-curtailment/backend/internal/house"
	"climate-hacktion-curtailment/backend/internal/planner"
	"climate-hacktion-curtailment/backend/internal/policy"
	"climate-hacktion-curtailment/backend/internal/sim"
	"climate-hacktion-curtailment/backend/internal/split"
)

// Forecaster produces the forecast curve for row i of the frame. It is
// *forecast.Models in production and a stand-in in tests. ok is false when
// the history is too short to fill every feature.
type Forecaster interface {
	Curve(f *data.Frame, i int, spec house.HouseSpec, horizonHours int) (curve planner.ForecastCurve, ok bool, err error)
}

var _ Forecaster = (*forecast.Models)(nil)

// Config is what a replay needs besides the data.
type Config struct {
	Spec         house.HouseSpec
	HorizonHours int // planner horizon, 8 for the frozen controller
	// StepEvery keeps forecast detail for every Nth step of the window.
	StepEvery int
	// StoryGrid is "stages" (one column per planner stage) or "intervals".
	StoryGrid string
	// WearAudPerKwh is the battery wear cost used for savings_with_wear_aud.
	// It is reported after the fact and does not change decisions.
	WearAudPerKwh float64
}

// DefaultConfig is the default house, an 8-hour planner and detail every hour.
func DefaultConfig() Config {
	return Config{Spec: house.DefaultHouse(), HorizonHours: 8, StepEvery: split.StepsPerHour, StoryGrid: "stages", WearAudPerKwh: 0.05}
}

// Tick is one 5-minute step (interfacespec.md PlaygroundTick).
type Tick struct {
	I                   int     `json:"i"`
	T                   string  `json:"t"`
	PriceAudMwh         float64 `json:"price_aud_mwh"`
	PriceEstP10         float64 `json:"price_est_p10_aud_mwh"`
	PriceEstP50         float64 `json:"price_est_p50_aud_mwh"`
	PriceEstP90         float64 `json:"price_est_p90_aud_mwh"`
	PvKw                float64 `json:"pv_kw"`
	PvEstKw             float64 `json:"pv_est_kw"`
	LoadKw              float64 `json:"load_kw"`
	LoadEstKw           float64 `json:"load_est_kw"`
	Action              string  `json:"action"`
	SocKwh              float64 `json:"soc_kwh"`
	GridImportKwh       float64 `json:"grid_import_kwh"`
	GridExportKwh       float64 `json:"grid_export_kwh"`
	EnergyCashAud       float64 `json:"energy_cash_aud"`
	CumulativeSavingsAu float64 `json:"cumulative_savings_aud"`
}

// Bill is one policy's totals (interfacespec.md Bill).
type Bill struct {
	BillAud         float64 `json:"bill_aud"`
	EnergyCashAud   float64 `json:"energy_cash_aud"`
	ClippedKwh      float64 `json:"clipped_kwh"`
	ThroughputAcKwh float64 `json:"throughput_ac_kwh"`
	GridImportKwh   float64 `json:"grid_import_kwh"`
	GridExportKwh   float64 `json:"grid_export_kwh"`
}

// Summary is the last event of the stream (interfacespec.md PlaygroundSummary).
type Summary struct {
	SelfConsumption    Bill    `json:"self_consumption"`
	Planner            Bill    `json:"planner"`
	SavingsAud         float64 `json:"savings_aud"`
	SavingsWithWearAud float64 `json:"savings_with_wear_aud"`
	SupplyAud          float64 `json:"supply_aud"`
}

// Lead is one lead of the forecast issued at a step.
type Lead struct {
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

// Story is one of the four forecast stories over the planning grid.
type Story struct {
	ID          string    `json:"id"`
	Count       int       `json:"count"`
	PriceAudMwh []float64 `json:"price_aud_mwh"`
	PvKw        []float64 `json:"pv_kw"`
	LoadKw      []float64 `json:"load_kw"`
}

// Measured is what the controller saw when it decided.
type Measured struct {
	PriceAudMwh float64 `json:"price_aud_mwh"`
	PvKw        float64 `json:"pv_kw"`
	LoadKw      float64 `json:"load_kw"`
	SocKwh      float64 `json:"soc_kwh"`
}

// StepDecision is the forecast detail for one step (interfacespec.md StepDecision).
type StepDecision struct {
	T             string   `json:"t"`
	Action        string   `json:"action"`
	Measured      Measured `json:"measured"`
	EnergyCashAud float64  `json:"energy_cash_aud"`
	Leads         []Lead   `json:"leads"`
	Stories       []Story  `json:"stories"`
}

// Result is a finished replay.
type Result struct {
	Ticks         []Tick
	Summary       Summary
	Steps         map[int]StepDecision // keyed by index within the window
	FallbackSteps int                  // steps decided by the self-consumption rule for want of a forecast
	Start, End    time.Time
}

func round(x float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(x*p) / p
}

func stamp(t time.Time) string { return t.Format("2006-01-02T15:04:05-07:00") }

// Replay runs rows [start, end) of the frame. The first forecast the replay
// needs is issued 12 steps before start (it becomes the 1-hour-ahead estimate
// for the first tick), so start must leave a week and an hour of history.
func Replay(f *data.Frame, fc Forecaster, cfg Config, start, end int) (*Result, error) {
	if err := cfg.Spec.Validate(); err != nil {
		return nil, err
	}
	if cfg.HorizonHours < 1 || cfg.StepEvery < 1 {
		return nil, fmt.Errorf("horizon and step interval must be at least 1")
	}
	warm := forecast.MaxLookbackV2 + split.StepsPerHour
	if start < warm {
		return nil, fmt.Errorf("window starts at row %d but needs %d rows of history before it", start, warm)
	}
	if end > f.Len() || start >= end {
		return nil, fmt.Errorf("window [%d, %d) does not fit a frame of %d rows", start, end, f.Len())
	}
	spec := cfg.Spec
	tariff, err := house.GetTariff(spec.TariffID)
	if err != nil {
		return nil, err
	}
	clock, err := planner.StageEnds(cfg.HorizonHours)
	if err != nil {
		return nil, err
	}

	// A forecast per step, from 12 steps before the window. Step j's forecast
	// drives the decision at j and is the 1-hour-ahead estimate for j+12.
	type issued struct {
		curve planner.ForecastCurve
		ok    bool
	}
	first := start - split.StepsPerHour
	forecasts := make([]issued, end-first)
	for j := first; j < end; j++ {
		curve, ok, err := fc.Curve(f, j, spec, cfg.HorizonHours)
		if err != nil {
			return nil, fmt.Errorf("forecast at row %d: %w", j, err)
		}
		forecasts[j-first] = issued{curve, ok}
	}

	n := end - start
	res := &Result{
		Ticks: make([]Tick, 0, n),
		Steps: map[int]StepDecision{},
		Start: f.Times[start],
		End:   f.Times[end-1],
	}
	planned := sim.NewMeter(spec)
	baseline := sim.NewMeter(spec)
	deg := spec.DegradationAudPerKwh
	cumulative := 0.0

	for k := 0; k < n; k++ {
		i := start + k
		ts := f.Times[i]
		pvKw, loadKw := house.ScaleUnits(f.PvUnit[i], f.LoadUnit[i], spec)
		price := f.Price[i]

		// the controller
		now := forecasts[i-first]
		socBefore := planned.SocKwh
		var action sim.Action
		if now.ok {
			batch, err := planner.BuildPointPath(now.curve, price, pvKw, loadKw, ts, cfg.HorizonHours, "intervals")
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", i, err)
			}
			action, err = planner.PlanFirstAction(socBefore, batch, spec, tariff, clock, 0, 0)
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", i, err)
			}
		} else {
			action = policy.ChooseSelfConsumption(pvKw, loadKw, socBefore, spec)
			res.FallbackSteps++
		}
		step, err := planned.Step(ts, action, pvKw, loadKw, price)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", i, err)
		}

		// the baseline, on the same clock
		baseAction := policy.ChooseSelfConsumption(pvKw, loadKw, baseline.SocKwh, spec)
		baseStep, err := baseline.Step(ts, baseAction, pvKw, loadKw, price)
		if err != nil {
			return nil, fmt.Errorf("row %d (baseline): %w", i, err)
		}

		cash := sim.SpotEnergyAud(step, price)
		baseCash := sim.SpotEnergyAud(baseStep, price)
		// bill so far, self-consumption minus planner; wear counts toward both
		cumulative += (baseCash + deg*(baseStep.BatteryChargeAcKwh+baseStep.BatteryDischargeAcKwh)) -
			(cash + deg*(step.BatteryChargeAcKwh+step.BatteryDischargeAcKwh))

		// the 1-hour-ahead forecast aimed at this step, issued 12 steps ago
		est, err := leadAt(forecasts[i-first-split.StepsPerHour].curve, forecasts[i-first-split.StepsPerHour].ok, split.StepsPerHour)
		if err != nil {
			return nil, fmt.Errorf("row %d: no estimate to compare with: %w", i, err)
		}

		res.Ticks = append(res.Ticks, Tick{
			I:                   k,
			T:                   stamp(ts),
			PriceAudMwh:         round(price, 2),
			PriceEstP10:         round(est.PriceP10, 2),
			PriceEstP50:         round(est.PriceP50, 2),
			PriceEstP90:         round(est.PriceP90, 2),
			PvKw:                round(pvKw, 3),
			PvEstKw:             round(math.Max(est.PvKw, 0), 3),
			LoadKw:              round(loadKw, 3),
			LoadEstKw:           round(est.LoadKw, 3),
			Action:              string(action),
			SocKwh:              round(step.SocKwh, 3),
			GridImportKwh:       round(step.GridImportKwh, 4),
			GridExportKwh:       round(step.GridExportKwh, 4),
			EnergyCashAud:       round(cash, 4),
			CumulativeSavingsAu: round(cumulative, 4),
		})

		if k%cfg.StepEvery == 0 {
			detail, err := stepDetail(now.curve, now.ok, cfg, ts, action, price, pvKw, loadKw, socBefore, cash)
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", i, err)
			}
			res.Steps[k] = detail
		}
	}

	if res.Summary, err = summarise(planned, baseline, res.Ticks, spec, cfg.WearAudPerKwh); err != nil {
		return nil, err
	}
	return res, nil
}

func leadAt(curve planner.ForecastCurve, ok bool, lead int) (planner.LeadForecast, error) {
	if !ok {
		return planner.LeadForecast{}, fmt.Errorf("the forecast is not available")
	}
	for _, l := range curve.Leads {
		if l.LeadSteps == lead {
			return l, nil
		}
	}
	return planner.LeadForecast{}, fmt.Errorf("the forecast has no %d-step lead", lead)
}

func stepDetail(curve planner.ForecastCurve, ok bool, cfg Config, ts time.Time, action sim.Action,
	price, pvKw, loadKw, soc, cash float64) (StepDecision, error) {
	d := StepDecision{
		T:             stamp(ts),
		Action:        string(action),
		Measured:      Measured{round(price, 2), round(pvKw, 3), round(loadKw, 3), round(soc, 3)},
		EnergyCashAud: round(cash, 4),
		Leads:         []Lead{},
		Stories:       []Story{},
	}
	if !ok {
		return d, nil
	}
	for _, l := range curve.Leads {
		d.Leads = append(d.Leads, Lead{
			LeadSteps: l.LeadSteps,
			PriceP10:  round(l.PriceP10, 2), PriceP50: round(l.PriceP50, 2), PriceP90: round(l.PriceP90, 2),
			PvKw: round(l.PvKw, 3), PvLo: round(l.PvLo, 3), PvHi: round(l.PvHi, 3),
			LoadKw: round(l.LoadKw, 3), LoadLo: round(l.LoadLo, 3), LoadHi: round(l.LoadHi, 3),
		})
	}
	stories, _, err := planner.Stories(curve, price, pvKw, loadKw, cfg.HorizonHours, cfg.StoryGrid)
	if err != nil {
		return StepDecision{}, err
	}
	for _, s := range stories {
		d.Stories = append(d.Stories, Story{
			ID: s.Name, Count: s.Count,
			PriceAudMwh: roundAll(s.PriceAudMwh, 2),
			PvKw:        roundAll(s.PvKw, 3),
			LoadKw:      roundAll(s.LoadKw, 3),
		})
	}
	return d, nil
}

func roundAll(xs []float64, places int) []float64 {
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = round(x, places)
	}
	return out
}

func summarise(planned, baseline *sim.Meter, ticks []Tick, spec house.HouseSpec, wear float64) (Summary, error) {
	p, err := planned.Finish()
	if err != nil {
		return Summary{}, err
	}
	b, err := baseline.Finish()
	if err != nil {
		return Summary{}, err
	}
	tariff, err := house.GetTariff(spec.TariffID)
	if err != nil {
		return Summary{}, err
	}
	days := map[string]struct{}{}
	for _, t := range ticks {
		days[t.T[:10]] = struct{}{}
	}
	bill := func(r sim.EpisodeResult) Bill {
		return Bill{
			BillAud:         round(r.BillAud, 4),
			EnergyCashAud:   round(r.SpotMtdAud, 4),
			ClippedKwh:      round(r.ClippedKwh, 3),
			ThroughputAcKwh: round(r.ThroughputAcKwh, 3),
			GridImportKwh:   round(r.GridImportKwh, 3),
			GridExportKwh:   round(r.GridExportKwh, 3),
		}
	}
	savings := b.BillAud - p.BillAud
	return Summary{
		SelfConsumption: bill(b),
		Planner:         bill(p),
		SavingsAud:      round(savings, 4),
		// wear is charged on the throughput each policy used
		SavingsWithWearAud: round(savings-wear*(p.ThroughputAcKwh-b.ThroughputAcKwh), 4),
		SupplyAud:          round(float64(len(days))*tariff.SupplyAudPerDay*(1.0+tariff.GST), 4),
	}, nil
}
