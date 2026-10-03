package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"climate-hacktion-curtailment/backend/internal/wire"
)

// The live path: for a house that is not one of the precomputed examples, the page's request is
// passed to the model worker, and the worker's stream is relayed back unchanged. This package
// does not import the model; the worker is another service.

// Option configures a Server.
type Option func(*Server)

// WithWorker sends the page's requests for houses that are not precomputed to the model worker
// at rawURL. Without it every request is answered from the precomputed runs.
func WithWorker(rawURL string) Option { return func(s *Server) { s.workerURL = rawURL } }

// WithHTTPClient sets the client used to reach the worker (mainly for tests).
func WithHTTPClient(c *http.Client) Option { return func(s *Server) { s.client = c } }

// WithTokenSource sets how the identity token for the worker is obtained (mainly for tests).
func WithTokenSource(t TokenSource) Option { return func(s *Server) { s.tokens = t } }

// WithRateLimit allows each client this many live runs a minute. Zero turns the limit off.
func WithRateLimit(perMinute int) Option { return func(s *Server) { s.ratePerMinute = perMinute } }

// WithLogger sets where the server logs (default: the standard logger).
func WithLogger(l *log.Logger) Option { return func(s *Server) { s.log = l } }

// DefaultRatePerMinute is how many live runs one client may start in a minute.
const DefaultRatePerMinute = 12

// stepsPerRun is how many step details a client may ask the worker for per live run it may
// start. Clicking through a run asks for a few; the limit stops step requests being used to make
// the worker compute houses without counting as runs.
const stepsPerRun = 10

// live is the connection to the model worker.
type live struct {
	base   *url.URL
	client *http.Client
	tokens TokenSource // nil: the worker needs no identity token
	limit  *limiter    // live runs; nil: no limit
	steps  *limiter    // step details from the worker; nil: no limit
	log    *log.Logger
}

func newLive(s *Server) (*live, error) {
	base, err := url.Parse(s.workerURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("WORKER_URL %q is not an http(s) URL", s.workerURL)
	}
	lv := &live{base: base, client: s.client, tokens: s.tokens, log: s.log}
	if lv.client == nil {
		// No overall timeout: a run streams for a while. Connecting and the first byte are bounded.
		lv.client = &http.Client{Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			ResponseHeaderTimeout: 60 * time.Second,
			MaxIdleConnsPerHost:   8,
			ForceAttemptHTTP2:     true,
		}}
	}
	if lv.tokens == nil && base.Scheme == "https" {
		// Cloud Run services are https and private: call them with this service's identity.
		lv.tokens = MetadataTokens(nil)
	}
	if s.ratePerMinute > 0 {
		lv.limit = newLimiter(s.ratePerMinute)
		lv.steps = newLimiter(s.ratePerMinute * stepsPerRun)
	}
	return lv, nil
}

// request builds a call to the worker, with the identity token if one is needed.
func (lv *live) request(ctx context.Context, method, path string, query url.Values, body []byte) (*http.Request, error) {
	u := *lv.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = query.Encode()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if lv.tokens != nil {
		tok, err := lv.tokens(ctx, lv.base.Scheme+"://"+lv.base.Host)
		if err != nil {
			return nil, fmt.Errorf("getting an identity token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return req, nil
}

// relayError turns a failed answer from the worker into one for the page. The page's own mistakes
// (400) and a busy worker (503) are passed on with the worker's message; anything else is the
// worker's problem and becomes a 502.
func (lv *live) relayError(w http.ResponseWriter, resp *http.Response) {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	var msg struct {
		Message string `json:"message"`
	}
	json.Unmarshal(raw, &msg)
	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound:
		if msg.Message == "" {
			msg.Message = "the model service did not accept this request"
		}
		writeError(w, resp.StatusCode, msg.Message)
	case http.StatusServiceUnavailable:
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			w.Header().Set("Retry-After", ra)
		}
		if msg.Message == "" {
			msg.Message = "the model is busy; try again in a moment"
		}
		writeError(w, http.StatusServiceUnavailable, msg.Message)
	default:
		lv.log.Printf("model worker answered %d: %.200s", resp.StatusCode, raw)
		writeError(w, http.StatusBadGateway, "the model service could not run this house")
	}
}

// liveRun asks the worker to run the house and relays its stream to the page.
func (s *Server) liveRun(w http.ResponseWriter, r *http.Request, p wire.Params, format string) {
	lv := s.live
	if lv.limit != nil {
		if ok, retry := lv.limit.allow(clientIP(r)); !ok {
			secs := int(retry.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf("too many runs; try again in %d seconds", secs))
			return
		}
	}
	body, err := json.Marshal(p.Request())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not encode the request")
		return
	}
	query := url.Values{}
	if r.URL.Query().Get("speed") == "max" {
		query.Set("speed", "max")
	}
	req, err := lv.request(r.Context(), http.MethodPost, "/v1/playground/run", query, body)
	if err != nil {
		lv.log.Printf("live run %s: %v", p.ID(), err)
		writeError(w, http.StatusBadGateway, "the model service could not be reached")
		return
	}
	req.Header.Set("Accept", r.Header.Get("Accept"))

	resp, err := lv.client.Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			return // the page has gone
		}
		lv.log.Printf("live run %s: %v", p.ID(), err)
		writeError(w, http.StatusBadGateway, "the model service could not be reached")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		lv.relayError(w, resp)
		return
	}

	h := w.Header()
	for _, name := range []string{"Content-Type", "Cache-Control", "X-Accel-Buffering", "X-Run-Id"} {
		if v := resp.Header.Get(name); v != "" {
			h.Set(name, v)
		}
	}
	w.WriteHeader(http.StatusOK)

	// Relay as it arrives: flush after every chunk, so the page sees steps as they are sent.
	rc := http.NewResponseController(w)
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return // the page has gone; returning cancels the worker's request
			}
			rc.Flush()
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			if r.Context().Err() != nil {
				return
			}
			// The worker stopped part-way. The status is already sent, so tell the page in-stream.
			lv.log.Printf("live run %s: stream broke: %v", p.ID(), err)
			w.Write(wire.Encode(format, wire.ErrorLine("the model service stopped before the run finished")))
			rc.Flush()
			return
		}
	}
}

