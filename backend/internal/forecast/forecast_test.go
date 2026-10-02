package forecast

import (
	"math"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/data"
	"climate-hacktion-curtailment/backend/internal/house"
	"climate-hacktion-curtailment/backend/internal/split"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol || math.IsNaN(got) != math.IsNaN(want) {
		t.Errorf("%s = %.12g, want %.12g", name, got, want)
	}
}

// ---- XGBoost ----

// Two stumps. Tree 0: f0 < 2 -> left leaf 1.0 else right leaf 3.0, missing goes
// left. Tree 1: f1 < 0.5 -> -0.5 else 0.5, missing goes right.
const xgbJSON = `{"learner":{"learner_model_param":{"base_score":"%s"},
 "gradient_booster":{"model":{"trees":[
  {"left_children":[1,-1,-1],"right_children":[2,-1,-1],"split_indices":[0,0,0],
   "split_conditions":[2.0,1.0,3.0],"default_left":[1,0,0]},
  {"left_children":[1,-1,-1],"right_children":[2,-1,-1],"split_indices":[1,0,0],
   "split_conditions":[0.5,-0.5,0.5],"default_left":[0,0,0]}
 ]}}}}`

func TestXGBPredict(t *testing.T) {
	for _, base := range []string{"5E-1", "[5E-1]"} {
		m, err := ParseXGB([]byte(sprintf(xgbJSON, base)))
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			row  []float32
			want float32
		}{
			{[]float32{1, 0}, 0.5 + 1.0 - 0.5},     // both go left
			{[]float32{2, 0}, 0.5 + 3.0 - 0.5},     // 2 < 2 is false: right
			{[]float32{1, 1}, 0.5 + 1.0 + 0.5},     // second tree right
			{[]float32{nan32, 0}, 0.5 + 1.0 - 0.5}, // NaN in tree 0 goes left
			{[]float32{5, nan32}, 0.5 + 3.0 + 0.5}, // NaN in tree 1 goes right
		}
		for _, c := range cases {
			if got := m.Predict(c.row); got != c.want {
				t.Errorf("base %s row %v: %v, want %v", base, c.row, got, c.want)
			}
		}
	}
}

var nan32 = float32(math.NaN())

func TestXGBRejectsBadModels(t *testing.T) {
	if _, err := ParseXGB([]byte(`{"learner":{"learner_model_param":{"base_score":"x"}}}`)); err == nil {
		t.Error("bad base score should fail")
	}
	if _, err := ParseXGB([]byte(sprintf(xgbJSON, "0.5")[:40])); err == nil {
		t.Error("truncated JSON should fail")
	}
	if _, err := ParseXGB([]byte(`{"learner":{"learner_model_param":{"base_score":"0.5"},"gradient_booster":{"model":{"trees":[]}}}}`)); err == nil {
		t.Error("no trees should fail")
	}
}

// ---- Unit model ----

func tinyNet() *UnitModel {
	// 2 inputs -> 2 hidden (ReLU) -> 2 outputs, with non-trivial scalers.
	return &UnitModel{
		InputDim: 2, OutputDim: 2,
		FeatureColumns: []string{"a", "b"},
		Layers: []layer{
			{W: [][]float64{{1, -1}, {0.5, 0.5}}, B: []float64{0, -1}},
			{W: [][]float64{{2, 1}, {-1, 3}}, B: []float64{0.25, 0}},
		},
		ScalerX: Scaler{Mean: []float64{10, 20}, Scale: []float64{2, 4}},
		ScalerY: Scaler{Mean: []float64{1, 2}, Scale: []float64{0.5, 10}},
	}
}

