// Package house describes the household: sizes, tariffs and the synthetic
// solar and load shapes. It mirrors app/houses/.
package house

import (
	"errors"
	"math"
)

// BatterySpec is a battery's size and limits. Energy is kWh, power is kW.
type BatterySpec struct {
	CapacityKwh         float64
	MaxPowerKw          float64
	RoundTripEfficiency float64
	SocMinFrac          float64
	SocMaxFrac          float64
	InitialSocFrac      float64
}

// DefaultBattery is the 10 kWh, 5 kW battery the models were built around.
func DefaultBattery() BatterySpec {
	return BatterySpec{
		CapacityKwh:         10.0,
		MaxPowerKw:          5.0,
		RoundTripEfficiency: 0.90,
		SocMinFrac:          0.10,
		SocMaxFrac:          0.90,
		InitialSocFrac:      0.50,
	}
}

func (b BatterySpec) Validate() error {
	switch {
	case b.CapacityKwh <= 0 || b.MaxPowerKw <= 0:
		return errors.New("battery capacity and power must be positive")
	case !(0 <= b.SocMinFrac && b.SocMinFrac < b.SocMaxFrac && b.SocMaxFrac <= 1):
		return errors.New("SOC band must satisfy 0 <= min < max <= 1")
	case !(0 < b.RoundTripEfficiency && b.RoundTripEfficiency <= 1):
		return errors.New("round-trip efficiency must be in (0, 1]")
	case !(b.SocMinFrac <= b.InitialSocFrac && b.InitialSocFrac <= b.SocMaxFrac):
		return errors.New("initial SOC must lie inside the band")
	}
	return nil
}

// LegEfficiency is the efficiency of one direction, charge or discharge.
func (b BatterySpec) LegEfficiency() float64 { return math.Sqrt(b.RoundTripEfficiency) }

func (b BatterySpec) UsableHours() float64 {
	return b.CapacityKwh * (b.SocMaxFrac - b.SocMinFrac) / b.MaxPowerKw
}

func (b BatterySpec) SocMinKwh() float64 { return b.CapacityKwh * b.SocMinFrac }
func (b BatterySpec) SocMaxKwh() float64 { return b.CapacityKwh * b.SocMaxFrac }

// InitialSocKwh is where a run starts.
func (b BatterySpec) InitialSocKwh() float64 { return b.CapacityKwh * b.InitialSocFrac }

// HouseSpec is one household.
type HouseSpec struct {
	PvKwAc               float64
	ExportCapKw          float64
	Battery              BatterySpec
	LoadArchetype        string
	TariffID             string
	DailyLoadKwh         float64
	DegradationAudPerKwh float64
}

// DefaultHouse is the 10.5 kW solar, 15 kWh/day house used by the examples.
func DefaultHouse() HouseSpec {
	return HouseSpec{
		PvKwAc:        10.5,
		ExportCapKw:   5.0,
		Battery:       DefaultBattery(),
		LoadArchetype: "evening_peak",
		TariffID:      "tou_fit",
		DailyLoadKwh:  15.0,
	}
}

func (h HouseSpec) Validate() error {
	if err := h.Battery.Validate(); err != nil {
		return err
	}
	if h.PvKwAc < 0 || h.ExportCapKw < 0 || h.DailyLoadKwh < 0 {
		return errors.New("PV, export cap, and daily load must be non-negative")
	}
	if h.DegradationAudPerKwh < 0 {
		return errors.New("degradation cost must be non-negative")
	}
	return nil
}
