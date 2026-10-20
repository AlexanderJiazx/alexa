# Alexa — always-on personal agent.
# Multi-arch: builds natively for linux/arm64 (Raspberry Pi 4/5) and amd64.

# ---------- web UI ----------
FROM node:24-trixie-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---------- server ----------
FROM golang:1.27-trixie AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/alexagent ./cmd/alexagent

# ---------- runtime ----------
FROM debian:trixie-slim
ARG DEBIAN_FRONTEND=noninteractive
# chromium: the agent's browser. fonts: render real-world pages (CJK, emoji).
# The rest is what the agent's shell works with: Python with the usual data,
# chart and office-file libraries, PDF tools, OCR and ffmpeg. The run_shell
# tool tells the model which of these it finds, so keep them in sync freely.
RUN apt-get update && apt-get install -y --no-install-recommends \
      chromium fonts-noto-core fonts-noto-cjk fonts-noto-color-emoji fonts-liberation \
      ca-certificates tini tzdata curl jq unzip zip poppler-utils ffmpeg tesseract-ocr \
      python3 python3-matplotlib python3-pandas python3-openpyxl python3-docx \
      python3-pypdf python3-pil python3-bs4 python3-requests \
    && rm -rf /var/lib/apt/lists/*
COPY --from=server /out/alexagent /usr/local/bin/alexagent
ENV ADDR=:8080 \
    DATA_DIR=/data \
    CHROME_PATH=/usr/bin/chromium \
    BROWSER_HEADLESS=true
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD curl -fsS http://127.0.0.1:8080/api/health || exit 1
# Runs as root so it can give each user their own UID for Chrome and the
# shell tool (SANDBOX_UIDS); the server itself never executes user code.
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/alexagent"]
