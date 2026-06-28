# syntax=docker/dockerfile:1
#
# Multi-stage build for the Cloud Sandbox Management API.
# Build the Go binary, then copy it into a minimal runtime image.

ARG GO_VERSION=1.23

FROM golang:${GO_VERSION}-bookworm AS builder
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Build a static-ish binary; CGO is disabled so the image can use scratch/distroless.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/api /api

# The API binds HTTP_ADDR (default :8080).
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/api"]
