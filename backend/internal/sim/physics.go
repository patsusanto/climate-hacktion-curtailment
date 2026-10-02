// Package sim settles a household's energy one 5-minute step at a time and
// prices the result. It mirrors app/env/.
package sim

import (
	"fmt"
	"math"

	"climate-hacktion-curtailment/backend/internal/house"
)

// Action is what the battery is told to do for one step.
type Action string

const (
	Hold          Action = "hold"
	ChargeSurplus Action = "charge_surplus"
	Charge        Action = "charge"
	DischargeLoad Action = "discharge_load"
	Discharge     Action = "discharge"
)

// IntervalHours is one 5-minute step in hours.
const IntervalHours = 5.0 / 60.0

const tol = 1e-9

// StepResult is one settled interval. Energies are kWh.
type StepResult struct {
	Action                Action
	SocKwh                float64
	PvAvailKwh            float64
	PvToLoadKwh           float64
	PvToBatteryKwh        float64
	PvToExportKwh         float64
	ClippedKwh            float64
	GridImportKwh         float64
	GridExportKwh         float64
	BatteryChargeAcKwh    float64
	BatteryDischargeAcKwh float64
}

// Apply settles one interval. It is a pure function of the state of charge,
// the action, PV, load and the house. A violated energy balance is an error.
func Apply(socKwh float64, action Action, pvKw, loadKw float64, spec house.HouseSpec) (StepResult, error) {
	battery := spec.Battery
	leg := battery.LegEfficiency()
	pvKwh := math.Max(pvKw, 0.0) * IntervalHours
	loadKwh := math.Max(loadKw, 0.0) * IntervalHours
	capKwh := spec.ExportCapKw * IntervalHours
	maxAc := battery.MaxPowerKw * IntervalHours
	chargeLimit := math.Min(maxAc, math.Max(0.0, (battery.SocMaxKwh()-socKwh)/leg))
	dischargeLimit := math.Min(maxAc, math.Max(0.0, (socKwh-battery.SocMinKwh())*leg))

	pvToLoad := math.Min(pvKwh, loadKwh)
	residualPv := pvKwh - pvToLoad
	unmet := loadKwh - pvToLoad

	var pvToBattery, chargeAc, dischargeAc, pvToExport, batteryToExport, gridImport float64

	switch action {
	case Hold:
		pvToExport = math.Min(residualPv, capKwh)
		gridImport = unmet
	case ChargeSurplus:
		chargeAc = math.Min(residualPv, chargeLimit)
		pvToBattery = chargeAc
		pvToExport = math.Min(residualPv-chargeAc, capKwh)
		gridImport = unmet
	case Charge:
		chargeAc = chargeLimit
		pvToBattery = math.Min(residualPv, chargeAc)
		gridImport = unmet + (chargeAc - pvToBattery)
		pvToExport = math.Min(residualPv-pvToBattery, capKwh)
	case DischargeLoad:
		dischargeAc = math.Min(unmet, dischargeLimit)
		pvToExport = math.Min(residualPv, capKwh)
		gridImport = unmet - dischargeAc
	case Discharge:
		toLoad := math.Min(unmet, dischargeLimit)
		pvToExport = math.Min(residualPv, capKwh)
		room := capKwh - pvToExport
		batteryToExport = math.Min(dischargeLimit-toLoad, math.Max(0.0, room))
		dischargeAc = toLoad + batteryToExport
		gridImport = unmet - toLoad
	default:
		return StepResult{}, fmt.Errorf("unknown action %q", action)
	}

	// Importing and exporting in one step would double-count the bus.
	// Charging from the grid leaves no PV to export; if float dust remains, drop it.
	if gridImport > tol && pvToExport > tol {
		pvToExport = 0.0
	}

	clipped := pvKwh - pvToLoad - pvToBattery - pvToExport
	gridExport := pvToExport + batteryToExport

	newSoc := socKwh
	if chargeAc > 0.0 {
		newSoc = socKwh + chargeAc*leg
	} else if dischargeAc > 0.0 {
		newSoc = socKwh - dischargeAc/leg
	}

	result := StepResult{
		Action:                action,
		SocKwh:                newSoc,
		PvAvailKwh:            pvKwh,
		PvToLoadKwh:           pvToLoad,
		PvToBatteryKwh:        pvToBattery,
		PvToExportKwh:         pvToExport,
		ClippedKwh:            clipped,
		GridImportKwh:         gridImport,
		GridExportKwh:         gridExport,
		BatteryChargeAcKwh:    chargeAc,
		BatteryDischargeAcKwh: dischargeAc,
	}
	if err := checkInvariants(result, spec, loadKwh); err != nil {
		return StepResult{}, err
	}
	return result, nil
}

func checkInvariants(s StepResult, spec house.HouseSpec, loadKwh float64) error {
	battery := spec.Battery
	pvParts := s.PvToLoadKwh + s.PvToBatteryKwh + s.PvToExportKwh + s.ClippedKwh
	if math.Abs(pvParts-s.PvAvailKwh) > tol {
		return fmt.Errorf("PV does not balance: %v != %v", pvParts, s.PvAvailKwh)
	}
	if s.GridImportKwh > tol && s.GridExportKwh > tol {
		return fmt.Errorf("import and export in the same step")
	}
	capKwh := spec.ExportCapKw * IntervalHours
	if s.GridExportKwh-capKwh > tol {
		return fmt.Errorf("export exceeds the cap")
	}
	if s.SocKwh < battery.SocMinKwh()-tol || s.SocKwh > battery.SocMaxKwh()+tol {
		return fmt.Errorf("SOC %v left the band", s.SocKwh)
	}
	if s.ClippedKwh < -tol {
		return fmt.Errorf("clipped energy went negative")
	}
	lhs := s.PvToLoadKwh + s.PvToBatteryKwh + s.PvToExportKwh + s.GridImportKwh + s.BatteryDischargeAcKwh
	rhs := loadKwh + s.BatteryChargeAcKwh + s.GridExportKwh
	if math.Abs(lhs-rhs) > tol {
		return fmt.Errorf("AC bus does not balance: %v != %v", lhs, rhs)
	}
	return nil
}
