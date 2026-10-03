// Command stats runs the planner on many randomised sites, on data the model never saw, and
// reports how much it saves.
//
//	go run ./analysis/stats -n 1000 -out analysis/stats/results/stats_1000.csv     (from backend/; about 7 minutes)
//
// See README.md next to this file for the results and how to read them.
//
// Three kinds of site, each with sizes and demand drawn from realistic ranges, each run over a
// random 4-week period between 1 Dec 2025 and 18 Sep 2026:
//
//	house       the model's household demand profile, Ausgrid residential tariff (EA025) + spot
//	industrial  a business's daytime demand, Ausgrid business tariff (EA225) + spot; no demand charges
//	utility     a solar farm with a battery and no demand of its own, selling at the spot price
//
// Every bill or revenue with a battery includes battery wear.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/battery"
	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/forecast"
	"climate-hacktion-curtailment/backend/internal/model/simulate"
)

// site is one randomised run.
type site struct {
	Kind                         string
	Start                        time.Time
	PVkW, BatteryKWh, BatteryKW  float64
	ExportCapKW, DailyLoadKWh    float64
	Wear                         float64 // $ per kWh moved through the battery
	load                         []float64
	shape                        string // how the demand was drawn, for the CSV
}

// result is what a run cost (house, industrial) or earned (utility), each way of running it.
type result struct {
	site
	Days int
	// Costs in $ over the period (negative: earned). Battery wear is included where there is one.
	NoSystem, SolarOnly, Default, Planner float64
	PlannerThroughput, DefaultThroughput float64
	err                                  error
}

