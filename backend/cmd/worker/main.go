// Command worker is the live model service. The public API sends it a validated request and
// relays the stream it returns to the page.
//
//	DATA_DIR=data go run ./cmd/worker
//
// DATA_DIR holds the data of each window the worker can replay, one folder per window
// (data/validation, ...), exactly as cmd/genrun downloads it. The trained models are embedded.
//
// Environment:
//
//	PORT            port to listen on (default 8080)
//	DATA_DIR        where the window data is (default "data")
//	STREAM_SECONDS  how long a full run takes to stream, so the page animates (default 25; 0 = as fast as computed)
//	MAX_RUNS        model runs at once (default 2)
package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"climate-hacktion-curtailment/backend/internal/worker"
)

func main() {
	cfg := worker.Config{
		DataDir:   env("DATA_DIR", "data"),
		StreamFor: time.Duration(envInt("STREAM_SECONDS", 25)) * time.Second,
		MaxRuns:   envInt("MAX_RUNS", 2),
	}
	srv, err := worker.New(cfg)
	if err != nil {
		log.Fatalf("starting the worker: %v", err)
	}

	addr := ":" + env("PORT", "8080")
	log.Printf("listening on %s", addr)
	// No write timeout: a run streams for many seconds.
	server := &http.Server{Addr: addr, Handler: srv.Routes(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v >= 0 {
		return v
	}
	return fallback
}
