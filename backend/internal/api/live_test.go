package api

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/wire"
)

// A house that is not one of the precomputed examples, so the request goes to the worker.
const liveBody = `{"address":"1 Example St","pv_kw_ac":8,"battery_kwh":20,"window":"validation"}`
const liveID = "pv8-b20-bp5-ec5-l15-validation" // what the worker is asked for

// worker is a stand-in for the model worker. It records what it was sent.
type worker struct {
	*httptest.Server
	calls  atomic.Int32
	mu     sync.Mutex
	last   *http.Request
	lastBy []byte
}

func newWorker(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) *worker {
	t.Helper()
	wk := &worker{}
	wk.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wk.calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		wk.mu.Lock()
		wk.last, wk.lastBy = r, b
		wk.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(wk.Close)
	return wk
}

func (wk *worker) request() (*http.Request, []byte) {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	return wk.last, wk.lastBy
}

func quiet() Option { return WithLogger(log.New(io.Discard, "", 0)) }

func newLiveServer(t *testing.T, workerURL string, opts ...Option) *httptest.Server {
	t.Helper()
	all := append([]Option{WithWorker(workerURL), WithRateLimit(0), quiet()}, opts...)
	srv, err := New(os.DirFS("../.."), "runs", time.Second, all...)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts
}

func postPage(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url+"/v1/playground/run", strings.NewReader(body))
	req.Header.Set("Accept", pageAccept)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// cannedRun answers like the worker: an NDJSON stream of meta, two steps and done.
func cannedRun(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Run-Id", liveID)
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	w.Write(wire.MetaLine(wire.Meta{Window: wire.Window{N: 2}}))
	w.Write(wire.StepLine(wire.Tick{I: 0}))
	w.Write(wire.StepLine(wire.Tick{I: 1}))
	w.Write(wire.DoneLine(wire.Summary{SavingsAud: 4.2}))
}

