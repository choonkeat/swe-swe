# Multi-stage build for swe-swe
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache make git

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

# Final stage - minimal image from scratch
FROM scratch

# Copy the binary
COPY --from=builder /build/bin/swe-swe /swe-swe

# The binary will need to access the current directory
WORKDIR /workspace

# Default command
ENTRYPOINT ["/swe-swe"]