// Package split holds the shared constants of the household model: the train
// and test dates, the forecast leads and the planner grid sizes. It mirrors
// app/split.py.
package split

import "time"

// NEM is the market clock: a fixed UTC+10 offset with no daylight saving.
var NEM = time.FixedZone("NEM", 10*3600)

func mustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.In(NEM)
}

var (
	TrainStart = mustParse("2025-09-13T00:00:00+10:00")
	TrainEnd   = mustParse("2026-08-18T23:55:00+10:00")
	TestStart  = mustParse("2026-08-19T00:00:00+10:00")
	TestEnd    = mustParse("2026-09-09T23:55:00+10:00")
	// Price models also learn from the year before TrainStart.
	PriceHistoryStart = mustParse("2024-10-02T00:00:00+10:00")
)

// Leads are the forecast horizons in 5-minute steps: 1h, 2h, 3h, 6h, 8h.
var Leads = [...]int{12, 24, 36, 72, 96}

const (
	OptionalLead   = 144 // 12h, not trained until a sweep asks
	ValidationFrac = 0.1
	NScenarios     = 21
	SocBins        = 17

	StepsPerHour = 12
	StepMinutes  = 5
	DaySteps     = 288
	WeekSteps    = 7 * DaySteps
)