func TestUnitModelForwardByHand(t *testing.T) {
	m := tinyNet()
	if err := m.validate(); err != nil {
		t.Fatal(err)
	}
	// input (14, 28) -> scaled (2, 2) -> hidden (0, 1) ... compute by hand:
	// h0 = 1*2 + -1*2 + 0 = 0 -> ReLU 0; h1 = 0.5*2 + 0.5*2 - 1 = 1 -> 1
	// o0 = 2*0 + 1*1 + 0.25 = 1.25; o1 = -1*0 + 3*1 + 0 = 3 (no ReLU on the last layer)
	// unscale: 1.25*0.5 + 1 = 1.625 ; 3*10 + 2 = 32
	out := m.Forward([]float64{14, 28})
	near(t, "out0", out[0], 1.625, 1e-6)
	near(t, "out1", out[1], 32, 1e-6)

	// A negative hidden value is clipped by the ReLU, and a negative output is not.
	out = m.Forward([]float64{10, 28}) // scaled (0, 2): h0 = -2 -> 0, h1 = 0.5*0+0.5*2-1 = 0
	near(t, "relu out0", out[0], 0.25*0.5+1, 1e-6)
	out = m.Forward([]float64{14, 20})                    // scaled (2, 0): h0 = 2, h1 = 0
	near(t, "negative output", out[1], (-1*2)*10+2, 1e-6) // o1 = -2 -> -18
}

func TestUnitModelValidate(t *testing.T) {
	m := tinyNet()
	m.OutputDim = 3
	if m.validate() == nil {
		t.Error("output size mismatch should fail")
	}
	m = tinyNet()
	m.FeatureColumns = []string{"a"}
	if m.validate() == nil {
		t.Error("feature column count mismatch should fail")
	}
}

func TestScalerRoundTrip(t *testing.T) {
	s := Scaler{Mean: []float64{3, -1}, Scale: []float64{2, 0.5}}
	x := []float64{7, 2}
	back := s.InverseTransform(s.Transform(x))
	near(t, "0", back[0], 7, 1e-12)
	near(t, "1", back[1], 2, 1e-12)
}

// ---- Features ----

// frame builds n rows from start with deterministic wobbling series.
func frame(start time.Time, n int) *data.Frame {
	f := &data.Frame{}
	for i := 0; i < n; i++ {
		ts := start.Add(time.Duration(i*split.StepMinutes) * time.Minute)
		f.Times = append(f.Times, ts)
		f.Price = append(f.Price, 100+30*math.Sin(float64(i)/17.0)+float64(i%11))
		// PV follows the real clear-sky curve at 80% so the index is meaningful
		f.PvUnit = append(f.PvUnit, 0.8*clearSky(ts))
		f.LoadUnit = append(f.LoadUnit, 0.3+0.1*math.Sin(float64(i)/9.0))
	}
	return f
}

var t0 = time.Date(2026, 7, 1, 0, 0, 0, 0, split.NEM)

func TestPriceFeaturesByDefinition(t *testing.T) {
	f := frame(t0, 3000)
	i, lo, lead := 2500, 2500-MaxLookbackV2, 12
	feats, err := PriceFeatures(f, lo, i, lead, nil)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "price_now", feats["price_now"], f.Price[i], 0)
	near(t, "price_lag_1", feats["price_lag_1"], f.Price[i-1], 0)
	near(t, "price_lag_288", feats["price_lag_288"], f.Price[i-288], 0)

	sum := 0.0
	for j := i - 11; j <= i; j++ {
		sum += f.Price[j]
	}
	mean := sum / 12
	near(t, "roll_mean_12", feats["roll_mean_12"], mean, 1e-12)
	ss := 0.0
	for j := i - 11; j <= i; j++ {
		ss += (f.Price[j] - mean) * (f.Price[j] - mean)
	}
	near(t, "roll_std_12 uses ddof=1", feats["roll_std_12"], math.Sqrt(ss/11), 1e-12)

	// the price at the target's clock time yesterday and last week
	near(t, "same_time_yday", feats["same_time_yday"], f.Price[i-(288-lead)], 0)
	near(t, "same_time_lastweek", feats["same_time_lastweek"], f.Price[i-(2016-lead)], 0)
	near(t, "same_time_yday_mean3", feats["same_time_yday_mean3"],
		(f.Price[i-(288-lead-1)]+f.Price[i-(288-lead)]+f.Price[i-(288-lead+1)])/3, 1e-12)

	// 2500 steps after 2026-07-01 00:00 is 16:20 on 2026-07-09, a Thursday (dow 3)
	if got := f.Times[i]; got.Hour() != 16 || got.Minute() != 20 || got.Weekday() != time.Thursday {
		t.Fatalf("unexpected time %v", got)
	}
	frac := (16 + 20.0/60) / 24
	near(t, "hour_sin", feats["hour_sin"], math.Sin(2*math.Pi*frac), 1e-12)
	near(t, "dow_cos", feats["dow_cos"], math.Cos(2*math.Pi*3/7.0), 1e-12)
}

