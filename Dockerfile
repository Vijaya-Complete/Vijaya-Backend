FROM golang:1.22-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/server ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata chromium \
    && adduser -D -g '' appuser
ENV CHROME_PATH=/usr/bin/chromium-browser
WORKDIR /app
COPY --from=builder /app/bin/server /app/server
COPY --from=builder /app/db/migrations /app/db/migrations
USER appuser
EXPOSE 8080
ENTRYPOINT ["/app/server"]
