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

// Spec is the house: roof, export limit, battery and demand.
type Spec struct {
	PVkWAC         float64 // inverter AC rating; the PV forecast is per kW of it
	ExportCapKW    float64
	DailyLoadKWh   float64
	WearAUDPerKWh  float64 // battery wear per kWh of AC throughput: what the planner weighs cycling against; not on the bill
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

// Flows is one interval's energy routing (kWh).
type Flows struct {
	PVToLoad, PVToBattery, PVToExport, PVClipped float64
	GridToLoad, GridToBattery                    float64
	BatteryToLoad, BatteryToExport               float64
}

// Clip is how solar may be clipped.
type Clip string

const (
	// ClipEconomic clips solar whenever exporting it would cost money.
	ClipEconomic Clip = "economic"
	// ClipForcedOnly clips only what the export cap forces.
	ClipForcedOnly Clip = "forced_only"
)

// GridNet is the best net grid energy (import positive, kWh) for an interval when the battery
// moves b kWh (AC, + charge), and what it costs. Solar can be clipped, so the net can be anything
// from load+b-pv (use all the solar) up to load+b (clip it all), but never below -cap.
func GridNet(b, pv, load, cap float64, p Prices, clip Clip) (net, cost float64) {
	lo, hi := math.Max(load+b-pv, -cap), load+b
	switch {
	case clip == ClipForcedOnly || lo >= hi:
		net = lo
	case p.Import < 0: // paid to import: clip everything
		net = hi
	case p.Export < 0: // exporting costs: clip down to zero export
		net = math.Min(math.Max(0, lo), hi)
	default:
		net = lo
	}
	if net >= 0 {
		return net, p.Import * net
	}
	return net, p.Export * net
}

// Route spells out an interval's flows when the battery moves b kWh (AC, + charge) and the grid
// net is net (from GridNet). Solar goes to the house first, then the battery, then export.
func Route(b, net, pv, load float64) Flows {
	used := math.Min(math.Max(load+b-net, 0), pv) // solar not clipped
	var f Flows
	f.PVClipped = pv - used
	f.PVToLoad = math.Min(used, load)
	rest := used - f.PVToLoad
	if b >= 0 {
		f.PVToBattery = math.Min(b, rest)
		f.GridToBattery = b - f.PVToBattery
		f.PVToExport = rest - f.PVToBattery
		f.GridToLoad = load - f.PVToLoad
		return f
	}
	d := -b
	f.BatteryToLoad = math.Min(d, load-f.PVToLoad)
	f.BatteryToExport = d - f.BatteryToLoad
	f.PVToExport = rest
	f.GridToLoad = load - f.PVToLoad - f.BatteryToLoad
	return f
}

// Settle carries out one interval of a plan against what the interval actually brought.
//
// The battery follows the plan's net power (charge minus discharge), within its limits. It buys
// from the grid to charge only if the plan meant to, and sends stored energy to the grid only if
// the plan meant to; otherwise it charges from spare solar and discharges into the house. Solar
// is then clipped or exported by the interval's actual prices, as an inverter on a spot plan does,
// and any solar that would be clipped goes into the battery if it has room.
func Settle(soc float64, plan Flows, pvKW, loadKW float64, p Prices, spec Spec, clip Clip) (Step, error) {
	leg := spec.Leg()
	pv := math.Max(pvKW, 0) * IntervalHours
	load := math.Max(loadKW, 0) * IntervalHours
	cap := spec.ExportCapKW * IntervalHours
	maxAC := spec.MaxPowerKW * IntervalHours
	b := plan.PVToBattery + plan.GridToBattery - plan.BatteryToLoad - plan.BatteryToExport
	switch {
	case b > tol:
		b = math.Min(b, math.Min(maxAC, math.Max(0, (spec.SOCMax()-soc)/leg)))
		if plan.GridToBattery <= tol {
			b = math.Min(b, math.Max(pv-load, 0))
		}
	case b < -tol:
		b = -math.Min(-b, math.Min(maxAC, math.Max(0, (soc-spec.SOCMin())*leg)))
		if plan.BatteryToExport <= tol {
			b = -math.Min(-b, math.Max(load-pv, 0))
		}
		b = -math.Min(-b, load+cap) // what the house and the export cap can take
	default:
		b = 0
	}
	net, _ := GridNet(b, pv, load, cap, p, clip)
	f := Route(b, net, pv, load)
	// Solar that would be clipped is free energy: store what the battery has room for, whatever
	// the plan said (its solar forecast may simply have been low).
	if room := math.Min(maxAC, math.Max(0, (spec.SOCMax()-soc)/leg)) - b; f.PVClipped > tol && b >= 0 && room > tol {
		b += math.Min(f.PVClipped, room)
		net, _ = GridNet(b, pv, load, cap, p, clip)
		f = Route(b, net, pv, load)
	}
	if f.GridToBattery > tol && f.PVToExport > tol { // never buy to charge while exporting solar
		move := math.Min(f.GridToBattery, f.PVToExport)
		f.GridToBattery -= move
		f.PVToBattery += move
		f.PVToExport -= move
	}
	chargeAC := f.PVToBattery + f.GridToBattery
	dischargeAC := f.BatteryToLoad + f.BatteryToExport
	action, newSOC := Hold, soc
	if chargeAC > tol {
		action, newSOC = ChargeSurplus, soc+chargeAC*leg
		if f.GridToBattery > tol {
			action = Charge
		}
	} else if dischargeAC > tol {
		action, newSOC = DischargeLoad, soc-dischargeAC/leg
		if f.BatteryToExport > tol {
			action = Discharge
		}
	}
	s := Step{Action: action, SOC: math.Min(math.Max(newSOC, spec.SOCMin()), spec.SOCMax()), PVAvail: pv, PVToLoad: f.PVToLoad,
		PVToBattery: f.PVToBattery, PVToExport: f.PVToExport, Clipped: f.PVClipped,
		GridImport: f.GridToLoad + f.GridToBattery, GridExport: f.PVToExport + f.BatteryToExport, ChargeAC: chargeAC, DischargeAC: dischargeAC}
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

// Cash is the interval's energy cost in $ at its prices (negative: the house was paid).
func (s Step) Cash(p Prices) float64 {
	return s.GridImport*p.Import - s.GridExport*p.Export
}

// Bill is the total over a run.
type Bill struct {
	BillAUD, EnergyCashAUD                     float64
	PVAvail, PVToLoad, PVToBattery, PVToExport float64
	Clipped, GridImport, GridExport            float64
	ChargeAC, DischargeAC                      float64
	Days                                       int
}

// Throughput is the battery's AC charge plus discharge (kWh).
func (b Bill) Throughput() float64 { return b.ChargeAC + b.DischargeAC }

// SupplyAUD is the daily charges over the run.
func (b Bill) SupplyAUD() float64 { return b.BillAUD - b.EnergyCashAUD }

// Meter adds up settled intervals into a bill: energy at the tariff's prices plus its daily
// supply charge. Battery wear is not on the bill; it is reported separately.
type Meter struct {
	tariff Tariff
	bill   Bill
	days   map[string]bool
}

func NewMeter(tf Tariff) *Meter { return &Meter{tariff: tf, days: map[string]bool{}} }

// Record adds the interval ending at t.
func (m *Meter) Record(t time.Time, s Step, p Prices) {
	b := &m.bill
	b.EnergyCashAUD += s.Cash(p)
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
	b.Days = len(m.days)
	b.BillAUD = b.EnergyCashAUD + float64(b.Days)*m.tariff.SupplyAUDPerDay
	return b
}
