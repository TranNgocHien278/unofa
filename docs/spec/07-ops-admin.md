# 07 — Ops và admin

## Kiến trúc

```
internet
   │
   ▼
 nginx (TLS, limit_req, limit_conn)
   │  Host: uno.example.com      Host: admin.example.com
   ▼                                ▼
 └──────────► một binary Go (hai router) ◄──────────┘
                   │              │
                   ▼              ▼
              Postgres          Redis
```

- Một binary Go, mono repo. Chưa tách microservice, chưa có load balancer, chưa Kafka, chưa green/blue.
- Một Postgres instance. Một Redis instance.
- State ván ở memory của goroutine ván. Không persist. Restart là mất ván (chấp nhận ở MVP).

Tải dự kiến dùng để chọn kiến trúc: tối đa `100` người online, `10` ván chạy cùng lúc. Vượt khoảng `1000` người online thì xem lại ba thứ: ghi `http_log` đồng bộ vào Postgres mỗi request, một instance Go, job chạy trong process.

## Config

`config.yaml`, quyền `0600`, không commit. Chỉ commit `config.example.yaml`.

```yaml
server:
  game_addr: ":8080"
  admin_addr: ":8081"
  game_origin: "https://uno.example.com"
  admin_origin: "https://admin.example.com"

db:
  dsn: "${DATABASE_URL}"          # đọc từ env, không ghi trong file
  max_conns: 20
  min_conns: 5
  conn_timeout: 5s

redis:
  addr: "127.0.0.1:6379"
  db: 0
  session_ttl: 15m
  heartbeat_interval: 1m

admin:
  - username: "root"
    bcrypt_hash: "$2a$12$..."      # sinh bằng `go run ./cmd/hashpw`
  - username: "risk"
    bcrypt_hash: "$2a$12$..."

money:
  signup_bonus: 200
  transfer_daily_limit: 1000
  transfer_min: 1

loan:
  packages: [500, 1000, 2000]
  terms:
    - { name: "24h", duration: 24h,  rate_bps: 100 }
    - { name: "3d",  duration: 72h,  rate_bps: 300 }
    - { name: "30d", duration: 720h, rate_bps: 1000 }
  overdue_bps: 500
  cap_multiplier: 2

game:
  min_players: 2
  max_players: 10
  hand_size: 7
  turn_timeout: 20s
  ready_timeout: 5s
  reconnect_grace: 90s
  max_message_bytes: 4096
  allowed_stakes: [10, 25, 100]     # PHẢI là tập con của (10,25,50,100,500)

jobs:
  invariant_interval: 1h
  loan_seize_interval: 1h
  http_log_retention: 60d
  http_log_cleanup_interval: 24h

log:
  level: "info"
  format: "json"
```

- Config chỉ chứa `username` + `bcrypt hash` cho admin. Cấm password plaintext.
- Đổi admin bắt buộc restart. Không có đường đổi khi server đang chạy.
- Mọi con số rate limit nằm trong config; đổi phải restart.

### Bí mật

| Biến | Dùng |
|---|---|
| `DATABASE_URL` | DSN Postgres |
| `REDIS_URL` | Addr Redis |
| `SENTRY_DSN` | Tuỳ chọn |

Đọc từ env, không từ file. File config tham chiếu `${...}`.

## Thời gian trong code

- Mọi timestamp DB: `TIMESTAMPTZ`, UTC.
- Server gửi `server_now` ở mọi snapshot và event. Client bù lệch.
- `clock` injectable (interface `Clock{ Now() time.Time }`) để test timer `20s`, `90s`, `5s` không phải sleep thật.

## Startup

Thứ tự, không đổi:

