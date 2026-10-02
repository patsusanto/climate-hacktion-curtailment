package forecast

import (
	"encoding/json"
	"fmt"
	"os"
)

// Scaler is a fitted sklearn StandardScaler: (x - mean) / scale.
type Scaler struct {
	Mean  []float64 `json:"mean"`
	Scale []float64 `json:"scale"`
}

// Transform scales a row. sklearn does this in float64.
func (s Scaler) Transform(x []float64) []float64 {
	out := make([]float64, len(x))
	for i, v := range x {
		out[i] = (v - s.Mean[i]) / s.Scale[i]
	}
	return out
}

// InverseTransform undoes Transform: x * scale + mean.
func (s Scaler) InverseTransform(x []float64) []float64 {
	out := make([]float64, len(x))
	for i, v := range x {
		out[i] = v*s.Scale[i] + s.Mean[i]
	}
	return out
}

// Band is the 10th and 90th percentile of the validation residuals at a lead.
type Band struct {
	P10 float64 `json:"p10"`
	P90 float64 `json:"p90"`
}

type layer struct {
	W [][]float64 `json:"w"` // [out][in]
	B []float64   `json:"b"`
}

// UnitModel is the PV and load net: a 64-32 feedforward MLP with ReLU, scalers
// on both sides and a residual band per lead. Dropout is inactive at inference.
//
// Outputs are PV units at each lead followed by load units at each lead.
type UnitModel struct {
	InputDim       int                        `json:"input_dim"`
	OutputDim      int                        `json:"output_dim"`
	FeatureColumns []string                   `json:"feature_columns"`
	Layers         []layer                    `json:"layers"`
	ScalerX        Scaler                     `json:"scaler_x"`
	ScalerY        Scaler                     `json:"scaler_y"`
	Residuals      map[string]map[string]Band `json:"residuals"` // column -> lead -> band
}

// LoadUnitModel reads the exported net.
func LoadUnitModel(path string) (*UnitModel, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m UnitModel
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &m, nil
}

func (m *UnitModel) validate() error {
	if len(m.Layers) == 0 {
		return fmt.Errorf("no layers")
	}
	in := m.InputDim
	for i, l := range m.Layers {
		if len(l.W) != len(l.B) {
			return fmt.Errorf("layer %d: %d weight rows but %d biases", i, len(l.W), len(l.B))
		}
		for _, row := range l.W {
			if len(row) != in {
				return fmt.Errorf("layer %d: expected %d inputs, got %d", i, in, len(row))
			}
		}
		in = len(l.W)
	}
	if in != m.OutputDim {
		return fmt.Errorf("final layer has %d outputs, want %d", in, m.OutputDim)
	}
	if len(m.FeatureColumns) != m.InputDim {
		return fmt.Errorf("%d feature columns for %d inputs", len(m.FeatureColumns), m.InputDim)
	}
	for _, s := range []Scaler{m.ScalerX, m.ScalerY} {
		if len(s.Mean) != len(s.Scale) {
			return fmt.Errorf("scaler mean and scale differ in length")
		}
	}
	if len(m.ScalerX.Mean) != m.InputDim || len(m.ScalerY.Mean) != m.OutputDim {
		return fmt.Errorf("scaler sizes do not match the net")
	}
	return nil
}

// Forward runs the net on a raw feature row and returns the unscaled outputs.
//
// torch holds the scaled input and every layer in float32, so the input is
// rounded to float32 and each layer's result is rounded back to float32.
// Products are summed in float64 inside a layer, which is at least as accurate
// as BLAS and well inside the 1e-4 tolerance of the golden tests.
func (m *UnitModel) Forward(raw []float64) []float64 {
	x := make([]float32, len(raw))
	for i, v := range m.ScalerX.Transform(raw) {
		x[i] = float32(v)
	}
	for li, l := range m.Layers {
		out := make([]float32, len(l.W))
		for o, row := range l.W {
			sum := l.B[o]
			for i, w := range row {
				sum += w * float64(x[i])
			}
			y := float32(sum)
			if li < len(m.Layers)-1 && y < 0 { // ReLU between layers, not after the last
				y = 0
			}
			out[o] = y
		}
		x = out
	}
	y := make([]float64, len(x))
	for i, v := range x {
		y[i] = float64(v)
	}
	return m.ScalerY.InverseTransform(y)
}
