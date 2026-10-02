// Package features computes the inputs the two models were trained on, column by column, on
// the data's 5-minute clock. Ported line for line from the training code.
//
// Timing: row i is the interval ending at T; its decision is made at the interval's start.
// The interval's dispatch price is published then, but its PV and load are not, so PV/load
// features read up to T - 5 min, and only pre-dispatch runs published by T - 5 min are used.
package features

import (
	"fmt"
	"math"
	"sort"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/house"
)

const (
	Day         = 288
	Week        = 2016
	maxRunAgeS  = 60 * 60
	horizonSecs = 8 * 3600
)

// Leads are the forecast horizons in 5-minute steps (1, 2, 3, 6 and 8 hours).
var Leads = []int{12, 24, 36, 72, 96}

var (
	lags         = []int{1, 3, 6, 12, 36, 72, 288}
	windows      = []int{12, 288}
	priceWeather = []string{"fc_sydney_temperature_2m", "fc_sydney_apparent_temperature", "fc_sydney_shortwave_radiation", "fc_sydney_cloud_cover", "fc_dubbo_shortwave_radiation", "fc_dubbo_cloud_cover", "fc_goulburn_wind_speed_100m"}
	unitWeather  = []string{"fc_sydney_shortwave_radiation", "fc_sydney_cloud_cover", "fc_sydney_temperature_2m"}
	nan          = math.NaN()
)

// Table is a set of named columns over the data's intervals.
type Table map[string][]float64

// Rows returns the columns in order for intervals [from, to].
func (t Table) Rows(columns []string, from, to int) ([][]float64, error) {
	cols := make([][]float64, len(columns))
	for j, c := range columns {
		v, ok := t[c]
		if !ok {
			return nil, fmt.Errorf("features: no column %q", c)
		}
		cols[j] = v
	}
	out := make([][]float64, 0, to-from+1)
	for i := from; i <= to; i++ {
		row := make([]float64, len(columns))
		for j := range columns {
			row[j] = cols[j][i]
		}
		out = append(out, row)
	}
	return out, nil
}

func shift(v []float64, k int) []float64 {
	out := make([]float64, len(v))
	for i := range out {
		if j := i - k; j >= 0 && j < len(v) {
			out[i] = v[j]
		} else {
			out[i] = nan
		}
	}
	return out
}

// rolling mean and sample std over the last w values; NaN unless all w are present.
func rolling(v []float64, w int, std bool) []float64 {
	out := make([]float64, len(v))
	for i := range out {
		out[i] = nan
		if i+1 < w {
			continue
		}
		sum, ok := 0.0, true
		for _, x := range v[i+1-w : i+1] {
			if math.IsNaN(x) {
				ok = false
				break
			}
			sum += x
		}
		if !ok {
			continue
		}
		mean := sum / float64(w)
		if !std {
			out[i] = mean
			continue
		}
		ss := 0.0
		for _, x := range v[i+1-w : i+1] {
			ss += (x - mean) * (x - mean)
		}
		out[i] = math.Sqrt(ss / float64(w-1))
	}
	return out
}

func clock(d *data.Data, t Table) {
	n := d.N()
	hs, hc := make([]float64, n), make([]float64, n)
	for i := 0; i < n; i++ {
		tm := d.Time(i)
		a := 2 * math.Pi * (float64(tm.Hour()) + float64(tm.Minute())/60) / 24
		hs[i], hc[i] = math.Sin(a), math.Cos(a)
	}
	t["hour_sin"], t["hour_cos"] = hs, hc
}

func weatherAt(d *data.Data, t Table, columns []string, offsetS int64, suffix string) {
	n := d.N()
	for _, c := range columns {
		v := make([]float64, n)
		for i := range v {
			v[i] = d.WeatherFC.Interp(c, d.T0+int64(i)*data.Step+offsetS, true)
		}
		t[c+suffix] = v
	}
}