func TestFeaturesAreNaNWithoutHistory(t *testing.T) {
	f := frame(t0, 3000)
	// a window that starts 100 rows back cannot see a day of lags
	feats, _ := PriceFeatures(f, 2400, 2500, 12, nil)
	if !math.IsNaN(feats["price_lag_288"]) || !math.IsNaN(feats["roll_std_288"]) || !math.IsNaN(feats["same_time_yday"]) {
		t.Error("features that reach before the window must be NaN")
	}
	if math.IsNaN(feats["price_lag_72"]) {
		t.Error("lag 72 is inside the window")
	}
	if _, ok := Row(feats, []string{"price_now", "price_lag_288"}); ok {
		t.Error("Row must report a NaN as missing")
	}
	if _, ok := Row(feats, []string{"price_now", "nope"}); ok {
		t.Error("Row must report an unknown column as missing")
	}
}

func TestWeatherInterpolation(t *testing.T) {
	w := &data.Weather{
		Times:   []time.Time{t0, t0.Add(time.Hour), t0.Add(2 * time.Hour)},
		Columns: []string{"a", "b"},
		Values:  [][]float64{{0, 10, 14}, {5, 5, 9}},
	}
	got, err := WeatherAt(w, t0.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "a", got[0], 5, 1e-9)
	near(t, "b", got[1], 5, 1e-9)
	got, _ = WeatherAt(w, t0.Add(90*time.Minute))
	near(t, "a at 1h30", got[0], 12, 1e-9)
	near(t, "b at 1h30", got[1], 7, 1e-9)
	if _, err := WeatherAt(w, t0.Add(3*time.Hour)); err == nil {
		t.Error("a target after the forecast must be an error")
	}
	if _, err := WeatherAt(w, t0.Add(-time.Minute)); err == nil {
		t.Error("a target before the forecast must be an error")
	}
}

func TestUnitFeaturesByDefinition(t *testing.T) {
	f := frame(t0, 3000)
	i := 2500 // 16:20 on 9 July
	lo := i - MaxLookbackV2
	feats := UnitFeatures(f, lo, i)

	near(t, "pv_unit_lag_12", feats["pv_unit_lag_12"], f.PvUnit[i-12], 0)
	near(t, "load_unit_lag_288", feats["load_unit_lag_288"], f.LoadUnit[i-288], 0)
	sum := 0.0
	for j := i - 287; j <= i; j++ {
		sum += f.PvUnit[j]
	}
	near(t, "pv_unit_roll_mean_288", feats["pv_unit_roll_mean_288"], sum/288, 1e-12)

	// doy = (day of year - 1) / 365; 9 July is day 190
	near(t, "doy_sin", feats["doy_sin"], math.Sin(2*math.Pi*189/365.0), 1e-12)

	// clear-sky output at the target, and whether the target is still today
	for _, lead := range split.Leads {
		target := f.Times[i].Add(time.Duration(5*lead) * time.Minute)
		near(t, "clear_target", feats[name("clear_target", lead)], clearSky(target), 1e-12)
		same := 0.0
		if target.Day() == f.Times[i].Day() {
			same = 1
		}
		near(t, "same_day", feats[name("same_day", lead)], same, 0)
	}

	// load at the target's clock time over the last week, and the gap from now
	lead := 36
	mean7 := 0.0
	for k := 1; k <= 7; k++ {
		mean7 += f.LoadUnit[i-(288*k-lead)]
	}
	near(t, "load_tod_mean7_target", feats[name("load_tod_mean7_target", lead)], mean7/7, 1e-12)
	todNow := 0.0
	for k := 1; k <= 7; k++ {
		todNow += f.LoadUnit[i-288*k]
	}
	near(t, "load_dev_now", feats[name("load_dev_now", lead)], f.LoadUnit[i]-todNow/7, 1e-12)

	// PV is exactly 0.8 of clear sky, so every clear-sky index is 0.8
	near(t, "csi_today", feats[name("csi_today", 12)], 0.8, 1e-9)
	near(t, "csi_recent_median", feats[name("csi_recent_median", 12)], 0.8, 1e-9)
}

