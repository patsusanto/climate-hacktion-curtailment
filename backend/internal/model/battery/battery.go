// Package battery is the house and battery: sizes, one 5-minute interval of physics, and the bill.
package battery

import (
	"fmt"
	"math"
	"time"
)

// IntervalHours is one 5-minute interval in hours.
const IntervalHours = 5.0 / 60.0

const tol = 1e-9

// SupplyAUDPerDay is the daily supply charge (100 c/day plus GST).
const SupplyAUDPerDay = 1.10

// Spec is the house: roof, export limit, battery and demand.
type Spec struct {
	PVkWAC         float64 // inverter AC rating; the PV forecast is per kW of it
	ExportCapKW    float64
	DailyLoadKWh   float64
	WearAUDPerKWh  float64 // battery wear per kWh of AC throughput
	CapacityKWh    float64
	MaxPowerKW     float64
	RoundTripEff   float64
	SOCMinFrac     float64
	SOCMaxFrac     float64
	InitialSOCFrac float64
}

// NewSpec is a house with the training defaults for the battery's efficiency and SOC band.
func NewSpec(pvKW, batteryKWh, batteryKW, exportCapKW, dailyLoadKWh, wear float64) (Spec, error) {
	s := Spec{PVkWAC: pvKW, ExportCapKW: exportCapKW, DailyLoadKWh: dailyLoadKWh, WearAUDPerKWh: wear,
		CapacityKWh: batteryKWh, MaxPowerKW: batteryKW, RoundTripEff: 0.90, SOCMinFrac: 0.10, SOCMaxFrac: 0.90, InitialSOCFrac: 0.50}
	switch {
	case batteryKWh <= 0 || batteryKW <= 0:
		return s, fmt.Errorf("battery capacity and power must be positive")
	case pvKW < 0 || exportCapKW < 0 || dailyLoadKWh < 0:
		return s, fmt.Errorf("PV, export cap, and daily load must be non-negative")
	case wear < 0:
		return s, fmt.Errorf("degradation cost must be non-negative")
	}
	return s, nil
}

func (s Spec) Leg() float64        { return math.Sqrt(s.RoundTripEff) }
func (s Spec) SOCMin() float64     { return s.CapacityKWh * s.SOCMinFrac }
func (s Spec) SOCMax() float64     { return s.CapacityKWh * s.SOCMaxFrac }
func (s Spec) InitialSOC() float64 { return s.CapacityKWh * s.InitialSOCFrac }
func (s Spec) Usable() float64     { return s.SOCMax() - s.SOCMin() }

// Action is what the battery did in an interval.
type Action string

const (
	Hold          Action = "hold"
	ChargeSurplus Action = "charge_surplus"
	Charge        Action = "charge"
	DischargeLoad Action = "discharge_load"
	Discharge     Action = "discharge"
)

// Step is one settled interval (kWh).
type Step struct {
	Action      Action
	SOC         float64 // after the interval
	PVAvail     float64
	PVToLoad    float64
	PVToBattery float64
	PVToExport  float64
	Clipped     float64
	GridImport  float64
	GridExport  float64
	ChargeAC    float64
	DischargeAC float64
}

