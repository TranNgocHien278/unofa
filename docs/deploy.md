# Hướng dẫn deploy UNOFA

Tài liệu này đi từ một VPS đã cài Docker đến khi chạy đủ hai site:

- `https://uno.tnhien.tech`: trang game
- `https://admin.tnhien.tech`: trang quản trị

## 1. Kiến trúc

```
Internet ──► VPS cổng 80/443 ──► container nginx ──┬─► file giao diện (game, admin), có sẵn trong image
                                                   └─► /api/... ──► container app (Go)
                                                                       ├─► redis
                                                                       └─► db (Postgres, stack riêng, qua mạng unofa-db)
```

| Container | Image                                 | Vai trò                                                                       |
| --------- | ------------------------------------- | ----------------------------------------------------------------------------- |
| `nginx`   | `ghcr.io/tranngochien278/unofa-nginx` | Cửa vào duy nhất. HTTPS, giới hạn request, trả giao diện, chuyển `/api` vào app |
| `app`     | `ghcr.io/tranngochien278/unofa`       | Backend Go. Cổng 8080 (game), 8081 (admin), chỉ mở trong mạng Docker            |
| `migrate` | `ghcr.io/tranngochien278/unofa`       | Chạy migration DB một lần rồi thoát (`Exited (0)` là bình thường)               |
| `db`      | `postgres:16-alpine`                  | Dữ liệu, lưu trong volume `unofa_pgdata`                                        |
| `redis`   | `redis:7-alpine`                      | Session, rate limit                                                             |

Container chia thành hai stack (compose project), bật tắt độc lập:

| Stack      | File                    | Container                          |
| ---------- | ----------------------- | ---------------------------------- |
| `unofa-db` | `docker-compose.db.yml` | `db`. Tạo mạng Docker `unofa-db`   |
| `unofa`    | `docker-compose.yml`    | `nginx`, `app`, `migrate`, `redis` |

`app` và `migrate` nối vào mạng `unofa-db` để gọi `db:5432`. DB tách riêng nên deploy lại app, `docker compose down` stack app, hay sau này chạy song song hai bản app (blue/green) đều không đụng tới DB.

Conf nginx (`deploy/nginx/*.conf`) không nằm trong image. Compose mount thư mục `deploy/nginx` vào container nginx (chỉ đọc). Khi container khởi động, script `admin-toggle.sh` có sẵn trong image nối các file `.conf` vào nginx. Sửa conf không cần build lại image (mục 7).

Hai image `unofa` và `unofa-nginx` build từ cùng một `Dockerfile` (target `app` và `nginx`). Image chỉ chứa binary và file build sẵn, không chứa source code. VPS không cần source code, chỉ cần: `docker-compose.yml`, `docker-compose.db.yml`, `.env`, `config.yaml` và thư mục `deploy/nginx/`.

## 2. Mạng: inbound, outbound, domain

### 2.1 Firewall

Inbound là kết nối từ internet vào VPS. Outbound là kết nối từ VPS ra internet.

| Chiều    | Giao thức | Cổng | Nguồn / đích               | Lý do                                                     |
| -------- | --------- | ---- | -------------------------- | --------------------------------------------------------- |
| Inbound  | TCP       | 22   | IP của bạn (hoặc mọi nơi)  | SSH. Không được xoá, xoá là mất quyền vào VPS             |
| Inbound  | TCP       | 80   | `0.0.0.0/0`, `::/0`        | Let's Encrypt xác minh domain; chuyển hướng HTTP sang HTTPS |
| Inbound  | TCP       | 443  | `0.0.0.0/0`, `::/0`        | HTTPS cho cả hai site                                     |
| Outbound | Tất cả    | Tất cả | `0.0.0.0/0`              | Kéo image, xin cert. Mặc định đã mở, không cần chỉnh      |

Có hai lớp firewall, cả hai đều phải mở:

1. **Firewall của nhà cung cấp.** AWS EC2: **EC2 → Instances → chọn máy → tab Security → Security groups → Edit inbound rules**. Thêm các dòng ở bảng trên.
2. **Firewall trong VPS (ufw).** Chạy `sudo ufw status`. Nếu `inactive` thì bỏ qua. Nếu `active`:
   ```sh
   sudo ufw allow 80/tcp
   sudo ufw allow 443/tcp
   ```

Cổng 5432 (Postgres), 6379 (Redis), 8080/8081 (app) **không mở ra ngoài**. Chúng chỉ dùng trong mạng nội bộ của Docker.

### 2.2 Domain (DNS)

Ở trang quản lý domain (.Tech), tạo hai bản ghi A trỏ về **cùng một** IP public của VPS:

| Loại | Host                | Giá trị       |
| ---- | ------------------- | ------------- |
| A    | `uno.tnhien.tech`   | IP public VPS |
| A    | `admin.tnhien.tech` | IP public VPS |

- Một VPS phục vụ được nhiều domain. Nginx đọc tên domain trong request để chọn site (`server_name` trong `deploy/nginx/uno.conf` và `admin.conf`).
- Không tạo bản ghi AAAA (IPv6) nếu VPS không có IPv6. Let's Encrypt ưu tiên IPv6, bản ghi AAAA sai làm xin cert thất bại.
- IP public AWS lấy ở trang EC2, hoặc chạy `curl -4 ifconfig.me` trên VPS. Nên gắn **Elastic IP** để IP không đổi khi khởi động lại máy.

Kiểm tra trên VPS:

```sh
curl -4 ifconfig.me; echo
getent ahosts uno.tnhien.tech
getent ahosts admin.tnhien.tech
```

Ba lệnh phải ra cùng một IP. DNS mới tạo có thể mất 5–30 phút mới có hiệu lực.

## 3. Chuẩn bị trên máy local (WSL)

Làm một lần, hoặc mỗi khi có code mới cần deploy.

### 3.1 Token GitHub

1. GitHub → **Settings → Developer settings → Personal access tokens → Tokens (classic) → Generate new token (classic)**.
2. Tick `write:packages`.
3. Lưu token (`ghp_...`) như mật khẩu. Không dán vào chat, log hay file trong repo. Nếu lộ, xoá token ngay trên GitHub và tạo token mới.

Đăng nhập registry:

```sh
echo 'TOKEN' | docker login ghcr.io -u tranngochien278 --password-stdin
```

### 3.2 Build và push image

VPS AWS dùng chip ARM (`arm64`), máy local dùng `amd64`. Build cả hai kiến trúc:

```sh
cd ~/projects/UNOFA
docker buildx build --platform linux/amd64,linux/arm64 --target app   -t ghcr.io/tranngochien278/unofa:latest       --push .
docker buildx build --platform linux/amd64,linux/arm64 --target nginx -t ghcr.io/tranngochien278/unofa-nginx:latest --push .
```

Kết quả: GitHub → **Profile → Packages** có `unofa` và `unofa-nginx`.

Image nginx chỉ chứa giao diện. Sửa code giao diện thì phải build và push lại. Sửa `deploy/nginx/*.conf` thì không cần build, chỉ gửi file lên VPS (mục 7).

### 3.3 Tạo `config.yaml` và `.env`

```sh
cp config.example.yaml config.yaml
echo -n 'MAT_KHAU_ADMIN' | go run ./cmd/hashpw
```

Dán chuỗi `$2a$...` vào `bcrypt_hash` ở mục `admin:` trong `config.yaml`. Để trống thì app không khởi động. Kiểm tra `server.game_origin` là `https://uno.tnhien.tech` và `server.admin_origin` là `https://admin.tnhien.tech`.

```sh
cp .env.example .env
sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$(openssl rand -hex 16)/" .env
```

Ý nghĩa các biến trong `.env`:

| Biến                | Ví dụ                            | Ý nghĩa                                                         |
| ------------------- | -------------------------------- | --------------------------------------------------------------- |
| `POSTGRES_PASSWORD` | chuỗi ngẫu nhiên                 | Mật khẩu Postgres. Đổi sau khi DB đã tạo sẽ làm app mất kết nối |
| `UNOFA_ADMIN`       | `true` / `false`                 | `false` thì nginx bỏ site admin, không truy cập được từ ngoài   |

Tên image (`ghcr.io/tranngochien278/unofa:latest`, `...unofa-nginx:latest`) ghi cố định trong `docker-compose.yml`.

Compose chỉ dùng `.env` để điền `${...}` trong `docker-compose.yml` và `docker-compose.db.yml`. Container chỉ nhận biến được khai báo ở `environment:` của service đó.

### 3.4 Gửi file lên VPS

```sh
ssh ubuntu@IP_VPS "sudo mkdir -p /opt/unofa/deploy/nginx && sudo chown -R ubuntu /opt/unofa"
scp docker-compose.yml docker-compose.db.yml .env config.yaml ubuntu@IP_VPS:/opt/unofa/
scp deploy/nginx/*.conf ubuntu@IP_VPS:/opt/unofa/deploy/nginx/
```

Conf nginx phải nằm đúng `/opt/unofa/deploy/nginx/`: compose mount đường dẫn `./deploy/nginx` tính từ thư mục chứa `docker-compose.yml`.

## 4. Cài đặt trên VPS

Tất cả lệnh dưới đây chạy trên VPS (`ssh ubuntu@IP_VPS`).

### 4.1 Cho user dùng Docker không cần sudo

```sh
sudo usermod -aG docker ubuntu
exit
```

SSH vào lại, chạy `docker ps`. Không có lỗi `permission denied` là được.

Không dùng `sudo docker ...`: lệnh đó chạy dưới root, root không có thông tin `docker login` nên pull image sẽ bị `unauthorized`.

### 4.2 Quyền file

```sh
cd /opt/unofa
chmod 600 .env
sudo chown 10001:10001 config.yaml && chmod 600 config.yaml
```

Container `app` chạy bằng uid `10001`. `config.yaml` thuộc uid này thì app đọc được còn user khác thì không.

### 4.3 Đăng nhập registry

Package trên ghcr.io mặc định là private:

```sh
echo 'TOKEN' | docker login ghcr.io -u tranngochien278 --password-stdin
```

Có thể dùng token riêng cho VPS chỉ có quyền `read:packages`.

### 4.4 Xin cert HTTPS

Certbot standalone cần cổng 80 trống. Chạy bước này **trước** khi bật nginx (hoặc `docker compose stop nginx` nếu nginx đang chạy):

```sh
sudo apt install -y certbot
sudo certbot certonly --standalone -d uno.tnhien.tech
sudo certbot certonly --standalone -d admin.tnhien.tech
```

Cert lưu ở `/etc/letsencrypt/live/<domain>/`. Compose mount `/etc/letsencrypt` vào container nginx, nên không cần copy cert.

Lỗi `The Certificate Authority failed to download the challenge files`: cổng 80 bị chặn hoặc DNS chưa trỏ đúng. Xem lại mục 2.

### 4.5 Chạy

```sh
cd /opt/unofa
docker volume create unofa_pgdata
docker compose -f docker-compose.db.yml up -d --wait
docker compose pull
docker compose up -d --no-build
```

- `docker volume create` cần cho máy mới, chạy lại khi volume đã có thì không sao. Volume khai báo `external` nên compose không bao giờ tự tạo hay xoá nó, kể cả `down -v`.
- Bật stack DB trước: nó tạo mạng `unofa-db` mà stack app cần. `--wait` chờ Postgres `healthy`, vì `migrate` chỉ thử kết nối một lần rồi thoát.
- `--no-build` bắt buộc trên VPS: không có `Dockerfile` ở đây, compose chỉ được pull image.

## 5. Kiểm tra

```sh
docker compose -f docker-compose.db.yml ps
docker compose ps
curl -sSI https://uno.tnhien.tech/healthz | head -1
curl -sSI https://admin.tnhien.tech | head -1
```

