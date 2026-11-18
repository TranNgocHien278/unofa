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
echo -n 'admin-password' | go run ./cmd/hashpw

# 3. Tạo config
cp config.example.yaml config.yaml && chmod 0600 config.yaml
# Điền bcrypt_hash vừa sinh vào config.yaml, section admin.

# 4. Tạo .env cho compose
printf 'POSTGRES_PASSWORD=%s\nUNOFA_TAG=latest\n' "$(openssl rand -hex 16)" > .env && chmod 0600 .env

# 5. Cấu hình nginx & TLS theo mục bên dưới rồi khởi động
docker compose up -d
```

## Setup nginx & TLS

Container `nginx` đọc cấu hình tại [deploy/nginx/uno.conf](deploy/nginx/uno.conf) và chứng chỉ tại `/etc/letsencrypt` trên host (xem [docker-compose.yml](docker-compose.yml)). Làm theo thứ tự trước khi chạy `docker compose up -d`:

1. **Trỏ DNS & mở firewall:** Trỏ bản ghi `A` của domain game (`uno.example.com`) và domain admin (`admin.example.com`) về IP VPS; mở port `80` và `443`.
2. **Đổi domain thật trong config:**
   - Trong `deploy/nginx/uno.conf`: thay `uno.example.com` ở `server_name`, đường dẫn `/etc/letsencrypt/live/...`, và `wss://uno.example.com` trong header `Content-Security-Policy`.
   - Trong `deploy/nginx/admin.conf` (nếu public domain admin): thay `admin.example.com` tương tự.
   - Trong `config.yaml`: cập nhật `server.game_origin` và `server.admin_origin` khớp hai domain HTTPS trên; giữ `trusted_proxies: ["172.16.0.0/12"]` để app nhận đúng `X-Real-IP` từ container `nginx`.
3. **Cấp chứng chỉ TLS trên host trước khi bật container `nginx`** (vì `nginx` sẽ thoát lỗi nếu `/etc/letsencrypt/live/...` chưa tồn tại; tách 2 lệnh để có đủ 2 thư mục cert tương ứng):
   ```bash
   sudo apt update && sudo apt install -y certbot
   sudo certbot certonly --standalone -d uno.example.com
   sudo certbot certonly --standalone -d admin.example.com
   ```
4. **Khởi động & kiểm tra:**
   ```bash
   docker compose up -d
   docker compose ps
   curl -fsS https://uno.example.com/healthz
   ```
   *(Lưu ý: `docker-compose.yml` hiện mới mount `uno.conf`; nếu muốn mở `admin.conf` qua cùng container `nginx`, cần mount thêm file đó và bỏ khai báo trùng `zone=uno_conn` / `map $http_upgrade $connection_upgrade` giữa hai file.)*
5. **Gia hạn chứng chỉ định kỳ:** Vì container `nginx` giữ port `80` và chuyển hướng toàn bộ sang HTTPS, cấu hình gia hạn bằng hook dừng tạm `nginx`:
   ```bash
   sudo certbot renew \
     --pre-hook "docker compose -f /path/to/unofa/docker-compose.yml stop nginx" \
     --post-hook "docker compose -f /path/to/unofa/docker-compose.yml start nginx"
   ```

## Deploy mới

```bash
export UNOFA_TAG=<commit-sha>
docker compose pull
docker compose up -d   # tự chạy migrate rồi khởi động app
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

| Triệu chứng                    | Kiểm                                        | Làm                                                                                                  |
| ------------------------------ | ------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Mọi người bị logout            | `docker logs unofa-redis`                   | Redis crash → `docker compose restart redis`; mọi người login lại.                                   |
| Người chơi mất tiền bất thường | Admin → `admin_flags` xem `MONEY_INVARIANT` | Không tự sửa. Đọc `transactions`, tìm dòng lạ, dùng `SYSTEM_ADD` admin endpoint + ghi `admin_audit`. |
| Ván treo ở `running`           | Admin `GET /api/admin/matches` hoặc log     | Restart app: ván `aborted`, cược mất (đúng §6).                                                      |
| Khoản vay không bị siết        | Log job `loan_seize`                        | Kiểm `clock.Now()`, `due_at`, idempotency key `loan:{id}:seize`.                                     |
| Admin không login được         | Redis sống? Config đúng?                    | Hash sinh bằng `go run ./cmd/hashpw` với Go cùng version.                                            |
| `429` không hiện trên chart    | Bình thường                                 | nginx-level 429 không tới Go, không vào `http_log`. Ghi trong §Giới hạn đã biết.                     |

## Giới hạn đã biết (MVP)

| Giới hạn                        | Ngưỡng an toàn                 | Nâng khi nào                                                          |
| ------------------------------- | ------------------------------ | --------------------------------------------------------------------- |
| Ghi `http_log` async            | ~1000 req/s                    | Drop counter > 0 liên tục → batch size hoặc chuyển async writer riêng |
| Một Go instance                 | ~100 người online              | > 1000 → Redis state + load balancer                                  |
| Fixed-window rate limit         | Cho phép 2× ở ranh giới window | Sliding window nếu bị lợi dụng                                        |
| nginx 429 không vào chart       | —                              | Forward error log vào bảng riêng                                      |
| Không khôi phục ván sau restart | Ván mất khi deploy             | Backlog: persist state ván vào Redis                                  |

## nginx 429 và chart

Chart HTTP code trong admin chỉ thấy request tới được Go. nginx-level 429 bị drop trước đó — điểm đã biết, không phải bug. Muốn theo dõi: đọc `nginx/access.log` hoặc export vào Prometheus.

## Logs

Mọi request Go: một dòng JSON mỗi request, format:

```json
{
  "ts": "...",
  "level": "info",
  "request_id": "...",
  "method": "POST",
  "route": "/api/transfers",
  "status": 201,
  "dur_ms": 14,
  "user_id": 7,
  "ip": "1.2.3.4"
}
```

Không log: password, session key, cookie, tay bài, Deck, body của `/login`/`/register`/`/api/transfers`.

## Liên hệ / data deletion

`unofa.contact@gmail.com` — xem `/about` trong app.