// Apply settles one interval under one of the five rule-based actions.
func Apply(soc float64, action Action, pvKW, loadKW float64, spec Spec) (Step, error) {
	leg := spec.Leg()
	pv := math.Max(pvKW, 0) * IntervalHours
	load := math.Max(loadKW, 0) * IntervalHours
	cap := spec.ExportCapKW * IntervalHours
	maxAC := spec.MaxPowerKW * IntervalHours
	chargeLimit := math.Min(maxAC, math.Max(0, (spec.SOCMax()-soc)/leg))
	dischargeLimit := math.Min(maxAC, math.Max(0, (soc-spec.SOCMin())*leg))

	pvToLoad := math.Min(pv, load)
	residual := pv - pvToLoad
	unmet := load - pvToLoad
	var pvToBattery, chargeAC, dischargeAC, pvToExport, batteryToExport, gridImport float64
	switch action {
	case Hold:
		pvToExport = math.Min(residual, cap)
		gridImport = unmet
	case ChargeSurplus:
		chargeAC = math.Min(residual, chargeLimit)
		pvToBattery = chargeAC
		pvToExport = math.Min(residual-chargeAC, cap)
		gridImport = unmet
	case Charge:
		chargeAC = chargeLimit
		pvToBattery = math.Min(residual, chargeAC)
		gridImport = unmet + (chargeAC - pvToBattery)
		pvToExport = math.Min(residual-pvToBattery, cap)
	case DischargeLoad:
		dischargeAC = math.Min(unmet, dischargeLimit)
		pvToExport = math.Min(residual, cap)
		gridImport = unmet - dischargeAC
	case Discharge:
		toLoad := math.Min(unmet, dischargeLimit)
		pvToExport = math.Min(residual, cap)
		room := cap - pvToExport
		batteryToExport = math.Min(dischargeLimit-toLoad, math.Max(0, room))
		dischargeAC = toLoad + batteryToExport
		gridImport = unmet - toLoad
	default:
		return Step{}, fmt.Errorf("unknown action %q", action)
	}
	// Importing and exporting in one step would double-count the bus.
	if gridImport > tol && pvToExport > tol {
		pvToExport = 0
	}
	newSOC := soc
	if chargeAC > 0 {
		newSOC = soc + chargeAC*leg
	} else if dischargeAC > 0 {
		newSOC = soc - dischargeAC/leg
	}
	s := Step{Action: action, SOC: newSOC, PVAvail: pv, PVToLoad: pvToLoad, PVToBattery: pvToBattery, PVToExport: pvToExport,
		Clipped: pv - pvToLoad - pvToBattery - pvToExport, GridImport: gridImport, GridExport: pvToExport + batteryToExport,
		ChargeAC: chargeAC, DischargeAC: dischargeAC}
	return s, check(s, spec, load)
}

// Flows is a planned interval (kWh), as the planner chose it.
type Flows struct {
	PVToLoad, PVToBattery, PVToExport, PVClipped float64
	GridToLoad, GridToBattery                    float64
	BatteryToLoad, BatteryToExport               float64
}

// ApplyFlows settles one interval from a plan.
//
// The plan is read as intent, not copied: the battery's net AC power (charge minus discharge,
// so a plan that does both nets out), whether it meant to buy from the grid to charge, and its
// total export (PV plus battery). These are fitted to the measured PV and load, the battery's
// power and SOC limits and the export cap. PV is clipped only where the plan exported less than
// it could, which it chooses at a negative price, or where the cap and battery force it.
func ApplyFlows(soc float64, f Flows, pvKW, loadKW float64, spec Spec) (Step, error) {
	leg := spec.Leg()
	pv := math.Max(pvKW, 0) * IntervalHours
	load := math.Max(loadKW, 0) * IntervalHours
	cap := spec.ExportCapKW * IntervalHours
	maxAC := spec.MaxPowerKW * IntervalHours
	chargeRoom := math.Min(maxAC, math.Max(0, (spec.SOCMax()-soc)/leg))
	dischargeRoom := math.Min(maxAC, math.Max(0, (soc-spec.SOCMin())*leg))

	pvToLoad := math.Min(pv, load)
	residual := pv - pvToLoad
	unmet := load - pvToLoad

	plannedCharge := math.Max(f.PVToBattery, 0) + math.Max(f.GridToBattery, 0)
	plannedDischarge := math.Max(f.BatteryToLoad, 0) + math.Max(f.BatteryToExport, 0)
	net := plannedCharge - plannedDischarge
	exportTarget := math.Max(f.PVToExport, 0) + math.Max(f.BatteryToExport, 0)
	gridChargeOK := f.GridToBattery > tol

	var pvToBattery, gridToBattery, batteryToLoad, batteryToExport float64
	if net > tol {
		charge := math.Min(net, chargeRoom)
		pvToBattery = math.Min(charge, residual)
		if gridChargeOK {
			gridToBattery = charge - pvToBattery
		}
	} else if net < -tol {
		discharge := math.Min(-net, dischargeRoom)
		batteryToLoad = math.Min(discharge, unmet)
		batteryToExport = math.Min(discharge-batteryToLoad, cap)
	}
	pvRoom := residual - pvToBattery
	pvToExport := math.Min(pvRoom, math.Min(math.Max(cap-batteryToExport, 0), math.Max(exportTarget-batteryToExport, 0)))
	gridToLoad := unmet - batteryToLoad
	if gridToBattery > tol && pvToExport > tol { // store the PV rather than export it and buy
		move := math.Min(gridToBattery, pvToExport)
		gridToBattery -= move
		pvToBattery += move
		pvToExport -= move
	}
	if gridToLoad+gridToBattery > tol && pvToExport+batteryToExport > tol {
		pvToExport, batteryToExport = 0, 0
	}
	chargeAC := pvToBattery + gridToBattery
	dischargeAC := batteryToLoad + batteryToExport
	action, newSOC := Hold, soc
	if chargeAC > tol {
		action, newSOC = ChargeSurplus, soc+chargeAC*leg
		if gridToBattery > tol {
			action = Charge
		}
	} else if dischargeAC > tol {
		action, newSOC = DischargeLoad, soc-dischargeAC/leg
		if batteryToExport > tol {
			action = Discharge
		}
	}
	s := Step{Action: action, SOC: math.Min(math.Max(newSOC, spec.SOCMin()), spec.SOCMax()), PVAvail: pv, PVToLoad: pvToLoad,
		PVToBattery: pvToBattery, PVToExport: pvToExport, Clipped: pv - pvToLoad - pvToBattery - pvToExport,
		GridImport: gridToLoad + gridToBattery, GridExport: pvToExport + batteryToExport, ChargeAC: chargeAC, DischargeAC: dischargeAC}
	return s, check(s, spec, load)
}

