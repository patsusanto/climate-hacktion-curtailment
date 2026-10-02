package forecast

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"climate-hacktion-curtailment/backend/internal/split"
)

// writeExport lays out an export directory the way the README describes it.
func writeExport(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "price"), 0o755); err != nil {
		t.Fatal(err)
	}
	mf := manifest{Units: "unit_model.json", Weather: "weather.csv.gz"}
	mf.Price.Quantiles = []float64{0.1, 0.5, 0.9}
	mf.Price.FeatureColumns = map[string][]string{}
	mf.Price.Models = map[string][]string{}
	for _, lead := range split.Leads {
		key := sprintf("%d", lead)
		mf.Price.FeatureColumns[key] = []string{"price_now", "fc_a"}
		for _, q := range []string{"10", "50", "90"} {
			name := sprintf("price/lead%s_q%s.json", key, q)
			if err := os.WriteFile(filepath.Join(dir, name), []byte(sprintf(xgbJSON, "0.5")), 0o644); err != nil {
				t.Fatal(err)
			}
			mf.Price.Models[key] = append(mf.Price.Models[key], name)
		}
	}
	raw, _ := json.Marshal(mf)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	net := tinyNet()
	net.Residuals = map[string]map[string]Band{"pv_unit": {"12": {P10: -0.1, P90: 0.1}}}
	raw, _ = json.Marshal(net)
	if err := os.WriteFile(filepath.Join(dir, "unit_model.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(filepath.Join(dir, "weather.csv.gz"))
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	gz.Write([]byte("time,fc_a\n2026-07-16T00:00:00+10:00,1\n2026-07-16T01:00:00+10:00,2\n"))
	gz.Close()
	f.Close()
	return dir
}

func TestLoadModels(t *testing.T) {
	m, err := LoadModels(writeExport(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, lead := range split.Leads {
		if len(m.Price.Models[lead]) != 3 || len(m.Price.FeatureColumns[lead]) != 2 {
			t.Errorf("lead %d: %d models, %d columns", lead, len(m.Price.Models[lead]), len(m.Price.FeatureColumns[lead]))
		}
	}
	if m.Units.InputDim != 2 || len(m.Weather.Times) != 2 {
		t.Errorf("units %d inputs, %d weather rows", m.Units.InputDim, len(m.Weather.Times))
	}
	// the loaded booster scores like the in-memory one
	if got := m.Price.Models[12][0].Predict([]float32{1, 0}); got != 0.5+1.0-0.5 {
		t.Errorf("loaded booster predicts %v", got)
	}
	if b, err := m.Units.band("pv_unit", 12); err != nil || b.P90 != 0.1 {
		t.Errorf("band %+v err %v", b, err)
	}
	if _, err := m.Units.band("pv_unit", 24); err == nil {
		t.Error("a missing band must be an error")
	}
}

func TestLoadModelsRejectsIncompleteExports(t *testing.T) {
	if _, err := LoadModels(t.TempDir()); err == nil {
		t.Error("a directory with no manifest must fail")
	}
	dir := writeExport(t)
	if err := os.Remove(filepath.Join(dir, "price", "lead72_q90.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadModels(dir); err == nil {
		t.Error("a missing model file must fail")
	}
	dir = writeExport(t)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"price":{"quantiles":[0.5]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadModels(dir); err == nil {
		t.Error("a manifest with the wrong quantiles must fail")
	}
}
