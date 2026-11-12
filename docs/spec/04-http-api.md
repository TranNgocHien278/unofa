# 04 — HTTP API

Base: `https://uno.example.com/api`. Admin: `https://admin.example.com/api/admin`.
Mọi body là JSON, `Content-Type: application/json`. Mọi response lỗi cùng shape.

## Quy ước chung

### Response lỗi

```json
{ "error": "INSUFFICIENT_BALANCE", "request_id": "01J8..." }
```

- `error`: mã trong bảng §Mã lỗi. FE dịch mã, không hiển thị câu chữ của server.
- Không bao giờ trả message kỹ thuật, stack trace, hay tên bảng DB.
- `request_id` trùng `http_log.request_id`, hiện được cho người dùng khi báo lỗi.

### Auth

Session key trong cookie `__Host-unofa_sid; HttpOnly; Secure; SameSite=Strict; Path=/`.
Không header `Authorization`. Không token trong URL.

Endpoint cần auth trả `401 UNAUTHENTICATED` khi thiếu hoặc sai session.

### Thời gian

Mọi timestamp là RFC3339 UTC, ví dụ `2026-11-12T09:30:00Z`.
Mọi response có field thời gian kèm `server_now`.

### Phân trang

Endpoint danh sách dùng `?limit=50&before=<cursor>`; `limit` tối đa `100`, mặc định `50`. Trả `{items: [...], next_cursor: "..." | null}`. Cursor là id dòng cuối (opaque với FE).

## Auth

### `POST /api/register`

```json
{ "username": "alice", "password": "hunter2hunter2", "terms_accepted": true }
```

`201`: `{ "user_id": 7, "username": "alice", "balance": 200 }` + set cookie.

Lỗi: `USERNAME_TAKEN`, `USERNAME_INVALID`, `PASSWORD_TOO_SHORT`, `PASSWORD_TOO_LONG`, `TERMS_NOT_ACCEPTED`, `RATE_LIMITED`.

### `POST /api/login`

```json
{ "username": "alice", "password": "hunter2hunter2", "terms_version": 1 }
```

`200`: `{ "user_id": 7, "username": "alice", "balance": 200, "locale": "vi", "terms_version": 1 }` + set cookie.

Lỗi: `INVALID_CREDENTIALS` (không phân biệt sai username hay sai password), `LOGIN_LOCKED`, `TERMS_OUTDATED`, `RATE_LIMITED`, `SERVICE_UNAVAILABLE`.

`LOGIN_LOCKED` trả kèm `retry_after_seconds`.

### `POST /api/logout`

`204`. Xoá session Redis, đóng WS.

### `POST /api/session/heartbeat`

`204` + `EXPIRE` lại key (sliding, TTL `15p`). FE gọi mỗi `1p` (D3).

Session hết TTL → `401 SESSION_EXPIRED`. FE hiện dialog nhập lại password.

### `GET /api/me`

`200`: `{ user_id, username, balance, locale, terms_version, active_loan: {...} | null }`.

### `PATCH /api/me`

```json
{ "locale": "en" }
```

Hoặc đổi username / password (D5):

```json
{ "username": "alice2", "current_password": "hunter2hunter2" }
{ "new_password": "newpass1234", "current_password": "hunter2hunter2" }
```

`200` với `{username}` hoặc `{locale}`. Không bao giờ trả lại password.

Lỗi: `INVALID_CREDENTIALS`, `USERNAME_TAKEN`, `USERNAME_INVALID`, `PASSWORD_TOO_SHORT`, `RATE_LIMITED`.

Đổi password: xoá **mọi** session của user (chỉ có 1 theo thiết kế), cấp session mới, đóng WS cũ.

## Tiền

### `POST /api/transfers`

```json
{ "to_username": "bob", "amount": 50, "client_uuid": "9f3c..." }
```

`201`: `{ transaction_id, amount: 50, to_username: "bob", balance_after: 150 }`.

Lỗi: `INSUFFICIENT_BALANCE`, `TRANSFER_LIMIT`, `SELF_TRANSFER`, `USER_NOT_FOUND`, `LOAN_OVERDUE`, `RATE_LIMITED`.

