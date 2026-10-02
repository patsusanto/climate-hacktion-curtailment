package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/wire"
)

const goodBody = `{"address":"1 Example St, Sydney","pv_kw_ac":10.5,"battery_kwh":10,"battery_kw":5,"window":"validation"}`

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	// the real example runs: backend/runs, two levels up from this package
	srv, err := New(os.DirFS("../.."), "runs", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts
}

type sseEvent struct{ id, event, data string }

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var out []sseEvent
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		var e sseEvent
		for _, line := range strings.Split(block, "\n") {
			k, v, _ := strings.Cut(line, ": ")
			switch k {
			case "id":
				e.id = v
			case "event":
				e.event = v
			case "data":
				e.data = v
			}
		}
		out = append(out, e)
	}
	return out
}

func get(t *testing.T, url string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestStartRun(t *testing.T) {
	ts := testServer(t)
	resp, err := http.Post(ts.URL+"/v1/playground/run", "application/json", strings.NewReader(goodBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var got struct {
		RunID string    `json:"run_id"`
		Meta  wire.Meta `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.RunID != "10kw-10kwh" {
		t.Errorf("run_id = %q", got.RunID)
	}
	if got.Meta.Assumptions.AddressLabel != "1 Example St, Sydney" {
		t.Errorf("address_label = %q", got.Meta.Assumptions.AddressLabel)
	}
	if got.Meta.Window.N != 9792 || got.Meta.Window.StepMinutes != 5 {
		t.Errorf("window = %+v", got.Meta.Window)
	}
}

func TestStartRunValidation(t *testing.T) {
	ts := testServer(t)
	cases := map[string]string{
		"not json":       `nope`,
		"no address":     `{"pv_kw_ac":5,"battery_kwh":5,"window":"validation"}`,
		"zero solar":     `{"address":"x","pv_kw_ac":0,"battery_kwh":5,"window":"validation"}`,
		"negative batt":  `{"address":"x","pv_kw_ac":5,"battery_kwh":-1,"window":"validation"}`,
		"negative kw":    `{"address":"x","pv_kw_ac":5,"battery_kwh":5,"battery_kw":-2,"window":"validation"}`,
		"no window":      `{"address":"x","pv_kw_ac":5,"battery_kwh":5}`,
		"bad window":     `{"address":"x","pv_kw_ac":5,"battery_kwh":5,"window":"nope"}`,
		"custom window":  `{"address":"x","pv_kw_ac":5,"battery_kwh":5,"window":{"start":"a","end":"b"}}`,
		"window no run":  `{"address":"x","pv_kw_ac":5,"battery_kwh":5,"window":"test"}`,
		"address length": `{"address":"` + strings.Repeat("a", 201) + `","pv_kw_ac":5,"battery_kwh":5,"window":"validation"}`,
	}
	for name, body := range cases {
		resp, err := http.Post(ts.URL+"/v1/playground/run", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 400 || !strings.Contains(string(b), `"message"`) {
			t.Errorf("%s: status %d body %s", name, resp.StatusCode, b)
		}
	}
}

func TestEventsFullStream(t *testing.T) {
	ts := testServer(t)
	resp, body := get(t, ts.URL+"/v1/playground/run/10kw-10kwh/events?speed=max", nil)
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("cache-control %q", cc)
	}
	events := parseSSE(t, body)
	if len(events) != 9793 {
		t.Fatalf("got %d events, want 9793", len(events))
	}
	var lastCum float64
	for i, e := range events[:9792] {
		if e.event != "step" || e.id != strconv.Itoa(i) {
			t.Fatalf("event %d = %+v", i, e)
		}
		var tick struct {
			I   int     `json:"i"`
			Cum float64 `json:"cumulative_savings_aud"`
		}
		if err := json.Unmarshal([]byte(e.data), &tick); err != nil || tick.I != i {
			t.Fatalf("event %d data %q: %v", i, e.data, err)
		}
		lastCum = tick.Cum
	}
	done := events[9792]
	if done.event != "done" || done.id != "9791" {
		t.Fatalf("done = %+v", done)
	}
	var summary struct {
		Savings float64 `json:"savings_aud"`
	}
	json.Unmarshal([]byte(done.data), &summary)
	if d := summary.Savings - lastCum; d > 0.01 || d < -0.01 {
		t.Errorf("summary savings %v does not match last tick %v", summary.Savings, lastCum)
	}
}

func TestEventsResume(t *testing.T) {
	ts := testServer(t)
	_, body := get(t, ts.URL+"/v1/playground/run/10kw-10kwh/events?speed=max", map[string]string{"Last-Event-ID": "9788"})
	events := parseSSE(t, body)
	// steps 9789, 9790, 9791, then done
	if len(events) != 4 || events[0].id != "9789" || events[2].id != "9791" || events[3].event != "done" {
		t.Fatalf("got %+v", events)
	}
	if strings.Contains(body, `"meta"`) {
		t.Error("meta must not be resent")
	}
}

func TestEventsErrors(t *testing.T) {
	ts := testServer(t)
	_, body := get(t, ts.URL+"/v1/playground/run/nope/events", nil)
	if !strings.HasPrefix(body, "event: error\ndata: {\"message\":") {
		t.Errorf("unknown run: %q", body)
	}
	for _, h := range []string{"abc", "-1", "9792"} {
		_, body = get(t, ts.URL+"/v1/playground/run/10kw-10kwh/events", map[string]string{"Last-Event-ID": h})
		if !strings.HasPrefix(body, "event: error") {
			t.Errorf("Last-Event-ID %q: %q", h, body)
		}
	}
}

func TestEventsPaced(t *testing.T) {
	ts := testServer(t) // streamFor is 1s
	start := time.Now()
	_, body := get(t, ts.URL+"/v1/playground/run/10kw-10kwh/events", nil)
	if n := len(parseSSE(t, body)); n != 9793 {
		t.Fatalf("got %d events", n)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Errorf("paced stream finished in %v", time.Since(start))
	}
}

func TestStep(t *testing.T) {
	ts := testServer(t)
	resp, body := get(t, ts.URL+"/v1/playground/run/10kw-10kwh/steps/12", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var d struct {
		T       string            `json:"t"`
		Leads   []json.RawMessage `json:"leads"`
		Stories []struct {
			ID string `json:"id"`
		} `json:"stories"`
	}
	if err := json.Unmarshal([]byte(body), &d); err != nil || d.T == "" || len(d.Leads) == 0 || len(d.Stories) != 4 {
		t.Errorf("unexpected detail: %v %s", err, body)
	}

	for path, want := range map[string]int{
		"/v1/playground/run/10kw-10kwh/steps/13":    404, // valid index, no detail stored
		"/v1/playground/run/10kw-10kwh/steps/99999": 400,
		"/v1/playground/run/10kw-10kwh/steps/abc":   400,
		"/v1/playground/run/nope/steps/12":          404,
	} {
		if resp, _ := get(t, ts.URL+path, nil); resp.StatusCode != want {
			t.Errorf("%s: status %d, want %d", path, resp.StatusCode, want)
		}
	}
}
