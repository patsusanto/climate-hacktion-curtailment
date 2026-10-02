package planner

import (
	"errors"
	"fmt"
	"math"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/house"
	"climate-hacktion-curtailment/backend/internal/model/sim"
	"climate-hacktion-curtailment/backend/internal/model/split"
)

// Expected-cash dynamic program. The planner is not trained.
//
// Each step is scored on the settled energy cash: grid import and grid export
// at NSW1 spot, ex-GST, minus degradation, plus any clip bonus. Leftover SOC
// is not cash, so the terminal mark is zero.

// Scores within this band are the same cash. SOC-bin interpolation leaves a
// flat spot a fraction of a cent apart, so an exact argmax keeps the battery
// and the receding plan never sells. The ranking below breaks that tie from
// the spot price of the step being solved.
const sameCashAud = 1e-3

// horizonActions: continuation ties keep energy for a later price.
var horizonActions = [...]sim.Action{
	sim.ChargeSurplus,
	sim.DischargeLoad,
	sim.Hold,
	sim.Charge,
	sim.Discharge,
}

// rankedActions orders the actions for the step that runs. Equal cash is taken
// now: a positive spot exports, a cheap spot grid-charges.
func rankedActions(priceAudMwh float64) [5]sim.Action {
	if priceAudMwh > 0.0 {
		return [5]sim.Action{sim.Discharge, sim.DischargeLoad, sim.ChargeSurplus, sim.Hold, sim.Charge}
	}
	return [5]sim.Action{sim.Charge, sim.ChargeSurplus, sim.DischargeLoad, sim.Hold, sim.Discharge}
}

// ScenarioBatch is a set of realised or forecast paths on the 5-minute clock,
// with the current interval at column 0. Rows are scenarios.
type ScenarioBatch struct {
	Timestamps  []time.Time
	PvKw        [][]float64
	LoadKw      [][]float64
	PriceAudMwh [][]float64
	Weights     []float64 // per scenario; nil means equal weights
}

// Validate checks the shapes, like ScenarioBatch.__post_init__.
func (b ScenarioBatch) Validate() error {
	n := len(b.Timestamps)
	for name, rows := range map[string][][]float64{"pv_kw": b.PvKw, "load_kw": b.LoadKw, "price_aud_mwh": b.PriceAudMwh} {
		if len(rows) < 1 {
			return fmt.Errorf("%s needs at least one scenario", name)
		}
		for _, row := range rows {
			if len(row) != n {
				return fmt.Errorf("%s must have shape (n_scenarios, %d)", name, n)
			}
		}
	}
	if b.Weights != nil {
		sum := 0.0
		for _, w := range b.Weights {
			if w < 0 {
				return errors.New("weights must be one non-negative number per scenario")
			}
			sum += w
		}
		if len(b.Weights) != len(b.PvKw) || sum <= 0 {
			return errors.New("weights must be one non-negative number per scenario")
		}
	}
	return nil
}

// NormalizedWeights are the scenario weights, summing to one.
func (b ScenarioBatch) NormalizedWeights() []float64 {
	n := len(b.PvKw)
	out := make([]float64, n)
	if b.Weights == nil {
		for i := range out {
			out[i] = 1.0 / float64(n)
		}
		return out
	}
	sum := 0.0
	for _, w := range b.Weights {
		sum += w
	}
	for i, w := range b.Weights {
		out[i] = w / sum
	}
	return out
}

// PlanFirstAction is the first action of a receding plan.
//
// The objective, summed over the horizon and averaged over the scenarios, is
// spot cash - degradation x AC throughput + clip bonus x clip avoided, plus
// terminalAudPerKwh x the energy left above the SOC floor at the end. The
// tariff is accepted so callers stay stable; its rates are not scored.
func PlanFirstAction(
	socKwh float64,
	sc ScenarioBatch,
	spec house.HouseSpec,
	tariff house.Tariff,
	stageClock []int,
	clipBonusAudPerKwh, terminalAudPerKwh float64,
) (sim.Action, error) {
	_ = tariff
	steps, err := valueSteps(stageClock)
	if err != nil {
		return "", err
	}
	end := steps[len(steps)-1][1]
	if len(sc.PvKw[0]) < end {
		return "", fmt.Errorf("scenarios cover %d steps, clock needs %d", len(sc.PvKw[0]), end)
	}

	battery := spec.Battery
	grid := linspace(battery.SocMinKwh(), battery.SocMaxKwh(), split.SocBins)
	continuation := terminalValues(grid, spec, terminalAudPerKwh)

	for k := len(steps) - 1; k >= 1; k-- {
		continuation, err = stageValues(steps[k][0], steps[k][1], continuation, grid, sc, spec, clipBonusAudPerKwh)
		if err != nil {
			return "", err
		}
	}

	start, stop := steps[0][0], steps[0][1]
	ranked := rankedActions(stepPrice(sc, start))
	scores := make([]float64, len(ranked))
	for i, action := range ranked {
		s, err := meanScore(start, stop, action, []float64{socKwh}, continuation, grid, sc, spec, clipBonusAudPerKwh)
		if err != nil {
			return "", err
		}
		scores[i] = s[0]
	}
	return ranked[firstWithin(scores)], nil
}

