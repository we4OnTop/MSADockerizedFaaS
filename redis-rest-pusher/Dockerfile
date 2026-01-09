FROM golang:1.25.5-alpine AS builder

WORKDIR /app

COPY ./go.mod ./
COPY ./go.sum ./
RUN go mod download

#Copy source
COPY ./redis-rest-pusher ./

#Build binaries
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -trimpath \
    -o redis-rest-pusher \
    ./

FROM gcr.io/distroless/static-debian12

WORKDIR /

# Copy binary from builder
COPY --from=builder /app/redis-rest-pusher /redis-rest-pusher

# COPY .env .env

USER nonroot:nonroot

ENV GIN_MODE=release

ENTRYPOINT ["/redis-rest-pusher"]