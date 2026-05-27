FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o relay-ssh ./cmd/relay-ssh

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 10001 relay && adduser -S -u 10001 -G relay relay
WORKDIR /app
COPY --from=builder /app/relay-ssh .
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO /dev/null http://localhost:8080/healthz
USER relay
CMD ["./relay-ssh"]