func TestLiveRunIsRelayedFromTheWorker(t *testing.T) {
	wk := newWorker(t, cannedRun)
	ts := newLiveServer(t, wk.URL)
	resp := postPage(t, ts.URL, liveBody)
	defer resp.Body.Close()

	if resp.StatusCode != 200 || resp.Header.Get("X-Run-Id") != liveID {
		t.Fatalf("status %d, X-Run-Id %q", resp.StatusCode, resp.Header.Get("X-Run-Id"))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("content-type %q", ct)
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Error("the no-buffering header must reach nginx")
	}
	evs := pageEvents(t, mustRead(t, resp.Body))
	if len(evs) != 4 || evs[0].Type != "meta" || evs[3].Type != "done" {
		t.Fatalf("events %v", evs)
	}

	// what the worker was asked: the same house, with every default filled in
	r, body := wk.request()
	if r.Method != "POST" || r.URL.Path != "/v1/playground/run" {
		t.Errorf("worker saw %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("Accept") != pageAccept {
		t.Errorf("Accept %q", r.Header.Get("Accept"))
	}
	var got wire.PlaygroundRequest
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	p, err := got.Resolve()
	if err != nil || p.ID() != liveID || p.Address != "1 Example St" {
		t.Errorf("worker was asked for %+v (%v), id %s", p, err, p.ID())
	}
	if got.BatteryKw == nil || got.ExportCapKw == nil || got.DailyLoadKwh == nil {
		t.Error("the defaults must be filled in before the worker sees the request")
	}
}

func mustRead(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The page must see the first event while the worker is still working on the rest.
func TestLiveRelayDoesNotBuffer(t *testing.T) {
	release := make(chan struct{})
	wk := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-Run-Id", liveID)
		w.WriteHeader(200)
		w.Write(wire.MetaLine(wire.Meta{}))
		w.(http.Flusher).Flush()
		<-release // the rest of the run is not ready yet
		w.Write(wire.StepLine(wire.Tick{I: 0}))
		w.Write(wire.DoneLine(wire.Summary{}))
	})
	ts := newLiveServer(t, wk.URL)
	resp := postPage(t, ts.URL, liveBody)
	defer resp.Body.Close()

	first := make(chan string, 1)
	br := bufio.NewReader(resp.Body)
	go func() {
		line, _ := br.ReadString('\n')
		first <- line
	}()
	select {
	case line := <-first:
		if !strings.HasPrefix(line, `{"type":"meta"`) {
			t.Fatalf("first line %q", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the meta event did not arrive while the worker was still busy: the relay is buffering")
	}
	close(release)
	rest, _ := io.ReadAll(br)
	if !strings.Contains(string(rest), `"type":"done"`) {
		t.Errorf("the run did not finish: %s", rest)
	}
}

func TestWorkerErrorsBecomeErrorsForThePage(t *testing.T) {
	cases := map[string]struct {
		status      int
		body        string
		retryAfter  string
		wantStatus  int
		wantMessage string
	}{
		"the worker rejects the house": {400, `{"message":"pv_kw_ac must be at most 100"}`, "", 400, "pv_kw_ac must be at most 100"},
		"the worker is busy":           {503, `{"message":"the model is busy"}`, "2", 503, "the model is busy"},
		"the worker breaks":            {500, `{"message":"panic: index out of range"}`, "", 502, "could not run this house"},
		"the worker is forbidden":      {403, `Forbidden`, "", 502, "could not run this house"},
	}
	for name, c := range cases {
		wk := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
			if c.retryAfter != "" {
				w.Header().Set("Retry-After", c.retryAfter)
			}
			w.WriteHeader(c.status)
			io.WriteString(w, c.body)
		})
		ts := newLiveServer(t, wk.URL)
		resp := postPage(t, ts.URL, liveBody)
		raw := mustRead(t, resp.Body)
		resp.Body.Close()
		var msg struct {
			Message string `json:"message"`
		}
		if resp.StatusCode != c.wantStatus || json.Unmarshal([]byte(raw), &msg) != nil || !strings.Contains(msg.Message, c.wantMessage) {
			t.Errorf("%s: status %d body %s", name, resp.StatusCode, raw)
		}
		if c.retryAfter != "" && resp.Header.Get("Retry-After") != c.retryAfter {
			t.Errorf("%s: Retry-After %q", name, resp.Header.Get("Retry-After"))
		}
		if strings.Contains(raw, "panic") || strings.Contains(raw, "Forbidden") {
			t.Errorf("%s: the worker's internals leaked to the page: %s", name, raw)
		}
	}
}

func TestUnreachableWorkerIsA502(t *testing.T) {
	wk := newWorker(t, cannedRun)
	url := wk.URL
	wk.Close()
	ts := newLiveServer(t, url)
	resp := postPage(t, ts.URL, liveBody)
	raw := mustRead(t, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 502 || !strings.Contains(raw, "could not be reached") {
		t.Errorf("status %d body %s", resp.StatusCode, raw)
	}
}

// The status is already sent when the worker dies part-way, so the page is told in the stream.
func TestStreamThatBreaksMidRunEndsWithAnErrorEvent(t *testing.T) {
	wk := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		w.Write(wire.MetaLine(wire.Meta{}))
		w.Write(wire.StepLine(wire.Tick{I: 0}))
		w.(http.Flusher).Flush()
		conn, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			conn.Close() // the connection drops with the response unfinished
		}
	})
	ts := newLiveServer(t, wk.URL)
	resp := postPage(t, ts.URL, liveBody)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	evs := pageEvents(t, string(raw))
	if len(evs) == 0 || evs[len(evs)-1].Type != "error" {
		t.Fatalf("the last event must be an error; events: %s", raw)
	}
	var tail struct{ Message string }
	json.Unmarshal([]byte(strings.TrimSpace(string(raw[strings.LastIndex(string(raw), `{"type":"error"`):]))), &tail)
	if !strings.Contains(string(raw), "stopped before the run finished") {
		t.Errorf("error message: %s", raw)
	}
}

// A page that leaves must cancel the worker's run.
func TestPageDisconnectCancelsTheWorkersRequest(t *testing.T) {
	cancelled := make(chan struct{})
	wk := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write(wire.MetaLine(wire.Meta{}))
		w.(http.Flusher).Flush()
		<-r.Context().Done() // runs until the request is cancelled
		close(cancelled)
	})
	ts := newLiveServer(t, wk.URL)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/v1/playground/run", strings.NewReader(liveBody))
	req.Header.Set("Accept", pageAccept)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	bufio.NewReader(resp.Body).ReadString('\n') // under way
	cancel()
	resp.Body.Close()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker kept running after the page left")
	}
}

