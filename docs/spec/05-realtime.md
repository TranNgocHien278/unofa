# 05 — Realtime (WebSocket)

## Kết nối

`wss://uno.example.com/api/ws`

- Không query param, không token trong URL. Auth bằng cookie session (`__Host-unofa_sid`).
- Server kiểm header `Origin` phải đúng domain game; sai → `403`, đóng. Trình duyệt không áp CORS cho WebSocket, nên đây là lớp chặn trang lạ mở socket bằng cookie của người chơi.
- Một session một kết nối. Mở kết nối thứ hai → server đóng kết nối cũ (close code `1000`, reason `SESSION_REPLACED`).
- Một endpoint duy nhất, không frame `subscribe` (D2). Server route event theo membership đọc từ `room_members`/`match_players`.
- Không dùng socket.io hay thư viện WS đặc thù. Go: `nhooyr.io/websocket` hoặc `gorilla/websocket`. FE: `WebSocket` gốc + wrapper tự reconnect.

## Khung message

Text frame, JSON. Mọi message có `type`.

Server → client:

```json
{ "type": "event", "name": "match_start", "seq": 1, "server_now": "...", "data": { ... } }
{ "type": "error", "code": "INVALID_MOVE", "request_id": "..." }
{ "type": "ack", "ref": "client-abc-123" }
```

Client → server:

```json
{ "type": "action", "ref": "client-abc-123", "match_id": "...", "action": "play", "card": {"color":"RED","rank":"7"} }
```

- `ref` do client sinh (chuỗi ngẫu nhiên), dùng để ghép `ack`/lỗi với hành động gửi đi. Không phải idempotency key: gửi hai lần là hai hành động, lần hai bị luật game từ chối.
- Message tối đa `4 KB` (server → client không giới hạn cứng, nhưng snapshot đầy đủ giữ dưới `8 KB`). Client gửi quá `4 KB` → đóng kết nối `1009`.
- Mọi event và snapshot kèm `server_now`.

## Hệ quả của `server_now`

Client tính:

```
offset = server_now - Date.now()          // mỗi lần nhận event
remaining = turn_deadline - (Date.now() + offset)
```

Đếm ngược theo `remaining`, và tính lại `offset` mỗi event để bù drift.

Không bao giờ dùng "còn X giây": countdown sẽ trôi khi lag. Server chỉ gửi timestamp tuyệt đối.

## Event server → client

| `name` | Gửi tới | `data` |
|---|---|---|
| `hello` | người kết nối | `{user_id, room: {room_id, seat, code} \| null, match_id: string \| null}` |
| `room_state` | cả phòng | `{room_id, code, stake, status, host_user_id, players: [{user_id, username, seat, lobby_ready}]}` |
| `host_changed` | cả phòng | `{host_user_id}` |
| `ready_check` | từng người | `{match_id}` |
| `match_start` | kênh riêng từng người | `{match_id, hand, ...public_state}` |
| `action` | cả phòng | event nước đi, xem dưới |
| `player_status` | cả phòng | `{user_id, status: "connected"\|"disconnected"\|"forfeit"}` |
| `match_end` | cả phòng | `{winner_id, pot, reason: "out_of_cards"\|"last_standing"\|"burn"}` |
| `balance` | kênh riêng | `{balance, delta, reason}` |
| `loan` | kênh riêng | `{loan \| null, seized?: bool}` |
| `state` | kênh riêng từng người | Snapshot đầy đủ theo viewer (cùng shape `GET /api/matches/{id}/state`), gửi sau **mỗi lô** event của ván. Không có `seq` ở top-level; `data.seq` = event cuối của lô. Không gửi sau `match_end`. |

`match_start` gửi **lần đầu tiên** state, sau khi chia bài và trừ tiền xong. `hand` là tay bài của chính người nhận.

`state` có vì event `action` chỉ là delta (không mang lượt hiện tại, `turn_deadline`, số lá). Client coi `state` là sự thật, dùng `action` cho animation và thông báo. Áp `state` khi `data.seq >= last_seq`.

`match_end` chỉ gửi **sau khi thắng được xác nhận** (sau khi `PAYOUT` commit), không gửi lúc tay bài về `0`.

## Event `action`