func stepPrice(sc ScenarioBatch, start int) float64 {
	weights := sc.NormalizedWeights()
	total := 0.0
	for s, w := range weights {
		total += w * sc.PriceAudMwh[s][start]
	}
	return total
}

// firstWithin is the first action within sameCashAud of the best score.
func firstWithin(scores []float64) int {
	best := math.Inf(-1)
	for _, s := range scores {
		best = math.Max(best, s)
	}
	for i, s := range scores {
		if s >= best-sameCashAud {
			return i
		}
	}
	return 0
}

// valueSteps is one decision per 5-minute interval across the horizon the
// coarse clock spans. A later hour valued as a single held action fills the
// battery in one lump, so the continuation is solved interval by interval.
func valueSteps(stageClock []int) ([][2]int, error) {
	end, err := horizonEnd(stageClock)
	if err != nil {
		return nil, err
	}
	steps := make([][2]int, end)
	for t := range steps {
		steps[t] = [2]int{t, t + 1}
	}
	return steps, nil
}

func horizonEnd(stageClock []int) (int, error) {
	if len(stageClock) == 0 || stageClock[0] != 0 {
		return 0, errors.New("stage clock must start at offset 0")
	}
	end := 0
	for i, start := range stageClock {
		var stop int
		if i+1 < len(stageClock) {
			stop = stageClock[i+1]
		} else if start < split.StepsPerHour {
			stop = start + 1
		} else {
			stop = start + split.StepsPerHour
		}
		if stop <= start {
			return 0, errors.New("stage clock offsets must increase")
		}
		end = stop
	}
	return end, nil
}

func terminalValues(grid []float64, spec house.HouseSpec, terminalAudPerKwh float64) []float64 {
	// Unsold energy is not cash, so the default mark is 0.
	marks := make([]float64, len(grid))
	if terminalAudPerKwh != 0 {
		leg := spec.Battery.LegEfficiency()
		for i, soc := range grid {
			usable := math.Max(soc-spec.Battery.SocMinKwh(), 0.0) * leg
			marks[i] = marks[i] + terminalAudPerKwh*usable
		}
	}
	return marks
}

func stageValues(start, stop int, continuation, grid []float64, sc ScenarioBatch, spec house.HouseSpec, clipBonus float64) ([]float64, error) {
	// Exact best score inside the horizon. The cash-now tie is applied only to
	// the step that runs; applying it here sells the battery before a later spike.
	best := make([]float64, len(grid))
	for i := range best {
		best[i] = math.Inf(-1)
	}
	for _, action := range horizonActions {
		scores, err := meanScore(start, stop, action, grid, continuation, grid, sc, spec, clipBonus)
		if err != nil {
			return nil, err
		}
		for i, s := range scores {
			if s > best[i] {
				best[i] = s
			}
		}
	}
	return best, nil
}

func meanScore(
	start, stop int,
	action sim.Action,
	soc0, continuation, grid []float64,
	sc ScenarioBatch,
	spec house.HouseSpec,
	clipBonus float64,
) ([]float64, error) {
	weights := sc.NormalizedWeights()
	total := make([]float64, len(soc0))
	part := make([]float64, len(soc0))
	endSoc := make([]float64, len(soc0))
	for s := range sc.PvKw {
		prices := sc.PriceAudMwh[s]
		if clipBonus != 0 {
			for i, soc := range soc0 {
				p, e, err := roll(soc, action, start, stop, sc.PvKw[s], sc.LoadKw[s], prices, spec, clipBonus)
				if err != nil {
					return nil, err
				}
				part[i], endSoc[i] = p, e
			}
		} else {
			rollVec(soc0, action, start, stop, sc.PvKw[s], sc.LoadKw[s], prices, spec, spec.DegradationAudPerKwh, endSoc, part)
		}
		for i := range total {
			total[i] += weights[s] * (part[i] + interp(endSoc[i], grid, continuation))
		}
	}
	return total, nil
}

// rollVec has the same physics as sim.Apply. Cash is spot on import and
// export, ex-GST, minus degradation. It writes the end state and the cash into
// state and cash.
func rollVec(
	soc []float64,
	action sim.Action,
	start, stop int,
	pv, load, priceAudMwh []float64,
	spec house.HouseSpec,
	degrade float64,
	state, cash []float64,
) {
	battery := spec.Battery
	leg := battery.LegEfficiency()
	capKwh := spec.ExportCapKw * sim.IntervalHours
	maxAc := battery.MaxPowerKw * sim.IntervalHours
	socMin, socMax := battery.SocMinKwh(), battery.SocMaxKwh()
	copy(state, soc)
	for i := range cash {
		cash[i] = 0
	}
	for t := start; t < stop; t++ {
		pvKwh := math.Max(pv[t], 0.0) * sim.IntervalHours
		loadKwh := math.Max(load[t], 0.0) * sim.IntervalHours
		pvToLoad := math.Min(pvKwh, loadKwh)
		residual := pvKwh - pvToLoad
		unmet := loadKwh - pvToLoad
		spot := priceAudMwh[t] / 1000.0
		for i := range state {
			chargeLimit := math.Min(maxAc, math.Max(0.0, (socMax-state[i])/leg))
			dischargeLimit := math.Min(maxAc, math.Max(0.0, (state[i]-socMin)*leg))
			chargeAc, dischargeAc, _, pvToExport, batteryToExport, gridImport :=
				dispatch(action, residual, unmet, chargeLimit, dischargeLimit, capKwh)
			gridExport := pvToExport + batteryToExport
			cash[i] += (gridExport - gridImport) * spot
			if degrade != 0 {
				cash[i] -= degrade * (chargeAc + dischargeAc)
			}
			if chargeAc > 0.0 {
				state[i] = state[i] + chargeAc*leg
			} else if dischargeAc > 0.0 {
				state[i] = state[i] - dischargeAc/leg
			}
		}
	}
}

