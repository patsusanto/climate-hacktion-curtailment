package runfile

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/battery"
	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/forecast"
	"climate-hacktion-curtailment/backend/internal/model/planner"
	"climate-hacktion-curtailment/backend/internal/model/simulate"
	"climate-hacktion-curtailment/backend/internal/model/synthetic"
	"climate-hacktion-curtailment/backend/internal/wire"
)

// replay runs the real models over three synthetic days.
func replay(t *testing.T) *simulate.Result {
	t.Helper()
	dir := t.TempDir()
	start := time.Date(2026, 8, 19, 0, 0, 0, 0, data.NEM)
	end := time.Date(2026, 8, 21, 23, 55, 0, 0, data.NEM)
	if err := synthetic.Write(dir, start.AddDate(0, 0, -9), end.AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}
	m, err := forecast.Load()
	if err != nil {
		t.Fatal(err)
	}
	d, err := data.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	in, err := m.Prepare(d)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := battery.NewSpec(6.6, 13.5, 5, 5, 18, 0)
	if err != nil {
		t.Fatal(err)
	}
	res, err := simulate.Run(m, in, spec, start, end, planner.Economic, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

var opts = Options{ID: "t", WindowName: "test", DetailEvery: 12, WearAUDPerKWh: 0.05, TrainedBefore: "19 Aug 2026"}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A live run sends the pieces one at a time; together they must be exactly the run file.
func TestBuildIsMadeOfThePieces(t *testing.T) {
	res := replay(t)
	file, err := Build(res, opts)
	if err != nil {
		t.Fatal(err)
	}
	n := len(res.Steps)
	if got, want := mustJSON(t, file.Meta), mustJSON(t, Meta(res.Spec, res.Steps[0].Time, res.Steps[n-1].Time, n, opts)); got != want {
		t.Errorf("meta differs:\n%s\n%s", got, want)
	}
	var ticker Ticker
	for i, s := range res.Steps {
		if got, want := mustJSON(t, file.Ticks[i]), mustJSON(t, ticker.Tick(i, s)); got != want {
			t.Fatalf("tick %d differs", i)
		}
	}
	if got, want := mustJSON(t, file.Summary), mustJSON(t, Summary(res, opts)); got != want {
		t.Errorf("summary differs")
	}
	for i := 0; i < n; i += 12 {
		d, err := StepDecision(res, i)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := mustJSON(t, file.Steps[strconv.Itoa(i)]), mustJSON(t, d); got != want {
			t.Fatalf("step detail %d differs", i)
		}
	}
	if len(file.Steps) != (n+11)/12 {
		t.Errorf("%d detail steps, want %d", len(file.Steps), (n+11)/12)
	}
}

func TestRunningCostsAgreeWithTheSummary(t *testing.T) {
	res := replay(t)
	file, _ := Build(res, opts)
	last := file.Ticks[len(file.Ticks)-1]
	if d := last.CumulativeSelfAud - res.Self.EnergyCashAUD; math.Abs(d) > 0.01 {
		t.Errorf("self cost so far %v vs baseline energy cash %v", last.CumulativeSelfAud, res.Self.EnergyCashAUD)
	}
	if d := last.CumulativeSavingsAud - file.Summary.SavingsAud; math.Abs(d) > 0.01 {
		t.Errorf("savings so far %v vs summary %v", last.CumulativeSavingsAud, file.Summary.SavingsAud)
	}
	if file.Ticks[0].I != 0 || file.Ticks[len(file.Ticks)-1].I != len(file.Ticks)-1 {
		t.Error("ticks are not numbered from 0")
	}
}

// The page asks for the detail of whatever step is under the cursor.
func TestStepDecisionForAnyStep(t *testing.T) {
	res := replay(t)
	file, _ := Build(res, opts)
	for _, i := range []int{0, 1, 13, 100, len(res.Steps) - 1} {
		d, err := StepDecision(res, i)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if d.T != file.Ticks[i].T || d.Action != file.Ticks[i].Action {
			t.Errorf("step %d: detail %s/%s, tick %s/%s", i, d.T, d.Action, file.Ticks[i].T, file.Ticks[i].Action)
		}
		if len(d.Leads) != 5 || len(d.Stories) != 1 || len(d.Stories[0].PriceAudMwh) == 0 {
			t.Errorf("step %d: %d leads, %d stories", i, len(d.Leads), len(d.Stories))
		}
	}
	if _, err := StepDecision(res, len(res.Steps)); err == nil {
		t.Error("a step past the end must be an error")
	}
	if _, err := StepDecision(res, -1); err == nil {
		t.Error("a negative step must be an error")
	}
}

func TestBuildRejectsAnEmptyReplay(t *testing.T) {
	if _, err := Build(&simulate.Result{}, opts); err == nil {
		t.Error("an empty replay must be an error")
	}
	if _, err := Write(t.TempDir(), file0()); err == nil {
		t.Error("a run without an id must not be written")
	}
}

func file0() (f wire.RunFile) { return }