```json
{
  "type": "event", "name": "action", "seq": 7, "server_now": "...",
  "data": {
    "match_id": "...",
    "player_id": 7,
    "action": "play",
    "card": { "color": "RED", "rank": "7" },
    "chosen_color": "BLUE",
    "target_player_id": 12,
    "result": "success",
    "at": "2026-11-12T09:30:05Z"
  }
}
```

| Field | Có khi |
|---|---|
| `card` | `play`. `draw` **không** có `card`. |
| `chosen_color` | `play` là `WILD`/`WILD4`. |
| `target_player_id` | `catch_uno`, `challenge`, `accept` (người bị ảnh hưởng). |
| `result` | `catch_uno`/`challenge`: `"success"` hoặc `"fail"`. |

`action` ∈ `play | draw | pass | call_uno | catch_uno | challenge | accept`.

**Quan trọng:** `draw` gửi cho cả phòng **không có** `card`. Lá vừa rút chỉ đi qua kênh riêng của người rút, trong event `action` riêng hoặc trong snapshot.

## Phân kênh

| Kênh | Nội dung |
|---|---|
| **Riêng, per player** | Tay bài của chính họ, số dư, khoản vay, kết quả rút bài. |
| **Chung, per room** | `player_id`, **số lá** của từng người (không phải lá gì), lá đỉnh chồng bỏ, số lá trong chồng rút, lượt hiện tại, `turn_deadline`, pot, danh sách action. |

**Không bao giờ gửi**, ở bất kỳ kênh nào:

- Bài của người khác.
- Thứ tự chồng rút.
- `Deck` array.

`Deck` chỉ tồn tại trong memory của goroutine ván. Không serialize vào payload nào, kể cả payload debug và log.

## Snapshot state

`GET /api/matches/{id}/state` (04 §Ván) trả snapshot đầy đủ. Cùng shape `data` với `match_start`, nhưng theo viewer.

Dùng khi:

1. Reconnect sau mất mạng (`90s` grace).
2. Phát hiện thiếu `seq`.
3. Vào `/table/:matchId` lần đầu (F5 cũng rơi vào đây).

Nhờ vậy client không phải replay event cũ: F5 giữa ván vẫn chơi tiếp được.

## `seq`

- Tăng đơn điệu theo ván, bắt đầu `1` ở `match_start`.
- Client apply theo `seq`. Thiếu `seq` → gọi lại `GET /api/matches/{id}/state` thay vì đoán.
- Client giữ `last_seq`. Event có `seq <= last_seq` bị bỏ (duplicate sau reconnect).
- Event có `seq > last_seq + 1` → mất event, gọi snapshot.

## Optimistic UI

Không có client prediction cho bài ẩn.

Chỉ optimistic cho hành động của **chính mình**:

- Bấm `play`: lá mờ đi ngay (opacity, không xoá khỏi DOM).
- Server trả `error` → rollback, lá sáng lại, hiện message lỗi.
- Server trả event `action` thành công → lá biến mất khỏi tay.

Không dự đoán được kết quả của `draw`, `challenge`, hay nước đi của người khác.

## Thứ tự xử lý

Mỗi ván do **đúng một goroutine** xử lý, state nằm trong memory của goroutine đó.

Mọi thứ đi vào **một channel** của goroutine đó và chạy lần lượt:

- Message WS từ người chơi.
- Timer hết lượt `20s`.
- Timer forfeit `90s`.
- Message đóng socket.

Lý do: không có bước tuần tự này thì `play` đến đúng lúc timer `20s` hết có thể được xử lý cùng lúc với việc hết giờ, thành hai kết quả cho một lượt. `catch_uno` "ai bấm trước thắng" cũng cần đúng thứ tự này.

Server một binary nên không cần lock phân tán. Khi chạy nhiều instance thì mới cần.

Thứ tự verify khi nhận hành động: 02 §Thứ tự verify. Sai ở bước nào → frame `error` cho người gửi, **không broadcast**.

## Reconnect

### Trình tự

