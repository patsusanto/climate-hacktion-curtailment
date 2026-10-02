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
