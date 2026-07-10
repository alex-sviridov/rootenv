FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o relay-grader ./cmd/relay-grader

FROM gcr.io/distroless/static-debian12:nonroot
USER nonroot
COPY --from=builder /app/relay-grader /relay-grader
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/relay-grader", "--healthcheck"]
ENTRYPOINT ["/relay-grader"]
