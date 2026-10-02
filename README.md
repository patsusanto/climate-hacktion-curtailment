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

The backend serves precomputed example runs from `backend/runs/*.json` (embedded in the binary and loaded into memory at startup). There is no database. `run_id` selects a run, and the address only changes the label in `meta`. A house that matches one of them exactly is served from memory. Any other house is run live by the model worker (see [Live runs](#live-runs-the-model-worker)); when there is no worker, the closest example is served. Custom `{start, end}` windows return 400.

| Route | Description |
| --- | --- |
| `POST /v1/playground/run` | Body is a `PlaygroundRequest` (see `interfacespec.md`). With `Accept: application/x-ndjson` or `text/event-stream` (what the frontend sends) the response **is the stream**: a `meta` event, one `step` event per 5 minutes, then `done`, with the run id in the `X-Run-Id` header. Without those `Accept` values it returns `{run_id, meta}` as JSON, for the closest example. A bad request gets a plain JSON `{"message": ...}` with status 400, before any stream starts. Other statuses: 429 (too many live runs), 502 and 503 (the model worker is down or busy). |
| `GET /v1/playground/run/{run_id}/events` | The two-step alternative: Server-Sent Events with `id:`, `event:` and a raw `data:` per step, then `done`. Send `Last-Event-ID` to resume. An unknown id sends `event: error`. |
| `GET /v1/playground/run/{run_id}/steps/{i}` | `StepDecision` (forecast leads and stories) for one step. For a live run, or a step of an example with no stored detail, the model worker explains the step, so any step works. Without a worker, a step with no stored detail returns 404. |

**Stream format (the `POST` route).** Each event is a JSON object with a `type`: `{"type":"meta","meta":{...}}`, `{"type":"step","tick":{...}}`, `{"type":"done","summary":{...}}` or `{"type":"error","message":"..."}`. In NDJSON each event is one line. In SSE each is a single `data: {...}` line followed by a blank line, with no `id:` or `event:` lines, because the page parses every line it receives. If the client offers both formats, NDJSON is used.

Each tick carries `cumulative_self_aud` (the self-consumption energy cost so far; positive means paid) next to `cumulative_savings_aud`. The planner's cost so far is the difference of the two. A run file without it is refused at startup.

```sh
# what the frontend does: one request, and the response is the stream
curl -N -X POST "localhost:8080/v1/playground/run?speed=max" \
  -H "Accept: application/x-ndjson" -H "Content-Type: application/json" \
  -d '{"address":"1 Example St, Sydney","pv_kw_ac":10.5,"battery_kwh":10,"window":"validation"}'

# the two-step flow: JSON first, then the events
curl -X POST localhost:8080/v1/playground/run \
  -d '{"address":"1 Example St, Sydney","pv_kw_ac":10.5,"battery_kwh":10,"window":"validation"}'
curl -N localhost:8080/v1/playground/run/10kw-10kwh/events
```

A full stream takes about 25 seconds. Set `STREAM_SECONDS` to change that, or add `?speed=max` to skip the pacing. Run the tests with `cd backend && go test ./...`.

### Adding a run

Add `backend/runs/<run_id>.json` with `run_id`, `window_name`, `meta`, `ticks` (one per step, `i` from 0), `summary`, and optionally `steps` (a map from step index to `StepDecision`). The service refuses to start if `ticks` doesn't match `meta.window.n`, or if a tick has no `cumulative_self_aud`.

## Live runs (the model worker)

Pressing Run on a house that is not one of the precomputed examples runs the trained model on that house, live, and streams the result to the page.

```
page --POST (stream)--> frontend nginx --/api--> backend (checks the request)
                                                    |  an exact precomputed house? served from memory
                                                    |  anything else: same request + identity token
                                                    v
                                                 worker (private; runs the model, streams the events)
```

- **The backend** (`internal/api`) checks every request first: `wire.PlaygroundRequest.Resolve` fills in the defaults (battery 5 kW, export cap 5 kW, load 15 kWh/day) and rejects anything outside the limits below, with a plain JSON 400. A house that matches a precomputed run on solar, battery kWh, battery kW, export cap and daily load is served from memory. Any other house, when the client asks for a stream, goes to the worker, and the worker's stream is relayed unchanged. Clients that do not ask for a stream, or a backend with no `WORKER_URL`, get the closest precomputed example, as before.
- **The worker** (`cmd/worker`, `internal/worker`) loads the trained models and the window's data once at startup, then runs the model per request (about a second) and streams `meta`, a `step` per 5 minutes, and `done`. It keeps the last few finished runs for step detail.
- **Step detail works for any step.** The run id in `X-Run-Id` encodes the house (for example `pv8-b20-bp5-ec5-l15-validation`), so `GET /v1/playground/run/{id}/steps/{i}` can be answered by any worker instance. For a precomputed example, steps without stored detail are explained by the worker too.
- **Limits** (`internal/wire/params.go`): solar 0 to 100 kW, battery 0 to 200 kWh, battery power 0 to 100 kW, export cap 0 to 100 kW (zero allowed), daily load 0 to 200 kWh.
- **Errors the page can see:** 400 with a message (bad input), 429 (more than `RATE_LIMIT_PER_MINUTE` live runs a minute from one client, default 12; precomputed houses are not counted), 503 with `Retry-After` (the worker is busy), 502 (the worker is down or failed). If the worker dies part-way through a stream, the stream ends with an `error` event.

| Variable | Service | Meaning |
| --- | --- | --- |
| `WORKER_URL` | backend | The worker's URL. Unset means no live runs. For `https://` URLs the backend sends a Google identity token, which is how one private Cloud Run service calls another. |
| `RATE_LIMIT_PER_MINUTE` | backend | Live runs per client per minute (default 12; 0 turns the limit off). |
| `DATA_DIR` | worker | Folder holding one subfolder per window, for example `data/validation` (default `data`). |
| `STREAM_SECONDS` | worker | How long a full run takes to stream, so the chart animates (default 25; 0 streams as fast as it is computed). |
| `MAX_RUNS` | worker | Model runs at once (default 2). More are told to retry. |

### Run it locally

The worker needs the window's data. `cmd/genrun` downloads it into `backend/data/<window>/` the first time (see below).

```sh
cd backend
DATA_DIR=data go run ./cmd/worker                      # terminal 1: the model, on :8080
PORT=8081 WORKER_URL=http://localhost:8080 go run .    # terminal 2: the backend, on :8081
```

Then point the frontend's dev proxy at `http://localhost:8081`, or call the backend directly:

```sh
curl -N -X POST localhost:8081/v1/playground/run -H "Accept: application/x-ndjson" \
  -d '{"address":"1 Example St","pv_kw_ac":8,"battery_kwh":20,"window":"validation"}'
```

### Deploy the model worker

The existing backend and frontend deploys do not change. The worker is a second Cloud Run service, built from `backend/Dockerfile.worker` by `backend/cloudbuild.worker.yaml`, and it is private.

1. **Put the data in a bucket.** Upload `backend/data/validation/` (git-ignored, from `cmd/genrun`) to a Cloud Storage bucket so that the bucket's root holds `validation/prices.csv`, and so on. Give the worker's service account `roles/storage.objectViewer` on the bucket.
2. **Create a Cloud Build trigger** for `backend/cloudbuild.worker.yaml` on `main`, with the included-files filter `backend/internal/**`, `backend/cmd/worker/**`, `backend/Dockerfile.worker*` and `backend/cloudbuild.worker.yaml`, and the substitution `_DATA_BUCKET` set to your bucket. Its service account needs the same roles as the existing triggers (Cloud Run Admin, Service Account User). Run it once. It deploys the service `worker` with 2 CPUs, 2 GiB, concurrency 2 and at most 3 instances, and without public access.
3. **Let the backend call it.** Give the backend's service account `roles/run.invoker` on the worker, then point the backend at it:

   ```sh
   WORKER=$(gcloud run services describe worker --region europe-west1 --format 'value(status.url)')
   BACKEND_SA=$(gcloud run services describe backend --region europe-west1 --format 'value(spec.template.spec.serviceAccountName)')
   gcloud run services add-iam-policy-binding worker --region europe-west1 \
     --member "serviceAccount:$BACKEND_SA" --role roles/run.invoker
   gcloud run services update backend --region europe-west1 --set-env-vars WORKER_URL=$WORKER
   ```

   The backend's own `cloudbuild.yaml` does not set `WORKER_URL`, and Cloud Run keeps it across deploys.
4. **Check it.** The worker should answer `403` without a token (`curl $WORKER/health`), and a custom house on the page should stream. The first run after the worker has been idle also pays its start-up (loading the models and the data); `--min-instances=1` on the worker avoids that.

## Code layout

```
backend/
  main.go            entry point the Dockerfile builds; embeds runs/ and starts the API
  runs/              precomputed example runs (JSON), embedded in the binary
  Dockerfile, cloudbuild.yaml, .dockerignore, go.mod            (the backend service)
  Dockerfile.worker, cloudbuild.worker.yaml                     (the model worker service)
  cmd/genrun/        offline command that replays a window and writes a run
  cmd/worker/        the live model service
  internal/
    api/             the HTTP server: routes, validation, streaming, the proxy to the worker
    worker/          runs the model per request and streams the result
    wire/            the JSON shapes, request limits and run ids (plain data)
    model/           the model: data download, house, forecasts, planner, replay, run-file writer
    compat/          tests that the model's run files are served by the API
```

One rule keeps the deployed service small: `internal/api`, `internal/wire` and `main.go` must not import anything under `internal/model/`, or `internal/worker`: the backend calls the worker over HTTP. `internal/api/boundary_test.go` fails the build if they do. `internal/wire` may import only the standard library.

## Generating runs with the Go model

The model is in Go in `backend/internal/model/`, with its trained models embedded (`internal/model/models/`). `cmd/genrun` replays a window of history with it and writes the run JSON the service serves, so the deployed service never needs the model. Training happens outside this repo; `backend/internal/model/README.md` describes the model and how to update it.

| Package | What it does |
| --- | --- |
| `internal/model/fetch`, `internal/model/data` | Download the public data (AEMO prices and pre-dispatch, Open-Meteo weather) and read it |
| `internal/model/house` | The simulated house: a Sydney roof and household on observed weather |
| `internal/model/features`, `internal/model/xgb`, `internal/model/forecast` | The models' inputs, the XGBoost price models (P10/P50/P90), the linear solar/demand model |
| `internal/model/planner`, `internal/model/battery` | The 8-hour battery plan, re-solved every 5 minutes; battery physics and the bill |
| `internal/model/simulate`, `internal/model/runfile` | The replay (planner vs self-consumption) and the run-file writer |

```sh
cd backend
go run ./cmd/genrun -window validation -pv 6.6 -battery-kwh 13.5 -load 18   # writes runs/6.6kw-13.5kwh.json
go run .
```

The first run for a window downloads its data into `backend/data/<window>/` (git-ignored). AEMO's pre-dispatch archives are about 125 MB a week, so this takes a few minutes; `-no-predispatch` skips them, and the price model is then less accurate. After that, a full validation window replays in about a second.

`genrun` flags: `-window validation|test`, `-pv`, `-battery-kwh`, `-battery-kw`, `-load`, `-export-cap`, `-id`, `-wear` (only for `savings_with_wear_aud`), `-detail-every` (default 12: forecast detail is kept for every 12th step, about 3 KB each), `-data`, `-out` and `-curtail forced_only`.

The runs in `runs/` are the validation window (16 Jul - 18 Aug 2026) for the default house (`10kw-10kwh`: 10.5 kW, 10 kWh) and the page's three scenarios. The models were trained on data before 19 Aug 2026, so these runs replay data the models have seen; the `test` window (19 Aug - 9 Sep 2026) is out of sample.
