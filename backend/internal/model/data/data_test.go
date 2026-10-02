package data

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/split"
)

func writeGz(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

const frameCSV = `time,price_aud_mwh,pv_unit,load_unit
2026-07-16T00:00:00+10:00,82.4,0,0.25
2026-07-16T00:05:00+10:00,-3.5,0.001,0.26
2026-07-16T00:10:00+10:00,1554.16,0.5,0.31
`

func TestLoadFrame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame.csv.gz")
	writeGz(t, path, frameCSV)
	f, err := LoadFrame(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Len() != 3 || f.Price[1] != -3.5 || f.PvUnit[2] != 0.5 || f.LoadUnit[0] != 0.25 {
		t.Errorf("frame %+v", f)
	}
	if f.Times[0].Location() != split.NEM || f.Times[0].Hour() != 0 {
		t.Errorf("times should be on the market clock: %v", f.Times[0])
	}
	if i := f.IndexOf(f.Times[2]); i != 2 {
		t.Errorf("IndexOf = %d", i)
	}
	if i := f.IndexOf(f.Times[0].Add(7 * time.Minute)); i != -1 {
		t.Errorf("an off-grid time must not match: %d", i)
	}
	if i := f.IndexOf(f.Times[0].Add(-time.Hour)); i != -1 {
		t.Errorf("a time before the frame must not match: %d", i)
	}
}

func TestLoadFramePlainCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame.csv")
	if err := os.WriteFile(path, []byte(frameCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if f, err := LoadFrame(path); err != nil || f.Len() != 3 {
		t.Fatalf("plain csv: %v", err)
	}
}

func TestLoadFrameRejectsBadFiles(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"gap":            strings.Replace(frameCSV, "00:05:00", "00:10:00", 1),
		"missing column": "time,price_aud_mwh,pv_unit\n2026-07-16T00:00:00+10:00,1,2\n",
		"bad number":     strings.Replace(frameCSV, "82.4", "abc", 1),
		"bad time":       strings.Replace(frameCSV, "2026-07-16T00:00:00+10:00", "yesterday", 1),
	}
	for name, body := range cases {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".csv")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFrame(path); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := LoadFrame(filepath.Join(dir, "nope.csv.gz")); err == nil {
		t.Error("a missing file must be an error")
	}
}

func TestLoadWeather(t *testing.T) {
	path := filepath.Join(t.TempDir(), "weather.csv.gz")
	writeGz(t, path, "time,fc_a,fc_b\n2026-07-16T00:00:00+10:00,1.5,10\n2026-07-16T01:00:00+10:00,2.5,20\n")
	w, err := LoadWeather(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Times) != 2 || len(w.Columns) != 2 || w.Columns[1] != "fc_b" || w.Values[0][1] != 2.5 || w.Values[1][0] != 10 {
		t.Errorf("weather %+v", w)
	}
	bad := filepath.Join(t.TempDir(), "bad.csv")
	if err := os.WriteFile(bad, []byte("stamp,fc_a\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWeather(bad); err == nil {
		t.Error("a weather file without a time column must fail")
	}
}
