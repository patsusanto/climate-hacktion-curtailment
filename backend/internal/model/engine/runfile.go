package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"climate-hacktion-curtailment/backend/internal/model/house"
	"climate-hacktion-curtailment/backend/internal/model/split"
	"climate-hacktion-curtailment/backend/internal/wire"
)

// BuildRunFile packages a finished replay in the on-disk format the service
// loads from backend/runs/<run_id>.json.
func BuildRunFile(id, windowName string, cfg Config, res *Result) wire.RunFile {
	b := cfg.Spec.Battery
	steps := make(map[string]wire.StepDecision, len(res.Steps))
	for k, d := range res.Steps {
		steps[strconv.Itoa(k)] = d
	}
	return wire.RunFile{
		RunID:      id,
		WindowName: windowName,
		Meta: wire.Meta{
			Assumptions: wire.Assumptions{
				PriceRegion:  "NSW1",
				PriceSource:  "historical_spot",
				Roof:         "synthetic_clear_sky_scaled_by_pv_kw_ac",
				Load:         "evening_peak_synthetic",
				AddressLabel: "",
				Lat:          -33.87,
				Lon:          151.21,
				Note: "Solar kilowatts scale the synthetic roof. Demand is the 15 kWh evening-peak shape. " +
					"Prices are historical NSW1 spot.",
			},
			Spec: wire.Spec{
				PvKwAc:               cfg.Spec.PvKwAc,
				ExportCapKw:          cfg.Spec.ExportCapKw,
				BatteryKwh:           b.CapacityKwh,
				BatteryKw:            b.MaxPowerKw,
				UsableKwh:            round(b.SocMaxKwh()-b.SocMinKwh(), 4),
				DailyLoadKwh:         cfg.Spec.DailyLoadKwh,
				DegradationAudPerKwh: cfg.Spec.DegradationAudPerKwh,
			},
			Window: wire.Window{
				Start:       stamp(res.Start),
				End:         stamp(res.End),
				StepMinutes: split.StepMinutes,
				N:           len(res.Ticks),
			},
		},
		Ticks:   res.Ticks,
		Summary: res.Summary,
		Steps:   steps,
	}
}

// WriteRunFile writes the run as compact JSON, replacing any existing file
// only once the new one is complete.
func WriteRunFile(dir string, run wire.RunFile) (string, error) {
	if run.RunID == "" {
		return "", fmt.Errorf("run needs an id")
	}
	raw, err := json.Marshal(run)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, run.RunID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

// DefaultRunID names a run after its sizes, for example "10kw-10kwh".
func DefaultRunID(spec house.HouseSpec) string {
	return fmt.Sprintf("%gkw-%gkwh", spec.PvKwAc, spec.Battery.CapacityKwh)
}
