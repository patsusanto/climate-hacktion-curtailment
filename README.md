# climate-hacktion-curtailment

## Backend

A Go HTTP server in `backend/`.

| Endpoint      | Description                    |
| ------------- | ------------------------------ |
| `GET /`       | Returns `Hello, World!`        |
| `GET /health` | Returns `{"status":"ok"}`      |

The server listens on port `8080` by default. Set the `PORT` environment variable to change it.

### Run locally

Requires Go (see `backend/go.mod` for the version).

```sh
cd backend
go run .
```

### Run with Docker

```sh
cd backend
docker build -t curtailment-backend .
docker run --rm -p 8080:8080 curtailment-backend
```

Then check it:

```sh
curl localhost:8080/
curl localhost:8080/health
```

To use a different host port, change the first number in `-p`, e.g. `-p 3000:8080`.

If the build fails because the Go image tag doesn't exist, pass a Go version that does, and lower the `go` line in `backend/go.mod` to match:

```sh
docker build --build-arg GO_VERSION=1.24 -t curtailment-backend .
```

## Frontend

A React 19 + TypeScript + Vite app in `frontend/`. It shows the backend's `/health` status.

```sh
cd frontend
npm install
npm run dev      # http://localhost:5173, proxies /api/* to localhost:8080
npm run build
```

### Run both with Docker

The frontend container serves the built app with nginx on port 8080 and forwards `/api/*` to `BACKEND_URL` (default `http://backend:8080`).

```sh
docker network create app
docker run -d --rm --name backend --network app curtailment-backend
docker build -t curtailment-frontend frontend
docker run -d --rm --name frontend --network app -p 8080:8080 curtailment-frontend
```

Open http://localhost:8080.

## Deploy to Google Cloud Run

Each folder is its own Cloud Run service. Deploy the backend first, then point the frontend at it.

```sh
PROJECT=<your-project-id>
REGION=us-central1

gcloud config set project $PROJECT

# 1. Backend
gcloud run deploy backend \
  --source backend \
  --region $REGION \
  --allow-unauthenticated

# 2. Frontend, with the backend's URL (no trailing slash)
BACKEND_URL=$(gcloud run services describe backend --region $REGION --format 'value(status.url)')

gcloud run deploy frontend \
  --source frontend \
  --region $REGION \
  --allow-unauthenticated \
  --set-env-vars BACKEND_URL=$BACKEND_URL
```

`gcloud run deploy` prints the frontend URL when it finishes. Cloud Run sets `PORT` for you and both containers read it.

The backend must allow unauthenticated requests because the frontend proxies to its public URL. Making it private would require the proxy to send identity tokens.

## Playground API

The backend serves precomputed example runs from `backend/runs/*.json` (embedded in the binary and loaded into memory at startup). There is no database. `run_id` selects a run, and the address only changes the label in `meta`. A request gets the run for its window (`"validation"` or `"test"`) whose solar and battery sizes are closest. Custom `{start, end}` windows return 400.

| Route | Description |
| --- | --- |
| `POST /v1/playground/run` | Body is a `PlaygroundRequest` (see `interfacespec.md`). Returns `{run_id, meta}` as JSON. |
| `GET /v1/playground/run/{run_id}/events` | Server-Sent Events: `step` events in order of `i`, then one `done` with the summary. Send `Last-Event-ID` to resume. An unknown id sends `event: error`. |
| `GET /v1/playground/run/{run_id}/steps/{i}` | `StepDecision` (forecast leads and stories) for one step. Returns 404 for steps with no stored detail. |

```sh
curl -X POST localhost:8080/v1/playground/run \
  -d '{"address":"1 Example St, Sydney","pv_kw_ac":10.5,"battery_kwh":10,"window":"validation"}'
curl -N localhost:8080/v1/playground/run/10kw-10kwh/events
```

A full stream takes about 25 seconds. Set `STREAM_SECONDS` to change that, or add `?speed=max` to skip the pacing. Run the tests with `cd backend && go test ./...`.

### Adding a run

