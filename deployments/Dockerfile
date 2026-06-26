FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o /bin/mez-go-mono ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    ffmpeg \
    libwebp-tools

COPY --from=builder /bin/mez-go-mono /usr/local/bin/mez-go-mono
COPY --from=builder /src/migrations /migrations
COPY deployments/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

EXPOSE 8080 9090

ENTRYPOINT ["/entrypoint.sh"]
