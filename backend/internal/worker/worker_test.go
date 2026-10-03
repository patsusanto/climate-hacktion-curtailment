package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/data"
	"climate-hacktion-curtailment/backend/internal/model/runfile"
	"climate-hacktion-curtailment/backend/internal/model/simulate"
	"climate-hacktion-curtailment/backend/internal/model/synthetic"
	"climate-hacktion-curtailment/backend/internal/wire"
)

// A three-day "validation" window on synthetic data, so no download is needed. The real models
// run on it.
var (
	winStart = time.Date(2026, 8, 19, 0, 0, 0, 0, data.NEM)
	winEnd   = time.Date(2026, 8, 21, 23, 55, 0, 0, data.NEM)
)

const pageAccept = "application/x-ndjson, text/event-stream"

func newDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := synthetic.Write(filepath.Join(dir, "validation"), winStart.AddDate(0, 0, -9), winEnd.AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newServer(t *testing.T, tweak func(*Config)) (*Server, *httptest.Server) {
	t.Helper()
	cfg := Config{
		DataDir: newDataDir(t),
		Windows: map[string][2]time.Time{"validation": {winStart, winEnd}},
		Logger:  log.New(io.Discard, "", 0),
	}
	if tweak != nil {
		tweak(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	return s, ts
}

func body(pv, battery float64) string {
	b, _ := json.Marshal(map[string]any{"address": "1 Example St", "pv_kw_ac": pv, "battery_kwh": battery, "window": "validation"})
	return string(b)
}

func post(t *testing.T, url, accept, reqBody string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url+"/v1/playground/run", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

type event struct {
	Type    string          `json:"type"`
	Meta    json.RawMessage `json:"meta"`
	Tick    json.RawMessage `json:"tick"`
	Summary json.RawMessage `json:"summary"`
}

func events(t *testing.T, r io.Reader) []event {
	t.Helper()
	var out []event
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "data:"))
		if line == "" {
			continue
		}
		var e event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("not an event: %.100q (%v)", line, err)
		}
		out = append(out, e)
	}
	return out
}

func nSteps() int { return int(winEnd.Sub(winStart)/(5*time.Minute)) + 1 }

func TestNewNeedsData(t *testing.T) {
	_, err := New(Config{DataDir: t.TempDir(), Logger: log.New(io.Discard, "", 0)})
	if err == nil || !strings.Contains(err.Error(), "no window has data") {
		t.Errorf("an empty data folder must be a clear error, got %v", err)
	}
	s, _ := newServer(t, nil)
	if got := s.Windows(); len(got) != 1 || got[0] != "validation" {
		t.Errorf("windows %v", got)
	}
	// data that does not cover the window is refused
	dir := t.TempDir()
	if err := synthetic.Write(filepath.Join(dir, "validation"), winStart.AddDate(0, 0, -9), winStart.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	_, err = New(Config{DataDir: dir, Windows: map[string][2]time.Time{"validation": {winStart, winEnd}}, Logger: log.New(io.Discard, "", 0)})
	if err == nil || !strings.Contains(err.Error(), "does not cover") {
		t.Errorf("short data: %v", err)
	}
}

// The stream is the run file, one event at a time.
func TestRunStreamsTheRunFile(t *testing.T) {
	s, ts := newServer(t, nil)
	resp := post(t, ts.URL, pageAccept, body(6.6, 13.5))
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("content-type %q", ct)
	}
	id := resp.Header.Get("X-Run-Id")
	if id != "pv6.6-b13.5-bp5-ec5-l15-validation" {
		t.Errorf("X-Run-Id %q", id)
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Error("the stream must not be buffered by nginx")
	}
	evs := events(t, resp.Body)
	n := nSteps()
	if len(evs) != n+2 || evs[0].Type != "meta" || evs[n+1].Type != "done" {
		t.Fatalf("%d events; first %q last %q; want %d", len(evs), evs[0].Type, evs[len(evs)-1].Type, n+2)
	}

	// the same house replayed directly and packaged as a run file must match event for event
	sp, _ := spec(wire.Params{PvKwAc: 6.6, BatteryKwh: 13.5, BatteryKw: 5, ExportCapKw: 5, DailyLoadKwh: 15})
	win := s.windows["validation"]
	res, err := simulate.Run(s.models, win.in, sp, win.start, win.end, simulate.DefaultOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := wire.ParseID(id)
	file, err := runfile.Build(res, s.options(p))
	if err != nil {
		t.Fatal(err)
	}
	for i, tick := range file.Ticks {
		want, _ := json.Marshal(tick)
		var got, wantC bytesBuf
		got.compact(t, evs[i+1].Tick)
		wantC.compact(t, want)
		if got.s != wantC.s {
			t.Fatalf("tick %d:\n got  %s\n want %s", i, got.s, wantC.s)
		}
	}
	wantSummary, _ := json.Marshal(file.Summary)
	var gs, ws bytesBuf
	gs.compact(t, evs[n+1].Summary)
	ws.compact(t, wantSummary)
	if gs.s != ws.s {
		t.Errorf("summary:\n got  %s\n want %s", gs.s, ws.s)
	}

	var meta wire.Meta
	if err := json.Unmarshal(evs[0].Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Assumptions.AddressLabel != "1 Example St" || meta.Window.N != n || meta.Spec.BatteryKwh != 13.5 || meta.Spec.PvKwAc != 6.6 {
		t.Errorf("meta %+v", meta)
	}
}

type bytesBuf struct{ s string }

func (b *bytesBuf) compact(t *testing.T, raw []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(v)
	b.s = string(out)
}

func lastSavings(t *testing.T, evs []event) float64 {
	t.Helper()
	var s wire.Summary
	if err := json.Unmarshal(evs[len(evs)-1].Summary, &s); err != nil {
		t.Fatal(err)
	}
	return s.SavingsAud
}

// The point of running live: the answer depends on the house that was asked for.
func TestDifferentHousesGiveDifferentResults(t *testing.T) {
	_, ts := newServer(t, nil)
	results := map[string]float64{}
	for name, b := range map[string]string{"small": body(3.3, 6.5), "large": body(10, 27)} {
		resp := post(t, ts.URL, pageAccept, b)
		results[name] = lastSavings(t, events(t, resp.Body))
		resp.Body.Close()
	}
	if results["small"] == results["large"] {
		t.Errorf("two different houses gave the same savings: %v", results)
	}
}

func TestSSEFormat(t *testing.T) {
	_, ts := newServer(t, nil)
	resp := post(t, ts.URL, "text/event-stream", body(6.6, 13.5))
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type %q", ct)
	}
	raw, _ := io.ReadAll(resp.Body)
	for i, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line != "" && !strings.HasPrefix(line, "data: ") {
			t.Fatalf("line %d would break the page: %.80q", i, line)
		}
	}
	if len(events(t, strings.NewReader(string(raw)))) != nSteps()+2 {
		t.Error("wrong number of events")
	}
}

