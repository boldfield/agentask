# Multi-stage build. Go 1.25: go.mod requires >= 1.25.6 (pulled in by
# modernc.org/sqlite), so the builder must match — golang:1.22 fails with
# GOTOOLCHAIN=local since it cannot auto-download a newer toolchain.
FROM golang:1.25 AS builder

WORKDIR /src

# Copy go mod files first and download deps so this layer is cached
# unless go.mod/go.sum change (avoids re-downloading on every source edit).
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build argument for version (can be overridden with --build-arg VERSION=vX.Y.Z)
ARG VERSION=dev

# Build static binary with CGO disabled for pure-Go SQLite
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.version=$VERSION" -o /agentask ./cmd/agentask

# Final stage: distroless static image
FROM gcr.io/distroless/static:nonroot

# Copy the static binary from builder
COPY --from=builder /agentask /agentask

# Expose the API port
EXPOSE 8080

# Run as non-root (nonroot user is built-in to distroless/static:nonroot)
ENTRYPOINT ["/agentask", "server"]
