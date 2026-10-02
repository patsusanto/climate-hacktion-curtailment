// Package api is the playground HTTP server. It serves precomputed example
// runs over Server-Sent Events and keeps no state beyond them.
//
// It must stay free of the model packages (internal/model/...): the service
// that ships to production only needs to read and stream finished runs. The
// boundary test in this package enforces that.
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"time"
)

// DefaultStreamFor is how long a full run takes to stream unless configured.
const DefaultStreamFor = 25 * time.Second

// Server serves the playground API.
type Server struct {
	runs map[string]*Run
	// streamFor is how long a full run takes to stream. ?speed=max skips pacing.
	streamFor time.Duration
}

// New loads every run in dir of fsys and returns a server for them. The runs
// are embedded by the caller, so this package never needs to know where the
// JSON files live.
func New(fsys fs.FS, dir string, streamFor time.Duration) (*Server, error) {
	runs, err := loadRuns(fsys, dir)
	if err != nil {
		return nil, err
	}
	return &Server{runs: runs, streamFor: streamFor}, nil
}

// RunCount is the number of runs loaded.
func (s *Server) RunCount() int { return len(s.runs) }

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
