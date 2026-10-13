# Multi-stage build — final image is Alpine-based (~12 MB)
FROM golang:1.21-alpine AS builder

WORKDIR /build
COPY . .
RUN go mod download && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o statsagent .

# ─────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /build/statsagent /usr/local/bin/statsagent

# Default port
EXPOSE 9100

# Environment variable defaults (override with -e)
ENV STATSAGENT_PORT=9100 \
    STATSAGENT_INTERVAL=15s \
    STATSAGENT_DOCKER_SOCKET=/var/run/docker.sock

# Entrypoint
ENTRYPOINT ["/usr/local/bin/statsagent"]
CMD ["serve"]

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:${STATSAGENT_PORT}/health || exit 1
