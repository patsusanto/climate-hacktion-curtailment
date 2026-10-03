package api

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The page (frontend/src/playground/api.ts) sends this Accept header.
const pageAccept = "application/x-ndjson, text/event-stream"

func postRun(t *testing.T, url, accept, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url+"/v1/playground/run?speed=max", strings.NewReader(body))
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

// event is what the page understands: {type, meta|tick|summary|message}.
type event struct {
	Type    string          `json:"type"`
	Meta    json.RawMessage `json:"meta"`
	Tick    json.RawMessage `json:"tick"`
	Summary json.RawMessage `json:"summary"`
	Message string          `json:"message"`
}

// parseLikeThePage reproduces parseLine and isEvent from api.ts: blank lines and
// ":" comments are skipped, a "data:" prefix is stripped, and anything else that
// is not a JSON event is an error the page would throw.
func parseLikeThePage(line string) (ev *event, skip bool, err error) {
	payload := strings.TrimSpace(line)
	if payload == "" || strings.HasPrefix(payload, ":") {
		return nil, true, nil
	}
	if strings.HasPrefix(payload, "data:") {
		payload = strings.TrimSpace(payload[5:])
	}
	if payload == "" || payload == "[DONE]" {
		return nil, true, nil
	}
	var e event
	if err := json.Unmarshal([]byte(payload), &e); err != nil {
		return nil, false, err
	}
	switch {
	case e.Type == "meta" && e.Meta != nil,
		e.Type == "step" && e.Tick != nil,
		e.Type == "done" && e.Summary != nil,
		e.Type == "error":
		return &e, false, nil
	}
	return nil, false, io.ErrUnexpectedEOF // "an event this page does not understand"
}

var lineBreaks = regexp.MustCompile(`\r?\n`)

func pageEvents(t *testing.T, body string) []*event {
	t.Helper()
	var out []*event
	for _, line := range lineBreaks.Split(body, -1) {
		ev, skip, err := parseLikeThePage(line)
		if err != nil {
			t.Fatalf("the page would fail on this line: %.120q (%v)", line, err)
		}
		if !skip {
			out = append(out, ev)
		}
	}
	return out
}

func checkRunStream(t *testing.T, events []*event) {
	t.Helper()
	if len(events) != 9794 { // meta + 9,792 steps + done
		t.Fatalf("%d events, want 9794", len(events))
	}
	if events[0].Type != "meta" || events[len(events)-1].Type != "done" {
		t.Fatalf("first is %q, last is %q", events[0].Type, events[len(events)-1].Type)
	}
	var meta struct {
		Assumptions struct {
			AddressLabel string `json:"address_label"`
		} `json:"assumptions"`
		Window struct {
			N int `json:"n"`
		} `json:"window"`
	}
	if err := json.Unmarshal(events[0].Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Assumptions.AddressLabel != "1 Example St, Sydney" || meta.Window.N != 9792 {
		t.Errorf("meta %+v", meta)
	}

	var last struct {
		I            int      `json:"i"`
		SelfCost     *float64 `json:"cumulative_self_aud"`
		SavingsSoFar float64  `json:"cumulative_savings_aud"`
	}
	for i, ev := range events[1 : len(events)-1] {
		if ev.Type != "step" {
			t.Fatalf("event %d is %q, want step", i+1, ev.Type)
		}
		var tick struct {
			I        int      `json:"i"`
			SelfCost *float64 `json:"cumulative_self_aud"`
		}
		if err := json.Unmarshal(ev.Tick, &tick); err != nil || tick.I != i {
			t.Fatalf("step %d has i=%d (%v)", i, tick.I, err)
		}
		if tick.SelfCost == nil {
			t.Fatalf("step %d has no cumulative_self_aud", i)
		}
		if i == 9791 {
			json.Unmarshal(ev.Tick, &last)
		}
	}
	var summary struct {
		SavingsAud float64 `json:"savings_aud"`
	}
	if err := json.Unmarshal(events[len(events)-1].Summary, &summary); err != nil {
		t.Fatal(err)
	}
	if d := summary.SavingsAud - last.SavingsSoFar; d > 0.01 || d < -0.01 {
		t.Errorf("summary savings %v vs last tick %v", summary.SavingsAud, last.SavingsSoFar)
	}
}

func TestStartStreamsNDJSONLikeThePageExpects(t *testing.T) {
	ts := testServer(t)
	resp := postRun(t, ts.URL, pageAccept, goodBody)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("content-type %q", ct)
	}
	if id := resp.Header.Get("X-Run-Id"); id != "10kw-10kwh" {
		t.Errorf("X-Run-Id %q", id)
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Error("the stream must not be buffered by nginx or cached")
	}
	body, _ := io.ReadAll(resp.Body)
	checkRunStream(t, pageEvents(t, string(body)))

	// NDJSON: every non-empty line is a complete JSON object, nothing else
	for i, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if !strings.HasPrefix(line, `{"type":`) {
			t.Fatalf("line %d is not an event object: %.80q", i, line)
		}
	}
}