func check(s Step, spec Spec, load float64) error {
	if math.Abs(s.PVToLoad+s.PVToBattery+s.PVToExport+s.Clipped-s.PVAvail) > tol {
		return fmt.Errorf("PV does not balance")
	}
	if s.GridImport > tol && s.GridExport > tol {
		return fmt.Errorf("import and export in the same step")
	}
	if s.GridExport-spec.ExportCapKW*IntervalHours > tol {
		return fmt.Errorf("export exceeds the cap")
	}
	if s.SOC < spec.SOCMin()-tol || s.SOC > spec.SOCMax()+tol {
		return fmt.Errorf("SOC %g left the band", s.SOC)
	}
	if s.Clipped < -tol {
		return fmt.Errorf("clipped energy went negative")
	}
	lhs := s.PVToLoad + s.PVToBattery + s.PVToExport + s.GridImport + s.DischargeAC
	if math.Abs(lhs-(load+s.ChargeAC+s.GridExport)) > tol {
		return fmt.Errorf("AC bus does not balance")
	}
	return nil
}

// Cash is the interval's spot energy cost in $ (negative: the house was paid).
func (s Step) Cash(priceAUDMWh float64) float64 {
	return (s.GridImport - s.GridExport) * priceAUDMWh / 1000
}

// Bill is the total over a run.
type Bill struct {
	BillAUD, EnergyCashAUD                     float64
	PVAvail, PVToLoad, PVToBattery, PVToExport float64
	Clipped, GridImport, GridExport            float64
	ChargeAC, DischargeAC                      float64
}

// Throughput is the battery's AC charge plus discharge (kWh).
func (b Bill) Throughput() float64 { return b.ChargeAC + b.DischargeAC }

// Meter adds up settled intervals into a bill: spot energy, the daily supply charge, and wear.
type Meter struct {
	spec Spec
	bill Bill
	days map[string]bool
}

func NewMeter(spec Spec) *Meter { return &Meter{spec: spec, days: map[string]bool{}} }

// Record adds the interval ending at t.
func (m *Meter) Record(t time.Time, s Step, priceAUDMWh float64) {
	b := &m.bill
	b.EnergyCashAUD += s.Cash(priceAUDMWh)
	b.PVAvail += s.PVAvail
	b.PVToLoad += s.PVToLoad
	b.PVToBattery += s.PVToBattery
	b.PVToExport += s.PVToExport
	b.Clipped += s.Clipped
	b.GridImport += s.GridImport
	b.GridExport += s.GridExport
	b.ChargeAC += s.ChargeAC
	b.DischargeAC += s.DischargeAC
	m.days[t.Format("2006-01-02")] = true
}

// Finish returns the bill so far.
func (m *Meter) Finish() Bill {
	b := m.bill
	b.BillAUD = b.EnergyCashAUD + float64(len(m.days))*SupplyAUDPerDay + b.Throughput()*m.spec.WearAUDPerKWh
	return b
}