1. Đọc config, validate. `allowed_stakes ⊆ {10,25,50,100,500}`, `max_players ∈ [2,10]`, hash admin parse được.
2. Kết nối Postgres, ping.
3. Kết nối Redis, ping. Chết → log `warn`, chạy tiếp? **Không.** Thoát. Server không có session thì không phục vụ được gì.
4. Chạy một transaction duy nhất:
   - `UPDATE matches SET status = 'aborted' WHERE status = 'running'`
   - `DELETE FROM room_members`
   - `DELETE FROM rooms`
   - `DELETE FROM loans WHERE status = 'active' AND ...` — **không**, giữ nguyên, job siết sẽ xử lý.
   - `INSERT INTO admin_flags(kind, detail)` kind `SERVER_RESTARTED` với `{count_of_aborted_matches}`.
5. Khởi động job nền.
6. Mở hai listener.

Không chạy câu đã dồn, không khôi phục ván. Tiền pot của ván `aborted` nằm lại trong `matches.pot` (D14): invariant tính cả pot nên vẫn khớp.

## nginx

`uno.example.com.conf`:

```nginx
limit_req_zone  $binary_remote_addr zone=uno_http:10m rate=30r/s;
limit_req_zone  $binary_remote_addr zone=uno_ws:10m   rate=10r/m;
limit_conn_zone $binary_remote_addr zone=uno_conn:10m;

map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl http2;
    server_name uno.example.com;

    ssl_certificate     /etc/letsencrypt/live/uno.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/uno.example.com/privkey.pem;

    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Content-Security-Policy "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; connect-src 'self' wss://uno.example.com; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'" always;

    client_max_body_size 16k;

    limit_conn uno_conn 50;

    # WebSocket: không dùng limit_req_zone uno_http, dùng uno_ws
    location = /api/ws {
        limit_req zone=uno_ws burst=5 nodelay;
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header X-Real-IP  $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Host $host;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }

    location / {
        limit_req zone=uno_http burst=60 nodelay;
        limit_req_status 429;
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Host $host;
    }
}

server {
    listen 80;
    server_name uno.example.com;
    return 301 https://$host$request_uri;
}
```

`admin.example.com.conf`: cùng cấu trúc, `server_name admin.example.com`, `limit_req_zone admin_http:10m rate=10r/s`, `client_max_body_size 8k`, proxy tới `127.0.0.1:8081`.

Mọi `429` của nginx **không** vào được `http_log` (chưa tới Go). Đây là điểm đã biết: chart HTTP code chỉ thấy lượt bị app limit. Ghi trong README.

IP thật: Go đọc `X-Real-IP` **chỉ khi** kết nối đến từ một địa chỉ tin cậy (loopback / dải nginx). Không thì ai cũng giả được IP trong header và vượt rate limit.

## CI/CD

Một workflow, ba job tuần tự:

1. **lint + test**
   - Go: `go vet ./...`, `golangci-lint run`, `go test ./... -race -count=1`.
   - FE: `tsc --noEmit`, `eslint`, `vitest run`.
   - Migration: chạy lên DB tạm rỗng, rồi xuống (`down`) rồi lên lại — bắt migration không đảo được.
2. **build**
   - Go: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w"`.
   - FE: `vite build` hai entry (`web`, `web/admin`).
   - Docker image, tag = commit SHA.
3. **deploy** (chỉ nhánh `main`)
   - SSH, `docker compose pull && up -d`.
   - Chờ `/healthz` trả `200`.
   - Nếu không: rollback tag trước.

Không green/blue ở MVP. Downtime khi deploy chấp nhận; ván đang chạy sẽ mất và tiền cược ở lại pot — đúng §6 của bản miễn trừ.

### Test bắt buộc chạy trong CI

- `go test -race ./...` phải pass. Race ở goroutine ván là bug nghiêm trọng nhất.
- Invariant tiền (T1) chạy trong suite test tích hợp.
- `axe` cho 5 route FE (F6).

## `/healthz`

`GET /healthz` (không auth, không rate limit, không ghi `http_log`):

```json
{ "status": "ok", "db": "ok", "redis": "ok", "uptime_s": 3600 }
```

`503` nếu một trong hai dependency chết. Dùng cho deploy check và uptime monitor.

## Job nền

Chạy trong process, một goroutine mỗi job, `ticker`. Không cron ngoài, không worker riêng.

