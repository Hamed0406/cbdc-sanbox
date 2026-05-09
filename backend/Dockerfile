# ── Build stage ───────────────────────────────────────────────────────────────
# Use the full Go image to compile. The compiled binary is then copied to a
# minimal runtime image — the final image contains no Go toolchain or source code.
FROM golang:1.22-alpine AS builder

# Install build tools needed for some CGO dependencies.
# git is required for `go mod download` with git-based dependencies.
RUN apk add --no-cache gcc musl-dev git

WORKDIR /app

# Copy dependency files first. Docker caches this layer separately from source code.
# If only .go files change (not go.mod/go.sum), Docker skips re-downloading modules.
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build
COPY . .

ARG VERSION=dev

# Build flags:
# CGO_ENABLED=0 — pure Go binary, no C runtime dependency (required for scratch image)
# GOOS=linux    — target OS
# -ldflags "-w -s" — strip debug info and DWARF tables (reduces binary size ~30%)
# -X main.Version — inject version string at compile time
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.Version=${VERSION}" \
    -o /app/server \
    ./cmd/server

# ── Runtime stage ─────────────────────────────────────────────────────────────
# alpine (not scratch) so that wget is available for Docker health checks.
# scratch has zero tools, which makes CMD health checks impossible.
# alpine adds ~5MB but keeps the image minimal and still has no Go toolchain.
FROM alpine:3.19

# wget is needed by the Docker HEALTHCHECK and docker-compose healthcheck.
# ca-certificates enables HTTPS in the Go binary at runtime.
RUN apk add --no-cache wget ca-certificates

# Copy only the compiled binary
COPY --from=builder /app/server /server
COPY --from=builder /app/migrations /migrations

EXPOSE 8080

# Run as non-root user for least-privilege principle.
USER 1000:1000

ENTRYPOINT ["/server"]
