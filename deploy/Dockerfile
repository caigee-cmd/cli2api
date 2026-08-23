# ---- frontend ----
FROM node:22-alpine AS frontend
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --cache /tmp/npm-cache
COPY frontend/ ./
RUN npm run build

# ---- go control plane ----
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist/ ./internal/webui/static/
RUN CGO_ENABLED=0 go build -o /out/qoder-api-proxy ./cmd/server

# ---- single runtime ----
FROM node:20-bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl tini \
  && rm -rf /var/lib/apt/lists/* \
  && npm install -g @qoder-ai/qodercli@1.1.27
WORKDIR /app
COPY --from=build /out/qoder-api-proxy /app/qoder-api-proxy
COPY worker/src /app/worker
COPY worker/last-plain.sample.json /app/worker/last-plain.sample.json
RUN mkdir -p /data /import/.qoder && chmod 700 /data
ENV HOST=0.0.0.0 \
    PORT=3010 \
    QODER_DATA_DIR=/data \
    QODER_HOME=/import/.qoder \
    QODER_NODE_BINARY=/usr/local/bin/node \
    QODER_WORKER_DAEMON=/app/worker/daemon.mjs \
    QODERCLI_JS=/usr/local/lib/node_modules/@qoder-ai/qodercli/bundle/qodercli.js \
    PLAIN_TEMPLATE_PATH=/app/worker/last-plain.sample.json \
    QODER_WORKER_BASE_PORT=32100
EXPOSE 3010
ENTRYPOINT ["/usr/bin/tini","--"]
CMD ["/app/qoder-api-proxy"]