| Job | Chu kỳ | Việc | Ghi |
|---|---|---|---|
| Đánh dấu ván cũ | lúc startup | `UPDATE matches SET status='aborted' WHERE status='running'` | 07 §Startup |
| Kiểm tra invariant tiền | `1h` | Câu SQL `SUM = 0` (03 §Invariant). Lệch → `admin_flags` `MONEY_INVARIANT` | Không tự sửa tiền |
| Siết nợ | `1h` | `Seize` các khoản `active` có `owed >= 2 × principal`, key `loan:{id}:seize`, `status='seized'`, `admin_flags` `LOAN_SEIZED` | 03 §Siết nợ |
| Dọn `http_log` | `24h` | `DELETE FROM http_log WHERE at < now() - interval '60 days'` | Bằng khoảng dài nhất của chart |

Mọi job:

- Log một dòng JSON mỗi lần chạy: `{job, duration_ms, affected, error}`.
- Panic trong job không làm chết process: `recover()` + log + tiếp tục chu kỳ sau.
- Idempotent. Job chạy hai lần liên tiếp không sinh tác dụng hai lần (nhờ idempotency key).
- Bắt đầu chạy sau `30s` kể từ lúc server mở listener, không phải ngay: cho request đầu tiên xong trước.

## Admin

### Nguồn danh tính

Config đọc lúc startup, không phải DB. Admin **không** nằm trong bảng `users`. Không có đường nào từ session người chơi escalate thành admin.

### Session admin

- Cookie `__Host-unofa_admin_sid`, namespace Redis `admin:session:*`.
- TTL `15p`, heartbeat `1p`, cùng cơ chế người chơi.
- Redis chết → admin không đăng nhập được. Cờ "Redis chết" chỉ hiện sau khi Redis sống lại. Chấp nhận ở MVP.

### Cách ly origin

- Tiền tố `__Host-` buộc cookie không có `Domain`, nên cookie game không được gửi sang `admin.example.com` và ngược lại.
- CORS của game API không nhận origin admin, và API admin không nhận origin game.
- Admin app build riêng, không share bundle với game.

### Hành động phải log DB

Mọi ghi (`SYSTEM_ADD`, `SYSTEM_GET`, `DELETE_USER`, `CLEAR_FLAG`) ghi `admin_audit` **trong cùng transaction** với thay đổi: `admin_username, action, target_user_id, amount, at, ip, request_id`.

Yêu cầu v2 về "chỉnh số dư qua endpoint, label `SYSTEM {ADD|GET} {amount}`" đọc từ bảng này — chart balance metric của admin lấy label riêng từ đó.

### 4 cờ cảnh báo

Hiện trên dashboard, **không gửi email/Slack**:

| `kind` | Sinh khi | `detail` |
|---|---|---|
| `MONEY_INVARIANT` | Job invariant thấy `SUM != 0` | `{sum, checked_at}` |
| `LOGIN_LOCKED` | Vượt `POST /login` `10/giờ` theo username | `{username, user_id, until}` |
| `REDIS_DOWN` | Ping Redis fail kể cả sau retry | `{since}` |
| `LOAN_SEIZED` | Job siết nợ chạy | `{loan_id, user_id, seized, forgiven}` |

Thêm `SERVER_RESTARTED` ở startup (không thuộc 4 cờ gốc, nhưng cần cho việc xoá pot).

Cờ lưu `admin_flags(id, kind, detail JSONB, at, cleared_at)`. Dashboard hiện `cleared_at IS NULL`, mới nhất trước. Nút "xoá cờ" ghi `cleared_at` + `admin_audit`.

### Metric

Nguồn: bảng `http_log(request_id, endpoint, status_code, ip, user_id, at)`, ghi bằng middleware mỗi request. **Không parse log text** — parse log là việc phải sửa mỗi lần đổi format.

