FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /relay-authenticator ./cmd/main.go

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /relay-authenticator /relay-authenticator
ENTRYPOINT ["/relay-authenticator"]