func name(base string, lead int) string { return sprintf("%s_%d", base, lead) }

func TestClearSkyIndexBeforeSunrise(t *testing.T) {
	f := frame(t0, 3000)
	i := 2304 + 12 // 01:00 on 9 July: the sun is down, so today has no index yet
	if f.Times[i].Hour() != 1 {
		t.Fatalf("expected 01:00, got %v", f.Times[i])
	}
	today, recent := clearSkyIndex(f, i-MaxLookbackV2, i)
	near(t, "csi_today before sunrise is -1", today, -1, 0)
	near(t, "recent still comes from earlier days", recent, 0.8, 1e-9)
}

func TestClearSkyIndexPartialOldestDay(t *testing.T) {
	// Make the oldest day in the window look different. When the window starts
	// mid-day, that day's mean covers only the rows after its start, and the
	// median of the seven previous days must still pick it up.
	f := frame(t0, 3000)
	i := 2500
	lo := i - MaxLookbackV2
	for j := lo; j < lo+50; j++ { // only the first 50 rows of the oldest day
		f.PvUnit[j] = 0.1 * clearSky(f.Times[j])
	}
	_, recent := clearSkyIndex(f, lo, i)
	// the oldest day's mean over its tail rows is 0.8 and the other six are 0.8
	near(t, "median unaffected", recent, 0.8, 1e-9)

	// now drag the whole partial oldest day down: seven values, one low, median stays 0.8
	for j := lo; j < lo+288; j++ {
		f.PvUnit[j] = 0.1 * clearSky(f.Times[j])
	}
	_, recent = clearSkyIndex(f, lo, i)
	near(t, "one low day of seven does not move the median", recent, 0.8, 1e-9)
	// two low days of seven still keep the median at the typical value
	for j := lo + 288; j < lo+576; j++ {
		f.PvUnit[j] = 0.1 * clearSky(f.Times[j])
	}
	_, recent = clearSkyIndex(f, lo, i)
	near(t, "two low days of seven", recent, 0.8, 1e-9)
}

func TestMedian(t *testing.T) {
	near(t, "odd", median([]float64{3, 1, 2}), 2, 0)
	near(t, "even", median([]float64{4, 1, 3, 2}), 2.5, 0)
	if !math.IsNaN(median(nil)) {
		t.Error("median of nothing is NaN")
	}
}

// ---- Forecast curve ----

// constTree is a one-leaf booster that always predicts v.
func constXGB(v float32) *XGB {
	return &XGB{Trees: []xgbTree{{
		left: []int32{-1}, right: []int32{-1}, feature: []int32{0},
		cond: []float32{v}, defaultLt: []bool{false},
	}}}
}