- `endpoint` lưu theo **route pattern** (`/api/matches/{id}/state`), không lưu URL thật. URL thật làm mỗi `match_id` thành một nhóm riêng trên chart.
- Giữ `60` ngày, bằng khoảng dài nhất của chart.
- Ghi bất đồng bộ qua buffered channel + batch insert (`500` dòng hoặc `1s`). Ghi đồng bộ mỗi request làm tăng latency toàn hệ.
- Buffer đầy → drop + tăng counter `http_log_dropped`. Không block request.
- Không ghi `http_log` cho `/healthz` và asset tĩnh.

## Deploy

`docker-compose.yml`:

```yaml
services:
  app:
    image: ghcr.io/<org>/unofa:${TAG}
    restart: unless-stopped
    env_file: .env
    ports: ["127.0.0.1:8080:8080", "127.0.0.1:8081:8081"]
    volumes: ["./config.yaml:/etc/unofa/config.yaml:ro"]
    depends_on: [postgres, redis]
    healthcheck:
      test: ["CMD", "/app/unofa", "healthcheck"]
      interval: 10s
      timeout: 3s
      retries: 3

  postgres:
    image: postgres:16
    restart: unless-stopped
    env_file: .env
    volumes: ["pgdata:/var/lib/postgresql/data"]
    command: >
      postgres -c shared_buffers=256MB -c max_connections=50
               -c log_min_duration_statement=500

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: ["redis-server", "--appendonly", "no", "--maxmemory", "256mb",
              "--maxmemory-policy", "allkeys-lru"]
    volumes: ["redisdata:/data"]

volumes: { pgdata: {}, redisdata: {} }
```

Redis `appendonly no`: chỉ giữ session + rate limit. Mất thì mọi người login lại. Đúng thiết kế — Redis không phải nguồn sự thật cho tiền.

Backup Postgres: `pg_dump` hằng ngày, giữ `7` bản, ra ngoài máy. Test restore mỗi tháng một lần. **Không** backup Redis.

## Log

JSON, một dòng một request:

```json
{"ts":"2026-11-12T09:30:00Z","level":"info","request_id":"01J8...","method":"POST","route":"/api/transfers","status":201,"dur_ms":14,"user_id":7,"ip":"1.2.3.4"}
```

Không log:

- Password, session key, cookie.
- Tay bài, `Deck`, `matches.pot` chi tiết theo người.
- Body của `/login`, `/register`, `/api/transfers`.

Log lỗi `500`: kèm stack, nhưng **không** kèm body.

## Vận hành sự cố

| Triệu chứng | Kiểm | Xử lý |
|---|---|---|
| Mọi người bị đá ra | Redis sống? `redis-cli ping` | Redis chết → restart; mọi người login lại. |
| Người chơi mất tiền mà không chơi | `admin_flags` `MONEY_INVARIANT`? | Không tự sửa. Đọc `transactions`, tìm dòng lạ, hoàn bằng `SYSTEM_ADD` có `admin_audit`. |
| Ván treo | `GET /api/matches/{id}` trạng thái `running` quá lâu | Restart app: ván `aborted`, cược mất (đúng §6). |
| Khoản vay không bị siết | Job log có chạy? | Kiểm `clock`, `due_at`, idempotency key `loan:{id}:seize` đã dùng. |
| Admin không login được | Redis, config hash | Hash sinh bằng tool cùng version bcrypt. |
| Chart admin trống | `http_log` có dòng mới? buffer drop? | Tăng batch, kiểm `http_log_dropped`. |

## Bảo mật triển khai

- TLS bắt buộc, HSTS `includeSubDomains`.
- CSP như trên. `script-src 'self'` — không inline script, không CDN.
- Postgres/Redis chỉ bind `127.0.0.1`, không mở ra ngoài.
- User chạy container không phải root.
- `config.yaml` `0600`, không commit. `.env` cũng vậy. `.gitignore` phải chứa cả hai.
- Dependency: `go mod tidy` + `npm ci` với lockfile commit; Dependabot bật.
- Không có endpoint nào trả stack trace hay tên bảng.
- Backup `pg_dump` chứa dữ liệu người chơi → mã hoá file, khoá tách máy.
