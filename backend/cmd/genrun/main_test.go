package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/synthetic"
	"climate-hacktion-curtailment/backend/internal/wire"
)

// A synthetic download for the test window, laid out where genrun looks for it, so nothing is
// fetched. The real models run on it.
func TestGenrunEndToEnd(t *testing.T) {
	dir := t.TempDir()
	w := Windows["test"]
	if err := synthetic.Write(filepath.Join(dir, "data", "test-nopd"), w[0].AddDate(0, 0, -9), w[1].AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run([]string{"-data", filepath.Join(dir, "data"), "-out", filepath.Join(dir, "runs"), "-window", "test",
		"-no-predispatch", "-pv", "6.6", "-battery-kwh", "13.5"}, &out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "runs", "6.6kw-13.5kwh.json"))
	if err != nil {
		t.Fatalf("%v (stdout: %s)", err, out.String())
	}
	var f wire.RunFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	n := int(w[1].Sub(w[0])/(5*time.Minute)) + 1
	if f.WindowName != "test" || f.Meta.Window.N != n || len(f.Ticks) != n {
		t.Fatalf("window %q, n %d, %d ticks; want test, %d", f.WindowName, f.Meta.Window.N, len(f.Ticks), n)
	}
	if f.Meta.Spec.PvKwAc != 6.6 || f.Meta.Spec.BatteryKwh != 13.5 || f.Meta.Spec.UsableKwh != 10.8 {
		t.Errorf("spec %+v", f.Meta.Spec)
	}
	if len(f.Steps) != (n+11)/12 {
		t.Errorf("%d steps of detail, want every 12th (%d)", len(f.Steps), (n+11)/12)
	}
	if !strings.Contains(out.String(), "planner bill") {
		t.Errorf("stdout: %s", out.String())
	}
	// Prices swing from -$20 to $300 every day: the planner must beat self-consumption.
	if f.Summary.SavingsAud <= 0 {
		t.Errorf("savings %.2f, want > 0", f.Summary.SavingsAud)
	}
}

func TestGenrunErrors(t *testing.T) {
	quiet := log.New(io.Discard, "", 0)
	for name, args := range map[string][]string{
		"bad window":  {"-window", "nope"},
		"bad battery": {"-battery-kwh", "0"},
		"bad flag":    {"-nope"},
	} {
		if err := run(args, io.Discard, quiet); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestDefaultID(t *testing.T) {
	if got := defaultID(10.5, 10); got != "10.5kw-10kwh" {
		t.Errorf("defaultID = %q", got)
	}
}
