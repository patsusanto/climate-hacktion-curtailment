package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"climate-hacktion-curtailment/backend/internal/house"
	"climate-hacktion-curtailment/backend/internal/split"
)

// RunFile is the on-disk run the service loads from backend/runs/<run_id>.json.
// Its shape is what backend/runs.go parses: the field names must stay in step.
type RunFile struct {
	RunID      string                  `json:"run_id"`
	WindowName string                  `json:"window_name"`
	Meta       Meta                    `json:"meta"`
	Ticks      []Tick                  `json:"ticks"`
	Summary    Summary                 `json:"summary"`
	Steps      map[string]StepDecision `json:"steps"`
}

type Meta struct {
	Assumptions Assumptions `json:"assumptions"`
	Spec        Spec        `json:"spec"`
	Window      Window      `json:"window"`
}

type Assumptions struct {
	PriceRegion  string  `json:"price_region"`
	PriceSource  string  `json:"price_source"`
	Roof         string  `json:"roof"`
	Load         string  `json:"load"`
	AddressLabel string  `json:"address_label"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
	Note         string  `json:"note"`
}

type Spec struct {
	PvKwAc               float64 `json:"pv_kw_ac"`
	ExportCapKw          float64 `json:"export_cap_kw"`
	BatteryKwh           float64 `json:"battery_kwh"`
	BatteryKw            float64 `json:"battery_kw"`
	UsableKwh            float64 `json:"usable_kwh"`
	DailyLoadKwh         float64 `json:"daily_load_kwh"`
	DegradationAudPerKwh float64 `json:"degradation_aud_per_kwh"`
}

type Window struct {
	Start       string `json:"start"`
	End         string `json:"end"`
	StepMinutes int    `json:"step_minutes"`
	N           int    `json:"n"`
}

// BuildRunFile packages a finished replay for the service.
func BuildRunFile(id, windowName string, cfg Config, res *Result) RunFile {
	b := cfg.Spec.Battery
	steps := make(map[string]StepDecision, len(res.Steps))
	for k, d := range res.Steps {
		steps[strconv.Itoa(k)] = d
	}
	return RunFile{
		RunID:      id,
		WindowName: windowName,
		Meta: Meta{
			Assumptions: Assumptions{
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
			Spec: Spec{
				PvKwAc:               cfg.Spec.PvKwAc,
				ExportCapKw:          cfg.Spec.ExportCapKw,
				BatteryKwh:           b.CapacityKwh,
				BatteryKw:            b.MaxPowerKw,
				UsableKwh:            round(b.SocMaxKwh()-b.SocMinKwh(), 4),
				DailyLoadKwh:         cfg.Spec.DailyLoadKwh,
				DegradationAudPerKwh: cfg.Spec.DegradationAudPerKwh,
			},
			Window: Window{
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
func WriteRunFile(dir string, run RunFile) (string, error) {
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