`client_uuid` do FE sinh (`crypto.randomUUID()`), dùng làm idempotency key `transfer:{client_uuid}`. Gửi lại cùng `client_uuid` → trả kết quả cũ, `201` vẫn trả về.

### `GET /api/transactions`

Query: `?limit&before&reason=BET,PAYOUT&direction=in,out`

`200`:

```json
{ "items": [{
    "id": 1234, "amount": 50, "reason": "TRANSFER",
    "direction": "out", "counterparty": "bob",
    "balance_after": 150, "at": "2026-11-12T09:30:00Z"
}], "next_cursor": null }
```

`direction` tính theo chiều dòng: `out` nếu user là `from_user_id`, `in` nếu là `to_user_id`.
`counterparty`: username bên kia, hoặc `"System"`, hoặc tên phòng/ván nếu là pot.

### `GET /api/me/balance-chart?range=30d`

`range` ∈ `Today | 24h | 3d | 7d | 30d | 60d`. Mặc định `30d`.

`200`: `{ "points": [{ "at": "...", "balance": 150 }, ...], "server_now": "..." }`

Bucket **phía server**, một điểm mỗi bucket. Không gửi raw log xuống client (60 ngày là hàng chục nghìn dòng, và client không được thấy chi tiết hơn mức cần).

### `GET /api/me/matches`

Lịch sử ván (D15, cam kết §8 của bản miễn trừ).

`200`: `{ "items": [{ "match_id", "stake", "pot", "result": "win"|"lose"|"aborted", "ended_at" }], "next_cursor": null }`.

## Loan

### `GET /api/loans/products` (D6)

`200`:

```json
{
  "packages": [500, 1000, 2000],
  "terms": [
    { "term": "24h", "rate_bps": 100 },
    { "term": "3d",  "rate_bps": 300 },
    { "term": "30d", "rate_bps": 1000 }
  ],
  "overdue_bps": 500,
  "cap_multiplier": 2
}
```

### `POST /api/loans`

```json
{ "principal": 500, "term": "24h" }
```

`201`: `{ loan_id, principal, term, due_at, owed_now, balance_after }`.

Lỗi: `LOAN_ACTIVE_EXISTS`, `INVALID_LOAN_PRODUCT`, `RATE_LIMITED`.

### `GET /api/loans/active`

`200`: `{ loan_id, principal, due_at, owed_now, overdue: bool, periods_overdue: n, seize_at: "..." }` hoặc `null`.

`seize_at` tính được ngay lúc vay vì lãi cố định — dùng để FE hiện "bị siết lúc …".

### `POST /api/loans/{id}/repay`

Không body. Trừ toàn bộ `owed`.

`200`: `{ owed_paid, balance_after }`.

Lỗi: `INSUFFICIENT_BALANCE`, `LOAN_NOT_FOUND`, `LOAN_NOT_ACTIVE`.

## Phòng

### `POST /api/rooms`

```json
{ "stake": 50 }
```

`201`: `{ room_id, code: "K7M2QX", stake, host_user_id }`.

Lỗi: `INVALID_STAKE`, `RATE_LIMITED`.

### `POST /api/rooms/{code}/join`

Không body. `200`: `{ room_id, code, stake, players: [...] }`.

Lỗi: `ROOM_NOT_FOUND`, `ROOM_FULL`, `ROOM_LOCKED`, `ALREADY_IN_ROOM`, `RATE_LIMITED`.

### `POST /api/rooms/{code}/leave`

`204`. Nếu đang trong ván `running` → `forfeit` ngay (D4), rồi `204`.

### `POST /api/rooms/{code}/ready`

```json
{ "ready": true }
```

`204`. Chỉ đổi `lobby_ready`. Bị từ chối khi `rooms.status != 'open'` → `ROOM_LOCKED`.

### `POST /api/rooms/{code}/stake` (host)

```json
{ "stake": 100 }
```

`204`. Chỉ khi `status = 'open'`.

### `POST /api/rooms/{code}/kick` (host)

```json
{ "user_id": 12 }
```

`204`. Từ chối khi `status != 'open'` (D11) → `ROOM_LOCKED`. Không kick được chính host.

### `POST /api/rooms/{code}/play` (host)

Bắt đầu ván. Không body.

