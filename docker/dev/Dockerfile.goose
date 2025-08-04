# Multi-stage build for swe-swe with goose CLI
FROM golang:1.23-alpine AS builder

COPY docker/ca-certificates/*.* /usr/local/share/ca-certificates/

# Install build dependencies
RUN update-ca-certificates && apk add --no-cache make git

# Install elm
RUN wget -O elm.gz https://github.com/elm/compiler/releases/download/0.19.1/binary-for-linux-64-bit.gz && \
    gunzip elm.gz && \
    chmod +x elm && \
    mv elm /usr/local/bin/

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy elm package file
COPY elm/elm.json elm/elm.json
RUN cd elm && elm make --docs=/tmp/docs.json || true

# Copy source code
COPY . .

# Build the binary
RUN make build

# Final stage - use debian with goose CLI
FROM debian:bookworm-slim

# Create the target directory and copy the bundle
COPY --from=builder /etc/ssl/certs/ /etc/ssl/certs/
# Point Go’s crypto/x509 code at the bundle
ENV SSL_CERT_DIR=/etc/ssl/certs

# Install runtime dependencies and curl for goose installation
RUN apt-get update && apt-get install -y ca-certificates bash curl bzip2 libxcb1 git && rm -rf /var/lib/apt/lists/*

# Install goose using official installation script
RUN curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | CONFIGURE=false GOOSE_BIN_DIR=/bin bash

# Copy the swe-swe binary
COPY --from=builder /build/bin/swe-swe /usr/local/bin/swe-swe

# Create workspace directory
RUN mkdir -p /workspace

WORKDIR /workspace

# Default command
ENTRYPOINT ["/usr/local/bin/swe-swe"]
