package forecast

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/house"
	"climate-hacktion-curtailment/backend/internal/model/planner"
	"climate-hacktion-curtailment/backend/internal/model/split"
)

// PriceModel is one XGBoost model per lead and quantile (feature set v2).
type PriceModel struct {
	Quantiles      []float64
	FeatureColumns map[int][]string
	Models         map[int][]*XGB // per lead, in Quantiles order
}

// Models is everything the forecast curve needs.
type Models struct {
	Price   *PriceModel
	Units   *UnitModel
	Weather *data.Weather
}

type manifest struct {
	Price struct {
		Quantiles      []float64           `json:"quantiles"`
		FeatureColumns map[string][]string `json:"feature_columns"`
		Models         map[string][]string `json:"models"` // lead -> one file per quantile
	} `json:"price"`
	Units   string `json:"units"`
	Weather string `json:"weather"`
}

// LoadModels reads manifest.json in dir and the files it names (paths are
// relative to dir). The layout is described in the README.
func LoadModels(dir string) (*Models, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var mf manifest
	if err := json.Unmarshal(raw, &mf); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	price := &PriceModel{
		Quantiles:      mf.Price.Quantiles,
		FeatureColumns: map[int][]string{},
		Models:         map[int][]*XGB{},
	}
	if len(price.Quantiles) != 3 {
		return nil, fmt.Errorf("manifest.json: expected 3 quantiles, got %d", len(price.Quantiles))
	}
	for _, lead := range split.Leads {
		key := strconv.Itoa(lead)
		cols, ok := mf.Price.FeatureColumns[key]
		files := mf.Price.Models[key]
		if !ok || len(files) != len(price.Quantiles) {
			return nil, fmt.Errorf("manifest.json: lead %d needs feature columns and %d model files", lead, len(price.Quantiles))
		}
		price.FeatureColumns[lead] = cols
		for _, name := range files {
			m, err := LoadXGB(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			price.Models[lead] = append(price.Models[lead], m)
		}
	}
	units, err := LoadUnitModel(filepath.Join(dir, mf.Units))
	if err != nil {
		return nil, err
	}
	weather, err := data.LoadWeather(filepath.Join(dir, mf.Weather))
	if err != nil {
		return nil, err
	}
	return &Models{Price: price, Units: units, Weather: weather}, nil
}

// pvLoadBands reads the residual band for one lead of one output.
func (m *UnitModel) band(column string, lead int) (Band, error) {
	b, ok := m.Residuals[column][strconv.Itoa(lead)]
	if !ok {
		return Band{}, fmt.Errorf("no residual band for %s at lead %d", column, lead)
	}
	return b, nil
}

// Curve forecasts the price quantiles, PV and load at every lead up to the
// horizon, for row i of the frame, using only rows up to i (the trailing
// MaxLookbackV2+1). The second result is false when the window cannot fill
// every feature yet; callers then fall back to a rule. This is
// policies/mpc.py forecast_curve.
func (m *Models) Curve(f *data.Frame, i int, spec house.HouseSpec, horizonHours int) (planner.ForecastCurve, bool, error) {
	lo := i - MaxLookbackV2
	if lo < 0 || i >= f.Len() {
		return planner.ForecastCurve{}, false, nil // not enough history yet
	}
	limit := horizonHours * split.StepsPerHour

	unitRow, ok := Row(UnitFeatures(f, lo, i), m.Units.FeatureColumns)
	if !ok {
		return planner.ForecastCurve{}, false, nil
	}
	units := m.Units.Forward(unitRow)

	priceRows := map[int][]float64{}
	for _, lead := range split.Leads {
		if lead > limit {
			continue
		}
		features, err := PriceFeatures(f, lo, i, lead, m.Weather)
		if err != nil {
			return planner.ForecastCurve{}, false, err
		}
		row, ok := Row(features, m.Price.FeatureColumns[lead])
		if !ok {
			return planner.ForecastCurve{}, false, nil
		}
		priceRows[lead] = row
	}

	n := len(split.Leads)
	pvScale := spec.PvKwAc
	loadScale := spec.DailyLoadKwh / house.LoadUnitDailyKwh
	var curve planner.ForecastCurve
	for index, lead := range split.Leads {
		row, ok := priceRows[lead]
		if !ok {
			continue
		}
		var raw [3]float64
		x := float32Row(row)
		for k, model := range m.Price.Models[lead] {
			raw[k] = float64(model.Predict(x))
		}
		sort.Float64s(raw[:]) // order the band so P10 <= P50 <= P90

		pvBand, err := m.Units.band("pv_unit", lead)
		if err != nil {
			return planner.ForecastCurve{}, false, err
		}
		loadBand, err := m.Units.band("load_unit", lead)
		if err != nil {
			return planner.ForecastCurve{}, false, err
		}
		pvUnit, loadUnit := units[index], units[n+index]
		curve.Leads = append(curve.Leads, planner.LeadForecast{
			LeadSteps: lead,
			PriceP10:  raw[0],
			PriceP50:  raw[1],
			PriceP90:  raw[2],
			PvKw:      pvUnit * pvScale,
			PvLo:      (pvUnit + pvBand.P10) * pvScale,
			PvHi:      (pvUnit + pvBand.P90) * pvScale,
			LoadKw:    loadUnit * loadScale,
			LoadLo:    (loadUnit + loadBand.P10) * loadScale,
			LoadHi:    (loadUnit + loadBand.P90) * loadScale,
		})
	}
	if len(curve.Leads) == 0 {
		return planner.ForecastCurve{}, false, nil
	}
	for _, l := range curve.Leads {
		if math.IsNaN(l.PriceP50) || math.IsNaN(l.PvKw) {
			return planner.ForecastCurve{}, false, fmt.Errorf("forecast produced NaN at lead %d", l.LeadSteps)
		}
	}
	return curve, true, nil
}
