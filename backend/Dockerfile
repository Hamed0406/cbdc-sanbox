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
# `scratch` is an empty image with no OS, shell, or tools.
# This minimises the attack surface: there's nothing to exploit even if the
# binary has a vulnerability that allows arbitrary code execution.
FROM scratch

# CA certificates are needed for HTTPS calls to external services.
# Even though this simulator doesn't call external APIs, good practice to include.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy only the compiled binary and migration files
COPY --from=builder /app/server /server
COPY --from=builder /app/migrations /migrations

EXPOSE 8080

# Run as non-root user (UID 1000) for least-privilege principle.
# Even though `scratch` has no /etc/passwd, numeric UIDs still work.
USER 1000:1000

ENTRYPOINT ["/server"]