func TestPrecomputedHousesNeverReachTheWorker(t *testing.T) {
	wk := newWorker(t, cannedRun)
	ts := newLiveServer(t, wk.URL)

	// exactly the second scenario card: solar, battery, battery power, export cap and load all match
	card := `{"address":"x","pv_kw_ac":6.6,"battery_kwh":13.5,"battery_kw":5,"export_cap_kw":5,"daily_load_kwh":18,"window":"validation"}`
	resp := postPage(t, ts.URL, card)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.Header.Get("X-Run-Id") != "6.6kw-13.5kwh" {
		t.Errorf("the card should be served from memory, got X-Run-Id %q", resp.Header.Get("X-Run-Id"))
	}
	// the same card with a different load is a different house
	other := strings.Replace(card, `"daily_load_kwh":18`, `"daily_load_kwh":19`, 1)
	resp = postPage(t, ts.URL, other)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if _, body := wk.request(); !strings.Contains(string(body), `"daily_load_kwh":19`) {
		t.Errorf("a different load must go to the worker, which was sent %s", body)
	}
	if wk.calls.Load() != 1 {
		t.Errorf("the worker was called %d times, want 1 (only the non-example house)", wk.calls.Load())
	}
}

func liveIDFor(pv, b, bp, ec, l float64) string {
	return wire.Params{PvKwAc: pv, BatteryKwh: b, BatteryKw: bp, ExportCapKw: ec, DailyLoadKwh: l, Window: "validation"}.ID()
}

func TestBadRequestsNeverReachTheWorker(t *testing.T) {
	wk := newWorker(t, cannedRun)
	ts := newLiveServer(t, wk.URL)
	for _, body := range []string{`{`, `{"address":"","pv_kw_ac":5,"battery_kwh":5,"window":"validation"}`,
		`{"address":"x","pv_kw_ac":5000,"battery_kwh":5,"window":"validation"}`,
		`{"address":"x","pv_kw_ac":5,"battery_kwh":0,"window":"validation"}`,
		`{"address":"x","pv_kw_ac":5,"battery_kwh":5,"window":"nope"}`} {
		resp := postPage(t, ts.URL, body)
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("%s: status %d", body, resp.StatusCode)
		}
	}
	if wk.calls.Load() != 0 {
		t.Errorf("the worker saw %d invalid requests", wk.calls.Load())
	}
}

// Clients that do not ask for a stream keep the old behaviour: the closest example.
func TestNonStreamingClientsKeepTheClosestExample(t *testing.T) {
	wk := newWorker(t, cannedRun)
	ts := newLiveServer(t, wk.URL)
	resp, err := http.Post(ts.URL+"/v1/playground/run", "application/json", strings.NewReader(liveBody))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		RunID string `json:"run_id"`
	}
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.RunID == "" || wk.calls.Load() != 0 {
		t.Errorf("run id %q, worker calls %d", got.RunID, wk.calls.Load())
	}
}

