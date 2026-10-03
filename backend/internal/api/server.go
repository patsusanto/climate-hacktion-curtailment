// Package api is the playground HTTP server. It checks every request and streams the answer to
// the page: a precomputed example run from memory, or, for any other house, a live run that the
// model worker computes and this server relays.
//
// It must stay free of the model packages (internal/model/...) and of the worker
// (internal/worker): the service that ships to production only reads and streams runs, and calls
// the model over HTTP. The boundary test in this package enforces that.
package api

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"time"
)

// DefaultStreamFor is how long a full run takes to stream unless configured.
const DefaultStreamFor = 25 * time.Second

// Server serves the playground API.
type Server struct {
	runs map[string]*Run
	// streamFor is how long a full example run takes to stream. ?speed=max skips pacing.
	streamFor time.Duration

	// The model worker, if one is set up (see WithWorker).
	workerURL     string
	client        *http.Client
	tokens        TokenSource
	ratePerMinute int
	log           *log.Logger
	live          *live
}

// New loads every run in dir of fsys and returns a server for them. The runs
// are embedded by the caller, so this package never needs to know where the
// JSON files live. Options add the live path to the model worker.
func New(fsys fs.FS, dir string, streamFor time.Duration, opts ...Option) (*Server, error) {
	runs, err := loadRuns(fsys, dir)
	if err != nil {
		return nil, err
	}
	s := &Server{runs: runs, streamFor: streamFor, ratePerMinute: DefaultRatePerMinute, log: log.Default()}
	for _, opt := range opts {
		opt(s)
	}
	if s.workerURL != "" {
		if s.live, err = newLive(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// RunCount is the number of runs loaded.
func (s *Server) RunCount() int { return len(s.runs) }

// Live reports whether houses that are not precomputed are run by a model worker.
func (s *Server) Live() bool { return s.live != nil }

// Routes is the server's HTTP handler.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!"))
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /v1/playground/run", s.startRun)
	mux.HandleFunc("GET /v1/playground/run/{id}/events", s.events)
	mux.HandleFunc("GET /v1/playground/run/{id}/steps/{i}", s.step)

	return mux
}
