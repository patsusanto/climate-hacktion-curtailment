// Command genrun replays a window of history with the trained models and writes the run the
// playground service serves.
//
//	go run ./cmd/genrun -window validation -pv 6.6 -battery-kwh 13.5 -load 18
//
// It downloads the window's public data (AEMO prices and pre-dispatch, Open-Meteo weather) into
// -data the first time, then reuses it.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/battery"
	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/fetch"
	"climate-hacktion-curtailment/backend/internal/model/forecast"
	"climate-hacktion-curtailment/backend/internal/model/planner"
	"climate-hacktion-curtailment/backend/internal/model/runfile"
	"climate-hacktion-curtailment/backend/internal/model/simulate"
)

// Windows are the named replay windows (see data.Windows).
var Windows = data.Windows

func main() {
	if err := run(os.Args[1:], os.Stdout, log.Default()); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdout io.Writer, logger *log.Logger) error {
	fs := flag.NewFlagSet("genrun", flag.ContinueOnError)
	var (
		dataDir     = fs.String("data", "data", "where to download the public data (one folder per window)")
		outDir      = fs.String("out", "runs", "directory to write <run_id>.json into")
		window      = fs.String("window", "validation", `"validation" or "test"`)
		runID       = fs.String("id", "", `run id (default "<pv>kw-<battery>kwh")`)
		pv          = fs.Float64("pv", 10.5, "solar size, kW AC")
		batteryKwh  = fs.Float64("battery-kwh", 10, "battery capacity, kWh")
		batteryKw   = fs.Float64("battery-kw", 5, "battery power, kW")
		load        = fs.Float64("load", 15, "daily load, kWh")
		exportCap   = fs.Float64("export-cap", 5, "export cap, kW")
		detailEvery = fs.Int("detail-every", 12, "keep forecast detail for every Nth step")
		wear        = fs.Float64("wear", 0.05, "battery wear, AUD per kWh moved: the planner weighs cycling against it, and the bills include it")
		noPD        = fs.Bool("no-predispatch", false, "skip AEMO pre-dispatch (~125 MB a week); the price model is less accurate without it")
		curtail     = fs.String("curtail", string(planner.Economic), `"economic" or "forced_only" (never clip solar by choice)`)
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	bounds, ok := Windows[*window]
	if !ok {
		return fmt.Errorf(`window must be "validation" or "test"`)
	}
	start, end := bounds[0], bounds[1]
	spec, err := battery.NewSpec(*pv, *batteryKwh, *batteryKw, *exportCap, *load, 0)
	if err != nil {
		return err
	}

	t0 := time.Now()
	dir := filepath.Join(*dataDir, *window)
	if *noPD {
		dir += "-nopd"
	}
	if !complete(dir, *noPD) {
		logger.Printf("downloading %s .. %s into %s", start.Format("2006-01-02"), end.Format("2006-01-02"), dir)
		if err := fetch.Days(start, end, dir, *noPD, logger.Writer()); err != nil {
			return err
		}
	}
	models, err := forecast.Load()
	if err != nil {
		return err
	}
	d, err := data.Load(dir)
	if err != nil {
		return err
	}
	in, err := models.Prepare(d)
	if err != nil {
		return err
	}
	logger.Printf("loaded the data and models in %s", time.Since(t0).Round(time.Millisecond))

	t1 := time.Now()
	opt := simulate.DefaultOptions()
	opt.Curtail = planner.Curtail(*curtail)
	opt.WearAUDPerKWh = *wear
	res, err := simulate.Run(models, in, spec, start, end, opt, nil)
	if err != nil {
		return err
	}
	logger.Printf("replayed %d steps in %s", len(res.Steps), time.Since(t1).Round(time.Millisecond))

	// The payback figures need a year of data: data/year, built once (see the README).
	if yearDir := filepath.Join(*dataDir, "year"); data.Exists(filepath.Join(yearDir, "prices.csv")) {
		t2 := time.Now()
		yd, err := data.Load(yearDir)
		if err != nil {
			return err
		}
		year, err := models.Prepare(yd)
		if err != nil {
			return err
		}
		a, err := simulate.RunAnnual(models, year, spec, data.Year[0], data.Year[1], simulate.AnnualWeeks, opt)
		if err != nil {
			return err
		}
		res.Annual = &a
		logger.Printf("replayed %d weeks of the year for the payback in %s", a.Weeks, time.Since(t2).Round(time.Millisecond))
	} else {
		logger.Printf("no data in %s: the run has no payback figures", yearDir)
	}

	id := *runID
	if id == "" {
		id = defaultID(*pv, *batteryKwh)
	}
	file, err := runfile.Build(res, runfile.Options{ID: id, WindowName: *window, DetailEvery: *detailEvery,
		TrainedBefore: models.TrainedBefore()})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	path, err := runfile.Write(*outDir, file)
	if err != nil {
		return err
	}
	s := file.Summary
	fmt.Fprintf(stdout, "wrote %s\n  self-consumption bill %.2f   planner bill %.2f   savings %.2f   with wear %.2f   supply %.2f\n",
		path, s.SelfConsumption.BillAud, s.Planner.BillAud, s.SavingsAud, s.SavingsWithWearAud, s.SupplyAud)
	return nil
}

func defaultID(pv, batteryKwh float64) string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return f(pv) + "kw-" + f(batteryKwh) + "kwh"
}

func complete(dir string, skipPredispatch bool) bool {
	for _, f := range fetch.Files {
		if skipPredispatch && f == "predispatch.csv" {
			continue
		}
		if !data.Exists(filepath.Join(dir, f)) {
			return false
		}
	}
	return true
}