`200`: `{ match_id }` — trả ngay sau khi khoá phòng. Handshake tải asset chạy qua WS.

Lỗi: `ROOM_LOCKED`, `NOT_HOST`, `NOT_ENOUGH_PLAYERS`, `INSUFFICIENT_BALANCE`, `RATE_LIMITED`.

`INSUFFICIENT_BALANCE` khi **host** không đủ tiền; người khác thiếu bị loại ở bước 5 của handshake, không phải lỗi của host.

### `GET /api/rooms/{code}`

`200`: `{ room_id, code, stake, status, host_user_id, players: [{user_id, username, seat, lobby_ready, balance}] }`.

## Ván

### `GET /api/matches/{id}/state`

Snapshot riêng tư đầy đủ. Dùng khi reconnect, khi thiếu `seq`, hoặc lần đầu vào `/table/:matchId`.

`200`:

```json
{
  "match_id": "...",
  "seq": 42,
  "server_now": "2026-11-12T09:30:00Z",
  "you": {
    "user_id": 7,
    "hand": [{ "color": "RED", "rank": "7" }, ...],
    "can_call_uno": false,
    "can_catch_uno": 12,
    "is_your_turn": true
  },
  "public": {
    "players": [{ "user_id": 7, "seat": 0, "hand_count": 3, "status": "connected" }, ...],
    "top_card": { "color": "RED", "rank": "7" },
    "active_color": "RED",
    "draw_pile_count": 63,
    "current_player": 7,
    "direction": 1,
    "turn_deadline": "2026-11-12T09:30:20Z",
    "pot": 200,
    "phase": "playing"
  }
}
```

- `hand` chỉ của chính người gọi. Không có bài người khác ở bất kỳ đâu.
- `draw_pile_count` là số lá, không phải danh sách.
- `phase` ∈ `playing | awaiting_challenge | ended`.
- Người không phải người chơi của ván → `403 NOT_IN_MATCH`.

Rate limit `20/phút`: chỉ dùng khi reconnect hoặc thiếu `seq`; nhiều hơn tức client lỗi vòng lặp.

### `GET /api/matches/{id}`

Thông tin công khai của ván: stake, pot, players (username, seat), status, winner_id.

## Admin

Auth riêng: cookie `__Host-unofa_admin_sid`, namespace Redis `admin:session:*`. Origin khác, CORS chéo không được nhận.

| Method | Path | Việc |
|---|---|---|
| `POST` | `/api/admin/login` | `{username, password}` → session admin |
| `POST` | `/api/admin/logout` | `204` |
| `POST` | `/api/admin/session/heartbeat` | `204` |
| `GET` | `/api/admin/metrics/requests?range=` | `{buckets: [{at, by_endpoint: {"/api/login": 12, ...}}]}` |
| `GET` | `/api/admin/metrics/status-codes?range=` | `{buckets: [{at, by_code: {"200": 100, "429": 3}}]}` |
| `GET` | `/api/admin/accounts?q=&limit=&before=` | Danh sách tài khoản |
| `GET` | `/api/admin/accounts/{id}` | Chi tiết + loan + 50 giao dịch gần nhất |
| `POST` | `/api/admin/accounts/{id}/balance` | `{delta: -100}` hoặc `{delta: 100}` → `SYSTEM_ADD`/`SYSTEM_GET` |
| `DELETE` | `/api/admin/accounts/{id}` | Xoá tài khoản theo 03 §Xoá tài khoản |
| `GET` | `/api/admin/flags` | Cờ chưa xoá |
| `POST` | `/api/admin/flags/{id}/clear` | Ghi `cleared_at` |
| `GET` | `/api/admin/audit?limit=&before=` | `admin_audit` |

`range` giống 06 §Chart: `Today | 24h | 3d | 7d | 30d | 60d`.

Mọi thao tác ghi (`balance`, `delete`, `clear`) ghi `admin_audit` trong **cùng transaction** với thay đổi. Key idempotency `admin:{request_id}` với endpoint `balance`.

`balance`: `delta` dương → `SYSTEM_ADD` từ `System`; âm → `SYSTEM_GET` về `System`. Không cho đưa số dư người chơi xuống âm: `delta` âm lớn hơn số dư → `INSUFFICIENT_BALANCE`.

