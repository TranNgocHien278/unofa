# UNOFA images, one Dockerfile, two final targets:
#   app   - Go binaries (unofa + migrate), docs/spec/07-ops-admin.md §Kiến trúc
#   nginx - TLS edge serving both SPA builds and proxying /api to app
FROM node:24-slim AS web

WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund 2>/dev/null || npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS api

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/unofa ./cmd/unofa \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/migrate ./cmd/migrate

FROM nginx:1.27-alpine AS nginx

COPY deploy/nginx/uno.conf /etc/nginx/conf.d/default.conf
COPY deploy/nginx/admin.conf /etc/nginx/conf.d/admin.conf
COPY --chmod=755 deploy/nginx/admin-toggle.sh /docker-entrypoint.d/05-admin-toggle.sh
COPY --from=web /src/web/dist/game /srv/uno
COPY --from=web /src/web/dist/admin /srv/admin

FROM alpine:3.21 AS app

RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 unofa
WORKDIR /app

COPY --from=api /out/unofa /out/migrate /app/
COPY config.example.yaml /app/config.example.yaml

USER unofa
EXPOSE 8080 8081

ENTRYPOINT ["/app/unofa"]
CMD ["-config", "/app/config.yaml"]
