package simulate

import (
	"fmt"
	"math"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/battery"
	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/planner"
)

const smallKWh = 5e-4 // below this an interval's energy is noise (0.006 kW)

// reason says in plain words why the battery did what it did in step s, from the plan it came
// from and the prices that plan saw.
func reason(s Step, plan planner.Plan, h planner.Horizon, now time.Time, spec battery.Spec) string {
	st := s.Planner
	p := s.Prices
	if st.Clipped > smallKWh && p.Export < 0 && st.GridExport <= smallKWh {
		return fmt.Sprintf("Clipping %.1f kW of solar: exporting would cost %s right now.", st.Clipped*12, cents(-p.Export))
	}
	if s.Fallback {
		return selfReason(st, p) + " The forecasts showed no gain worth the battery wear."
	}
	when, worth, ok := nextUse(plan, h, now)
	switch st.Action {
	case battery.Charge:
		if ok {
			return fmt.Sprintf("Buying at %s to charge, for %s, when power is forecast at %s.", cents(p.Import), when, cents(worth))
		}
		return fmt.Sprintf("Buying at %s to charge while it is cheap.", cents(p.Import))
	case battery.ChargeSurplus:
		if ok {
			return fmt.Sprintf("Storing spare solar (exporting earns %s) to use %s, when power is forecast at %s.", cents(p.Export), when, cents(worth))
		}
		return fmt.Sprintf("Storing spare solar: exporting earns only %s.", cents(p.Export))
	case battery.Discharge:
		if ok && worth < p.Export {
			return fmt.Sprintf("Selling stored energy at %s, more than the %s it is forecast to be worth %s.", cents(p.Export), cents(worth), when)
		}
		return fmt.Sprintf("Selling stored energy at %s, the best price in the plan.", cents(p.Export))
	case battery.DischargeLoad:
		return fmt.Sprintf("Running the house from the battery instead of buying at %s.", cents(p.Import))
	}
	// Hold.
	switch {
	case s.SOCBefore >= spec.SOCMax()-0.01 && st.GridExport > smallKWh:
		return fmt.Sprintf("Battery full; exporting spare solar at %s.", cents(p.Export))
	case st.Clipped > smallKWh:
		return fmt.Sprintf("Battery full and the export cap reached; %.1f kW of solar clipped.", st.Clipped*12)
	case s.SOCBefore <= spec.SOCMin()+0.01:
		if when, ok := nextCharge(plan, now); ok {
			return fmt.Sprintf("Battery at its floor; buying at %s and planning to recharge %s.", cents(p.Import), when)
		}
		return fmt.Sprintf("Battery at its floor; buying at %s.", cents(p.Import))
	case ok:
		return fmt.Sprintf("Saving the charge for %s, when power is forecast at %s (now %s).", when, cents(worth), cents(p.Import))
	}
	return "Holding: the price differences ahead do not cover the battery's wear."
}

func selfReason(st battery.Step, p battery.Prices) string {
	switch {
	case st.ChargeAC > smallKWh:
		return "Storing spare solar (self-consumption)."
	case st.DischargeAC > smallKWh:
		return fmt.Sprintf("Running the house from the battery instead of buying at %s (self-consumption).", cents(p.Import))
	}
	return "Idle (self-consumption)."
}

// nextUse finds where the plan uses the energy it holds: the planned discharge worth most, and
// when it is. A discharge into the house is worth the import price it avoids; one sent to the
// grid is worth the export price.
func nextUse(plan planner.Plan, h planner.Horizon, now time.Time) (when string, worth float64, ok bool) {
	best := -1
	for j := 1; j < len(plan.Battery); j++ {
		d := -plan.Battery[j]
		if d <= smallKWh {
			continue
		}
		v := value(h, j, d)
		if best < 0 || v > worth {
			best, worth = j, v
		}
	}
	if best < 0 {
		return "", 0, false
	}
	return at(now, plan.Start[best]), worth, true
}

func value(h planner.Horizon, j int, d float64) float64 {
	if (h.Load[j]-h.PV[j])*battery.IntervalHours*float64(h.Intervals[j]) >= d {
		return h.Price[j].Import
	}
	return h.Price[j].Export
}

// nextCharge is when the plan next charges.
func nextCharge(plan planner.Plan, now time.Time) (string, bool) {
	for j := 1; j < len(plan.Battery); j++ {
		if plan.Battery[j] > smallKWh {
			return at(now, plan.Start[j]), true
		}
	}
	return "", false
}

// at is the time a horizon step starts, given its start in intervals from now (step 0 is the
// interval ending at now), e.g. "at 6:30 pm" or "tomorrow at 7:00 am".
func at(now time.Time, start int) string {
	t := now.Add(time.Duration(5*(start-1)) * time.Minute).In(data.NEM)
	today := now.Add(-5 * time.Minute).In(data.NEM)
	day := ""
	if t.YearDay() != today.YearDay() {
		day = "tomorrow "
	}
	return day + "at " + t.Format("3:04 pm")
}

// cents formats $/kWh as cents per kWh.
func cents(aud float64) string {
	c := aud * 100
	if math.Abs(c) < 10 {
		return fmt.Sprintf("%.1fc/kWh", c)
	}
	return fmt.Sprintf("%.0fc/kWh", c)
}
