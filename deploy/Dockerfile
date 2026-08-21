FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/qoder-api-proxy ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tini
WORKDIR /app
COPY --from=build /out/qoder-api-proxy /app/qoder-api-proxy
ENV HOST=0.0.0.0 PORT=3010
EXPOSE 3010
ENTRYPOINT ["/sbin/tini","--"]
CMD ["/app/qoder-api-proxy"]
