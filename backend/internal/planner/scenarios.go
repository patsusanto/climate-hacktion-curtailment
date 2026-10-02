package planner

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"climate-hacktion-curtailment/backend/internal/split"
)

// Day-shaped paths from one forecast curve. Rows are repeated copies of four
// stories. There is no draw and no smeared one-hour band. Column 0 of every
// path is the measured price, PV and load.

// LeadForecast is one lead of the forecast curve. Prices are NaN until the
// price model has filled them in. PV and load are in kW.
type LeadForecast struct {
	LeadSteps int
	PriceP10  float64
	PriceP50  float64
	PriceP90  float64
	PvKw      float64
	PvLo      float64
	PvHi      float64
	LoadKw    float64
	LoadLo    float64
	LoadHi    float64
}

// ForecastCurve is the forecast at each trained lead.
type ForecastCurve struct {
	Leads []LeadForecast
}

// Group names the four stories and how many of the 21 paths each stands for.
type Group struct {
	Name  string
	Count int
}

// Groups is the row order in the batch. Counts sum to split.NScenarios.
var Groups = [...]Group{
	{"mid", 7},
	{"bright", 5},
	{"dull", 5},
	{"spike", 4},
}

const sixHours = 6 * split.StepsPerHour

// Story is one path over the planning grid.
type Story struct {
	Name        string
	Count       int
	PriceAudMwh []float64
	PvKw        []float64
	LoadKw      []float64
}

// Stories interpolates the curve into the four stories. grid is "stages" (one
// column per planner stage) or "intervals" (every 5-minute step). The
// returned offsets are the column offsets in 5-minute steps.
func Stories(curve ForecastCurve, measuredPrice, measuredPv, measuredLoad float64, horizonHours int, grid string) ([]Story, []int, error) {
	total := 0
	for _, g := range Groups {
		total += g.Count
	}
	if total != split.NScenarios {
		return nil, nil, errors.New("scenario groups must sum to N_SCENARIOS")
	}
	leads := append([]LeadForecast(nil), curve.Leads...)
	sort.SliceStable(leads, func(i, j int) bool { return leads[i].LeadSteps < leads[j].LeadSteps })
	if len(leads) == 0 {
		return nil, nil, errors.New("forecast curve has no leads")
	}
	for _, l := range leads {
		if math.IsNaN(l.PriceP10) || math.IsNaN(l.PriceP50) || math.IsNaN(l.PriceP90) {
			return nil, nil, fmt.Errorf("lead %d is missing a price quantile", l.LeadSteps)
		}
	}

	var offsets []int
	switch grid {
	case "stages":
		var err error
		if offsets, err = StageEnds(horizonHours); err != nil {
			return nil, nil, err
		}
	case "intervals":
		for i := 0; i < horizonHours*split.StepsPerHour; i++ {
			offsets = append(offsets, i)
		}
	default:
		return nil, nil, errors.New("grid must be 'stages' or 'intervals'")
	}
	hours := make([]float64, len(offsets))
	for i, o := range offsets {
		hours[i] = float64(o) / float64(split.StepsPerHour)
	}

	knotHours := []float64{0.0}
	var knotLeads []LeadForecast
	limit := horizonHours * split.StepsPerHour
	for _, l := range leads {
		if l.LeadSteps <= 0 || l.LeadSteps > limit {
			continue
		}
		knotHours = append(knotHours, float64(l.LeadSteps)/float64(split.StepsPerHour))
		knotLeads = append(knotLeads, l)
	}
	if len(knotLeads) == 0 {
		return nil, nil, errors.New("no lead falls inside the horizon")
	}

	measured := [3]float64{measuredPrice, measuredPv, measuredLoad}
	stories := make([]Story, len(Groups))
	for gi, g := range Groups {
		knots := storyKnots(knotLeads, g.Name)
		s := Story{
			Name:        g.Name,
			Count:       g.Count,
			PriceAudMwh: make([]float64, len(hours)),
			PvKw:        make([]float64, len(hours)),
			LoadKw:      make([]float64, len(hours)),
		}
		for col, dst := range [3][]float64{s.PriceAudMwh, s.PvKw, s.LoadKw} {
			fp := make([]float64, 0, len(knots)+1)
			fp = append(fp, measured[col])
			for _, k := range knots {
				fp = append(fp, k[col])
			}
			for i, h := range hours {
				dst[i] = NpInterp(h, knotHours, fp)
			}
			dst[0] = measured[col] // column 0 is the measurement
		}
		stories[gi] = s
	}
	return stories, offsets, nil
}

