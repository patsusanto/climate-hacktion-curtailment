package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/split"
)

const layout = "2006-01-02T15:04:05-07:00"

func gzWrite(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	gz.Write([]byte(body))
	gz.Close()
	f.Close()
}

// writeSyntheticExport writes a small export in the layout the README describes
// with constant-prediction models so the run is predictable.
func writeSyntheticExport(t *testing.T, days int) (dir string, t0 time.Time) {
	t.Helper()
	dir = t.TempDir()
	t0 = time.Date(2026, 7, 1, 0, 0, 0, 0, split.NEM)

	var frame strings.Builder
	frame.WriteString("time,price_aud_mwh,pv_unit,load_unit\n")
	for i := 0; i < days*split.DaySteps; i++ {
		ts := t0.Add(time.Duration(i*split.StepMinutes) * time.Minute)
		h := float64(ts.Hour())
		price := 90.0
		if h >= 17 && h < 21 {
			price = 300
		}
		pv := 0.0
		if h > 7 && h < 17 {
			pv = 0.7 * math.Sin(math.Pi*(h-7)/10)
		}
		fmt.Fprintf(&frame, "%s,%v,%v,0.2\n", ts.Format(layout), price, pv)
	}
	gzWrite(t, filepath.Join(dir, "frame.csv.gz"), frame.String())

	last := t0.Add(time.Duration(days*split.DaySteps) * 5 * time.Minute)
	gzWrite(t, filepath.Join(dir, "weather.csv.gz"), fmt.Sprintf("time,fc_a\n%s,1\n%s,1\n",
		t0.Add(-24*time.Hour).Format(layout), last.Add(24*time.Hour).Format(layout)))

	if err := os.Mkdir(filepath.Join(dir, "price"), 0o755); err != nil {
		t.Fatal(err)
	}
	stump := func(v float64) string { // one leaf that always predicts v
		return fmt.Sprintf(`{"learner":{"learner_model_param":{"base_score":"0"},"gradient_booster":{"model":{"trees":[`+
			`{"left_children":[-1],"right_children":[-1],"split_indices":[0],"split_conditions":[%v],"default_left":[0]}]}}}}`, v)
	}
	models := map[string][]string{}
	cols := map[string][]string{}
	for _, lead := range split.Leads {
		key := fmt.Sprint(lead)
		cols[key] = []string{"price_now", "fc_a"}
		for _, q := range []struct {
			tag string
			v   float64
		}{{"10", 100}, {"50", 150}, {"90", 250}} {
			name := fmt.Sprintf("price/lead%s_q%s.json", key, q.tag)
			if err := os.WriteFile(filepath.Join(dir, name), []byte(stump(q.v)), 0o644); err != nil {
				t.Fatal(err)
			}
			models[key] = append(models[key], name)
		}
	}
	mf, _ := json.Marshal(map[string]any{
		"price": map[string]any{"quantiles": []float64{0.1, 0.5, 0.9}, "feature_columns": cols, "models": models},
		"units": "unit_model.json", "weather": "weather.csv.gz",
	})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mf, 0o644); err != nil {
		t.Fatal(err)
	}

	n := 2 * len(split.Leads)
	w := make([][]float64, n)
	mean := make([]float64, n)
	scale := make([]float64, n)
	for i := range w {
		w[i] = []float64{0}
		mean[i] = 0.4
		scale[i] = 1
	}
	band := map[string]map[string]map[string]float64{"pv_unit": {}, "load_unit": {}}
	for _, lead := range split.Leads {
		key := fmt.Sprint(lead)
		band["pv_unit"][key] = map[string]float64{"p10": -0.1, "p90": 0.1}
		band["load_unit"][key] = map[string]float64{"p10": -0.05, "p90": 0.05}
	}
	net, _ := json.Marshal(map[string]any{
		"input_dim": 1, "output_dim": n, "feature_columns": []string{"hour_sin"},
		"layers":    []map[string]any{{"w": w, "b": make([]float64, n)}},
		"scaler_x":  map[string]any{"mean": []float64{0}, "scale": []float64{1}},
		"scaler_y":  map[string]any{"mean": mean, "scale": scale},
		"residuals": band,
	})
	if err := os.WriteFile(filepath.Join(dir, "unit_model.json"), net, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, t0
}

func TestGenrunEndToEnd(t *testing.T) {
	dir, t0 := writeSyntheticExport(t, 12)
	out := filepath.Join(t.TempDir(), "runs")
	start := t0.Add(9 * 24 * time.Hour) // nine days in: a week of history plus an hour
	end := start.Add(24*time.Hour - 5*time.Minute)

	var stdout bytes.Buffer
	args := []string{
		"-export", dir, "-out", out, "-window", "validation", "-id", "smoke",
		"-start", start.Format(time.RFC3339), "-end", end.Format(time.RFC3339),
	}
	if err := run(args, &stdout, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "smoke.json") || !strings.Contains(stdout.String(), "savings") {
		t.Errorf("unexpected output %q", stdout.String())
	}

	raw, err := os.ReadFile(filepath.Join(out, "smoke.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		RunID      string            `json:"run_id"`
		WindowName string            `json:"window_name"`
		Ticks      []json.RawMessage `json:"ticks"`
		Steps      map[string]any    `json:"steps"`
		Meta       struct {
			Window struct {
				N int `json:"n"`
			} `json:"window"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.RunID != "smoke" || got.WindowName != "validation" || len(got.Ticks) != 288 || got.Meta.Window.N != 288 {
		t.Errorf("run: id=%s window=%s ticks=%d n=%d", got.RunID, got.WindowName, len(got.Ticks), got.Meta.Window.N)
	}
	if len(got.Steps) != 288/12 {
		t.Errorf("%d step details", len(got.Steps))
	}
}

func TestGenrunErrors(t *testing.T) {
	dir, t0 := writeSyntheticExport(t, 12)
	quiet := log.New(io.Discard, "", 0)
	out := t.TempDir()
	early := t0.Add(2 * 24 * time.Hour).Format(time.RFC3339)
	cases := map[string][]string{
		"bad window name":     {"-export", dir, "-out", out, "-window", "nope"},
		"window off the data": {"-export", dir, "-out", out, "-window", "validation"},
		"too little history":  {"-export", dir, "-out", out, "-start", early, "-end", t0.Add(3 * 24 * time.Hour).Format(time.RFC3339)},
		"missing export":      {"-export", filepath.Join(dir, "nope"), "-out", out},
		"invalid battery":     {"-export", dir, "-out", out, "-battery-kwh", "0"},
		"unknown flag":        {"-bogus"},
	}
	for name, args := range cases {
		if err := run(args, io.Discard, quiet); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
