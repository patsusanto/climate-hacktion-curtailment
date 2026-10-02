// Package xgb evaluates an XGBoost model saved as JSON (Booster.save_model("model.json")).
//
// It supports what the price models use: gbtree, numerical splits, one output per tree
// (multi-target models list each tree's target in tree_info), and missing values (NaN)
// following each node's default direction. Like XGBoost, inputs and sums are float32.
package xgb

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type tree struct {
	left, right []int32
	feature     []int32
	value       []float32 // split threshold, or the leaf value at a leaf
	defaultLeft []bool
}

// Model is a loaded booster.
type Model struct {
	Features  []string
	baseScore []float32
	trees     []tree
	group     []int // output each tree adds to
}

type fileFormat struct {
	Learner struct {
		FeatureNames []string `json:"feature_names"`
		Param        struct {
			BaseScore string `json:"base_score"`
			NumTarget string `json:"num_target"`
		} `json:"learner_model_param"`
		Booster struct {
			Name  string `json:"name"`
			Model struct {
				TreeInfo []int `json:"tree_info"`
				Trees    []struct {
					Left        []int32   `json:"left_children"`
					Right       []int32   `json:"right_children"`
					Split       []int32   `json:"split_indices"`
					Conditions  []float64 `json:"split_conditions"`
					DefaultLeft []int     `json:"default_left"`
					SplitType   []int     `json:"split_type"`
				} `json:"trees"`
			} `json:"model"`
		} `json:"gradient_booster"`
	} `json:"learner"`
}

// Load parses a model from its JSON.
func Load(raw []byte) (*Model, error) {
	var f fileFormat
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	l := f.Learner
	if l.Booster.Name != "gbtree" {
		return nil, fmt.Errorf("xgb: booster %q not supported", l.Booster.Name)
	}
	var base []float64
	s := strings.TrimSpace(l.Param.BaseScore)
	if !strings.HasPrefix(s, "[") {
		s = "[" + s + "]"
	}
	if err := json.Unmarshal([]byte(s), &base); err != nil {
		return nil, fmt.Errorf("xgb: base_score: %w", err)
	}
	m := &Model{Features: l.FeatureNames, group: l.Booster.Model.TreeInfo}
	for _, b := range base {
		m.baseScore = append(m.baseScore, float32(b))
	}
	for i, t := range l.Booster.Model.Trees {
		for _, st := range t.SplitType {
			if st != 0 {
				return nil, fmt.Errorf("xgb: tree %d has a categorical split", i)
			}
		}
		tr := tree{left: t.Left, right: t.Right, feature: t.Split, value: make([]float32, len(t.Conditions)), defaultLeft: make([]bool, len(t.DefaultLeft))}
		for j, c := range t.Conditions {
			tr.value[j] = float32(c)
		}
		for j, d := range t.DefaultLeft {
			tr.defaultLeft[j] = d != 0
		}
		m.trees = append(m.trees, tr)
	}
	if len(m.group) != len(m.trees) {
		return nil, fmt.Errorf("xgb: %d trees but %d tree_info entries", len(m.trees), len(m.group))
	}
	return m, nil
}

// Outputs is the number of values Predict returns (3 for a P10/P50/P90 model).
func (m *Model) Outputs() int { return len(m.baseScore) }

// Predict returns one value per output for a row of features in m.Features order (NaN = missing).
func (m *Model) Predict(x []float64) []float64 {
	row := make([]float32, len(x))
	missing := make([]bool, len(x))
	for i, v := range x {
		missing[i] = math.IsNaN(v)
		row[i] = float32(v)
	}
	sum := append([]float32(nil), m.baseScore...)
	for i := range m.trees {
		t := &m.trees[i]
		n := int32(0)
		for t.left[n] != -1 {
			f := t.feature[n]
			switch {
			case missing[f]:
				if t.defaultLeft[n] {
					n = t.left[n]
				} else {
					n = t.right[n]
				}
			case row[f] < t.value[n]:
				n = t.left[n]
			default:
				n = t.right[n]
			}
		}
		sum[m.group[i]] += t.value[n]
	}
	out := make([]float64, len(sum))
	for i, v := range sum {
		out[i] = float64(v)
	}
	return out
}
