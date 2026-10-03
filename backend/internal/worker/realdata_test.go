package worker

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"climate-hacktion-curtailment/backend/internal/model/runfile"
	"climate-hacktion-curtailment/backend/internal/wire"
)

// The data in backend/data and the models embedded in the binary are what generated the example
// runs in backend/runs. The worker, given the same house, must therefore reproduce every one of
// them exactly: each tick, the summary, the metadata and the stored step detail. This is the
// check that the deployed worker, its data and the precomputed runs all agree, and it will fail
// the day someone updates the models or the data without regenerating the runs.
func TestRealDataReproducesTheCommittedRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("replays the real windows; skipped with -short")
	}
	s, err := New(Config{DataDir: filepath.Join("..", "..", "data"), Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(s.Windows()); got != 2 {
		t.Errorf("%d windows loaded, want validation and test", got)
	}

	files, err := filepath.Glob(filepath.Join("..", "..", "runs", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no committed runs found: %v", err)
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var want wire.RunFile
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			sp := want.Meta.Spec
			p := wire.Params{PvKwAc: sp.PvKwAc, BatteryKwh: sp.BatteryKwh, BatteryKw: sp.BatteryKw,
				ExportCapKw: sp.ExportCapKw, DailyLoadKwh: sp.DailyLoadKwh, Window: want.WindowName}
			win := s.windows[p.Window]
			if win == nil {
				t.Fatalf("no data for window %q", p.Window)
			}
			spec, err := spec(p)
			if err != nil {
				t.Fatal(err)
			}
			res, err := s.result(p, spec, win, func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			opts := s.options(p)

			n := len(res.Steps)
			if n != len(want.Ticks) {
				t.Fatalf("%d steps, the committed run has %d", n, len(want.Ticks))
			}
			if got := runfile.Meta(res.Spec, res.Steps[0].Time, res.Steps[n-1].Time, n, opts); !reflect.DeepEqual(got, want.Meta) {
				t.Errorf("metadata differs:\n got  %+v\n want %+v", got, want.Meta)
			}
			var ticker runfile.Ticker
			bad := 0
			for i, step := range res.Steps {
				if got := ticker.Tick(i, step); got != want.Ticks[i] {
					if bad < 3 {
						t.Errorf("tick %d differs:\n got  %+v\n want %+v", i, got, want.Ticks[i])
					}
					bad++
				}
			}
			if bad > 0 {
				t.Errorf("%d of %d ticks differ", bad, n)
			}
			if got := runfile.Summary(res, opts); got != want.Summary {
				t.Errorf("summary differs:\n got  %+v\n want %+v", got, want.Summary)
			}
			for key, detail := range want.Steps {
				i, _ := strconv.Atoi(key)
				got, err := runfile.StepDecision(res, i)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, detail) {
					t.Fatalf("step detail %d differs:\n got  %+v\n want %+v", i, got, detail)
				}
			}
			t.Logf("%s: %d ticks, %d detail steps and the summary match", filepath.Base(path), n, len(want.Steps))
		})
	}
}
