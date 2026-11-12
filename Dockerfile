# UNOFA runtime image: one Go binary serving both game and admin listeners
# (docs/spec/07-ops-admin.md §Kiến trúc), plus the built SPAs on disk.
FROM node:24-alpine AS web

WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund 2>/dev/null || npm install --no-audit --no-fund
COPY web/ ./
RUN npx tsc --noEmit && npx vite build

FROM golang:1.26-alpine AS api

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/unofa ./cmd/unofa \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 unofa
WORKDIR /app

COPY --from=api /out/unofa /out/migrate /app/
COPY --from=web /src/web/dist /app/static
COPY config.example.yaml /app/config.example.yaml

USER unofa
EXPOSE 8080 8081
ENV UNOFA_STATIC_DIR=/app/static

ENTRYPOINT ["/app/unofa"]
CMD ["-config", "/app/config.yaml"]
