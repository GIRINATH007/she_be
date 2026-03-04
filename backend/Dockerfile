FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install git for go modules
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /sheguard-server .

# ── Runtime ──
FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=builder /sheguard-server /sheguard-server

EXPOSE 8080

CMD ["/sheguard-server"]