func testModels(t *testing.T, f *data.Frame) *Models {
	t.Helper()
	// price quantiles come out unordered on purpose: 300, 100, 200
	price := &PriceModel{
		Quantiles:      []float64{0.1, 0.5, 0.9},
		FeatureColumns: map[int][]string{},
		Models:         map[int][]*XGB{},
	}
	for _, lead := range split.Leads {
		price.FeatureColumns[lead] = []string{"price_now", "same_time_yday", "fc_a"}
		price.Models[lead] = []*XGB{constXGB(300), constXGB(100), constXGB(200)}
	}
	// a net whose scaled outputs are all zero, so outputs equal the scaler means
	n := len(split.Leads)
	net := &UnitModel{
		InputDim: 1, OutputDim: 2 * n,
		FeatureColumns: []string{"hour_sin"},
		Layers:         []layer{{W: make([][]float64, 2*n), B: make([]float64, 2*n)}},
		ScalerX:        Scaler{Mean: []float64{0}, Scale: []float64{1}},
		Residuals:      map[string]map[string]Band{"pv_unit": {}, "load_unit": {}},
	}
	for o := range net.Layers[0].W {
		net.Layers[0].W[o] = []float64{0}
	}
	for o := 0; o < 2*n; o++ {
		net.ScalerY.Mean = append(net.ScalerY.Mean, 0.5) // 0.5 units everywhere
		net.ScalerY.Scale = append(net.ScalerY.Scale, 1)
	}
	for _, lead := range split.Leads {
		key := sprintf("%d", lead)
		net.Residuals["pv_unit"][key] = Band{P10: -0.1, P90: 0.2}
		net.Residuals["load_unit"][key] = Band{P10: -0.05, P90: 0.05}
	}
	w := &data.Weather{
		Times:   []time.Time{f.Times[0].Add(-24 * time.Hour), f.Times[len(f.Times)-1].Add(24 * time.Hour)},
		Columns: []string{"fc_a"},
		Values:  [][]float64{{1, 1}},
	}
	return &Models{Price: price, Units: net, Weather: w}
}

func TestCurveByHand(t *testing.T) {
	f := frame(t0, 3000)
	m := testModels(t, f)
	spec := house.DefaultHouse() // 10.5 kW PV, 15 kWh/day load

	curve, ok, err := m.Curve(f, 2500, spec, 8)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(curve.Leads) != 5 {
		t.Fatalf("%d leads", len(curve.Leads))
	}
	l := curve.Leads[0]
	near(t, "p10 (sorted)", l.PriceP10, 100, 0)
	near(t, "p50 (sorted)", l.PriceP50, 200, 0)
	near(t, "p90 (sorted)", l.PriceP90, 300, 0)
	near(t, "pv_kw = 0.5 unit x 10.5 kW", l.PvKw, 0.5*10.5, 1e-12)
	near(t, "pv_lo", l.PvLo, (0.5-0.1)*10.5, 1e-12)
	near(t, "pv_hi", l.PvHi, (0.5+0.2)*10.5, 1e-12)
	near(t, "load_kw = 0.5 unit x (15/15)", l.LoadKw, 0.5, 1e-12)
	near(t, "load_hi", l.LoadHi, 0.55, 1e-12)

	// a house with twice the load scales the load forecast, not the price
	big := spec
	big.DailyLoadKwh = 30
	curve, _, _ = m.Curve(f, 2500, big, 8)
	near(t, "load scales with daily load", curve.Leads[0].LoadKw, 1.0, 1e-12)
	near(t, "price unchanged", curve.Leads[0].PriceP50, 200, 0)

	// a 2-hour horizon only has the 1h and 2h leads
	curve, ok, _ = m.Curve(f, 2500, spec, 2)
	if !ok || len(curve.Leads) != 2 || curve.Leads[1].LeadSteps != 24 {
		t.Errorf("2h horizon: ok=%v leads=%+v", ok, curve.Leads)
	}
}

func TestCurveNeedsAWeekOfHistory(t *testing.T) {
	f := frame(t0, 3000)
	m := testModels(t, f)
	spec := house.DefaultHouse()
	if _, ok, err := m.Curve(f, MaxLookbackV2-1, spec, 8); ok || err != nil {
		t.Errorf("too little history: ok=%v err=%v", ok, err)
	}
	if _, ok, err := m.Curve(f, MaxLookbackV2, spec, 8); !ok || err != nil {
		t.Errorf("exactly enough history: ok=%v err=%v", ok, err)
	}
}

func TestCurveWeatherOutOfRangeIsAnError(t *testing.T) {
	f := frame(t0, 3000)
	m := testModels(t, f)
	m.Weather.Times[1] = f.Times[2500] // the forecast ends before the 1h-ahead target
	if _, _, err := m.Curve(f, 2500, house.DefaultHouse(), 8); err == nil {
		t.Error("a target beyond the weather forecast must be an error, as in Python")
	}
}