// Without an Accept header the worker still streams (it has no other form of answer).
func TestStreamsWithoutAcceptHeader(t *testing.T) {
	_, ts := newServer(t, nil)
	resp := post(t, ts.URL, "", body(6.6, 13.5))
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("content-type %q", ct)
	}
}

// The page asks for the detail of whatever step is under the cursor, mid-stream or later.
func TestStepDetailForAnyStep(t *testing.T) {
	_, ts := newServer(t, nil)
	resp := post(t, ts.URL, pageAccept, body(6.6, 13.5))
	id := resp.Header.Get("X-Run-Id")
	evs := events(t, resp.Body)
	resp.Body.Close()

	for _, i := range []int{0, 1, 13, 100, nSteps() - 1} {
		r, err := http.Get(ts.URL + "/v1/playground/run/" + id + "/steps/" + itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		var d wire.StepDecision
		err = json.NewDecoder(r.Body).Decode(&d)
		r.Body.Close()
		if err != nil || r.StatusCode != 200 {
			t.Fatalf("step %d: status %d (%v)", i, r.StatusCode, err)
		}
		var tick wire.Tick
		json.Unmarshal(evs[i+1].Tick, &tick)
		if d.T != tick.T || d.Action != tick.Action {
			t.Errorf("step %d: detail %s/%s but tick %s/%s", i, d.T, d.Action, tick.T, tick.Action)
		}
		if len(d.Leads) != 5 || len(d.Stories) == 0 {
			t.Errorf("step %d: %d leads, %d stories", i, len(d.Leads), len(d.Stories))
		}
	}
	for path, want := range map[string]int{
		"/v1/playground/run/" + id + "/steps/" + itoa(nSteps()):         400,
		"/v1/playground/run/" + id + "/steps/-1":                        400,
		"/v1/playground/run/" + id + "/steps/abc":                       400,
		"/v1/playground/run/nonsense/steps/0":                           400,
		"/v1/playground/run/10kw-10kwh/steps/0":                         400, // an example id, not a parameter id
		"/v1/playground/run/pv6.6-b13.5-bp5-ec5-l15-test/steps/0":       404, // no data for that window
		"/v1/playground/run/pv999-b13.5-bp5-ec5-l15-validation/steps/0": 400, // outside the limits
	} {
		r, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != want {
			t.Errorf("%s: status %d, want %d", path, r.StatusCode, want)
		}
	}
}

// A different worker instance (or an expired cache) has no run in memory: the id alone is enough.
func TestStepDetailRecomputesWhenNotCached(t *testing.T) {
	_, ts := newServer(t, nil) // fresh server: nothing cached
	r, err := http.Get(ts.URL + "/v1/playground/run/pv6.6-b13.5-bp5-ec5-l15-validation/steps/13")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var d wire.StepDecision
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil || r.StatusCode != 200 || d.T == "" {
		t.Errorf("status %d, %+v (%v)", r.StatusCode, d, err)
	}
}

func TestBadRequestsAreJSONBeforeAnyStream(t *testing.T) {
	_, ts := newServer(t, nil)
	cases := map[string]string{
		"not json":      `{`,
		"no address":    `{"pv_kw_ac":5,"battery_kwh":5,"window":"validation"}`,
		"zero battery":  `{"address":"x","pv_kw_ac":5,"battery_kwh":0,"window":"validation"}`,
		"huge solar":    `{"address":"x","pv_kw_ac":500,"battery_kwh":5,"window":"validation"}`,
		"bad window":    `{"address":"x","pv_kw_ac":5,"battery_kwh":5,"window":"nope"}`,
		"unloaded":      `{"address":"x","pv_kw_ac":5,"battery_kwh":5,"window":"test"}`,
		"custom window": `{"address":"x","pv_kw_ac":5,"battery_kwh":5,"window":{"start":"a","end":"b"}}`,
	}
	for name, b := range cases {
		resp := post(t, ts.URL, pageAccept, b)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var msg struct {
			Message string `json:"message"`
		}
		if resp.StatusCode != 400 || json.Unmarshal(raw, &msg) != nil || msg.Message == "" {
			t.Errorf("%s: status %d body %s", name, resp.StatusCode, raw)
		}
		if resp.Header.Get("X-Run-Id") != "" {
			t.Errorf("%s: an error must not carry a run id", name)
		}
	}
}

func TestBusyWorkerSaysSoButStillServesCachedRuns(t *testing.T) {
	s, ts := newServer(t, func(c *Config) { c.MaxRuns = 1 })
	// one run finishes and is cached
	resp := post(t, ts.URL, pageAccept, body(6.6, 13.5))
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	s.sem <- struct{}{} // another run is using the only slot
	defer func() { <-s.sem }()

	resp = post(t, ts.URL, pageAccept, body(8, 20)) // new house: needs the model
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 503 || resp.Header.Get("Retry-After") == "" || !strings.Contains(string(raw), "busy") {
		t.Errorf("busy: status %d, Retry-After %q, body %s", resp.StatusCode, resp.Header.Get("Retry-After"), raw)
	}

	resp = post(t, ts.URL, pageAccept, body(6.6, 13.5)) // the cached house needs no slot
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("a cached run must still be served when busy, got %d", resp.StatusCode)
	}
}

