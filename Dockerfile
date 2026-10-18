# Multi-stage build: the panel is compiled with node, the backend with
# Go (embedding the panel dist), the run stage is a minimal alpine
# without any toolchain, as non-root.

# --- panel (React + Vite + TS) ---
FROM node:22-alpine AS panel
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
# Cache mount keeps npm downloads across image builds (first build warms it).
RUN --mount=type=cache,target=/root/.npm npm ci --no-fund --no-audit
COPY frontend/ ./
RUN npm run build

# --- backend (Go, embeds the panel) ---
FROM golang:1.27.1-alpine AS build
WORKDIR /src

# Cache dependencies: go.mod/go.sum change rarely.
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
# Panel dist from the node stage lands where the embed expects it.
COPY --from=panel /web/dist ./internal/infra/httpapi/dist/app
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/aibot ./cmd/aibot

FROM alpine:3.20
# tzdata for the family time zone (APP_TZ), ca-certificates for HTTPS
# (Telegram API, LLM API), both tiny. Non-root user.
RUN apk add --no-cache tzdata ca-certificates && adduser -D -u 10001 app
USER app
WORKDIR /app
COPY --from=build /out/aibot /app/aibot

# Config comes from the environment (docker compose injects .env values).
ENV HTTP_ADDR=":8080"
EXPOSE 8080
ENTRYPOINT ["/app/aibot"]
