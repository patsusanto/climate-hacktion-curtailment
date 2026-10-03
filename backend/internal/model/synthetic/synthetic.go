// Package synthetic writes a small made-up download (the CSVs fetch writes) for tests that must
// not touch the network: a flat price with an evening peak, and clear-sky-ish weather.
package synthetic

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Write writes prices.csv, weather_forecast.csv and weather_observed.csv to dir covering
// [from, to] (no pre-dispatch).
func Write(dir string, from, to time.Time) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var prices strings.Builder
	prices.WriteString("time,price_aud_mwh,demand_mw\n")
	for t := from; !t.After(to); t = t.Add(5 * time.Minute) {
		price := 90.0
		if h := t.Hour(); h >= 17 && h < 21 {
			price = 300
		} else if h >= 10 && h < 14 {
			price = -20
		}
		fmt.Fprintf(&prices, "%s,%g,8000\n", t.Format(time.RFC3339), price)
	}
	var fc, obs strings.Builder
	fc.WriteString("time,fc_sydney_temperature_2m,fc_sydney_apparent_temperature,fc_sydney_shortwave_radiation,fc_sydney_cloud_cover,fc_dubbo_shortwave_radiation,fc_dubbo_cloud_cover,fc_goulburn_wind_speed_100m\n")
	obs.WriteString("time,ghi,temperature,cloud_cover\n")
	for t := from.Truncate(time.Hour); !t.After(to.Add(time.Hour)); t = t.Add(time.Hour) {
		h := float64(t.Hour())
		ghi := math.Max(0, 700*math.Sin(math.Pi*(h-6)/13))
		temp := 14 + 6*math.Sin(math.Pi*(h-8)/12)
		fmt.Fprintf(&fc, "%s,%.1f,%.1f,%.0f,20,%.0f,10,25\n", t.Format(time.RFC3339), temp, temp-2, ghi, ghi)
		fmt.Fprintf(&obs, "%s,%.0f,%.1f,20\n", t.Format(time.RFC3339), ghi, temp)
	}
	for name, body := range map[string]string{"prices.csv": prices.String(), "weather_forecast.csv": fc.String(), "weather_observed.csv": obs.String()} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}
