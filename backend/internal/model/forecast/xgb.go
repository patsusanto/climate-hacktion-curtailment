// Package forecast runs the trained models: XGBoost price quantiles and the
// PV and load net, plus the features they read. Training happens elsewhere;
// this package loads the exported models (see the README).
package forecast

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// XGB is one XGBoost booster loaded from its JSON dump (Booster.save_model
// with a .json name). Only what scoring needs is read.
//
// XGBoost scores float32 features against float32 split conditions and sums
// float32 leaf values onto a float32 base score, so this does the same.
type XGB struct {
	Base  float32
	Trees []xgbTree
}

type xgbTree struct {
	left      []int32
	right     []int32
	feature   []int32
	cond      []float32 // split condition, or the leaf value on a leaf
	defaultLt []bool    // missing values go left when true
}

type xgbFile struct {
	Learner struct {
		Param struct {
			BaseScore string `json:"base_score"`
		} `json:"learner_model_param"`
		Booster struct {
			Model struct {
				Trees []struct {
					Left      []int32   `json:"left_children"`
					Right     []int32   `json:"right_children"`
					Feature   []int32   `json:"split_indices"`
					Cond      []float32 `json:"split_conditions"`
					DefaultLt []int     `json:"default_left"`
				} `json:"trees"`
			} `json:"model"`
		} `json:"gradient_booster"`
	} `json:"learner"`
}

// LoadXGB reads a booster from a JSON dump.
func LoadXGB(path string) (*XGB, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := ParseXGB(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// ParseXGB parses a booster JSON dump.
func ParseXGB(raw []byte) (*XGB, error) {
	var f xgbFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	base, err := parseBaseScore(f.Learner.Param.BaseScore)
	if err != nil {
		return nil, err
	}
	m := &XGB{Base: base}
	for i, t := range f.Learner.Booster.Model.Trees {
		n := len(t.Left)
		if n == 0 || len(t.Right) != n || len(t.Feature) != n || len(t.Cond) != n || len(t.DefaultLt) != n {
			return nil, fmt.Errorf("tree %d has inconsistent arrays", i)
		}
		tree := xgbTree{left: t.Left, right: t.Right, feature: t.Feature, cond: t.Cond, defaultLt: make([]bool, n)}
		for j, d := range t.DefaultLt {
			tree.defaultLt[j] = d != 0
		}
		m.Trees = append(m.Trees, tree)
	}
	if len(m.Trees) == 0 {
		return nil, fmt.Errorf("booster has no trees")
	}
	return m, nil
}

// parseBaseScore reads "5E-1" or, in newer XGBoost, "[5E-1]".
func parseBaseScore(s string) (float32, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if i := strings.Index(s, ","); i >= 0 {
		s = s[:i]
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil {
		return 0, fmt.Errorf("base_score %q: %w", s, err)
	}
	return float32(v), nil
}

// Predict scores one feature row. NaN means missing.
func (m *XGB) Predict(features []float32) float32 {
	sum := float32(0)
	for i := range m.Trees {
		sum += m.Trees[i].leaf(features)
	}
	return m.Base + sum
}

func (t *xgbTree) leaf(features []float32) float32 {
	node := int32(0)
	for t.left[node] != -1 {
		x := features[t.feature[node]]
		switch {
		case x != x: // NaN
			if t.defaultLt[node] {
				node = t.left[node]
			} else {
				node = t.right[node]
			}
		case x < t.cond[node]:
			node = t.left[node]
		default:
			node = t.right[node]
		}
	}
	return t.cond[node]
}

// float32Row converts a float64 feature row the way XGBoost's DMatrix does.
func float32Row(row []float64) []float32 {
	out := make([]float32, len(row))
	for i, v := range row {
		if math.IsNaN(v) {
			out[i] = float32(math.NaN())
		} else {
			out[i] = float32(v)
		}
	}
	return out
}