// dispatch returns charge_ac, discharge_ac, pv_to_battery, pv_to_export,
// battery_to_export and grid_import for one action.
func dispatch(action sim.Action, residual, unmet, chargeLimit, dischargeLimit, capKwh float64) (float64, float64, float64, float64, float64, float64) {
	switch action {
	case sim.Hold:
		return 0, 0, 0, math.Min(residual, capKwh), 0, unmet
	case sim.ChargeSurplus:
		chargeAc := math.Min(residual, chargeLimit)
		return chargeAc, 0, chargeAc, math.Min(residual-chargeAc, capKwh), 0, unmet
	case sim.Charge:
		chargeAc := chargeLimit
		pvToBattery := math.Min(residual, chargeAc)
		gridImport := unmet + (chargeAc - pvToBattery)
		pvToExport := math.Min(math.Max(residual-pvToBattery, 0.0), capKwh)
		if gridImport > 1e-9 && pvToExport > 1e-9 {
			pvToExport = 0.0
		}
		return chargeAc, 0, pvToBattery, pvToExport, 0, gridImport
	case sim.DischargeLoad:
		dischargeAc := math.Min(unmet, dischargeLimit)
		return 0, dischargeAc, 0, math.Min(residual, capKwh), 0, unmet - dischargeAc
	case sim.Discharge:
		toLoad := math.Min(unmet, dischargeLimit)
		pvToExport := math.Min(residual, capKwh)
		room := capKwh - pvToExport
		batteryToExport := math.Min(dischargeLimit-toLoad, math.Max(0.0, room))
		dischargeAc := toLoad + batteryToExport
		gridImport := unmet - toLoad
		if gridImport > 1e-9 && pvToExport > 1e-9 {
			pvToExport = 0.0
		}
		return 0, dischargeAc, 0, pvToExport, batteryToExport, gridImport
	}
	panic(fmt.Sprintf("unknown action %q", action))
}

// interp is linear value between SOC bins. A 5-minute step is smaller than one bin.
func interp(soc float64, grid, continuation []float64) float64 {
	span := grid[1] - grid[0]
	x := (soc - grid[0]) / span
	x = math.Min(math.Max(x, 0.0), float64(len(grid)-1))
	i := int(math.Floor(x))
	if i > len(grid)-2 {
		i = len(grid) - 2
	}
	frac := x - float64(i)
	return continuation[i]*(1.0-frac) + continuation[i+1]*frac
}

// roll is the slow scalar path through sim.Apply, used when a clip bonus is on.
func roll(soc float64, action sim.Action, start, stop int, pv, load, priceAudMwh []float64, spec house.HouseSpec, clipBonus float64) (float64, float64, error) {
	score := 0.0
	degrade := spec.DegradationAudPerKwh
	for t := start; t < stop; t++ {
		step, err := sim.Apply(soc, action, pv[t], load[t], spec)
		if err != nil {
			return 0, 0, err
		}
		spot := priceAudMwh[t] / 1000.0
		score += (step.GridExportKwh - step.GridImportKwh) * spot
		if degrade != 0 {
			score -= degrade * (step.BatteryChargeAcKwh + step.BatteryDischargeAcKwh)
		}
		if clipBonus != 0 {
			held, err := sim.Apply(soc, sim.Hold, pv[t], load[t], spec)
			if err != nil {
				return 0, 0, err
			}
			if saved := held.ClippedKwh - step.ClippedKwh; saved > 0.0 {
				score += clipBonus * saved
			}
		}
		soc = step.SocKwh
	}
	return score, soc, nil
}

// linspace matches numpy.linspace: evenly spaced, with the last point exact.
func linspace(start, stop float64, n int) []float64 {
	out := make([]float64, n)
	if n == 1 {
		out[0] = start
		return out
	}
	step := (stop - start) / float64(n-1)
	for i := 0; i < n-1; i++ {
		out[i] = float64(i)*step + start
	}
	out[n-1] = stop
	return out
}

// SocGrid is the SOC grid the planner values.
func SocGrid(spec house.HouseSpec) []float64 {
	return linspace(spec.Battery.SocMinKwh(), spec.Battery.SocMaxKwh(), split.SocBins)
}
