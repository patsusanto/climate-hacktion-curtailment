package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

func (s *server) routes() *http.ServeMux {
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

func main() {
	runs, err := loadRuns(runsFS, "runs")
	if err != nil {
		log.Fatalf("loading runs: %v", err)
	}
	log.Printf("loaded %d run(s)", len(runs))

	// STREAM_SECONDS sets how long a full run takes to stream.
	secs := defaultStreamS
	if v, err := strconv.Atoi(os.Getenv("STREAM_SECONDS")); err == nil && v > 0 {
		secs = v
	}
	s := &server{runs: runs, streamFor: time.Duration(secs) * time.Second}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, s.routes()))
}