func TestStartStreamsSSEWithOnlyDataLines(t *testing.T) {
	ts := testServer(t)
	resp := postRun(t, ts.URL, "text/event-stream", goodBody)
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	for i, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
		// the page throws on an "id:" or "event:" line, so they must not appear
		if line != "" && !strings.HasPrefix(line, "data: ") {
			t.Fatalf("line %d would break the page: %.80q", i, line)
		}
	}
	checkRunStream(t, pageEvents(t, string(body)))
}

// The page asks for step detail with the id from X-Run-Id.
func TestRunIdHeaderWorksForStepDetail(t *testing.T) {
	ts := testServer(t)
	resp := postRun(t, ts.URL, pageAccept, goodBody)
	id := resp.Header.Get("X-Run-Id")
	resp.Body.Close()
	if id == "" {
		t.Fatal("no X-Run-Id header")
	}
	r, body := get(t, ts.URL+"/v1/playground/run/"+id+"/steps/12", nil)
	if r.StatusCode != 200 || !strings.Contains(body, `"leads"`) {
		t.Errorf("step 12: status %d body %.80s", r.StatusCode, body)
	}
}

// Without a streaming Accept header the original response is unchanged, so
// curl and the two-step flow keep working.
func TestStartWithoutStreamAcceptStaysJSON(t *testing.T) {
	ts := testServer(t)
	for _, accept := range []string{"", "*/*", "application/json"} {
		resp := postRun(t, ts.URL, accept, goodBody)
		var got struct {
			RunID string          `json:"run_id"`
			Meta  json.RawMessage `json:"meta"`
		}
		err := json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		if err != nil || got.RunID != "10kw-10kwh" || got.Meta == nil {
			t.Errorf("Accept %q: %+v (%v)", accept, got, err)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Accept %q: content-type %q", accept, ct)
		}
	}
}

// A bad request is a plain JSON error before any stream starts, which is what
// the page's readError expects.
func TestStreamingRequestWithBadInputGetsAJSONError(t *testing.T) {
	ts := testServer(t)
	resp := postRun(t, ts.URL, pageAccept, `{"address":"","pv_kw_ac":5,"battery_kwh":5,"window":"validation"}`)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var msg struct {
		Message string `json:"message"`
	}
	if resp.StatusCode != 400 || json.Unmarshal(body, &msg) != nil || msg.Message == "" {
		t.Errorf("status %d body %s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Run-Id") != "" {
		t.Error("an error response must not carry a run id")
	}
}

func TestStreamFormat(t *testing.T) {
	cases := map[string]string{
		pageAccept:               formatNDJSON,
		"application/x-ndjson":   formatNDJSON,
		"text/event-stream":      formatSSE,
		"Text/Event-Stream, */*": formatSSE,
		"*/*":                    "",
		"application/json":       "",
		"":                       "",
		"text/event-stream, application/x-ndjson": formatNDJSON, // NDJSON wins when both are offered
	}
	for accept, want := range cases {
		if got := streamFormat(accept); got != want {
			t.Errorf("streamFormat(%q) = %q, want %q", accept, got, want)
		}
	}
}

// A run file from before cumulative_self_aud existed must be refused at
// startup rather than served with blank chart values.
func TestRunWithoutCumulativeSelfIsRejected(t *testing.T) {
	run := `{"run_id":"old","window_name":"validation",
	  "meta":{"window":{"n":1,"step_minutes":5}},
	  "ticks":[{"i":0,"t":"2026-07-16T00:00:00+10:00","cumulative_savings_aud":0}],
	  "summary":{"savings_aud":0},"steps":{}}`
	if _, err := parseRun([]byte(run)); err == nil || !strings.Contains(err.Error(), "cumulative_self_aud") {
		t.Errorf("expected a cumulative_self_aud error, got %v", err)
	}
	ok := strings.Replace(run, `"cumulative_savings_aud":0`, `"cumulative_self_aud":0,"cumulative_savings_aud":0`, 1)
	if _, err := parseRun([]byte(ok)); err != nil {
		t.Errorf("a complete run should load: %v", err)
	}
}
