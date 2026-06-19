FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o relay-primitive ./cmd/relay-primitive

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 10001 relay && adduser -S -u 10001 -G relay relay
WORKDIR /app
COPY --from=builder /app/relay-primitive .
EXPOSE 8080
USER relay
CMD ["./relay-primitive"]
