// Package data reads the CSV files written by the fetch package.
package data

import (
	"compress/gzip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// NEM is the market's clock: UTC+10, no daylight saving.
var NEM = time.FixedZone("NEM", 10*3600)

// Windows are the named replay windows: the first and last interval of each. The models were
// trained on data before the test window.
var Windows = map[string][2]time.Time{
	// Summer: December 2025 to February 2026. The models were trained on it.
	"summer":     {time.Date(2025, 12, 1, 0, 0, 0, 0, NEM), time.Date(2026, 2, 28, 23, 55, 0, 0, NEM)},
	"validation": {time.Date(2026, 7, 16, 0, 0, 0, 0, NEM), time.Date(2026, 8, 18, 23, 55, 0, 0, NEM)},
	"test":       {time.Date(2026, 8, 19, 0, 0, 0, 0, NEM), time.Date(2026, 9, 9, 23, 55, 0, 0, NEM)},
}

// Year is the full year the payback estimate replays: 19 Sep 2025 to 18 Sep 2026, every season.
// The models were trained on data before 19 Aug 2026, so most of it is data they have seen.
var Year = [2]time.Time{time.Date(2025, 9, 19, 0, 0, 0, 0, NEM), time.Date(2026, 9, 18, 23, 55, 0, 0, NEM)}

// Step is one market interval, in seconds.
const Step = 300

// Hourly is an hourly weather table (unix seconds, one column per variable).
type Hourly struct {
	T    []int64
	Cols map[string][]float64
}

// Interp returns col at t, linear between hours. Outside the table it returns NaN when
// strict, else the nearest end's value.
func (h *Hourly) Interp(col string, t int64, strict bool) float64 {
	v, ok := h.Cols[col]
	n := len(h.T)
	if !ok || n == 0 {
		return math.NaN()
	}
	if t <= h.T[0] {
		if strict && t < h.T[0] {
			return math.NaN()
		}
		return v[0]
	}
	if t >= h.T[n-1] {
		if strict && t > h.T[n-1] {
			return math.NaN()
		}
		return v[n-1]
	}
	k := sort.Search(n, func(i int) bool { return h.T[i] >= t })
	if h.T[k] == t {
		return v[k]
	}
	a, b := h.T[k-1], h.T[k]
	w := float64(t-a) / float64(b-a)
	return v[k-1] + w*(v[k]-v[k-1])
}

// Predispatch holds AEMO pre-dispatch rows sorted by publication, then period end.
type Predispatch struct {
	Published, End []int64
	RRP, Demand    []float64
}

// Data is one download: the price clock and the tables around it.
type Data struct {
	T0        int64        // unix seconds of the first price interval (interval end)
	Price     []float64    // $/MWh per 5-minute interval from T0
	WeatherFC *Hourly      // day-ahead forecasts, fc_<site>_<variable>
	Observed  *Hourly      // ghi, temperature, cloud_cover at the roof
	PD        *Predispatch // nil when not downloaded
}

// N is the number of 5-minute intervals.
func (d *Data) N() int { return len(d.Price) }

// Time is interval i's end time.
func (d *Data) Time(i int) time.Time { return time.Unix(d.T0+int64(i)*Step, 0).In(NEM) }

// Index is the interval ending at t, or -1.
func (d *Data) Index(t time.Time) int {
	s := t.Unix() - d.T0
	if s < 0 || s%Step != 0 || s/Step >= int64(len(d.Price)) {
		return -1
	}
	return int(s / Step)
}

// Load reads a folder written by fetch.
func Load(dir string) (*Data, error) {
	prices, err := readCSV(filepath.Join(dir, "prices.csv"))
	if err != nil {
		return nil, err
	}
	d := &Data{}
	if len(prices.rows) == 0 {
		return nil, fmt.Errorf("data: prices.csv is empty")
	}
	times := make([]int64, len(prices.rows))
	for i, r := range prices.rows {
		if times[i], err = unix(r[0]); err != nil {
			return nil, err
		}
	}
	d.T0 = times[0]
	d.Price = make([]float64, (times[len(times)-1]-d.T0)/Step+1)
	for i := range d.Price {
		d.Price[i] = math.NaN()
	}
	col := prices.col["price_aud_mwh"]
	for i, r := range prices.rows {
		d.Price[(times[i]-d.T0)/Step] = number(r[col])
	}
	if d.WeatherFC, err = readHourly(filepath.Join(dir, "weather_forecast.csv")); err != nil {
		return nil, err
	}
	if d.Observed, err = readHourly(filepath.Join(dir, "weather_observed.csv")); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "predispatch.csv")
	if Exists(path) {
		if d.PD, err = readPredispatch(path); err != nil {
			return nil, err
		}
	}
	return d, nil
}