1. Socket đóng (mất mạng, tab ngủ, server lỗi).
2. Server: `match_players.status = 'disconnected'`, `disconnected_at = now()`, broadcast `player_status`. Tay bài, ghế, pot giữ nguyên.
3. Client wrapper thử lại với backoff lũy thừa: `1s, 2s, 4s, 8s, 16s` (cộng jitter ±20%), tối đa `90s`, dừng khi quá grace.
4. Kết nối lại thành công → nhận `hello` → nếu `match_id != null` thì gọi `GET /api/matches/{id}/state` → apply snapshot.
5. Trong `90s`: chơi tiếp bình thường. Server gửi `player_status: connected` cho cả phòng.
6. Session hết TTL nhưng grace còn: client login lại bằng password, rồi gọi lại state. Đây là lý do hai đồng hồ phải tách nhau.
7. Hết `90s` không quay lại: `forfeit` (02 §Rớt mạng và forfeit).

### Hai đồng hồ

| Đồng hồ | Độ dài | Gắn với | Hết thì |
|---|---|---|---|
| Session TTL | `15p` | session Redis | Login lại bằng password. |
| Reconnect grace | `90s` | `match_players` | `forfeit`, mất ghế. |

Không gộp làm một. Session TTL dài hơn grace rất nhiều, nên bình thường người chơi quay lại trong grace không cần login lại.

### Timer vẫn chạy khi disconnected

Đồng hồ lượt `20s` chạy bất kể người chơi online hay không.

Hết `20s` mà người chơi chưa hành động: **phạt rút 1 lá và mất lượt** (02 §Hết giờ). Người disconnected hết grace sau `90s`, tức bị phạt tối đa khoảng 4 lượt trước khi `forfeit`.

## Heartbeat

`POST /api/session/heartbeat` mỗi `1p` từ FE (D3). Traffic WS **không** gia hạn session: nếu WS cũng gia hạn thì một socket treo vẫn giữ session sống ngoài ý muốn.

FE dừng heartbeat sau khi đã hiện dialog login lại, và dừng khi tab ẩn quá lâu (dùng `document.visibilityState`, không dựa vào `setInterval` bị throttle).

## Đóng kết nối

| Close code | Khi nào | Client làm gì |
|---|---|---|
| `1000` | Server đóng bình thường, hoặc `SESSION_REPLACED` | Không reconnect nếu là `SESSION_REPLACED`. |
| `1008` | Vượt rate limit `3` lần / `1p`, hoặc `Origin` sai | Không reconnect ngay; chờ `Retry-After`. |
| `1009` | Message client quá `4 KB` | Bug client; log, không reconnect. |
| `1011` | Lỗi server nội bộ | Reconnect theo backoff. |

Server đóng kết nối **không** đồng nghĩa `forfeit`: grace `90s` vẫn còn.

## Gửi event

- Mỗi client có một buffered channel (`256` message). Đầy → đóng kết nối client đó (`1011`), không block goroutine ván. Block goroutine ván vì một client chậm là làm hỏng cả bàn.
- Gửi theo thứ tự `seq` tăng dần. Không gộp, không nén ở MVP.
- Kênh riêng và kênh chung dùng cùng một socket, phân biệt bằng `data`/`name`, không cần nhiều socket.

## Test bắt buộc

| # | Test |
|---|---|
| W1 | Client gửi `play` đến đúng lúc timer `20s` hết: một kết quả duy nhất. Test bằng channel tuần tự, không sleep. |
| W2 | `catch_uno` từ hai client đồng thời: đúng một người thắng. |
| W3 | Client gửi message `5 KB` → đóng `1009`. |
| W4 | `Origin` sai → `403`, không mở socket. |
| W5 | Mở socket thứ hai từ cùng session → socket cũ nhận `1000 SESSION_REPLACED`. |
| W6 | Mất mạng `60s` rồi reconnect: snapshot đầy đủ, chơi tiếp được, không mất ghế. |
| W7 | Mất mạng `95s`: `forfeit`, bài vào Cọc Bỏ. |
| W8 | Socket đóng nhưng session còn: `hello` sau reconnect có `match_id` đúng. |
| W9 | Client chậm (không đọc) → server đóng socket đó, các client khác vẫn nhận event đúng `seq`. |
| W10 | Payload `draw` không chứa `card`. |
| W11 | Payload bất kỳ không chứa bài người khác: test bằng cách serialize toàn bộ event trong một ván đầy đủ và grep. |