## Mã lỗi

Danh sách đóng (D9). Thêm mã mới là sửa file này trước, rồi FE.

| Mã | HTTP | Nghĩa |
|---|---|---|
| `UNAUTHENTICATED` | 401 | Thiếu/sai session. |
| `SESSION_EXPIRED` | 401 | Session hết TTL, cần login lại. |
| `INVALID_CREDENTIALS` | 401 | Sai username/password. |
| `LOGIN_LOCKED` | 403 | Tài khoản bị khoá `15p`. |
| `TERMS_OUTDATED` | 403 | Cần tick lại điều khoản. |
| `TERMS_NOT_ACCEPTED` | 400 | Đăng ký thiếu tick. |
| `FORBIDDEN` | 403 | Không có quyền (không phải host, không phải admin). |
| `NOT_FOUND` | 404 | Không tìm thấy tài nguyên. |
| `USERNAME_TAKEN` | 409 | Trùng username. |
| `USERNAME_INVALID` | 400 | Sai format hoặc `system`/`deleted_`. |
| `PASSWORD_TOO_SHORT` | 400 | `< 8` ký tự. |
| `PASSWORD_TOO_LONG` | 400 | `> 72` byte. |
| `RATE_LIMITED` | 429 | Vượt rate limit, có `Retry-After`. |
| `INSUFFICIENT_BALANCE` | 400 | Không đủ `U`. |
| `TRANSFER_LIMIT` | 400 | Vượt `1000U/24h`. |
| `SELF_TRANSFER` | 400 | Chuyển cho chính mình. |
| `USER_NOT_FOUND` | 404 | Username không tồn tại. |
| `LOAN_OVERDUE` | 403 | Có khoản vay quá hạn. |
| `LOAN_ACTIVE_EXISTS` | 409 | Đã có khoản active. |
| `LOAN_NOT_FOUND` | 404 | Không có khoản vay. |
| `LOAN_NOT_ACTIVE` | 400 | Khoản đã đóng. |
| `INVALID_LOAN_PRODUCT` | 400 | Gói/kỳ hạn ngoài danh sách. |
| `INVALID_STAKE` | 400 | Ngoài `{10,25,50,100,500}`. |
| `ROOM_NOT_FOUND` | 404 | Mã sai hoặc phòng đã xoá. |
| `ROOM_FULL` | 409 | Đủ `10` người. |
| `ROOM_LOCKED` | 409 | Phòng đang khoá handshake hoặc đang có ván. |
| `ALREADY_IN_ROOM` | 409 | Đã ở trong một phòng khác. |
| `NOT_HOST` | 403 | Không phải host. |
| `NOT_ENOUGH_PLAYERS` | 400 | Dưới `2` người sẵn sàng. |
| `NOT_IN_MATCH` | 403 | Không phải người chơi của ván. |
| `MATCH_ENDED` | 409 | Ván đã xong. |
| `USER_IN_MATCH` | 409 | Admin xoá user đang trong ván `running`. |
| `SERVICE_UNAVAILABLE` | 503 | Redis/Postgres chết. |
| `INTERNAL` | 500 | Lỗi không lường trước; chỉ log, không lộ chi tiết. |

Trùng tên với bảng engine ở 02 §Mã lỗi của engine (`NOT_YOUR_TURN`, `TURN_EXPIRED`, `CARD_NOT_IN_HAND`, `INVALID_MOVE`, `MISSING_COLOR`, `MUST_PLAY_DRAWN_CARD`, `ALREADY_DREW`, `PASS_WITHOUT_DRAW`, `CHALLENGE_PENDING`, `CANNOT_CALL_UNO`, `UNO_ALREADY_CALLED`, `UNO_WINDOW_CLOSED`). Các mã đó chỉ đến qua WS frame `error`, trừ khi trùng endpoint HTTP.

## Rate limit

### Hai lớp, mỗi lớp một việc

- **nginx, theo IP:** chặn flood thô trước khi tới Go. Đặt **lỏng** — sinh viên chung Wi-Fi ký túc xá/trường học chung một IP NAT; đặt chặt thì cả phòng lab bị khoá vì một người.
- **Go + Redis, theo user (và theo IP với endpoint chưa đăng nhập):** lớp chính, số đặt theo từng nhóm endpoint.

