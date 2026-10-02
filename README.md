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
