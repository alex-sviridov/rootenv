FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /ingress-authenticator ./cmd/main.go

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /ingress-authenticator /ingress-authenticator
ENTRYPOINT ["/ingress-authenticator"]