Add `backend/runs/<run_id>.json` with `run_id`, `window_name`, `meta`, `ticks` (one per step, `i` from 0), `summary`, and optionally `steps` (a map from step index to `StepDecision`). The service refuses to start if `ticks` doesn't match `meta.window.n`.

## Code layout

```
backend/
  main.go            entry point the Dockerfile builds; embeds runs/ and starts the API
  runs/              precomputed example runs (JSON), embedded in the binary
  Dockerfile, cloudbuild.yaml, .dockerignore, go.mod
  cmd/genrun/        offline command that replays a window and writes a run
  internal/
    api/             the HTTP server: routes, validation, SSE streaming
    wire/            the JSON shapes from interfacespec.md (plain data)
    model/           house, sim, planner, policy, forecast, data, engine, split, golden
    compat/          tests that the engine's run files are served by the API
```

One rule keeps the deployed service small: `internal/api`, `internal/wire` and `main.go` must not import anything under `internal/model/`. `internal/api/boundary_test.go` fails the build if they do. `internal/wire` may import only the standard library.

## Generating runs with the Go model

The model is implemented in Go in `backend/internal/model/`, ported from an original Python model that lives outside this repo. It replays a window of history with trained models and writes the run JSON the service serves, so the deployed service never needs the models. Training happens outside this repo.

| Package | What it does |
| --- | --- |
| `internal/model/house`, `internal/model/sim` | House specs, tariffs, battery physics, billing |
| `internal/model/planner`, `internal/model/policy` | The 8-hour dynamic program, the forecast stories, the rule baselines |
| `internal/model/forecast`, `internal/model/data` | Features, XGBoost price quantiles, the PV/load net; loading the export |
| `internal/model/engine`, `cmd/genrun` | The replay and the run-file writer |

**1. Provide an export** in `backend/export/` (git-ignored):

| File | Contents |
| --- | --- |
| `manifest.json` | `{"price": {"quantiles": [0.1, 0.5, 0.9], "feature_columns": {"12": [...]}, "models": {"12": ["price/lead12_q10.json", ...]}}, "units": "unit_model.json", "weather": "weather.csv.gz"}`, with an entry for each lead: 12, 24, 36, 72, 96 |
| `price/lead{L}_q{Q}.json` | One XGBoost booster per lead and quantile, saved with `Booster.save_model("x.json")` and cut to the early-stopping best round (`booster[:best_iteration + 1]`) |
| `unit_model.json` | The PV/load net: `input_dim`, `output_dim`, `feature_columns`, `layers` (`[{"w": [[...]], "b": [...]}]`, ReLU between layers), `scaler_x` and `scaler_y` (`{"mean", "scale"}`), `residuals` (`{"pv_unit": {"12": {"p10", "p90"}}, "load_unit": {...}}`). Outputs are PV units at each lead, then load units at each lead |
| `weather.csv.gz` | Hourly day-ahead forecast: `time` plus the seven `fc_*` columns listed in `internal/model/forecast/features.go` |
| `frame.csv.gz` | `time,price_aud_mwh,pv_unit,load_unit` every 5 minutes with no gaps |
| `golden.json` (optional) | Reference answers from the original model; see step 2 |

Times are RFC 3339 with a `+10:00` offset. Feature names are those of feature set v2 (`internal/model/forecast/features.go`). A window needs a week and an hour of history before it.

**2. Check the Go port against reference answers (optional).** If the export includes `golden.json` (features, forecast curves, planner decisions and a replay from the original model; the shape is in `internal/model/golden/golden_test.go`):

```sh
cd backend
go test ./internal/model/golden -v
```

These tests are skipped when there is no `golden.json`. They require at least 99.5% of replay actions to agree and bills within 0.02 AUD.

**3. Generate a run and serve it:**

```sh
cd backend
go run ./cmd/genrun -export export -window validation   # writes runs/10.5kw-10kwh.json
go run .
```

`genrun` flags include `-pv`, `-battery-kwh`, `-battery-kw`, `-load`, `-export-cap`, `-window validation|test`, `-id`, `-wear` and `-detail-every`. A full validation window takes seconds.
