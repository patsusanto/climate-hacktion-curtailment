package battery

import (
	"fmt"
	"time"
)

// Prices are what one kWh costs to import and earns when exported, in $/kWh, for one interval.
type Prices struct {
	Import, Export float64
}

// Tariff is a spot-exposed retail plan (like Amber's): imports pay the wholesale spot price plus
// the network's charge for that time of day, plus GST; exports earn the spot price.
//
// GST is charged on the network charge and on positive spot prices only, so importing never
// costs less than exporting earns. That keeps the planner's cost of a step convex.
type Tariff struct {
	Name             string
	PeakAUDPerKWh    float64 // network charge in the peak window, ex GST
	OffPeakAUDPerKWh float64 // network charge at all other times, ex GST
	PeakFromHour     int     // peak window, in NEM local hours [from, to)
	PeakToHour       int
	PeakMonths       [13]bool // months (1-12) that have a peak window
	PeakWeekdaysOnly bool     // weekends are off-peak
	GST              float64
	SupplyAUDPerDay  float64 // daily network, metering and retail charges, incl. GST
}

// Ausgrid is Ausgrid's residential time-of-use network tariff (EA025) from 1 July 2026 under a
// spot pass-through retailer: peak 32.52 c/kWh from 3 to 9 pm in summer (Nov-Mar) and winter
// (Jun-Aug), 5.36 c/kWh otherwise, ex GST. The retailer's own per-kWh fees and environmental
// charges are left out; the daily charge covers the network's 63.23 c/day plus metering and fees.
var Ausgrid = Tariff{
	Name:             "Ausgrid EA025 time-of-use network charges + NSW1 spot",
	PeakAUDPerKWh:    0.3252,
	OffPeakAUDPerKWh: 0.0536,
	PeakFromHour:     15,
	PeakToHour:       21,
	PeakMonths:       [13]bool{1: true, 2: true, 3: true, 6: true, 7: true, 8: true, 11: true, 12: true},
	GST:              0.10,
	SupplyAUDPerDay:  1.10,
}

// AusgridBusiness is Ausgrid's small-business time-of-use network tariff (EA225) from 1 July 2026
// under a spot pass-through retailer: peak 39.76 c/kWh from 3 to 9 pm on weekdays in summer
// (Nov-Mar) and winter (Jun-Aug), 5.90 c/kWh otherwise, ex GST. Larger sites pay demand charges
// (on their highest half-hour) as well; those are not modelled.
var AusgridBusiness = Tariff{
	Name:             "Ausgrid EA225 business time-of-use network charges + NSW1 spot",
	PeakAUDPerKWh:    0.3976,
	OffPeakAUDPerKWh: 0.0590,
	PeakFromHour:     15,
	PeakToHour:       21,
	PeakMonths:       [13]bool{1: true, 2: true, 3: true, 6: true, 7: true, 8: true, 11: true, 12: true},
	PeakWeekdaysOnly: true,
	GST:              0.10,
	SupplyAUDPerDay:  1.10,
}

// Wholesale is a generator's view: everything at the NSW1 spot price, no network charges, no
// supply charge. A solar farm with a battery earns this.
var Wholesale = Tariff{Name: "NSW1 spot price (wholesale)"}

// SpotOnly prices imports and exports at the spot price, with no network charges or GST: the
// tariff the models were evaluated on in training.
var SpotOnly = Tariff{Name: "NSW1 spot only", SupplyAUDPerDay: 1.10}

// Network is the network charge ($/kWh, ex GST) for the interval ending at t.
func (tf Tariff) Network(t time.Time) float64 {
	start := t.Add(-5 * time.Minute) // the interval's own hour
	h := start.Hour()
	weekend := start.Weekday() == time.Saturday || start.Weekday() == time.Sunday
	if tf.PeakMonths[int(start.Month())] && h >= tf.PeakFromHour && h < tf.PeakToHour && !(tf.PeakWeekdaysOnly && weekend) {
		return tf.PeakAUDPerKWh
	}
	return tf.OffPeakAUDPerKWh
}

// Peak says whether the interval ending at t is in the network's peak window.
func (tf Tariff) Peak(t time.Time) bool {
	return tf.PeakAUDPerKWh != tf.OffPeakAUDPerKWh && tf.Network(t) == tf.PeakAUDPerKWh
}

// At is the interval's prices for a spot price in $/MWh.
func (tf Tariff) At(t time.Time, spotAUDMWh float64) Prices {
	spot := spotAUDMWh / 1000
	imp := tf.Network(t) * (1 + tf.GST)
	if spot > 0 {
		imp += spot * (1 + tf.GST)
	} else {
		imp += spot
	}
	return Prices{Import: imp, Export: spot}
}

// Describe is a one-line summary for people.
func (tf Tariff) Describe() string {
	if tf.PeakAUDPerKWh == 0 && tf.OffPeakAUDPerKWh == 0 {
		return fmt.Sprintf("%s; $%.2f/day supply.", tf.Name, tf.SupplyAUDPerDay)
	}
	return fmt.Sprintf("Imports pay the NSW1 spot price plus Ausgrid's network charge (%.1f c/kWh 3-9 pm in summer and winter, "+
		"%.1f c/kWh otherwise) plus GST; exports earn the spot price; $%.2f/day supply.",
		tf.PeakAUDPerKWh*100*(1+tf.GST), tf.OffPeakAUDPerKWh*100*(1+tf.GST), tf.SupplyAUDPerDay)
}