// storyKnots are the knot values after hour 0, as (price, pv, load) per lead.
func storyKnots(leads []LeadForecast, name string) [][3]float64 {
	rows := make([][3]float64, len(leads))
	for i, l := range leads {
		switch name {
		case "mid":
			rows[i] = [3]float64{l.PriceP50, l.PvKw, l.LoadKw}
		case "bright":
			rows[i] = [3]float64{l.PriceP10, l.PvHi, l.LoadKw}
		case "dull":
			rows[i] = [3]float64{l.PriceP50, l.PvLo, l.LoadHi}
		case "spike":
			price := l.PriceP50
			if l.LeadSteps >= sixHours {
				price = l.PriceP90
			}
			rows[i] = [3]float64{price, l.PvKw, l.LoadKw}
		}
	}
	return rows
}

// BuildScenarios interpolates the curve and repeats the four stories. With
// dedupe, each story appears once, weighted by its count; the planner's
// expected score is the same and it runs about five times faster.
func BuildScenarios(
	curve ForecastCurve,
	measuredPrice, measuredPv, measuredLoad float64,
	now time.Time,
	horizonHours int,
	grid string,
	dedupe bool,
) (ScenarioBatch, error) {
	stories, offsets, err := Stories(curve, measuredPrice, measuredPv, measuredLoad, horizonHours, grid)
	if err != nil {
		return ScenarioBatch{}, err
	}
	var batch ScenarioBatch
	for _, s := range stories {
		copies := s.Count
		if dedupe {
			copies = 1
		}
		for c := 0; c < copies; c++ {
			batch.PriceAudMwh = append(batch.PriceAudMwh, append([]float64(nil), s.PriceAudMwh...))
			batch.PvKw = append(batch.PvKw, append([]float64(nil), s.PvKw...))
			batch.LoadKw = append(batch.LoadKw, append([]float64(nil), s.LoadKw...))
		}
		if dedupe {
			batch.Weights = append(batch.Weights, float64(s.Count))
		}
	}
	batch.Timestamps = make([]time.Time, len(offsets))
	for i, o := range offsets {
		batch.Timestamps[i] = now.Add(time.Duration(split.StepMinutes*o) * time.Minute)
	}
	return batch, nil
}

// BuildPointPath is one path: the middle story. Price is P50, and PV and load
// are the point forecasts.
func BuildPointPath(
	curve ForecastCurve,
	measuredPrice, measuredPv, measuredLoad float64,
	now time.Time,
	horizonHours int,
	grid string,
) (ScenarioBatch, error) {
	batch, err := BuildScenarios(curve, measuredPrice, measuredPv, measuredLoad, now, horizonHours, grid, false)
	if err != nil {
		return ScenarioBatch{}, err
	}
	return ScenarioBatch{
		Timestamps:  batch.Timestamps,
		PriceAudMwh: batch.PriceAudMwh[:1],
		PvKw:        batch.PvKw[:1],
		LoadKw:      batch.LoadKw[:1],
	}, nil
}

// NpInterp is numpy.interp for increasing xp: clamped at both ends and linear
// in between, using numpy's own slope arithmetic.
func NpInterp(x float64, xp, fp []float64) float64 {
	n := len(xp)
	if x < xp[0] {
		return fp[0]
	}
	if x > xp[n-1] {
		return fp[n-1]
	}
	// j is the last index with xp[j] <= x
	j := sort.Search(n, func(i int) bool { return xp[i] > x }) - 1
	if j == n-1 || xp[j] == x {
		return fp[j]
	}
	slope := (fp[j+1] - fp[j]) / (xp[j+1] - xp[j])
	res := slope*(x-xp[j]) + fp[j]
	if math.IsNaN(res) {
		res = slope*(x-xp[j+1]) + fp[j+1]
	}
	return res
}
