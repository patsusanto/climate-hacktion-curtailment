// Package data reads the household frames of an export (see the README).
//
// The Go port never reads parquet. An export holds plain gzip CSV files and
// this package loads them.
package data

import (
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/split"
)

// Frame is the joined history on the 5-minute market clock: PV and load in
// units (kW per kW of PV, and kW at 15 kWh/day) plus the NSW1 spot price.
type Frame struct {
	Times    []time.Time
	Price    []float64
	PvUnit   []float64
	LoadUnit []float64
}

func (f *Frame) Len() int { return len(f.Times) }

// Validate checks the columns line up and the clock is a regular 5-minute grid.
func (f *Frame) Validate() error {
	n := len(f.Times)
	if len(f.Price) != n || len(f.PvUnit) != n || len(f.LoadUnit) != n {
		return fmt.Errorf("frame columns differ in length")
	}
	for i := 1; i < n; i++ {
		if f.Times[i].Sub(f.Times[i-1]) != time.Duration(split.StepMinutes)*time.Minute {
			return fmt.Errorf("gap in the 5-minute clock at row %d (%s -> %s)", i, f.Times[i-1], f.Times[i])
		}
	}
	return nil
}

// IndexOf returns the row index of ts, or -1.
func (f *Frame) IndexOf(ts time.Time) int {
	if len(f.Times) == 0 {
		return -1
	}
	off := int(ts.Sub(f.Times[0]) / (time.Duration(split.StepMinutes) * time.Minute))
	if off < 0 || off >= len(f.Times) || !f.Times[off].Equal(ts) {
		return -1
	}
	return off
}

// Weather is the hourly day-ahead forecast, one series per column.
type Weather struct {
	Times   []time.Time
	Columns []string
	Values  [][]float64 // [column][hour]
}

// openCSV opens a plain or gzip CSV and returns a reader over its records.
func openCSV(path string) (*csv.Reader, io.Closer, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	var r io.Reader = file
	closer := io.Closer(file)
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			file.Close()
			return nil, nil, err
		}
		r = gz
		closer = multiCloser{gz, file}
	}
	cr := csv.NewReader(r)
	cr.ReuseRecord = true
	return cr, closer, nil
}

type multiCloser []io.Closer

func (m multiCloser) Close() error {
	var first error
	for _, c := range m {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

const timeLayout = "2006-01-02T15:04:05-07:00"

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.In(split.NEM), nil
}

func header(cr *csv.Reader, want ...string) (map[string]int, error) {
	row, err := cr.Read()
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i, name := range row {
		idx[name] = i
	}
	for _, name := range want {
		if _, ok := idx[name]; !ok {
			return nil, fmt.Errorf("missing column %q", name)
		}
	}
	return idx, nil
}

// LoadFrame reads a CSV with columns time, price_aud_mwh, pv_unit, load_unit.
// time is RFC 3339 with a +10:00 offset.
func LoadFrame(path string) (*Frame, error) {
	cr, closer, err := openCSV(path)
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	idx, err := header(cr, "time", "price_aud_mwh", "pv_unit", "load_unit")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f := &Frame{}
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		ts, err := parseTime(rec[idx["time"]])
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		vals := [3]float64{}
		for k, name := range [3]string{"price_aud_mwh", "pv_unit", "load_unit"} {
			vals[k], err = strconv.ParseFloat(rec[idx[name]], 64)
			if err != nil {
				return nil, fmt.Errorf("%s line %d %s: %w", path, line, name, err)
			}
		}
		f.Times = append(f.Times, ts)
		f.Price = append(f.Price, vals[0])
		f.PvUnit = append(f.PvUnit, vals[1])
		f.LoadUnit = append(f.LoadUnit, vals[2])
	}
	return f, f.Validate()
}

// LoadWeather reads a CSV with a time column plus one column per forecast
// series. time is RFC 3339 with a +10:00 offset.
func LoadWeather(path string) (*Weather, error) {
	cr, closer, err := openCSV(path)
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	head, err := cr.Read()
	if err != nil {
		return nil, err
	}
	head = append([]string(nil), head...)
	if len(head) < 2 || head[0] != "time" {
		return nil, fmt.Errorf("%s: first column must be time", path)
	}
	w := &Weather{Columns: head[1:], Values: make([][]float64, len(head)-1)}
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		ts, err := parseTime(rec[0])
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		w.Times = append(w.Times, ts)
		for c := range w.Columns {
			v, err := strconv.ParseFloat(rec[c+1], 64)
			if err != nil {
				return nil, fmt.Errorf("%s line %d %s: %w", path, line, w.Columns[c], err)
			}
			w.Values[c] = append(w.Values[c], v)
		}
	}
	return w, nil
}