### Lớp nginx

| Phạm vi | Giới hạn | Burst |
|---|---|---|
| Mọi request HTTP, theo IP | `30 r/s` | `60`, `nodelay` |
| Kết nối đồng thời, theo IP | `50` | — |
| Mở WebSocket mới, theo IP | `10 /phút` | `5` |

### Lớp app

| Nhóm | Khoá | Giới hạn | Lý do con số |
|---|---|---|---|
| `POST /login` | IP | `5 /phút` | Chặn dò password từ một máy. |
| `POST /login` | username | `10 /giờ` | Chặn dò từ nhiều IP. Vượt → khoá login tài khoản `15p`, báo admin. |
| `POST /register` | IP | `10 /giờ` | Mỗi tài khoản mới nhận `200U`. Không giới hạn → tạo tài khoản rác gom tiền. Không đặt `3`: nhóm bạn chung Wi-Fi trường sẽ chung IP. |
| Heartbeat | session | `3 /phút` | Client gửi `1 /phút`. Dư gấp 3 cho tab ngủ dậy gửi dồn. |
| Hành động trong ván qua WS | user | `5 /s` | Một lượt tối đa 2–3 hành động; `5 /s` chỉ chặn script. |
| `call_uno`, `catch_uno` | user | `2 /s` | Cuộc đua ai bấm trước; không giới hạn riêng thì script spam luôn thắng. |
| `GET /api/matches/{id}/state` | user | `20 /phút` | Chỉ dùng khi reconnect/thiếu `seq`. |
| Chuyển khoản, vay, trả nợ | user | `10 /phút` | Ngoài hạn mức `1000U/24h` sẵn có. |
| Đổi password, đổi username | user | `5 /giờ` | Đổi password cần password cũ → cũng là đường dò password. |
| Đọc: lịch sử, chart, profile, room info | user | `60 /phút` | Đủ cho người chuyển tab liên tục. |
| Tạo phòng | user | `10 /giờ` | Chặn tạo phòng rác. |
| Vào phòng bằng mã | user | `10 /phút` | Chặn dò mã phòng. |
| Admin login | IP | `3 /phút` | Admin sửa được số dư. |
| Admin API | admin | `60 /phút` | Một người dùng. |

### WebSocket

- Một session một kết nối WS. Mở kết nối thứ hai → đóng kết nối cũ.
- Message tối đa `4 KB`. Lớn hơn → đóng kết nối. Message hợp lệ lớn nhất của game dưới `200 byte`.
- Tổng mọi message từ client: `10 /s`, cộng dồn mọi loại.

### Khi vượt

- HTTP: `429`, header `Retry-After` (giây), body `{"error": "RATE_LIMITED", "request_id": "..."}`.
- WS: frame lỗi `RATE_LIMITED`, message đó không xử lý. Vượt `3` lần trong `1 phút` → đóng kết nối, close code `1008`. Người chơi vẫn còn `90s` reconnect grace nên không mất ghế.
- Mọi `429` ghi vào `http_log` → hiện trên chart HTTP code của admin, không cần code thêm.

### Cài đặt

Fixed window, một Redis round-trip:

```
MULTI
INCR rl:{nhóm}:{khoá}:{window}
EXPIRE rl:{nhóm}:{khoá}:{window} {độ dài window} NX
EXEC
```

`NX` (Redis 7+) chỉ đặt TTL khi key chưa có TTL. Không dùng "lần đầu thì EXPIRE" trong `MULTI`: lệnh trong `MULTI` không đọc được kết quả `INCR` để rẽ nhánh.

`shortcut: fixed window cho phép tới 2× giới hạn ở ranh giới hai window, chuyển sang sliding window khi log admin cho thấy có người lợi dụng ranh giới.`

### Redis chết

- Session chết theo → mọi API cần đăng nhập trả `503 SERVICE_UNAVAILABLE`.
- `POST /login` và `POST /register` **từ chối** khi Redis chết, không cho qua: cho qua lúc đó là mở cửa cho dò password.
- Mọi con số nằm trong config, đọc lúc startup. Đổi phải restart.
