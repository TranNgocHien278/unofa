# UNOFA — vận hành

## Yêu cầu

- Docker + Docker Compose
- nginx với certbot (TLS)
- Domain `uno.example.com` và `admin.example.com` trỏ về server

## Cài đặt lần đầu

```bash
# 1. Clone
git clone <repo> && cd unofa

# 2. Sinh hash admin password
echo -n 'password-mạnh' | go run ./cmd/hashpw

# 3. Tạo config
cp config.example.yaml config.yaml   # chmod 0600 config.yaml
# Điền bcrypt_hash vừa sinh vào config.yaml, section admin.

# 4. Tạo .env cho compose
printf 'POSTGRES_PASSWORD=%s\nUNOFA_TAG=latest\n' "$(openssl rand -hex 16)" > .env
chmod 0600 .env

# 5. Chạy migration
docker compose run --rm -e DATABASE_URL="postgres://unofa:${POSTGRES_PASSWORD}@db:5432/unofa?sslmode=disable" app /app/migrate

# 6. Khởi động
docker compose up -d
```

## Deploy mới

```bash
export UNOFA_TAG=<commit-sha>
docker compose pull
docker compose up -d
# Chờ healthz
until curl -fsS http://localhost:8080/healthz; do sleep 2; done
```

Nếu healthz không trả `200` trong 60s, rollback:

```bash
export UNOFA_TAG=<sha-trước>
docker compose up -d
```

Downtime khi restart là chủ ý (không có green/blue ở MVP). Ván đang chạy sẽ bị `aborted`; cược ở lại pot (xem §6 của bản miễn trừ).

## Backup

```bash
# Dump thủ công
docker exec unofa-db pg_dump -U unofa unofa | gzip > backups/$(date +%Y%m%d_%H%M%S).sql.gz

# Test restore vào DB tạm
docker exec -i unofa-db psql -U unofa -d postgres -c "CREATE DATABASE unofa_restore"
zcat backups/latest.sql.gz | docker exec -i unofa-db psql -U unofa unofa_restore
```

Không có restore tự động trong MVP: backup phải test tay ít nhất một lần.

## Xử lý sự cố

| Triệu chứng | Kiểm | Làm |
|---|---|---|
| Mọi người bị logout | `docker logs unofa-redis` | Redis crash → `docker compose restart redis`; mọi người login lại. |
| Người chơi mất tiền bất thường | Admin → `admin_flags` xem `MONEY_INVARIANT` | Không tự sửa. Đọc `transactions`, tìm dòng lạ, dùng `SYSTEM_ADD` admin endpoint + ghi `admin_audit`. |
| Ván treo ở `running` | Admin `GET /api/admin/matches` hoặc log | Restart app: ván `aborted`, cược mất (đúng §6). |
| Khoản vay không bị siết | Log job `loan_seize` | Kiểm `clock.Now()`, `due_at`, idempotency key `loan:{id}:seize`. |
| Admin không login được | Redis sống? Config đúng? | Hash sinh bằng `go run ./cmd/hashpw` với Go cùng version. |
| `429` không hiện trên chart | Bình thường | nginx-level 429 không tới Go, không vào `http_log`. Ghi trong §Giới hạn đã biết. |

## Giới hạn đã biết (MVP)

| Giới hạn | Ngưỡng an toàn | Nâng khi nào |
|---|---|---|
| Ghi `http_log` async | ~1000 req/s | Drop counter > 0 liên tục → batch size hoặc chuyển async writer riêng |
| Một Go instance | ~100 người online | > 1000 → Redis state + load balancer |
| Fixed-window rate limit | Cho phép 2× ở ranh giới window | Sliding window nếu bị lợi dụng |
| nginx 429 không vào chart | — | Forward error log vào bảng riêng |
| Không khôi phục ván sau restart | Ván mất khi deploy | Backlog: persist state ván vào Redis |

## nginx 429 và chart

Chart HTTP code trong admin chỉ thấy request tới được Go. nginx-level 429 bị drop trước đó — điểm đã biết, không phải bug. Muốn theo dõi: đọc `nginx/access.log` hoặc export vào Prometheus.

## Logs

Mọi request Go: một dòng JSON mỗi request, format:

```json
{"ts":"...","level":"info","request_id":"...","method":"POST","route":"/api/transfers","status":201,"dur_ms":14,"user_id":7,"ip":"1.2.3.4"}
```

Không log: password, session key, cookie, tay bài, Deck, body của `/login`/`/register`/`/api/transfers`.

## Liên hệ / data deletion

`unofa.contact@gmail.com` — xem `/about` trong app.
