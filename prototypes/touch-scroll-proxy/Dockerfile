FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go .
RUN CGO_ENABLED=0 go build -o server .

FROM alpine:latest

RUN apk add --no-cache bash

WORKDIR /app
COPY --from=builder /app/server .
COPY index.html .

EXPOSE 8080
CMD ["./server"]
