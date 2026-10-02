// Package forecast runs the two trained models:
//   - price: XGBoost, P10/P50/P90 of the NSW1 price at each lead (one model per lead);
//   - solar and demand: a linear model, the house's PV and load at the same leads.
package forecast

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"sync"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/features"
	"climate-hacktion-curtailment/backend/internal/model/house"
	"climate-hacktion-curtailment/backend/internal/model/models"
	"climate-hacktion-curtailment/backend/internal/model/xgb"
)

// Meta is models/meta.json.
type Meta struct {
	TrainedOnDataBefore string `json:"trained_on_data_before"`
	Price               map[string]struct {
		File     string   `json:"file"`
		Features []string `json:"features"`
	} `json:"price"`
	Units struct {
		Features  []string              `json:"features"`
		Mean      []float64             `json:"mean"`
		Scale     []float64             `json:"scale"`
		Coef      [][]float64           `json:"coef"`
		Intercept []float64             `json:"intercept"`
		Targets   []string              `json:"targets"`
		Bands     map[string][2]float64 `json:"bands_p10_p90"`
	} `json:"units"`
	House struct {
		LoadScale float64 `json:"load_scale"`
		Noise     struct {
			Origin   string `json:"origin"`
			PVFile   string `json:"pv_file"`
			LoadFile string `json:"load_file"`
		} `json:"noise"`
	} `json:"house"`
}

// Models are the loaded models and house noise.
type Models struct {
	Meta  Meta
	Price map[int]*xgb.Model
	Noise *house.Noise
}

var (
	once    sync.Once
	loaded  *Models
	errLoad error
)

// Load reads the embedded models (once).
func Load() (*Models, error) {
	once.Do(func() { loaded, errLoad = load(models.FS) })
	return loaded, errLoad
}

func load(fsys fs.FS) (*Models, error) {
	raw, err := fs.ReadFile(fsys, "meta.json")
	if err != nil {
		return nil, err
	}
	m := &Models{Price: map[int]*xgb.Model{}}
	if err := json.Unmarshal(raw, &m.Meta); err != nil {
		return nil, fmt.Errorf("meta.json: %w", err)
	}
	for _, lead := range features.Leads {
		entry, ok := m.Meta.Price[fmt.Sprint(lead)]
		if !ok {
			return nil, fmt.Errorf("meta.json: no price model for lead %d", lead)
		}
		raw, err := fs.ReadFile(fsys, entry.File)
		if err != nil {
			return nil, err
		}
		if m.Price[lead], err = xgb.Load(raw); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.File, err)
		}
		m.Price[lead].Features = entry.Features
	}
	origin, err := time.Parse(time.RFC3339, m.Meta.House.Noise.Origin)
	if err != nil {
		return nil, fmt.Errorf("meta.json noise origin: %w", err)
	}
	m.Noise = &house.Noise{Origin: origin.Unix()}
	if m.Noise.PV, err = floats(fsys, m.Meta.House.Noise.PVFile); err != nil {
		return nil, err
	}
	if m.Noise.Load, err = floats(fsys, m.Meta.House.Noise.LoadFile); err != nil {
		return nil, err
	}
	return m, nil
}

func floats(fsys fs.FS, name string) ([]float64, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	out := make([]float64, len(raw)/8)
	for i := range out {
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(raw[8*i:]))
	}
	return out, nil
}

// TrainedBefore is the day the models' training data ends, for display ("19 Aug 2026").
func (m *Models) TrainedBefore() string {
	t, err := time.Parse("2006-01-02 15:04:05-07:00", m.Meta.TrainedOnDataBefore)
	if err != nil {
		return m.Meta.TrainedOnDataBefore
	}
	return t.Format("2 Jan 2006")
}

// Inputs is one download with its house built.
type Inputs struct {
	*data.Data
	PV, Load []float64 // house: kW per kW of panel, kW at 15 kWh/day
}

// Prepare builds the house on the data's clock.
func (m *Models) Prepare(d *data.Data) (*Inputs, error) {
	pv, load, err := house.Build(d, m.Meta.House.LoadScale, m.Noise)
	if err != nil {
		return nil, err
	}
	return &Inputs{Data: d, PV: pv, Load: load}, nil
}

// Forecasts at every decision time in a range of intervals; [i][lead] (and [quantile]).
type Forecasts struct {
	From, To int            // interval indices, inclusive
	Price    [][][3]float64 // $/MWh, P10/P50/P90 (sorted)
	PV, Load [][]float64    // kW per kW of panel; kW at 15 kWh/day
}

// At returns the row for interval i.
func (f *Forecasts) At(i int) (price [][3]float64, pv, load []float64) {
	k := i - f.From
	return f.Price[k], f.PV[k], f.Load[k]
}

// Run forecasts at every interval in [from, to]. It needs 8 days of data before from and
// 8 hours after to.
func (m *Models) Run(in *Inputs, from, to int) (*Forecasts, error) {
	if from-8*features.Day < 0 || to+96 >= in.N() || to < from {
		return nil, fmt.Errorf("fetch a wider window: the model needs 8 days before and 8 hours after the times asked for")
	}
	n := to - from + 1
	f := &Forecasts{From: from, To: to, Price: make([][][3]float64, n), PV: make([][]float64, n), Load: make([][]float64, n)}
	for k := range f.Price {
		f.Price[k] = make([][3]float64, len(features.Leads))
	}
	var wg sync.WaitGroup
	errs := make([]error, len(features.Leads))
	for j, lead := range features.Leads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			model := m.Price[lead]
			rows, err := features.Price(in.Data, lead).Rows(model.Features, from, to)
			if err != nil {
				errs[j] = err
				return
			}
			for k, row := range rows {
				q := model.Predict(row)
				sort.Float64s(q)
				copy(f.Price[k][j][:], q)
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	u := m.Meta.Units
	rows, err := features.Units(in.Data, in.PV, in.Load).Rows(u.Features, from, to)
	if err != nil {
		return nil, err
	}
	leads := len(features.Leads)
	for k, row := range rows {
		f.PV[k], f.Load[k] = make([]float64, leads), make([]float64, leads)
		z := make([]float64, len(row))
		for c, v := range row {
			if math.IsNaN(v) {
				v = 0
			}
			z[c] = (v - u.Mean[c]) / u.Scale[c]
		}
		for t := range u.Coef {
			y := u.Intercept[t]
			for c, w := range u.Coef[t] {
				y += w * z[c]
			}
			y = math.Max(y, 0)
			if t < leads {
				f.PV[k][t] = y
			} else {
				f.Load[k][t-leads] = y
			}
		}
	}
	return f, nil
}
