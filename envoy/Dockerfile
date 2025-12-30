# --- Stage 1: Builder ---
FROM golang:1.25.5-alpine AS builder

WORKDIR /app

# Download dependencies first (cached layers)
COPY ./go.mod ./
COPY ./go.sum ./
RUN go mod download

# Copy source
COPY ./envoy/xDS ./

# Build the binary
# -trimpath removes file system paths from the binary (security/reproducibility)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -trimpath \
    -o envoyXDsConnector \
    ./

# --- Stage 2: Runner ---
# Use 'distroless' for maximum security (no shell, no package manager)
# Or use 'alpine:latest' if you need a shell for debugging
FROM gcr.io/distroless/static-debian12

WORKDIR /

# Copy binary from builder
COPY --from=builder /app/envoyXDsConnector /envoyXDsConnector

# Copy configuration files if needed
# COPY .env .env

USER nonroot:nonroot

ENTRYPOINT ["/envoyXDsConnector"]