type table struct {
	col  map[string]int
	rows [][]string
}

// readCSV reads path, or path.gz if only the gzipped file is there.
func readCSV(path string) (*table, error) {
	f, err := os.Open(path)
	var r io.Reader = f
	if errors.Is(err, fs.ErrNotExist) {
		if f, err = os.Open(path + ".gz"); err == nil {
			gz, gzErr := gzip.NewReader(f)
			if gzErr != nil {
				f.Close()
				return nil, fmt.Errorf("%s.gz: %w", path, gzErr)
			}
			r = gz
		}
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	records, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("%s: empty", path)
	}
	t := &table{col: map[string]int{}, rows: records[1:]}
	for i, name := range records[0] {
		t.col[name] = i
	}
	return t, nil
}

func unix(s string) (int64, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0, err
	}
	return t.Unix(), nil
}

func number(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

// readHourly reads an hourly table and fills gaps linearly (the ends with the nearest value).
func readHourly(path string) (*Hourly, error) {
	t, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	h := &Hourly{T: make([]int64, len(t.rows)), Cols: map[string][]float64{}}
	for i, r := range t.rows {
		if h.T[i], err = unix(r[0]); err != nil {
			return nil, err
		}
	}
	for name, c := range t.col {
		if name == "time" {
			continue
		}
		v := make([]float64, len(t.rows))
		for i, r := range t.rows {
			v[i] = number(r[c])
		}
		fillGaps(v)
		h.Cols[name] = v
	}
	return h, nil
}

func fillGaps(v []float64) {
	last := -1
	for i, x := range v {
		if math.IsNaN(x) {
			continue
		}
		if last == -1 {
			for j := 0; j < i; j++ {
				v[j] = x
			}
		} else {
			for j := last + 1; j < i; j++ {
				v[j] = v[last] + (x-v[last])*float64(j-last)/float64(i-last)
			}
		}
		last = i
	}
	if last >= 0 {
		for j := last + 1; j < len(v); j++ {
			v[j] = v[last]
		}
	}
}

func readPredispatch(path string) (*Predispatch, error) {
	t, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	n := len(t.rows)
	p := &Predispatch{make([]int64, n), make([]int64, n), make([]float64, n), make([]float64, n)}
	for i, r := range t.rows {
		if p.Published[i], err = unix(r[t.col["published"]]); err != nil {
			return nil, err
		}
		if p.End[i], err = unix(r[t.col["period_end"]]); err != nil {
			return nil, err
		}
		p.RRP[i] = number(r[t.col["rrp"]])
		p.Demand[i] = number(r[t.col["demand_mw"]])
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return p.Published[order[a]] < p.Published[order[b]] })
	s := &Predispatch{make([]int64, n), make([]int64, n), make([]float64, n), make([]float64, n)}
	for i, j := range order {
		s.Published[i], s.End[i], s.RRP[i], s.Demand[i] = p.Published[j], p.End[j], p.RRP[j], p.Demand[j]
	}
	return s, nil
}

// Exists says whether a data file is there, plain or gzipped (path.gz).
func Exists(path string) bool {
	for _, p := range []string{path, path + ".gz"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}
