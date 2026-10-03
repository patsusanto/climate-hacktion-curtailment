// Package payback prices a solar and battery system and works out how fast its bill savings
// repay it.
//
// The costs are typical Australian installed prices in 2026 and are only defaults: the page lets
// the user put in a quote.
//   - Solar: about $0.83/W installed in NSW after the federal STC rebate (Solar Choice price
//     index, mid 2026); premium panels and inverters add 20-30%. $900 per kW is used.
//   - Battery: about $650-$1,100 per kWh installed before rebates; $800 per kWh is used.
//   - Cheaper Home Batteries rebate, May-Dec 2026: $272 per kWh of usable capacity for the first
//     14 kWh, 60% of that from 14 to 28 kWh, 15% from 28 to 50 kWh, nothing above.
package payback

import "math"

const (
	SolarAUDPerKW    = 900.0
	BatteryAUDPerKWh = 800.0
	RebateAUDPerKWh  = 272.0
	// Sources says where the defaults come from, for the page.
	Sources = "Typical 2026 installed prices: solar about $0.83/W in NSW after the STC rebate (Solar Choice price index); " +
		"batteries $650-$1,100/kWh before rebates. Federal Cheaper Home Batteries rebate at the May-Dec 2026 rate " +
		"($272/kWh for the first 14 kWh, 60% to 28 kWh, 15% to 50 kWh)."
)

// Rebate is the federal battery rebate for a battery of usable capacity kWh.
func Rebate(kWh float64) float64 {
	tiers := []struct{ upTo, share float64 }{{14, 1}, {28, 0.6}, {50, 0.15}}
	total, from := 0.0, 0.0
	for _, t := range tiers {
		if kWh > from {
			total += (math.Min(kWh, t.upTo) - from) * t.share * RebateAUDPerKWh
		}
		from = t.upTo
	}
	return total
}

// Cost is the installed cost of the system after rebates: solar (kW) and battery (kWh).
func Cost(solarKW, batteryKWh float64) (total, rebate float64) {
	rebate = Rebate(batteryKWh)
	battery := math.Max(batteryKWh*BatteryAUDPerKWh-rebate, 0)
	return solarKW*SolarAUDPerKW + battery, rebate
}

// Years is how long savings of annual AUD a year take to repay cost, or 0 if they never do.
func Years(cost, annual float64) float64 {
	if annual <= 0 {
		return 0
	}
	return cost / annual
}
