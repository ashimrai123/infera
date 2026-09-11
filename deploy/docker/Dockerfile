# --- Build stage ---
FROM golang:1.26-alpine AS builder
WORKDIR /src
# Copy dependency manifests first so Docker can cache the download layer.
# Re-downloading only happens when go.mod or go.sum changes, not on every code edit.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/infera ./cmd/infera

# --- Final stage ---
# distroless has no shell, no package manager -- minimal attack surface.
FROM gcr.io/distroless/static-debian12
COPY --from=builder /out/infera /infera
EXPOSE 8080
ENTRYPOINT ["/infera"]
