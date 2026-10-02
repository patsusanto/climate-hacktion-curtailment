// Package house builds the house the models were trained on: a Sydney roof and household on
// real weather.
//
// PV: the hour's clear-sky index from observed irradiance, interpolated to 5 minutes,
// x the roof's clear-sky output x a 0.9 performance ratio x small 5-minute noise.
// Load: an evening-peak profile (15 kWh/day) scaled down, plus heating and cooling from the
// observed temperature, plus noise, with a 0.1 kW floor.
//
// The noise is the training house's seeded sequence (shipped in models/), indexed from
// 1 Oct 2024 00:05, so the same weather gives exactly the training house.
package house

import (
	"fmt"
	"math"
	"sort"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/data"
)

const (
	Lat, Lon            = -33.8688, 151.2093
	standardMeridianDeg = 150.0
	performanceRatio    = 0.9
	csiMax              = 1.1
	clearMinW           = 25.0
	heatKWPerC          = 0.06
	heatBelowC          = 16.0
	coolKWPerC          = 0.10
	coolAboveC          = 25.0
	baseloadKW          = 0.1
	// LoadUnitDailyKWh is the daily demand of the unit house; loads scale from it.
	LoadUnitDailyKWh = 15.0
)

var loadBlocks = []struct{ start, stop, share, hours float64 }{
	{0, 6, 0.10, 6}, {6, 9, 0.15, 3}, {9, 16, 0.20, 7}, {16, 21, 0.40, 5}, {21, 24, 0.15, 3},
}

func rad(deg float64) float64 { return deg * math.Pi / 180 }

// ClearSky is kW per kW of panel under a clear sky at t (NEM time); 0 overnight.
func ClearSky(t time.Time) float64 {
	t = t.In(data.NEM)
	day := float64(t.YearDay())
	decl := 23.45 * math.Sin(rad(360*(284+day)/365))
	b := rad(360 * (day - 81) / 365)
	eot := 9.87*math.Sin(2*b) - 7.53*math.Cos(b) - 1.5*math.Sin(b)
	minutes := float64(t.Hour())*60 + float64(t.Minute()) + float64(t.Second())/60
	hourAngle := rad(((minutes+4*(Lon-standardMeridianDeg)+eot)/60 - 12) * 15)
	lat, dec := rad(Lat), rad(decl)
	sinElev := math.Sin(lat)*math.Sin(dec) + math.Cos(lat)*math.Cos(dec)*math.Cos(hourAngle)
	elev := math.Asin(math.Max(-1, math.Min(1, sinElev))) * 180 / math.Pi
	return math.Max(0, math.Sin(rad(elev)))
}

func clearGHI(t time.Time) float64 {
	cz := ClearSky(t)
	if cz <= 0 {
		return 0
	}
	return 1098 * cz * math.Exp(-0.057/math.Max(cz, 1e-3))
}

// Noise is the house's 5-minute noise, position 0 being the interval ending at Origin.
type Noise struct {
	Origin   int64
	PV, Load []float64
}

// Build returns pv (kW per kW of panel) and load (kW at 15 kWh/day) on d's price clock.
func Build(d *data.Data, loadScale float64, noise *Noise) (pv, load []float64, err error) {
	n := d.N()
	pos0 := (d.T0 - noise.Origin) / data.Step
	if pos0 < 0 || int(pos0)+n > len(noise.PV) || int(pos0)+n > len(noise.Load) {
		return nil, nil, fmt.Errorf("house: defined from %s for %d intervals; data is outside that",
			time.Unix(noise.Origin, 0).In(data.NEM).Format(time.RFC3339), len(noise.PV))
	}
	csi := clearSkyIndex(d)
	pv = make([]float64, n)
	load = make([]float64, n)
	for i := 0; i < n; i++ {
		t := d.Time(i)
		k := int(pos0) + i
		pv[i] = math.Max(0, performanceRatio*ClearSky(t)*csi[i]*(1+noise.PV[k]))
		load[i] = math.Max(baseloadKW, loadBase(t)*loadScale+hvac(d, t)+noise.Load[k])
	}
	return pv, load, nil
}

// clearSkyIndex: observed GHI over clear-sky GHI per hour (the mean of the hour's 5-minute
// clear-sky values), placed at mid-hour, filled within each day, interpolated to 5 minutes.
func clearSkyIndex(d *data.Data) []float64 {
	n := d.N()
	var hours []int64
	sum := map[int64]float64{}
	count := map[int64]int{}
	for i := 0; i < n; i++ {
		u := d.T0 + int64(i)*data.Step
		h := (u + 3599) / 3600 * 3600 // ceil to the hour (NEM hours are whole UTC hours)
		if count[h] == 0 {
			hours = append(hours, h)
		}
		sum[h] += clearGHI(time.Unix(u, 0))
		count[h]++
	}
	observed := map[int64]float64{}
	if ghi, ok := d.Observed.Cols["ghi"]; ok {
		for j, t := range d.Observed.T {
			observed[t] = ghi[j]
		}
	}
	mid := make([]int64, len(hours))
	csi := make([]float64, len(hours))
	for j, h := range hours {
		mid[j] = h - 1800
		clear := sum[h] / float64(count[h])
		g, ok := observed[h]
		csi[j] = math.NaN()
		if ok && clear > clearMinW && !math.IsNaN(g) {
			csi[j] = math.Max(0, math.Min(csiMax, g/clear))
		}
	}
	// Forward then backward fill within each NEM day; days with no daylight value get 1.
	for a := 0; a < len(mid); {
		b := a
		day := time.Unix(mid[a], 0).In(data.NEM).YearDay()
		for b < len(mid) && time.Unix(mid[b], 0).In(data.NEM).YearDay() == day {
			b++
		}
		last := math.NaN()
		for j := a; j < b; j++ {
			if math.IsNaN(csi[j]) {
				csi[j] = last
			} else {
				last = csi[j]
			}
		}
		next := math.NaN()
		for j := b - 1; j >= a; j-- {
			if math.IsNaN(csi[j]) {
				csi[j] = next
			} else {
				next = csi[j]
			}
		}
		for j := a; j < b; j++ {
			if math.IsNaN(csi[j]) {
				csi[j] = 1
			}
		}
		a = b
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = interp(mid, csi, d.T0+int64(i)*data.Step)
	}
	return out
}

func interp(x []int64, y []float64, t int64) float64 {
	if t <= x[0] {
		return y[0]
	}
	if t >= x[len(x)-1] {
		return y[len(y)-1]
	}
	k := sort.Search(len(x), func(i int) bool { return x[i] >= t })
	if x[k] == t {
		return y[k]
	}
	w := float64(t-x[k-1]) / float64(x[k]-x[k-1])
	return y[k-1] + w*(y[k]-y[k-1])
}

func hvac(d *data.Data, t time.Time) float64 {
	temp := d.Observed.Interp("temperature", t.Unix(), false)
	h := t.In(data.NEM).Hour()
	out := 0.0
	if (h >= 6 && h < 9) || (h >= 16 && h < 23) {
		out += heatKWPerC * math.Max(0, heatBelowC-temp)
	}
	if h >= 12 && h < 22 {
		out += coolKWPerC * math.Max(0, temp-coolAboveC)
	}
	return out
}

func loadBase(t time.Time) float64 {
	h := float64(t.In(data.NEM).Hour())
	for _, b := range loadBlocks {
		if h >= b.start && h < b.stop {
			return b.share * LoadUnitDailyKWh / b.hours
		}
	}
	return 0
}
