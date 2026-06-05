# Multi-stage build: golang:1.22 builder
FROM golang:1.22 AS builder

WORKDIR /src

# Copy go mod files
COPY go.mod go.sum ./

# Copy source code
COPY . .

# Build static binary with CGO disabled for pure-Go SQLite
RUN CGO_ENABLED=0 GOOS=linux go build -o /agentask ./cmd/agentask

# Final stage: distroless static image
FROM gcr.io/distroless/static:nonroot

# Copy the static binary from builder
COPY --from=builder /agentask /agentask

# Expose the API port
EXPOSE 8080

# Run as non-root (nonroot user is built-in to distroless/static:nonroot)
ENTRYPOINT ["/agentask"]
