// Package planner is the expected-cash dynamic program and the forecast
// scenarios it plans over. It mirrors app/planner/ and app/forecast/scenarios.py.
package planner

import (
	"errors"

	"climate-hacktion-curtailment/backend/internal/split"
)

// StageEnds are the offsets, in 5-minute steps, where a new action may be
// chosen: every step of the first hour, then one per hour.
// horizonHours 8 gives 19 stages and horizonHours 2 gives 13.
func StageEnds(horizonHours int) ([]int, error) {
	if horizonHours < 1 {
		return nil, errors.New("horizon_hours must be an integer >= 1")
	}
	out := make([]int, 0, split.StepsPerHour+horizonHours)
	for i := 0; i < split.StepsPerHour; i++ {
		out = append(out, i)
	}
	for i := split.StepsPerHour; i < horizonHours*split.StepsPerHour; i += split.StepsPerHour {
		out = append(out, i)
	}
	return out, nil
}
