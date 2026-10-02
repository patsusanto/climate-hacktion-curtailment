// Command server is the playground API. It is the entry point the Dockerfile
// builds (go build .), so it stays in the backend root; the code lives in
// internal/api.
package main

import (
	"embed"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"climate-hacktion-curtailment/backend/internal/api"
)

// Precomputed example runs, baked into the binary and loaded at startup.
//
//go:embed runs/*.json
var runsFS embed.FS

func main() {
	// STREAM_SECONDS sets how long a full run takes to stream.
	streamFor := api.DefaultStreamFor
	if v, err := strconv.Atoi(os.Getenv("STREAM_SECONDS")); err == nil && v > 0 {
		streamFor = time.Duration(v) * time.Second
	}

	// WORKER_URL is the model worker. With it, houses that are not precomputed are run live;
	// without it every request is answered from the precomputed runs.
	var opts []api.Option
	if url := os.Getenv("WORKER_URL"); url != "" {
		opts = append(opts, api.WithWorker(url))
	}
	if v, err := strconv.Atoi(os.Getenv("RATE_LIMIT_PER_MINUTE")); err == nil && v >= 0 {
		opts = append(opts, api.WithRateLimit(v))
	}

	srv, err := api.New(runsFS, "runs", streamFor, opts...)
	if err != nil {
		log.Fatalf("starting: %v", err)
	}
	log.Printf("loaded %d run(s); live runs: %v", srv.RunCount(), srv.Live())

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, srv.Routes()))
}
