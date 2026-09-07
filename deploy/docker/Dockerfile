# --- Build stage ---
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/infera ./cmd/infera

# --- Final stage ---
FROM gcr.io/distroless/static-debian12
COPY --from=builder /out/infera /infera
EXPOSE 8080
ENTRYPOINT ["/infera"]
