FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o contmgr .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 10001 contmgr && adduser -S -u 10001 -G contmgr contmgr
WORKDIR /app
COPY --from=builder /app/contmgr .
EXPOSE 8081
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO /dev/null http://localhost:8081/healthz
USER contmgr
CMD ["./contmgr"]
