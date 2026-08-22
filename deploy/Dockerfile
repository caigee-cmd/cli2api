# ---- frontend ----
FROM node:22-alpine AS frontend
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --cache /tmp/npm-cache
COPY frontend/ ./
RUN npm run build

# ---- go proxy ----
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist/ ./internal/webui/static/
RUN CGO_ENABLED=0 go build -o /out/qoder-api-proxy ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tini wget
WORKDIR /app
COPY --from=build /out/qoder-api-proxy /app/qoder-api-proxy
ENV HOST=0.0.0.0 PORT=3010
EXPOSE 3010
ENTRYPOINT ["/sbin/tini","--"]
CMD ["/app/qoder-api-proxy"]
