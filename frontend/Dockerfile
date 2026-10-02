# syntax=docker/dockerfile:1

# ---- build stage ----
FROM node:22-alpine AS build
WORKDIR /app

# Copy manifests first so dependency installs are cached
COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

# ---- runtime stage ----
FROM nginx:1.27-alpine
# The official image renders *.template files with env vars at startup
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html

# Where /api/* requests are forwarded (override at runtime)
ENV BACKEND_URL=http://backend:8080
EXPOSE 80
