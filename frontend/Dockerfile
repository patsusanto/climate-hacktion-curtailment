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

# Cloud Run sets PORT (default 8080); nginx listens on it
ENV PORT=8080
# Let the entrypoint detect DNS resolvers for runtime backend lookups
ENV NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1
# Where /api/* requests are forwarded, no trailing slash (override at runtime)
ENV BACKEND_URL=http://backend:8080
EXPOSE 8080
