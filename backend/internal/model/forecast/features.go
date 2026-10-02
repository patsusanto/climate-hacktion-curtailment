package forecast

import (
	"fmt"
	"math"
	"sort"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/house"
	"climate-hacktion-curtailment/backend/internal/model/planner"
	"climate-hacktion-curtailment/backend/internal/model/split"
)

// Features for the price and PV/load models, feature set v2. Each function
// computes the features of ONE row from a window of trailing rows, exactly as
// app/forecast/features.py would for the last row of that window. Nothing
// later than the row is read.
//
// The window matters: a few features (the per-day clear-sky index) depend on
// where it starts, so callers pass the same window the Python controller used,
// the trailing MaxLookbackV2+1 rows.

const (
	// MaxLookbackV2 is a week: last week's price at the target's time of day.
	MaxLookbackV2 = split.WeekSteps
	daySteps      = split.DaySteps
)

var (
	lags           = [...]int{1, 3, 6, 12, 36, 72, 288} // 5min .. 1 day
	rollingWindows = [...]int{12, 288}                  // 1h, 1 day
)

// WeatherColumns are the day-ahead forecast series, in the order the price
// models were trained with.
var WeatherColumns = []string{
	"fc_sydney_temperature_2m", "fc_sydney_apparent_temperature", "fc_sydney_shortwave_radiation", "fc_sydney_cloud_cover",
	"fc_dubbo_shortwave_radiation", "fc_dubbo_cloud_cover",
	"fc_goulburn_wind_speed_100m",
}

var nan = math.NaN()

// at returns f.col[i-k], or NaN when that row is before the window start.
func at(col []float64, lo, i, k int) float64 {
	j := i - k
	if j < lo {
		return nan
	}
	return col[j]
}

func rollingMean(col []float64, lo, i, window int) float64 {
	start := i - window + 1
	if start < lo {
		return nan
	}
	sum := 0.0
	for j := start; j <= i; j++ {
		sum += col[j]
	}
	return sum / float64(window)
}

// rollingStd is the sample standard deviation (ddof=1), as pandas computes it.
func rollingStd(col []float64, lo, i, window int) float64 {
	start := i - window + 1
	if start < lo {
		return nan
	}
	mean := rollingMean(col, lo, i, window)
	ss := 0.0
	for j := start; j <= i; j++ {
		d := col[j] - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(window-1))
}

func dayKey(t time.Time) int { return t.Year()*10000 + int(t.Month())*100 + t.Day() }

// todFrac is the fraction of the day, from the clock.
func todFrac(t time.Time) float64 { return (float64(t.Hour()) + float64(t.Minute())/60.0) / 24.0 }

func dayOfWeek(t time.Time) int { return (int(t.Weekday()) + 6) % 7 } // Monday = 0

// PriceFeatures builds the v2 price features for row i from rows lo..i, at the
// given lead (in 5-minute steps). The result maps column name to value.
func PriceFeatures(f *data.Frame, lo, i, lead int, w *data.Weather) (map[string]float64, error) {
	out := make(map[string]float64, 32)
	price := f.Price
	for _, lag := range lags {
		out[fmt.Sprintf("price_lag_%d", lag)] = at(price, lo, i, lag)
	}
	for _, win := range rollingWindows {
		out[fmt.Sprintf("roll_mean_%d", win)] = rollingMean(price, lo, i, win)
	}
	for _, win := range rollingWindows {
		out[fmt.Sprintf("roll_std_%d", win)] = rollingStd(price, lo, i, win)
	}
	frac := todFrac(f.Times[i])
	out["hour_sin"] = math.Sin(2 * math.Pi * frac)
	out["hour_cos"] = math.Cos(2 * math.Pi * frac)
	dow := float64(dayOfWeek(f.Times[i]))
	out["dow_sin"] = math.Sin(2 * math.Pi * dow / 7.0)
	out["dow_cos"] = math.Cos(2 * math.Pi * dow / 7.0)
	out["price_now"] = price[i]

	// The price at the target's time of day yesterday and last week.
	out["same_time_yday"] = at(price, lo, i, daySteps-lead)
	out["same_time_lastweek"] = at(price, lo, i, split.WeekSteps-lead)
	out["same_time_yday_mean3"] = (at(price, lo, i, daySteps-lead-1) +
		at(price, lo, i, daySteps-lead) +
		at(price, lo, i, daySteps-lead+1)) / 3.0

	if w != nil {
		target := f.Times[i].Add(time.Duration(split.StepMinutes*lead) * time.Minute)
		vals, err := WeatherAt(w, target)
		if err != nil {
			return nil, err
		}
		for c, name := range w.Columns {
			out[name] = vals[c]
		}
	}
	return out, nil
}

// WeatherAt is the hourly forecast linearly interpolated to one 5-minute
// target time. It errors if the target is outside the forecast, like
// weather.at_targets. Times are compared as float64 nanoseconds, as numpy does.
func WeatherAt(w *data.Weather, target time.Time) ([]float64, error) {
	n := len(w.Times)
	if n == 0 {
		return nil, fmt.Errorf("weather forecast is empty")
	}
	x := make([]float64, n)
	for i, t := range w.Times {
		x[i] = float64(t.UnixNano())
	}
	xt := float64(target.UnixNano())
	if xt < x[0] || xt > x[n-1] {
		return nil, fmt.Errorf("target time %s outside the weather forecast", target.Format(time.RFC3339))
	}
	out := make([]float64, len(w.Columns))
	for c := range w.Columns {
		out[c] = planner.NpInterp(xt, x, w.Values[c])
	}
	return out, nil
}

