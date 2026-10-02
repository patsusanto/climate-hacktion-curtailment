package fetch

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type site struct {
	name     string
	lat, lon float64
	vars     []string
}

// The forecast sites and variables the price and PV/load models were trained on.
var forecastSites = []site{
	{"sydney", -33.87, 151.21, []string{"temperature_2m", "apparent_temperature", "shortwave_radiation", "cloud_cover"}},
	{"dubbo", -32.25, 148.60, []string{"shortwave_radiation", "cloud_cover"}},
	{"goulburn", -34.75, 149.72, []string{"wind_speed_100m"}},
}

// The house is built from observed weather at the roof.
var roof = site{"sydney", -33.8688, 151.2093, []string{"shortwave_radiation", "temperature_2m", "cloud_cover"}}

const (
	previousRuns = "https://previous-runs-api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&start_date=%s&end_date=%s&hourly=%s&timezone=Australia%%2FBrisbane"
	archive      = "https://archive-api.open-meteo.com/v1/archive?latitude=%.4f&longitude=%.4f&start_date=%s&end_date=%s&hourly=%s&timezone=Australia%%2FBrisbane"
)

type hourly struct {
	Hourly map[string]json.RawMessage `json:"hourly"`
}

func openMeteo(url string) (times []string, values map[string][]*float64, err error) {
	body, err := get(url)
	if err != nil {
		return nil, nil, err
	}
	var h hourly
	if err := json.Unmarshal(body, &h); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(h.Hourly["time"], &times); err != nil {
		return nil, nil, fmt.Errorf("open-meteo: %w (%s)", err, strings.TrimSpace(string(body[:min(200, len(body))])))
	}
	values = map[string][]*float64{}
	for k, raw := range h.Hourly {
		if k == "time" {
			continue
		}
		var v []*float64
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, nil, err
		}
		values[k] = v
	}
	return times, values, nil
}

// fetchWeatherForecast writes, per hour, the forecast issued at least a day before that hour
// (Open-Meteo's *_previous_day1), as columns fc_<site>_<variable>.
func fetchWeatherForecast(from, to time.Time, path string) error {
	header := []string{"time"}
	columns := map[string][]*float64{}
	var times []string
	for _, s := range forecastSites {
		names := make([]string, len(s.vars))
		for i, v := range s.vars {
			names[i] = v + "_previous_day1"
		}
		t, values, err := openMeteo(fmt.Sprintf(previousRuns, s.lat, s.lon, from.Format(dateLayout), to.Format(dateLayout), strings.Join(names, ",")))
		if err != nil {
			return err
		}
		times = t
		for i, v := range s.vars {
			col := "fc_" + s.name + "_" + v
			header = append(header, col)
			columns[col] = values[names[i]]
		}
	}
	return writeHourly(path, header, times, columns)
}

// fetchWeatherObserved writes observed hourly irradiance (W/m2, mean of the hour ending at the
// stamp), temperature and cloud at the roof. The archive runs a few days behind real time.
func fetchWeatherObserved(from, to time.Time, path string) error {
	times, values, err := openMeteo(fmt.Sprintf(archive, roof.lat, roof.lon, from.Format(dateLayout), to.Format(dateLayout), strings.Join(roof.vars, ",")))
	if err != nil {
		return err
	}
	columns := map[string][]*float64{"ghi": values["shortwave_radiation"], "temperature": values["temperature_2m"], "cloud_cover": values["cloud_cover"]}
	return writeHourly(path, []string{"time", "ghi", "temperature", "cloud_cover"}, times, columns)
}

func writeHourly(path string, header []string, times []string, columns map[string][]*float64) error {
	return writeCSV(path, header, len(times), func(i int) []string {
		t, _ := time.ParseInLocation("2006-01-02T15:04", times[i], nem)
		row := []string{t.Format(time.RFC3339)}
		for _, col := range header[1:] {
			v := columns[col]
			if i < len(v) && v[i] != nil {
				row = append(row, fmt.Sprintf("%g", *v[i]))
			} else {
				row = append(row, "")
			}
		}
		return row
	})
}
