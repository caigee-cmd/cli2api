# ---- frontend ----
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --cache /tmp/npm-cache
COPY frontend/ ./
RUN npm run build

# ---- go control plane ----
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG APP_VERSION=dev
ARG APP_COMMIT=unknown
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist/ ./internal/webui/static/
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build -trimpath -ldflags "-X github.com/caigee-cmd/cli2api/internal/buildinfo.Version=${APP_VERSION} -X github.com/caigee-cmd/cli2api/internal/buildinfo.Commit=${APP_COMMIT}" -o /out/qoder-api-proxy ./cmd/server \
  && CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build -trimpath -o /out/cli2api-updater ./cmd/updater

# ---- single runtime ----
FROM node:20-bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl tini \
  && rm -rf /var/lib/apt/lists/* \
	  && npm install -g @qoder-ai/qodercli@1.1.27 @qodercn-ai/qoderclicn@1.1.27
WORKDIR /app
COPY --from=build /out/qoder-api-proxy /app/qoder-api-proxy
COPY --from=build /out/cli2api-updater /app/cli2api-updater
COPY worker/src /app/worker
COPY worker/last-plain.sample.json /app/worker/last-plain.sample.json
RUN mkdir -p /data && chmod 700 /data
ENV HOST=0.0.0.0 \
    PORT=3010 \
    QODER_DATA_DIR=/data \
    QODER_RUNTIME_DIR=/run/cli2api \
    QODER_NODE_BINARY=/usr/local/bin/node \
    QODER_WORKER_DAEMON=/app/worker/daemon.mjs \
    QODERCLI_JS=/usr/local/lib/node_modules/@qoder-ai/qodercli/bundle/qodercli.js \
    QODERCNCLI_JS=/usr/local/lib/node_modules/@qodercn-ai/qoderclicn/bundle/qoderclicn.js \
    PLAIN_TEMPLATE_PATH=/app/worker/last-plain.sample.json \
    QODER_WORKER_BASE_PORT=32100
EXPOSE 3010
ENTRYPOINT ["/usr/bin/tini","--"]
CMD ["/app/qoder-api-proxy"]