// Price returns the price model's columns for one lead.
func Price(d *data.Data, lead int) Table {
	n := d.N()
	p := d.Price
	t := Table{"price_now": p}
	for _, l := range lags {
		t[fmt.Sprintf("price_lag_%d", l)] = shift(p, l)
	}
	for _, w := range windows {
		t[fmt.Sprintf("roll_mean_%d", w)] = rolling(p, w, false)
		t[fmt.Sprintf("roll_std_%d", w)] = rolling(p, w, true)
	}
	clock(d, t)
	ds, dc := make([]float64, n), make([]float64, n)
	for i := 0; i < n; i++ {
		a := 2 * math.Pi * float64((int(d.Time(i).Weekday())+6)%7) / 7 // Monday = 0
		ds[i], dc[i] = math.Sin(a), math.Cos(a)
	}
	t["dow_sin"], t["dow_cos"] = ds, dc
	t["same_time_yday"] = shift(p, Day-lead)
	t["same_time_lastweek"] = shift(p, Week-lead)
	a, b, c := shift(p, Day-lead-1), shift(p, Day-lead), shift(p, Day-lead+1)
	mean3 := make([]float64, n)
	for i := range mean3 {
		mean3[i] = (a[i] + b[i] + c[i]) / 3
	}
	t["same_time_yday_mean3"] = mean3
	weatherAt(d, t, priceWeather, int64(lead)*data.Step, "")
	predispatch(d, t, lead+1)
	return t
}

// predispatch: AEMO's latest run published by each decision time (no older than an hour),
// read at the target interval and over the next 8 hours.
func predispatch(d *data.Data, t Table, leadSteps int) {
	n := d.N()
	names := []string{"pd_rrp_target", "pd_demand_target", "pd_rrp_max_8h", "pd_rrp_mean_8h", "pd_age_min"}
	cols := make([][]float64, len(names))
	for j := range cols {
		cols[j] = make([]float64, n)
		for i := range cols[j] {
			cols[j][i] = nan
		}
		t[names[j]] = cols[j]
	}
	pd := d.PD
	if pd == nil || len(pd.Published) == 0 {
		return
	}
	var runs []int64 // unique publication times
	var starts []int // first row of each run
	for i, p := range pd.Published {
		if i == 0 || p != pd.Published[i-1] {
			runs = append(runs, p)
			starts = append(starts, i)
		}
	}
	starts = append(starts, len(pd.Published))
	for i := 0; i < n; i++ {
		decision := d.T0 + int64(i)*data.Step - data.Step
		r := sort.Search(len(runs), func(k int) bool { return runs[k] > decision }) - 1
		if r < 0 || decision-runs[r] > maxRunAgeS {
			continue
		}
		ends := pd.End[starts[r]:starts[r+1]]
		rrp := pd.RRP[starts[r]:starts[r+1]]
		demand := pd.Demand[starts[r]:starts[r+1]]
		target := decision + int64(leadSteps)*data.Step
		if k := sort.Search(len(ends), func(k int) bool { return ends[k] >= target }); k < len(ends) {
			cols[0][i], cols[1][i] = rrp[k], demand[k]
		}
		lo := sort.Search(len(ends), func(k int) bool { return ends[k] > decision })
		hi := sort.Search(len(ends), func(k int) bool { return ends[k] > decision+horizonSecs })
		if hi > lo {
			mx, sum := math.Inf(-1), 0.0
			for _, v := range rrp[lo:hi] {
				mx = math.Max(mx, v)
				sum += v
			}
			cols[2][i], cols[3][i] = mx, sum/float64(hi-lo)
		}
		cols[4][i] = float64(decision-runs[r]) / 60
	}
}

func meanOf(vs ...float64) float64 {
	s := 0.0
	for _, v := range vs {
		s += v
	}
	return s / float64(len(vs))
}

