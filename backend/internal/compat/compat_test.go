// Package compat checks that the two halves of the system agree on the run
// file: what the model writes (internal/model/runfile) is accepted and served
// by the API (internal/api). It lives apart from both so that neither has to
// import the other.
package compat

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/api"
	"climate-hacktion-curtailment/backend/internal/model/battery"
	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/forecast"
	"climate-hacktion-curtailment/backend/internal/model/planner"
	"climate-hacktion-curtailment/backend/internal/model/runfile"
	"climate-hacktion-curtailment/backend/internal/model/simulate"
	"climate-hacktion-curtailment/backend/internal/model/synthetic"
	"climate-hacktion-curtailment/backend/internal/wire"
)

const daySteps = 288

// A run written by the model must load through the service's own loader and
// stream correctly, so that genrun output can simply be dropped into runs/.
func TestModelRunFileIsServedByTheAPI(t *testing.T) {
	start := time.Date(2026, 7, 16, 0, 0, 0, 0, data.NEM)
	end := start.Add(24*time.Hour - 5*time.Minute)
	dir := t.TempDir()
	if err := synthetic.Write(filepath.Join(dir, "data"), start.AddDate(0, 0, -9), end.AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}
	m, err := forecast.Load()
	if err != nil {
		t.Fatal(err)
	}
	d, err := data.Load(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	in, err := m.Prepare(d)
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := battery.NewSpec(10.5, 10, 5, 5, 15, 0)
	res, err := simulate.Run(m, in, spec, start, end, planner.Economic, nil)
	if err != nil {
		t.Fatal(err)
	}
	file, err := runfile.Build(res, runfile.Options{ID: "gen-test", WindowName: "validation", DetailEvery: 12, WearAUDPerKWh: 0.05, TrainedBefore: "19 Aug 2026"})
	if err != nil {
		t.Fatal(err)
	}

	parent := t.TempDir()
	runs := filepath.Join(parent, "runs") // the service loads from a subdirectory (runs/)
	if err := os.Mkdir(runs, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runfile.Write(runs, file); err != nil {
		t.Fatal(err)
	}

	srv, err := api.New(os.DirFS(parent), "runs", time.Second)
	if err != nil {
		t.Fatalf("the service rejected the model's run file: %v", err)
	}
	if srv.RunCount() != 1 {
		t.Fatalf("%d runs loaded", srv.RunCount())
	}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// start: the meta the model wrote comes back, with the address swapped in
	resp, err := http.Post(ts.URL+"/v1/playground/run", "application/json", strings.NewReader(
		`{"address":"1 Example St","pv_kw_ac":10.5,"battery_kwh":10,"window":"validation"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		RunID string    `json:"run_id"`
		Meta  wire.Meta `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if started.RunID != "gen-test" || started.Meta.Window.N != daySteps || started.Meta.Spec.BatteryKwh != 10 ||
		started.Meta.Assumptions.AddressLabel != "1 Example St" {
		t.Errorf("start response: %+v", started)
	}

	// stream: every tick arrives and the last event is the summary
	resp, err = http.Get(ts.URL + "/v1/playground/run/gen-test/events?speed=max")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if n := strings.Count(string(body), "event: step\n"); n != daySteps {
		t.Errorf("%d step events, want %d", n, daySteps)
	}
	if !strings.Contains(string(body), "event: done\n") {
		t.Error("the stream has no done event")
	}

	// detail: only the steps the model kept (every 12th) are served
	for step, want := range map[string]int{"0": 200, "12": 200, "1": 404} {
		resp, err := http.Get(ts.URL + "/v1/playground/run/gen-test/steps/" + step)
		if err != nil {
			t.Fatal(err)
		}
		if step == "12" {
			var decision wire.StepDecision
			json.NewDecoder(resp.Body).Decode(&decision)
			if len(decision.Leads) != 5 || len(decision.Stories) != 1 || len(decision.Stories[0].PriceAudMwh) != 96 {
				t.Errorf("step 12 detail: %d leads, %d stories", len(decision.Leads), len(decision.Stories))
			}
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("step %s: status %d, want %d", step, resp.StatusCode, want)
		}
	}
}