func main() {
	n := flag.Int("n", 1000, "runs per kind of site")
	days := flag.Int("days", 28, "length of each run")
	seed := flag.Int64("seed", 1, "random seed (the same seed gives the same sites)")
	dir := flag.String("data", "data/year", "data covering 1 Dec 2025 - 18 Sep 2026 plus 9 days before")
	out := flag.String("out", "analysis/stats/results/stats.csv", "CSV of every run")
	flag.Parse()

	t0 := time.Now()
	m, err := forecast.Load()
	check(err)
	d, err := data.Load(*dir)
	check(err)
	in, err := m.Prepare(d)
	check(err)
	from, to := data.Year[0], data.Year[1].AddDate(0, 0, -*days).Add(5*time.Minute)
	if _, err := m.Run(in, in.Index(data.Year[0]), in.Index(data.Year[1])); err != nil { // forecast once
		check(err)
	}
	log.Printf("loaded the model and data in %s; the model was trained on data before %s", time.Since(t0).Round(time.Second), m.TrainedBefore())

	rng := rand.New(rand.NewSource(*seed))
	var sites []site
	for _, kind := range []string{"house", "industrial", "utility"} {
		for i := 0; i < *n; i++ {
			sites = append(sites, draw(rng, kind, in, from, to))
		}
	}

	results := make([]result, len(sites))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var done int64
	var mu sync.Mutex
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = run(m, in, sites[i], *days)
				mu.Lock()
				done++
				if done%100 == 0 {
					log.Printf("%d of %d runs", done, len(sites))
				}
				mu.Unlock()
			}
		}()
	}
	for i := range sites {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	log.Printf("ran %d sites in %s", len(sites), time.Since(t0).Round(time.Second))

	check(writeCSV(*out, results))
	report(results)
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func uniform(r *rand.Rand, lo, hi float64) float64 { return lo + r.Float64()*(hi-lo) }

func pick(r *rand.Rand, values []float64, weights []float64) float64 {
	x := r.Float64()
	for i, w := range weights {
		if x < w {
			return values[i]
		}
		x -= w
	}
	return values[len(values)-1]
}

// draw makes one random site of a kind, with a random start day.
func draw(r *rand.Rand, kind string, in *forecast.Inputs, from, to time.Time) site {
	span := int(to.Sub(from).Hours() / 24)
	s := site{Kind: kind, Start: from.AddDate(0, 0, r.Intn(span+1))}
	switch kind {
	case "house":
		s.PVkW = math.Round(uniform(r, 3, 13)*10) / 10
		s.BatteryKWh = pick(r, []float64{5, 10, 13.5, 16, 20}, []float64{0.15, 0.25, 0.3, 0.15, 0.15})
		s.BatteryKW = math.Min(5, s.BatteryKWh/2)
		if s.BatteryKWh >= 16 {
			s.BatteryKW = 10
		}
		s.ExportCapKW = pick(r, []float64{0, 1.5, 5, 10}, []float64{0.1, 0.1, 0.6, 0.2})
		s.DailyLoadKWh = math.Round(uniform(r, 8, 30))
		s.Wear = 0.05
		s.shape = "household"
	case "industrial":
		s.DailyLoadKWh = math.Round(uniform(r, 300, 3000))
		s.PVkW = math.Round(s.DailyLoadKWh * uniform(r, 0.05, 0.25))
		s.BatteryKWh = math.Round(s.DailyLoadKWh * uniform(r, 0.05, 0.3))
		s.BatteryKW = s.BatteryKWh / 2
		s.ExportCapKW = s.PVKWCap(pick(r, []float64{0, 0.5, 1}, []float64{0.3, 0.4, 0.3}))
		s.Wear = 0.04
		s.load, s.shape = business(r, in, s.DailyLoadKWh)
	case "utility":
		s.PVkW = math.Round(uniform(r, 5000, 50000))
		s.BatteryKW = math.Round(s.PVkW * uniform(r, 0.25, 1))
		s.BatteryKWh = math.Round(s.BatteryKW * uniform(r, 1, 2) / 0.8) // 1-2 hours of usable energy (10-90%)
		s.ExportCapKW = math.Round(s.PVkW * uniform(r, 0.8, 1))           // the grid connection
		s.Wear = 0.03                                                       // grid-scale cells cost less per kWh
		s.load = make([]float64, in.N())
		s.shape = "none"
	}
	return s
}

func (s site) PVKWCap(share float64) float64 { return math.Round(s.PVkW * share) }

// business draws a daytime business's demand (kW) on the data's clock: a base load overnight,
// a working day of random hours, a lighter weekend, air conditioning on hot working hours, and
// a little random variation. It averages dailyKWh a day.
func business(r *rand.Rand, in *forecast.Inputs, dailyKWh float64) ([]float64, string) {
	base := uniform(r, 0.25, 0.45)    // overnight, as a share of the working-day level
	open := uniform(r, 6, 9)          // working hours
	shut := uniform(r, 16, 20)
	weekend := uniform(r, 0.2, 0.7)   // weekend activity, as a share of a working day's
	cooling := uniform(r, 0.01, 0.04) // extra per degree above 22 C in working hours, as a share
	n := in.N()
	shape := make([]float64, n)
	working := make([]bool, n)
	sum := 0.0
	for i := range shape {
		t := in.Time(i).Add(-5 * time.Minute)
		h := float64(t.Hour()) + float64(t.Minute())/60
		on := h >= open && h < shut
		weekday := t.Weekday() != time.Saturday && t.Weekday() != time.Sunday
		v := base
		switch {
		case on && weekday:
			v = 1
		case on:
			v = base + (1-base)*weekend
		}
		working[i] = on && weekday
		shape[i] = v
		sum += v
	}
	scale := dailyKWh / (sum / float64(n) * 24) // kW for the shape's level of 1
	load := make([]float64, n)
	noise := 0.0
	for i := range load {
		noise = 0.9*noise + r.NormFloat64()*0.04
		kw := shape[i] * scale * (1 + noise)
		if working[i] {
			temp := in.Observed.Interp("temperature", in.Time(i).Unix(), false)
			kw += cooling * scale * math.Max(0, temp-22)
		}
		load[i] = math.Max(kw, 0.05*scale)
	}
	return load, fmt.Sprintf("open %.1f-%.1f, base %.0f%%, weekend %.0f%%, cooling %.1f%%/C", open, shut, base*100, weekend*100, cooling*100)
}

func run(m *forecast.Models, in *forecast.Inputs, s site, days int) result {
	res := result{site: s}
	spec, err := battery.NewSpec(s.PVkW, s.BatteryKWh, s.BatteryKW, s.ExportCapKW, math.Max(s.DailyLoadKWh, 1), 0)
	if err != nil {
		res.err = err
		return res
	}
	opt := simulate.DefaultOptions()
	opt.WearAUDPerKWh = s.Wear
	opt.Load = s.load
	switch s.Kind {
	case "industrial":
		opt.Tariff = battery.AusgridBusiness
	case "utility":
		opt.Tariff = battery.Wholesale
	}
	end := s.Start.AddDate(0, 0, days).Add(-5 * time.Minute)
	sim, err := simulate.Run(m, in, spec, s.Start, end, opt, nil)
	if err != nil {
		res.err = err
		return res
	}
	res.Days = sim.Planner.Days
	supply := float64(res.Days) * opt.Tariff.SupplyAUDPerDay
	res.Planner = sim.Planner.BillAUD + s.Wear*sim.Planner.Throughput()
	res.PlannerThroughput = sim.Planner.Throughput()

	// The baselines, on the same intervals and prices.
	res.NoSystem = supply
	res.SolarOnly = supply
	sched := &baseline{spec: spec, soc: spec.InitialSOC(), wear: s.Wear, clipNegative: s.Kind == "utility"}
	solar := &baseline{spec: spec, soc: spec.InitialSOC(), clipNegative: s.Kind == "utility"}
	for _, st := range sim.Steps {
		res.NoSystem += st.LoadkW * battery.IntervalHours * st.Prices.Import
		res.SolarOnly += solar.step(battery.Hold, st)
		if s.Kind == "utility" {
			// A solar farm's battery without forecasts: charge from the sun 10 am - 3 pm, sell 5 - 9 pm.
			h := st.Time.Add(-5 * time.Minute).Hour()
			action := battery.Hold
			switch {
			case h >= 10 && h < 15:
				action = battery.ChargeSurplus
			case h >= 17 && h < 21:
				action = battery.Discharge
			}
			sched.add(sched.step(action, st))
		}
	}
	if s.Kind == "utility" {
		res.Default = sched.total + supply
		res.DefaultThroughput = sched.throughput
	} else {
		// The battery on its own default setting: store spare solar, run the site from it.
		res.Default = sim.Self.BillAUD + s.Wear*sim.Self.Throughput()
		res.DefaultThroughput = sim.Self.Throughput()
	}
	return res
}

// baseline settles a simple rule, interval by interval.
type baseline struct {
	spec         battery.Spec
	soc          float64
	wear         float64
	clipNegative bool // a solar farm stops exporting at a negative price
	total        float64
	throughput   float64
}

func (b *baseline) step(action battery.Action, st simulate.Step) float64 {
	s, err := battery.Apply(b.soc, action, st.PVkW, st.LoadkW, b.spec)
	if err != nil {
		s, _ = battery.Apply(b.soc, battery.Hold, st.PVkW, st.LoadkW, b.spec)
	}
	b.soc = s.SOC
	exp := s.GridExport * st.Prices.Export
	if b.clipNegative && st.Prices.Export < 0 {
		exp = 0
	}
	moved := s.ChargeAC + s.DischargeAC
	b.throughput += moved
	return s.GridImport*st.Prices.Import - exp + b.wear*moved
}

func (b *baseline) add(v float64) { b.total += v }

func writeCSV(path string, rs []result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"kind", "start", "days", "pv_kw", "battery_kwh", "battery_kw", "export_cap_kw", "daily_load_kwh", "demand",
		"cost_no_system_aud", "cost_solar_only_aud", "cost_default_aud", "cost_planner_aud",
		"throughput_default_kwh", "throughput_planner_kwh", "error"})
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
	for _, r := range rs {
		e := ""
		if r.err != nil {
			e = r.err.Error()
		}
		w.Write([]string{r.Kind, r.Start.Format("2006-01-02"), strconv.Itoa(r.Days), num(r.PVkW), num(r.BatteryKWh), num(r.BatteryKW),
			num(r.ExportCapKW), num(r.DailyLoadKWh), r.shape, num(r.NoSystem), num(r.SolarOnly), num(r.Default), num(r.Planner),
			num(r.DefaultThroughput), num(r.PlannerThroughput), e})
	}
	w.Flush()
	return w.Error()
}