- `db`: `Up (healthy)`.
- `app`, `redis`, `nginx`: `Up` (app có thêm `healthy`). `migrate`: `Exited (0)`.
- Hai lệnh `curl` trả `HTTP/2 200`. `curl` tự kiểm tra cert, lỗi cert sẽ báo `SSL certificate problem`.
- Mở `https://uno.tnhien.tech` để chơi. Mở `https://admin.tnhien.tech`, đăng nhập user `root` với mật khẩu đã hash ở bước 3.3.

Kiểm tra cert đúng domain:

```sh
echo | openssl s_client -connect admin.tnhien.tech:443 -servername admin.tnhien.tech 2>/dev/null | grep subject
```

Phải ra `CN = admin.tnhien.tech`.

## 6. Bật / tắt site admin

Sửa `UNOFA_ADMIN` trong `/opt/unofa/.env`, rồi:

```sh
docker compose up -d --no-build
docker compose logs nginx | grep admin-toggle
```

Khi `UNOFA_ADMIN=false`, log có dòng `admin site disabled`. Listener admin của app (8081) vẫn chạy nhưng chỉ trong mạng Docker, không truy cập được từ internet.

## 7. Cập nhật phiên bản mới

1. Máy local: build và push lại (mục 3.2).
2. VPS:
   ```sh
   cd /opt/unofa
   docker compose pull
   docker compose up -d --no-build
   ```

Compose tự chạy lại `migrate` trước khi khởi động `app`. Stack DB không bị khởi động lại, dữ liệu nằm trong volume `unofa_pgdata`.

Đang chạy bản cũ (`db` còn nằm trong `docker-compose.yml`)? Làm mục 10 một lần trước.

Sửa `config.yaml` trên VPS thì chạy `docker compose restart app`. Sửa `.env` thì chạy `docker compose up -d --no-build`.

Sửa conf nginx: gửi file lên (máy local), rồi nạp lại (VPS):

```sh
scp deploy/nginx/*.conf ubuntu@IP_VPS:/opt/unofa/deploy/nginx/
docker compose exec nginx nginx -t && docker compose exec nginx nginx -s reload
```

`nginx -t` kiểm tra cú pháp trước. `reload` nạp conf mới mà không ngắt kết nối đang mở, WebSocket của ván đang chơi vẫn giữ. Thêm hoặc xoá file `.conf` thì phải `docker compose restart nginx`, lệnh này ngắt mọi kết nối vài giây.

## 8. Gia hạn cert

Cert Let's Encrypt hết hạn sau 90 ngày. Gói `certbot` đã cài sẵn timer tự chạy `certbot renew` hằng ngày. Vì dùng chế độ standalone, nginx phải tắt trong lúc gia hạn. Tạo hai hook một lần:

```sh
sudo tee /etc/letsencrypt/renewal-hooks/pre/stop-nginx.sh >/dev/null <<'EOF'
#!/bin/sh
docker compose -f /opt/unofa/docker-compose.yml stop nginx
EOF
sudo tee /etc/letsencrypt/renewal-hooks/post/start-nginx.sh >/dev/null <<'EOF'
#!/bin/sh
docker compose -f /opt/unofa/docker-compose.yml start nginx
EOF
sudo chmod +x /etc/letsencrypt/renewal-hooks/pre/stop-nginx.sh /etc/letsencrypt/renewal-hooks/post/start-nginx.sh
sudo certbot renew --dry-run
```

`--dry-run` báo `Congratulations, all simulated renewals succeeded` là xong. Mỗi lần gia hạn, site ngừng khoảng 10–30 giây.

## 9. Xử lý lỗi thường gặp