func TestStepDetailRouting(t *testing.T) {
	wk := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"t":"from-worker"}`)
	})
	ts := newLiveServer(t, wk.URL)
	get := func(path string) (int, string) {
		r, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		return r.StatusCode, mustRead(t, r.Body)
	}

	// an example's stored step stays local
	if code, body := get("/v1/playground/run/10kw-10kwh/steps/12"); code != 200 || strings.Contains(body, "from-worker") {
		t.Errorf("stored step: %d %.60s", code, body)
	}
	if wk.calls.Load() != 0 {
		t.Fatalf("a stored step reached the worker")
	}
	// another step of an example is explained by the worker, for the example's own house
	code, body := get("/v1/playground/run/6.6kw-13.5kwh/steps/13")
	if code != 200 || !strings.Contains(body, "from-worker") {
		t.Errorf("unstored step: %d %s", code, body)
	}
	if r, _ := wk.request(); r.URL.Path != "/v1/playground/run/"+liveIDFor(6.6, 13.5, 5, 5, 18)+"/steps/13" {
		t.Errorf("worker was asked %s", r.URL.Path)
	}
	// the id of a live run (from X-Run-Id) is passed on as it is
	code, _ = get("/v1/playground/run/" + liveID + "/steps/200")
	if r, _ := wk.request(); code != 200 || r.URL.Path != "/v1/playground/run/"+liveID+"/steps/200" {
		t.Errorf("live id: %d %s", code, r.URL.Path)
	}
	// ids that are neither stay unknown, without bothering the worker
	calls := wk.calls.Load()
	if code, _ := get("/v1/playground/run/nonsense/steps/0"); code != 404 {
		t.Errorf("unknown id: %d", code)
	}
	if code, _ := get("/v1/playground/run/10kw-10kwh/steps/99999"); code != 400 {
		t.Errorf("step out of range for an example: %d", code)
	}
	if wk.calls.Load() != calls {
		t.Error("an unknown id or a bad step reached the worker")
	}
}

func TestWorkerErrorsOnStepsAreRelayed(t *testing.T) {
	wk := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, `{"message":"step must be an integer from 0 to 9791"}`)
	})
	ts := newLiveServer(t, wk.URL)
	r, err := http.Get(ts.URL + "/v1/playground/run/" + liveID + "/steps/99999")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if body := mustRead(t, r.Body); r.StatusCode != 400 || !strings.Contains(body, "from 0 to 9791") {
		t.Errorf("%d %s", r.StatusCode, body)
	}
}

func TestIdentityTokenIsSentToTheWorker(t *testing.T) {
	wk := newWorker(t, cannedRun)
	var audience string
	ts := newLiveServer(t, wk.URL, WithTokenSource(func(ctx context.Context, aud string) (string, error) {
		audience = aud
		return "tok123", nil
	}))
	resp := postPage(t, ts.URL, liveBody)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if r, _ := wk.request(); r.Header.Get("Authorization") != "Bearer tok123" {
		t.Errorf("Authorization %q", r.Header.Get("Authorization"))
	}
	if audience != wk.URL {
		t.Errorf("token asked for audience %q, want the worker's URL %q", audience, wk.URL)
	}

	// no source and a plain-http worker (local, or private): no token is sent
	wk2 := newWorker(t, cannedRun)
	ts2 := newLiveServer(t, wk2.URL)
	resp = postPage(t, ts2.URL, liveBody)
	resp.Body.Close()
	if r, _ := wk2.request(); r.Header.Get("Authorization") != "" {
		t.Errorf("an http worker should get no token, got %q", r.Header.Get("Authorization"))
	}

	// a failing token source is a clear 502, and the worker is not called
	wk3 := newWorker(t, cannedRun)
	ts3 := newLiveServer(t, wk3.URL, WithTokenSource(func(context.Context, string) (string, error) { return "", fmt.Errorf("no metadata server") }))
	resp = postPage(t, ts3.URL, liveBody)
	resp.Body.Close()
	if resp.StatusCode != 502 || wk3.calls.Load() != 0 {
		t.Errorf("status %d, worker calls %d", resp.StatusCode, wk3.calls.Load())
	}
}

func jwt(exp time.Time) string {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return enc(`{"alg":"RS256"}`) + "." + enc(fmt.Sprintf(`{"exp":%d}`, exp.Unix())) + "." + enc("signature")
}

func TestMetadataTokens(t *testing.T) {
	var hits atomic.Int32
	var gotAudience, gotFlavor string
	expiry := time.Now().Add(time.Hour)
	md := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotAudience, gotFlavor = r.URL.Query().Get("audience"), r.Header.Get("Metadata-Flavor")
		if !strings.HasSuffix(r.URL.Path, "/service-accounts/default/identity") {
			t.Errorf("path %s", r.URL.Path)
		}
		io.WriteString(w, jwt(expiry))
	}))
	defer md.Close()
	old := metadataBase
	metadataBase = md.URL
	defer func() { metadataBase = old }()

	tokens := MetadataTokens(nil)
	tok, err := tokens(context.Background(), "https://worker-abc.a.run.app")
	if err != nil || tok != jwt(expiry) {
		t.Fatalf("token %q (%v)", tok, err)
	}
	if gotAudience != "https://worker-abc.a.run.app" || gotFlavor != "Google" {
		t.Errorf("audience %q, Metadata-Flavor %q", gotAudience, gotFlavor)
	}
	tokens(context.Background(), "https://worker-abc.a.run.app")
	if hits.Load() != 1 {
		t.Errorf("a token that is still good must be reused; the metadata server was asked %d times", hits.Load())
	}
	tokens(context.Background(), "https://other.a.run.app") // another audience needs its own token
	if hits.Load() != 2 {
		t.Errorf("hits %d", hits.Load())
	}

	// a token that is about to expire is replaced
	expiry = time.Now().Add(time.Minute)
	soon := MetadataTokens(nil)
	soon(context.Background(), "aud")
	soon(context.Background(), "aud")
	if hits.Load() != 4 {
		t.Errorf("a token that expires within five minutes must be refreshed; hits %d", hits.Load())
	}

	md.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 403) })
	if _, err := MetadataTokens(nil)(context.Background(), "x"); err == nil {
		t.Error("a refused request must be an error")
	}
}

func TestRateLimitOnLiveRuns(t *testing.T) {
	wk := newWorker(t, cannedRun)
	ts := newLiveServer(t, wk.URL, WithRateLimit(2))
	run := func(ip string) (int, string) {
		req, _ := http.NewRequest("POST", ts.URL+"/v1/playground/run?speed=max", strings.NewReader(liveBody))
		req.Header.Set("Accept", pageAccept)
		if ip != "" {
			req.Header.Set("X-Forwarded-For", ip+", 10.0.0.1")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, resp.Header.Get("Retry-After")
	}
	for i := 0; i < 2; i++ {
		if code, _ := run("1.1.1.1"); code != 200 {
			t.Fatalf("run %d: %d", i, code)
		}
	}
	code, retry := run("1.1.1.1")
	if code != 429 || retry == "" {
		t.Errorf("the third run in a minute: status %d, Retry-After %q", code, retry)
	}
	if code, _ := run("2.2.2.2"); code != 200 {
		t.Errorf("another client must not be limited: %d", code)
	}
	// precomputed examples cost nothing, so they are not limited
	for i := 0; i < 4; i++ {
		req, _ := http.NewRequest("POST", ts.URL+"/v1/playground/run?speed=max", strings.NewReader(`{"address":"x","pv_kw_ac":10.5,"battery_kwh":10,"window":"validation"}`))
		req.Header.Set("Accept", pageAccept)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("example %d: %d", i, resp.StatusCode)
		}
	}
}

func TestLimiterWindow(t *testing.T) {
	l := newLimiter(2)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 2; i++ {
		if ok, _ := l.allow("a"); !ok {
			t.Fatal("under the limit")
		}
	}
	ok, retry := l.allow("a")
	if ok || retry <= 0 || retry > time.Minute {
		t.Errorf("over the limit: ok=%v retry=%v", ok, retry)
	}
	now = now.Add(61 * time.Second)
	if ok, _ := l.allow("a"); !ok {
		t.Error("a minute later the client may run again")
	}
}

func TestClientIP(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.1.2.3:5555"
	if clientIP(r) != "10.1.2.3" {
		t.Errorf("remote addr: %q", clientIP(r))
	}
	r.Header.Set("X-Forwarded-For", " 203.0.113.9 , 10.0.0.1")
	if clientIP(r) != "203.0.113.9" {
		t.Errorf("the first X-Forwarded-For entry is the client: %q", clientIP(r))
	}
}

func TestBadWorkerURLIsRejectedAtStartup(t *testing.T) {
	for _, u := range []string{"worker", "ftp://worker", "http://", "://x"} {
		if _, err := New(os.DirFS("../.."), "runs", time.Second, WithWorker(u), quiet()); err == nil {
			t.Errorf("%q should not be accepted", u)
		}
	}
	srv, err := New(os.DirFS("../.."), "runs", time.Second, quiet())
	if err != nil || srv.Live() {
		t.Errorf("without a worker the server is not live: %v", err)
	}
}
