package fetch

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	priceURL       = "https://www.aemo.com.au/aemo/data/nem/priceanddemand/PRICE_AND_DEMAND_%s_NSW1.csv"
	predispatchDir = "https://nemweb.com.au/Reports/ARCHIVE/Predispatch_Reports/"
	currentDir     = "https://nemweb.com.au/Reports/Current/Predispatch_Reports/"
	aemoTime       = "2006/01/02 15:04:05"
)

var (
	weekFile = regexp.MustCompile(`PUBLIC_PREDISPATCH_(\d{8})_(\d{8})\.zip`)
	runFile  = regexp.MustCompile(`PUBLIC_PREDISPATCH_(\d{12})_(\d{14})_LEGACY`)
)

func get(url string) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (curtailment-model)")
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			return io.ReadAll(resp.Body)
		}
		if err == nil {
			resp.Body.Close()
			err = fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
		}
		last = err
		time.Sleep(time.Duration(2<<attempt) * time.Second) // NEMweb rate-limits bursts
	}
	return nil, last
}

// fetchPrices writes time,price_aud_mwh,demand_mw for every 5-minute interval in [from, to].
// AEMO stamps each interval with its end time.
func fetchPrices(from, to time.Time, path string) error {
	type row struct {
		t             time.Time
		price, demand string
	}
	seen := map[time.Time]row{}
	for m := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, nem); !m.After(to); m = m.AddDate(0, 1, 0) {
		body, err := get(fmt.Sprintf(priceURL, m.Format("200601")))
		if err != nil {
			return err
		}
		records, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
		if err != nil {
			return err
		}
		for _, r := range records[1:] { // REGION,SETTLEMENTDATE,TOTALDEMAND,RRP,PERIODTYPE
			t, err := time.ParseInLocation(aemoTime, r[1], nem)
			if err != nil || t.Before(from) || t.After(to) {
				continue
			}
			seen[t] = row{t, r[3], r[2]}
		}
	}
	rows := make([]row, 0, len(seen))
	for _, r := range seen {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].t.Before(rows[j].t) })
	return writeCSV(path, []string{"time", "price_aud_mwh", "demand_mw"}, len(rows), func(i int) []string {
		return []string{rows[i].t.Format(time.RFC3339), rows[i].price, rows[i].demand}
	})
}

// fetchPredispatch writes published,period_end,rrp,demand_mw for every NSW1 pre-dispatch run
// published in [from, to]. Each run's publication time comes from its file name, so the model
// can use only runs that were public at each decision time.
func fetchPredispatch(from, to time.Time, path string) error {
	var rows [][]string
	weeks, err := listing(predispatchDir, weekFile)
	if err != nil {
		return err
	}
	for _, name := range weeks {
		m := weekFile.FindStringSubmatch(name)
		first, _ := time.ParseInLocation("20060102", m[1], nem)
		last, _ := time.ParseInLocation("20060102", m[2], nem)
		if last.AddDate(0, 0, 1).Before(from) || first.After(to) {
			continue
		}
		body, err := get(predispatchDir + name)
		if err != nil {
			return err
		}
		got, err := runsIn(body, from, to)
		if err != nil {
			return err
		}
		rows = append(rows, got...)
	}
	// The archive lags by up to two weeks; the Current folder holds one file per recent run.
	if time.Since(to) < 15*24*time.Hour {
		runs, err := listing(currentDir, runFile)
		if err != nil {
			return err
		}
		for _, name := range runs {
			published, ok := publishedAt(name)
			if !ok || published.Before(from) || published.After(to) {
				continue
			}
			body, err := get(currentDir + name)
			if err != nil {
				return err
			}
			got, err := runsIn(body, from, to)
			if err != nil {
				return err
			}
			rows = append(rows, got...)
			time.Sleep(300 * time.Millisecond)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][0] != rows[j][0] {
			return rows[i][0] < rows[j][0]
		}
		return rows[i][1] < rows[j][1]
	})
	return writeCSV(path, []string{"published", "period_end", "rrp", "demand_mw"}, len(rows), func(i int) []string { return rows[i] })
}

func listing(dir string, pattern *regexp.Regexp) ([]string, error) {
	body, err := get(dir)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, name := range pattern.FindAllString(string(body), -1) {
		if !strings.HasSuffix(name, ".zip") {
			name += ".zip"
		}
		set[name] = true
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func publishedAt(name string) (time.Time, bool) {
	m := runFile.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("20060102150405", m[2], nem)
	return t, err == nil
}

// runsIn reads a zip (a weekly archive of zipped runs, or one zipped run) and returns NSW1 rows.
func runsIn(body []byte, from, to time.Time) ([][]string, error) {
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	var rows [][]string
	for _, f := range z.File {
		data, err := readZipFile(f)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(strings.ToLower(f.Name), ".zip") {
			published, ok := publishedAt(f.Name)
			if !ok || published.Before(from) || published.After(to) {
				continue
			}
			inner, err := runsIn(data, from, to)
			if err != nil {
				return nil, err
			}
			rows = append(rows, inner...)
			continue
		}
		published, ok := publishedAt(f.Name)
		if !ok {
			continue
		}
		rows = append(rows, parseRun(data, published)...)
	}
	return rows, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// parseRun keeps the PDREGION rows for NSW1:
// D,PDREGION,,5,PREDISPATCHSEQNO,RUNNO,REGIONID,PERIODID,RRP,EEP,TOTALDEMAND,...
func parseRun(data []byte, published time.Time) [][]string {
	var rows [][]string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "D,PDREGION,") || !strings.Contains(line, ",NSW1,") {
			continue
		}
		p := strings.Split(strings.TrimSpace(line), ",")
		if len(p) < 11 {
			continue
		}
		end, err := time.ParseInLocation(aemoTime, strings.Trim(p[7], `"`), nem)
		if err != nil {
			continue
		}
		rows = append(rows, []string{published.Format(time.RFC3339), end.Format(time.RFC3339), p[8], p[10]})
	}
	return rows
}

func writeCSV(path string, header []string, n int, row func(int) []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		if err := w.Write(row(i)); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