| Triệu chứng                                                    | Nguyên nhân                                    | Cách sửa                                                   |
| -------------------------------------------------------------- | ---------------------------------------------- | ---------------------------------------------------------- |
| `permission denied ... docker.sock`                            | User chưa thuộc nhóm `docker`                  | Mục 4.1                                                    |
| Pull image `unauthorized`                                      | Chưa `docker login`, hoặc chạy bằng `sudo`     | Mục 4.3, bỏ `sudo`                                         |
| `no matching manifest for linux/arm64`                         | Image chỉ build cho `amd64`                    | Build lại với `--platform linux/amd64,linux/arm64` (3.2)   |
| `open Dockerfile: no such file or directory`                   | Compose cố build trên VPS                      | Thêm `--no-build`                                          |
| Push `denied` khi tạo package mới                              | Push 2 package mới cùng lúc                    | Push lại từng image một                                    |
| Certbot `failed to download the challenge files`               | Cổng 80 bị chặn, DNS sai, hoặc có bản ghi AAAA | Mục 2                                                      |
| Trình duyệt báo "Not secure" ở admin                           | Nginx chưa nạp `admin.conf`, trả cert của uno  | `UNOFA_ADMIN=true`, `docker compose up -d --no-build --force-recreate nginx` |
| App khởi động lại liên tục                                     | `config.yaml` sai hoặc thiếu `bcrypt_hash`     | `docker compose logs app`                                  |
| Đăng nhập admin bị chặn                                        | `admin_origin` sai domain                      | Sửa `config.yaml`, `docker compose restart app`            |
| `network unofa-db declared as external, but could not be found` | Stack DB chưa chạy                            | Bật stack DB trước (mục 4.5)                               |
| `external volume "unofa_pgdata" not found`                     | Máy mới, chưa tạo volume                       | `docker volume create unofa_pgdata` (mục 4.5)              |
| `service "migrate" didn't complete successfully`               | DB chưa `healthy`, hoặc sai `POSTGRES_PASSWORD` | `docker compose logs migrate`, rồi chạy lại mục 4.5        |
| Nginx khởi động lại liên tục, log `admin-toggle: no *.conf`    | Thiếu `/opt/unofa/deploy/nginx/*.conf`         | Mục 3.4                                                    |

Xem log: `docker compose logs <service> | tail -50`. Log DB: `docker compose -f docker-compose.db.yml logs db | tail -50`.

## 10. Chuyển từ cách deploy cũ (làm một lần)

Cách cũ: `db` nằm trong `docker-compose.yml`, conf nginx nằm trong image. Cách mới dùng lại đúng volume `unofa_pgdata` của cách cũ, không cần chép dữ liệu.

Làm đúng thứ tự dưới đây. Hai container Postgres cùng chạy trên một volume sẽ làm hỏng dữ liệu, nên container `db` cũ phải tắt hẳn trước khi bật stack DB mới.

1. Máy local: đợi CI build xong image `unofa-nginx` mới sau khi push lên `main`, hoặc build tay (mục 3.2). Image nginx cũ vẫn chạy được với compose mới nhưng bỏ qua conf mount vào.
2. VPS: backup DB. Container cũ tên `unofa-db-1` (xem bằng `docker ps`):
   ```sh
   cd /opt/unofa
   docker exec unofa-db-1 pg_dump -U unofa -Fc unofa > before-split.dump
   ls -lh before-split.dump
   ```
3. VPS: tắt toàn bộ stack cũ, giữ volume (không thêm `-v`):
   ```sh
   docker compose down --remove-orphans
   docker ps -a --filter volume=unofa_pgdata
   ```
   Lệnh thứ hai phải không in ra container nào. `--remove-orphans` xoá luôn `db` cũ nếu `docker-compose.yml` mới đã lỡ được gửi lên trước.
4. VPS: `docker volume ls | grep pgdata` phải ra `unofa_pgdata`. Nếu ra tên khác (thư mục deploy không tên `unofa`), sửa `name:` cuối file `docker-compose.db.yml` cho khớp.
5. Máy local: gửi file mới (mục 3.4).
6. VPS: chạy mục 4.5, rồi kiểm tra mục 5.

Dữ liệu sai sau khi chuyển thì khôi phục từ bản backup:

```sh
docker compose -f docker-compose.db.yml exec -T db pg_restore -U unofa -d unofa --clean --if-exists < before-split.dump
```
