FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o auth .

FROM scratch
COPY --from=builder /app/auth /auth
EXPOSE 4180
ENTRYPOINT ["/auth"]
