// Package fetch downloads the public data the models need for a window of days. No API keys.
//
// It writes four CSV files (times in NEM time, UTC+10):
//
//	prices.csv            NSW1 5-minute price and demand (AEMO)
//	predispatch.csv       AEMO pre-dispatch runs for NSW1: when each was published, and its forecasts
//	weather_forecast.csv  day-ahead weather forecasts for Sydney, Dubbo and Goulburn (Open-Meteo)
//	weather_observed.csv  observed Sydney irradiance, temperature and cloud, for the house (Open-Meteo)
//
// The models read 8 days of history before the first day and 8 hours after the last, so the
// window fetched is wider than the one asked for.
package fetch

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var nem = time.FixedZone("NEM", 10*3600)

const (
	HistoryDays = 9 // the models read 8 days back; one more day of margin
	dateLayout  = "2006-01-02"
)

// Files are the CSVs a complete download contains.
var Files = []string{"prices.csv", "weather_forecast.csv", "weather_observed.csv", "predispatch.csv"}

// Days downloads everything for the days first..last (NEM dates) into dir, logging to log.
// Pre-dispatch comes in weekly archives of about 125 MB; skipPredispatch leaves it out and the
// price model then runs without it (less accurate).
func Days(first, last time.Time, dir string, skipPredispatch bool, log io.Writer) error {
	first = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, nem)
	last = time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, nem)
	if last.Before(first) {
		return fmt.Errorf("fetch: last day is before the first")
	}
	from := first.AddDate(0, 0, -HistoryDays)
	to := last.AddDate(0, 0, 2) // the last decision plus 8 hours, with a margin
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	steps := []struct {
		name string
		run  func(from, to time.Time, path string) error
	}{
		{"prices.csv", fetchPrices},
		{"weather_forecast.csv", fetchWeatherForecast},
		{"weather_observed.csv", fetchWeatherObserved},
	}
	if !skipPredispatch {
		steps = append(steps, struct {
			name string
			run  func(from, to time.Time, path string) error
		}{"predispatch.csv", func(_, _ time.Time, path string) error {
			return fetchPredispatch(first.AddDate(0, 0, -1), last.AddDate(0, 0, 1), path)
		}})
	}
	for _, s := range steps {
		fmt.Fprintf(log, "fetch %s %s..%s\n", s.name, first.Format(dateLayout), last.Format(dateLayout))
		path := filepath.Join(dir, s.name)
		if err := s.run(from, to, path+".part"); err != nil {
			os.Remove(path + ".part")
			return fmt.Errorf("fetch %s: %w", s.name, err)
		}
		if err := os.Rename(path+".part", path); err != nil {
			return err
		}
	}
	return nil
}
