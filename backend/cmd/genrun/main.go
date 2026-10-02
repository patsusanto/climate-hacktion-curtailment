// Command genrun replays a window of history with the frozen controller and
// writes the run the playground service serves.
//
//	go run ./cmd/genrun -export export -window validation
//
// -export is the export directory described in the README: manifest.json,
// the models, weather.csv.gz and frame.csv.gz.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"climate-hacktion-curtailment/backend/internal/data"
	"climate-hacktion-curtailment/backend/internal/engine"
	"climate-hacktion-curtailment/backend/internal/forecast"
	"climate-hacktion-curtailment/backend/internal/split"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, log.Default()); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdout io.Writer, logger *log.Logger) error {
	cfg := engine.DefaultConfig()
	fs := flag.NewFlagSet("genrun", flag.ContinueOnError)
	var (
		exportDir  = fs.String("export", "export", "export directory (see the README)")
		outDir     = fs.String("out", "runs", "directory to write <run_id>.json into")
		window     = fs.String("window", "validation", `"validation" or "test"`)
		startFlag  = fs.String("start", "", "override the window start (RFC 3339)")
		endFlag    = fs.String("end", "", "override the window end, inclusive (RFC 3339)")
		runID      = fs.String("id", "", `run id (default "<pv>kw-<battery>kwh")`)
		pv         = fs.Float64("pv", cfg.Spec.PvKwAc, "solar size, kW AC")
		batteryKwh = fs.Float64("battery-kwh", cfg.Spec.Battery.CapacityKwh, "battery capacity, kWh")
		batteryKw  = fs.Float64("battery-kw", cfg.Spec.Battery.MaxPowerKw, "battery power, kW")
		load       = fs.Float64("load", cfg.Spec.DailyLoadKwh, "daily load, kWh")
		exportCap  = fs.Float64("export-cap", cfg.Spec.ExportCapKw, "export cap, kW")
	)
	fs.IntVar(&cfg.HorizonHours, "horizon", cfg.HorizonHours, "planner horizon, hours")
	fs.IntVar(&cfg.StepEvery, "detail-every", cfg.StepEvery, "keep forecast detail for every Nth step")
	fs.StringVar(&cfg.StoryGrid, "story-grid", cfg.StoryGrid, `story columns: "stages" or "intervals"`)
	fs.Float64Var(&cfg.WearAudPerKwh, "wear", cfg.WearAudPerKwh, "battery wear, AUD per kWh, for savings_with_wear_aud")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg.Spec.PvKwAc = *pv
	cfg.Spec.Battery.CapacityKwh = *batteryKwh
	cfg.Spec.Battery.MaxPowerKw = *batteryKw
	cfg.Spec.DailyLoadKwh = *load
	cfg.Spec.ExportCapKw = *exportCap
	if err := cfg.Spec.Validate(); err != nil {
		return err
	}

	start, end, err := windowBounds(*window, *startFlag, *endFlag)
	if err != nil {
		return err
	}

	t0 := time.Now()
	frame, err := data.LoadFrame(filepath.Join(*exportDir, "frame.csv.gz"))
	if err != nil {
		return err
	}
	models, err := forecast.LoadModels(*exportDir)
	if err != nil {
		return err
	}
	logger.Printf("loaded %d rows and the models in %s", frame.Len(), time.Since(t0).Round(time.Millisecond))

	from, to := frame.IndexOf(start), frame.IndexOf(end)
	if from < 0 || to < 0 {
		return fmt.Errorf("window %s .. %s is not on the clock of the frame (%s .. %s)",
			start.Format(time.RFC3339), end.Format(time.RFC3339),
			frame.Times[0].Format(time.RFC3339), frame.Times[frame.Len()-1].Format(time.RFC3339))
	}

	t1 := time.Now()
	res, err := engine.Replay(frame, models, cfg, from, to+1)
	if err != nil {
		return err
	}
	logger.Printf("replayed %d steps in %s (%d decided by the fallback rule)",
		len(res.Ticks), time.Since(t1).Round(time.Millisecond), res.FallbackSteps)

	id := *runID
	if id == "" {
		id = engine.DefaultRunID(cfg.Spec)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	path, err := engine.WriteRunFile(*outDir, engine.BuildRunFile(id, *window, cfg, res))
	if err != nil {
		return err
	}
	s := res.Summary
	fmt.Fprintf(stdout, "wrote %s\n  self-consumption bill %.2f   planner bill %.2f   savings %.2f   with wear %.2f   supply %.2f\n",
		path, s.SelfConsumption.BillAud, s.Planner.BillAud, s.SavingsAud, s.SavingsWithWearAud, s.SupplyAud)
	return nil
}

func windowBounds(name, startFlag, endFlag string) (start, end time.Time, err error) {
	switch name {
	case "validation":
		// the last tenth of the training window
		start, end = time.Date(2026, 7, 16, 0, 0, 0, 0, split.NEM), split.TrainEnd
	case "test":
		start, end = split.TestStart, split.TestEnd
	default:
		return start, end, fmt.Errorf(`window must be "validation" or "test"`)
	}
	if startFlag != "" {
		if start, err = time.Parse(time.RFC3339, startFlag); err != nil {
			return start, end, err
		}
	}
	if endFlag != "" {
		if end, err = time.Parse(time.RFC3339, endFlag); err != nil {
			return start, end, err
		}
	}
	return start.In(split.NEM), end.In(split.NEM), nil
}
