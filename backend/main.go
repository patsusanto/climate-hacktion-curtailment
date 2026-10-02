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

	srv, err := api.New(runsFS, "runs", streamFor)
	if err != nil {
		log.Fatalf("loading runs: %v", err)
	}
	log.Printf("loaded %d run(s)", srv.RunCount())

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, srv.Routes()))
}
