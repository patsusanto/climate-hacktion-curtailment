// Package policy holds the rule-based baselines. It mirrors
// app/policies/self_consumption.py and tariff_clock.py.
package policy

import (
	"time"

	"climate-hacktion-curtailment/backend/internal/model/house"
	"climate-hacktion-curtailment/backend/internal/model/sim"
)

// ChooseSelfConsumption charges from surplus PV and discharges only into load.
func ChooseSelfConsumption(pvKw, loadKw, socKwh float64, spec house.HouseSpec) sim.Action {
	battery := spec.Battery
	if pvKw > loadKw && socKwh < battery.SocMaxKwh() {
		return sim.ChargeSurplus
	}
	if loadKw > pvKw && socKwh > battery.SocMinKwh() {
		return sim.DischargeLoad
	}
	return sim.Hold
}

// Fixed clock baseline. Night is [22:00, 24:00) and [00:00, 07:00). Peak is
// [16:00, 21:00).
const (
	nightStartHour    = 22
	nightEndHour      = 7
	peakStartHour     = 16
	peakEndHour       = 21
	gridChargeSocFrac = 0.5
)

// ChooseTariffClock grid-charges at night, discharges into load at peak, and
// otherwise follows self-consumption.
func ChooseTariffClock(pvKw, loadKw, socKwh float64, ts time.Time, spec house.HouseSpec) sim.Action {
	hour := ts.Hour()
	battery := spec.Battery
	night := hour >= nightStartHour || hour < nightEndHour
	peak := peakStartHour <= hour && hour < peakEndHour
	if night {
		if socKwh/battery.CapacityKwh < gridChargeSocFrac {
			return sim.Charge
		}
		if pvKw > loadKw {
			return sim.ChargeSurplus
		}
		return sim.Hold
	}
	if peak && loadKw > pvKw && socKwh > battery.SocMinKwh() {
		return sim.DischargeLoad
	}
	return ChooseSelfConsumption(pvKw, loadKw, socKwh, spec)
}
