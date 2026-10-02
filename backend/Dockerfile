# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27

# ---- build stage ----
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

# Copy module files first so dependency downloads are cached
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server .

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server

ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