// unitEngineered are the per-lead features v2 adds to the unit inputs.
var unitEngineered = [...]string{"clear_target", "same_day", "csi_today", "csi_recent_median", "load_tod_mean7_target", "load_dev_now"}

// clearSky is the clear-sky output at a time, on the market clock.
func clearSky(t time.Time) float64 {
	return house.ClearSkyUnit(t.Hour(), t.Minute(), t.Second(), t.YearDay())
}

// UnitFeatures builds the v2 PV/load features for row i from rows lo..i. The
// result maps column name to value.
func UnitFeatures(f *data.Frame, lo, i int) map[string]float64 {
	out := make(map[string]float64, 64)
	frac := todFrac(f.Times[i])
	out["hour_sin"] = math.Sin(2 * math.Pi * frac)
	out["hour_cos"] = math.Cos(2 * math.Pi * frac)
	doy := float64(f.Times[i].YearDay()-1) / 365.0
	out["doy_sin"] = math.Sin(2 * math.Pi * doy)
	out["doy_cos"] = math.Cos(2 * math.Pi * doy)
	for name, col := range map[string][]float64{"pv_unit": f.PvUnit, "load_unit": f.LoadUnit} {
		for _, lag := range lags {
			out[fmt.Sprintf("%s_lag_%d", name, lag)] = at(col, lo, i, lag)
		}
		for _, win := range rollingWindows {
			out[fmt.Sprintf("%s_roll_mean_%d", name, win)] = rollingMean(col, lo, i, win)
		}
	}

	csiToday, csiRecent := clearSkyIndex(f, lo, i)
	today := dayKey(f.Times[i])
	// load at the same time of day over the last week, and its gap from now
	todNow := 0.0
	for k := 1; k <= 7; k++ {
		todNow += at(f.LoadUnit, lo, i, daySteps*k)
	}
	todNow /= 7.0

	for _, lead := range split.Leads {
		target := f.Times[i].Add(time.Duration(split.StepMinutes*lead) * time.Minute)
		out[fmt.Sprintf("clear_target_%d", lead)] = clearSky(target)
		same := 0.0
		if dayKey(target) == today {
			same = 1.0
		}
		out[fmt.Sprintf("same_day_%d", lead)] = same
		out[fmt.Sprintf("csi_today_%d", lead)] = csiToday
		out[fmt.Sprintf("csi_recent_median_%d", lead)] = csiRecent
		mean7 := 0.0
		for k := 1; k <= 7; k++ {
			mean7 += at(f.LoadUnit, lo, i, daySteps*k-lead)
		}
		out[fmt.Sprintf("load_tod_mean7_target_%d", lead)] = mean7 / 7.0
		out[fmt.Sprintf("load_dev_now_%d", lead)] = f.LoadUnit[i] - todNow
	}
	return out
}

// clearSkyIndex returns today's clear-sky index so far and the median of the
// previous days' means, for row i using rows lo..i. The index is PV over the
// clear-sky curve and is undefined before sunrise; missing values are -1.
func clearSkyIndex(f *data.Frame, lo, i int) (today, recentMedian float64) {
	ratio := func(j int) float64 {
		clear := clearSky(f.Times[j])
		if clear > 0.05 {
			return f.PvUnit[j] / math.Max(clear, 1e-6)
		}
		return nan
	}
	// the block of rows [s..i] on today's date
	s := i
	for s-1 >= lo && dayKey(f.Times[s-1]) == dayKey(f.Times[i]) {
		s--
	}
	today = meanSkipNaN(ratio, s, i)
	if math.IsNaN(today) {
		today = -1.0
	}

	// means of up to the seven days before today, newest first; the oldest day
	// is partial if the window starts part-way through it
	// A day with no daylight still takes up one of the seven slots of the
	// rolling window; only its value is skipped by the median.
	var means []float64
	end := s - 1
	for days := 0; end >= lo && days < 7; days++ {
		start := end
		for start-1 >= lo && dayKey(f.Times[start-1]) == dayKey(f.Times[end]) {
			start--
		}
		if m := meanSkipNaN(ratio, start, end); !math.IsNaN(m) {
			means = append(means, m)
		}
		end = start - 1
	}
	recentMedian = median(means)
	if math.IsNaN(recentMedian) {
		recentMedian = -1.0
	}
	return today, recentMedian
}

// meanSkipNaN is the mean of fn(j) for j in [from, to], ignoring NaN. It is
// NaN if every value is.
func meanSkipNaN(fn func(int) float64, from, to int) float64 {
	sum, n := 0.0, 0
	for j := from; j <= to; j++ {
		if v := fn(j); !math.IsNaN(v) {
			sum += v
			n++
		}
	}
	if n == 0 {
		return nan
	}
	return sum / float64(n)
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return nan
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	mid := len(v) / 2
	if len(v)%2 == 1 {
		return v[mid]
	}
	return (v[mid-1] + v[mid]) / 2.0
}

// Row picks columns out of a feature map in order. It returns false if any
// value is NaN or a column is missing, like the isna() checks in Python.
func Row(features map[string]float64, columns []string) ([]float64, bool) {
	row := make([]float64, len(columns))
	for i, name := range columns {
		v, ok := features[name]
		if !ok || math.IsNaN(v) {
			return nil, false
		}
		row[i] = v
	}
	return row, true
}