func quantile(v []float64, q float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	i := q * float64(len(s)-1)
	lo := int(math.Floor(i))
	hi := min(lo+1, len(s)-1)
	return s[lo] + (s[hi]-s[lo])*(i-float64(lo))
}

func line(name string, v []float64, unit string) {
	if len(v) == 0 {
		return
	}
	mean := 0.0
	for _, x := range v {
		mean += x
	}
	mean /= float64(len(v))
	fmt.Printf("  %-58s median %8.1f%s   middle 80%%: %8.1f%s to %8.1f%s   mean %8.1f%s\n", name,
		quantile(v, 0.5), unit, quantile(v, 0.1), unit, quantile(v, 0.9), unit, mean, unit)
}

func report(rs []result) {
	for _, kind := range []string{"house", "industrial", "utility"} {
		var vsDefault, vsDefaultOfBill, vsNone, perYear, perYearNone []float64
		wins, n, failed := 0, 0, 0
		for _, r := range rs {
			if r.Kind != kind {
				continue
			}
			if r.err != nil {
				failed++
				continue
			}
			n++
			if r.Planner < r.Default {
				wins++
			}
			year := 365 / float64(r.Days)
			if kind == "utility" {
				earned, def, solar := -r.Planner, -r.Default, -r.SolarOnly
				if def > 0 {
					vsDefault = append(vsDefault, 100*(earned-def)/def)
				}
				if solar > 0 {
					vsNone = append(vsNone, 100*(earned-solar)/solar)
				}
				perYear = append(perYear, (earned-def)*year/1e3)
				perYearNone = append(perYearNone, (earned-solar)*year/1e3)
				continue
			}
			if r.Default > 0 {
				vsDefault = append(vsDefault, 100*(r.Default-r.Planner)/r.Default)
			}
			vsDefaultOfBill = append(vsDefaultOfBill, 100*(r.Default-r.Planner)/r.NoSystem)
			vsNone = append(vsNone, 100*(r.NoSystem-r.Planner)/r.NoSystem)
			perYear = append(perYear, (r.Default-r.Planner)*year)
			perYearNone = append(perYearNone, (r.NoSystem-r.Planner)*year)
		}
		fmt.Printf("\n%s: %d runs (%d failed); the planner beat the default in %d (%.0f%%)\n", kind, n, failed, wins, 100*float64(wins)/math.Max(float64(n), 1))
		if kind == "utility" {
			line("revenue vs a fixed daily battery schedule", vsDefault, "%")
			line("revenue vs the solar farm with no battery", vsNone, "%")
			line("extra revenue vs the fixed schedule, $k a year", perYear, "")
			line("extra revenue vs no battery, $k a year", perYearNone, "")
			continue
		}
		line(fmt.Sprintf("bill vs battery on default setting (%d of %d had a bill > 0)", len(vsDefault), n), vsDefault, "%")
		line("saving vs default, as a share of the no-system bill", vsDefaultOfBill, "%")
		line("bill vs no solar and no battery", vsNone, "%")
		line("saving vs default, $ a year", perYear, "")
		line("saving vs no system, $ a year", perYearNone, "")
	}
}