// Units returns the solar/demand model's columns. pv and load are the house's
// (kW per kW of panel, and kW at 15 kWh/day) on the data's clock.
func Units(d *data.Data, pv, load []float64) Table {
	n := d.N()
	t := Table{}
	clock(d, t)
	ys, yc := make([]float64, n), make([]float64, n)
	for i := 0; i < n; i++ {
		a := 2 * math.Pi * float64(d.Time(i).YearDay()-1) / 365
		ys[i], yc[i] = math.Sin(a), math.Cos(a)
	}
	t["doy_sin"], t["doy_cos"] = ys, yc
	knownPV, knownLoad := shift(pv, 1), shift(load, 1)
	for _, s := range []struct {
		name string
		v    []float64
	}{{"pv", knownPV}, {"load", knownLoad}} {
		t[s.name+"_now"] = s.v
		for _, l := range lags {
			t[fmt.Sprintf("%s_lag_%d", s.name, l)] = shift(s.v, l)
		}
		for _, w := range windows {
			t[fmt.Sprintf("%s_roll_mean_%d", s.name, w)] = rolling(s.v, w, false)
		}
	}
	for _, lead := range Leads {
		t[fmt.Sprintf("pv_yday_%d", lead)] = shift(pv, Day-lead)
		t[fmt.Sprintf("load_yday_%d", lead)] = shift(load, Day-lead)
	}

	// Today's clear-sky index so far, and the median of the last 7 days' means.
	ratio := make([]float64, n)
	dayOf := make([]int, n) // day number, consecutive from 0
	for i := 0; i < n; i++ {
		clearNow := house.ClearSky(d.Time(i).Add(-5 * time.Minute))
		ratio[i] = nan
		if clearNow > 0.05 {
			ratio[i] = knownPV[i] / math.Max(clearNow, 1e-6)
		}
		if i > 0 {
			dayOf[i] = dayOf[i-1]
			if d.Time(i).YearDay() != d.Time(i-1).YearDay() {
				dayOf[i]++
			}
		}
	}
	days := dayOf[n-1] + 1
	daySum, dayCount := make([]float64, days), make([]int, days)
	csiToday := make([]float64, n)
	for i := 0; i < n; i++ {
		k := dayOf[i]
		if !math.IsNaN(ratio[i]) {
			daySum[k] += ratio[i]
			dayCount[k]++
		}
		csiToday[i] = -1
		if dayCount[k] > 0 {
			csiToday[i] = daySum[k] / float64(dayCount[k])
		}
	}
	recentByDay := make([]float64, days)
	for k := 0; k < days; k++ {
		var window []float64
		for j := k - 7; j < k; j++ { // shift(1), then the last 7 values
			if j >= 0 && dayCount[j] > 0 {
				window = append(window, daySum[j]/float64(dayCount[j]))
			}
		}
		recentByDay[k] = median(window)
	}
	recent := make([]float64, n)
	for i := range recent {
		recent[i] = recentByDay[dayOf[i]]
	}

	todNow := make([]float64, n)
	for i := range todNow {
		vs := make([]float64, 7)
		for k := 1; k <= 7; k++ {
			vs[k-1] = at(knownLoad, i-Day*k)
		}
		todNow[i] = meanOf(vs...)
	}
	for _, lead := range Leads {
		clearT, sameDay, tod, gap := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
		for i := 0; i < n; i++ {
			now := d.Time(i)
			target := now.Add(time.Duration(5*lead) * time.Minute)
			clearT[i] = house.ClearSky(target)
			if target.YearDay() == now.YearDay() {
				sameDay[i] = 1
			}
			vs := make([]float64, 7)
			for k := 1; k <= 7; k++ {
				vs[k-1] = at(knownLoad, i-(Day*k-lead))
			}
			tod[i] = meanOf(vs...)
			gap[i] = knownLoad[i] - todNow[i]
		}
		t[fmt.Sprintf("clear_target_%d", lead)] = clearT
		t[fmt.Sprintf("same_day_%d", lead)] = sameDay
		t[fmt.Sprintf("csi_today_%d", lead)] = csiToday
		t[fmt.Sprintf("csi_recent_%d", lead)] = recent
		t[fmt.Sprintf("load_tod7_target_%d", lead)] = tod
		t[fmt.Sprintf("load_gap_now_%d", lead)] = gap
		weatherAt(d, t, unitWeather, int64(lead)*data.Step, fmt.Sprintf("_%d", lead))
	}
	return t
}

func at(v []float64, i int) float64 {
	if i < 0 || i >= len(v) {
		return nan
	}
	return v[i]
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return -1 // the training code fills a missing recent index with -1
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 1 {
		return s[m]
	}
	return (s[m-1] + s[m]) / 2
}
