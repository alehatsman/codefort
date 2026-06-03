# moongit production image — multi-stage build.
#
# Stage 1: compile moongitd (CGO disabled; static binary runs on any glibc).
# Stage 2: build the web UI.
# Stage 3: minimal runtime — debian:stable-slim + git (needed for smart-HTTP
#           push/pull), ca-certificates (outbound TLS), and the two artifacts.
#
# Build from the repo root (Docker BuildKit, no extra context):
#   docker build -t moongit:latest .
#
# Run (see tasks.yml docker-run, or the CI deploy job in mgitci.yml):
#   docker run -d --name moongitd \
#     --restart unless-stopped \
#     -p 8080:8080 \
#     --add-host host.docker.internal:host-gateway \
#     -v "$HOME/.local/share/moongit:/data" \
#     --env-file ~/.config/moongit/moongit-container.env \
#     --env-file ~/.config/moongit/moongit.secret.env \
#     moongit:latest

# ── stage 1: Go binary ────────────────────────────────────────────────────
FROM golang:1.26.4 AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" \
      -o /moongitd ./cmd/moongitd

# ── stage 2: web UI ───────────────────────────────────────────────────────
FROM node:22.21.0-slim AS web-builder
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# ── stage 3: runtime ──────────────────────────────────────────────────────
FROM debian:stable-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates gnupg curl \
 && install -m 0755 -d /etc/apt/keyrings \
 && curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg \
 && chmod a+r /etc/apt/keyrings/docker.gpg \
 && echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian bookworm stable" \
      > /etc/apt/sources.list.d/docker.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends docker-ce-cli \
 && apt-get purge -y curl gnupg \
 && apt-get autoremove -y \
 && rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /moongitd /usr/local/bin/moongitd
COPY --from=web-builder /src/web/dist /app/web/dist

# Defaults — all overridable via env / --env-file at run time.
ENV MOONGIT_ADDR=:8080 \
    MOONGIT_DATA_DIR=/data \
    MOONGIT_REPOS_DIR=/data/repos \
    MOONGIT_WEB_DIR=/app/web/dist

EXPOSE 8080

# /data is the bind-mount point for the host data dir.
VOLUME ["/data"]

RUN moongitd --version 2>/dev/null || true

CMD ["moongitd", "serve"]