func TestPacingSpreadsTheStream(t *testing.T) {
	_, ts := newServer(t, func(c *Config) { c.StreamFor = 600 * time.Millisecond })
	post(t, ts.URL, pageAccept, body(6.6, 13.5)).Body.Close() // warm the cache so only streaming is timed

	timed := func(url string) time.Duration {
		t0 := time.Now()
		req, _ := http.NewRequest("POST", url, strings.NewReader(body(6.6, 13.5)))
		req.Header.Set("Accept", pageAccept)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return time.Since(t0)
	}
	paced := timed(ts.URL + "/v1/playground/run")
	fast := timed(ts.URL + "/v1/playground/run?speed=max")
	if paced < 450*time.Millisecond {
		t.Errorf("a 600 ms stream took %v", paced)
	}
	if fast > paced/2 {
		t.Errorf("?speed=max took %v, paced took %v", fast, paced)
	}
}

// A client that leaves must not keep the worker busy.
func TestDisconnectEndsTheStream(t *testing.T) {
	_, ts := newServer(t, func(c *Config) { c.StreamFor = time.Minute })
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/v1/playground/run", strings.NewReader(body(6.6, 13.5)))
	req.Header.Set("Accept", pageAccept)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(resp.Body)
	if _, err := br.ReadString('\n'); err != nil { // the meta line: the stream is under way
		t.Fatal(err)
	}
	cancel()
	resp.Body.Close()

	closed := make(chan struct{})
	go func() { ts.Close(); close(closed) }() // Close waits for the handler to return
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler kept streaming to a client that had gone")
	}
}

func TestCacheEvictsAndExpires(t *testing.T) {
	c := newCache(2, time.Minute)
	now := time.Now()
	c.now = func() time.Time { return now }
	r := &simulate.Result{}
	c.put("a", r)
	c.put("b", r)
	if _, ok := c.get("a"); !ok { // a is now the most recently used
		t.Fatal("a missing")
	}
	c.put("c", r) // evicts the least recently used: b
	if _, ok := c.get("b"); ok {
		t.Error("b should have been evicted")
	}
	if _, ok := c.get("a"); !ok || c.len() != 2 {
		t.Errorf("a should remain; len %d", c.len())
	}
	now = now.Add(2 * time.Minute)
	if _, ok := c.get("a"); ok {
		t.Error("a should have expired")
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