// liveStep asks the worker for the detail of one step.
func (s *Server) liveStep(w http.ResponseWriter, r *http.Request, p wire.Params, step string) {
	lv := s.live
	if lv.steps != nil {
		if ok, retry := lv.steps.allow(clientIP(r)); !ok {
			secs := int(retry.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf("too many requests; try again in %d seconds", secs))
			return
		}
	}
	req, err := lv.request(r.Context(), http.MethodGet, "/v1/playground/run/"+p.ID()+"/steps/"+url.PathEscape(step), nil, nil)
	if err != nil {
		lv.log.Printf("live step %s/%s: %v", p.ID(), step, err)
		writeError(w, http.StatusBadGateway, "the model service could not be reached")
		return
	}
	resp, err := lv.client.Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		lv.log.Printf("live step %s/%s: %v", p.ID(), step, err)
		writeError(w, http.StatusBadGateway, "the model service could not be reached")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		lv.relayError(w, resp)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, io.LimitReader(resp.Body, 1<<20))
}

// ---- identity tokens ----

// TokenSource returns an identity token for calling the service at audience.
type TokenSource func(ctx context.Context, audience string) (string, error)

// metadataBase is where a Google Cloud instance asks for its identity (a variable for tests).
var metadataBase = "http://metadata.google.internal"

// MetadataTokens gets Google identity tokens from the metadata server, which is how a Cloud Run
// service proves who it is to another private Cloud Run service. Tokens are cached until shortly
// before they expire.
func MetadataTokens(client *http.Client) TokenSource {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	var mu sync.Mutex
	type cached struct {
		token string
		exp   time.Time
	}
	cache := map[string]cached{}
	return func(ctx context.Context, audience string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if c, ok := cache[audience]; ok && time.Until(c.exp) > 5*time.Minute {
			return c.token, nil
		}
		u := metadataBase + "/computeMetadata/v1/instance/service-accounts/default/identity?audience=" + url.QueryEscape(audience)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Metadata-Flavor", "Google")
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("the metadata server answered %d", resp.StatusCode)
		}
		token := strings.TrimSpace(string(raw))
		exp, err := tokenExpiry(token)
		if err != nil {
			return "", err
		}
		cache[audience] = cached{token, exp}
		return token, nil
	}
}

// tokenExpiry reads the "exp" claim of a JWT, without checking its signature (the worker does).
func tokenExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("the identity token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, err
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("the identity token has no expiry")
	}
	return time.Unix(claims.Exp, 0), nil
}

// ---- rate limit ----

// limiter allows each client a number of events in any minute.
type limiter struct {
	mu        sync.Mutex
	perMinute int
	hits      map[string][]time.Time
	now       func() time.Time
}

func newLimiter(perMinute int) *limiter {
	return &limiter{perMinute: perMinute, hits: map[string][]time.Time{}, now: time.Now}
}

// allow records an event for key, or says how long to wait.
func (l *limiter) allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.perMinute {
		l.hits[key] = recent
		return false, time.Minute - now.Sub(recent[0])
	}
	l.hits[key] = append(recent, now)
	if len(l.hits) > 10000 { // forget clients that have been quiet
		for k, ts := range l.hits {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= time.Minute {
				delete(l.hits, k)
			}
		}
	}
	return true, 0
}

// clientIP is the page's address: the first entry of X-Forwarded-For (the proxies in front of this
// service put the client there), otherwise the connection's.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, _ := strings.Cut(xff, ","); strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
