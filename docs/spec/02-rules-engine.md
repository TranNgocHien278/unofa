# 02 — Rules engine

Nguồn luật: `../luat_choi_uno_tieu_chuan.md`. File này chỉ ghi chỗ luật giấy để ngỏ, hoặc chỗ game online buộc phải khác, cộng toàn bộ state machine cần cho code.

## Bộ bài

108 lá.

| Loại | Số lá | Mỗi màu |
|---|---|---|
| Số `0` | 4 | 1 |
| Số `1`–`9` | 72 | 2 mỗi số |
| `SKIP` | 8 | 2 |
| `REVERSE` | 8 | 2 |
| `DRAW2` | 8 | 2 |
| `WILD` | 4 | — (không màu) |
| `WILD4` | 4 | — (không màu) |

Màu: `RED`, `YELLOW`, `GREEN`, `BLUE`. Hai lá wild có `color = NULL`.

Kiểu dữ liệu trong Go:

```go
type Color uint8   // 0=RED 1=YELLOW 2=GREEN 3=BLUE 4=NONE
type Rank  uint8   // 0..9, 10=SKIP 11=REVERSE 12=DRAW2 13=WILD 14=WILD4
type Card  struct{ Color Color; Rank Rank }
```

`Card.Color == NONE` chỉ đúng với `WILD`/`WILD4`. Không có lá số không màu.

## Nguồn ngẫu nhiên

- Xáo: `crypto/rand` + Fisher–Yates. Không `math/rand`, không seed tái sử dụng.
- Dùng chung RNG cho: xáo bài, chọn người đi đầu, chọn chiều ban đầu, chọn host mới khi host rời.
- Không có bước mô phỏng "rút lá chọn người chia".

## Bắt đầu ván

1. Xáo 108 lá.
2. Chia `7` lá mỗi người, theo thứ tự ghế.
3. Lật lá đầu Cọc Bỏ. Nếu lá đó **không phải lá số** (`Rank > 9`), đưa lại vào Cọc Rút, xáo lại, lật lá khác. Lặp tới khi ra lá số. Đơn giản nhất, không cần luật riêng cho từng lá chức năng.
4. `current_player` = người ngồi sau người chia, nhưng không mô phỏng người chia: chọn ngẫu nhiên một người trong bàn.
5. `direction` chọn ngẫu nhiên: `+1` (thuận) hoặc `-1` (ngược).
6. `turn_deadline = now + 20s`.

Lá đầu là lá số nên người đầu tiên luôn có nước đi hợp lệ theo màu hoặc số.

## Vòng lượt

### Hành động hợp lệ

| Action | Điều kiện |
|---|---|
| `play` | Có lá trong tay, lá hợp lệ với đỉnh Cọc Bỏ, đúng lượt, chưa hết giờ. |
| `draw` | Đúng lượt, chưa `draw` trong lượt này. |
| `pass` | Đúng lượt, đã `draw` trong lượt này. |
| `call_uno` | `can_call_uno == true` cho người đó. |
| `catch_uno` | `can_catch_uno == user_id` của người gửi. |
| `challenge` | Ván đang ở trạng thái chờ quyết định `+4` và người gửi là người phải quyết định. |
| `accept` | Như `challenge`. |

### Luật khớp lá

Một lá `c` đánh được lên đỉnh `top` với màu hiệu lực `active_color` khi:

```
c.Rank in {WILD, WILD4}                          → true
c.Color == active_color                          → true
c.Rank == top.Rank and c.Rank <= 9               → true   // cùng số
c.Rank == top.Rank and top.Rank in {SKIP, REVERSE, DRAW2} → true
```

`active_color` = `top.Color`, trừ khi `top` là `WILD`/`WILD4` thì là màu đã chọn cho lá đó. Server giữ `active_color` là biến riêng của state ván, không suy ra từ `top.Color` mỗi lần.

`WILD4` không có điều kiện "chỉ đánh khi không có lá cùng màu". Luật giấy cho đánh sai rồi bị thách thức; server không chặn ở bước này.

### `play`

1. Lá phải nằm trong tay người gửi.
2. Lá phải hợp lệ theo hàm trên.
3. `WILD`/`WILD4`: `chosen_color` bắt buộc và phải khác `NONE`. Thiếu thì `MISSING_COLOR`.
4. Nếu lượt này đã `draw` rồi: chỉ được đánh **đúng lá vừa rút**. Đánh lá khác → `MUST_PLAY_DRAWN_CARD`.
5. Bỏ lá khỏi tay, đẩy lên Cọc Bỏ, cập nhật `active_color`.
6. Áp hiệu ứng:

| Lá | Hiệu ứng |
|---|---|
| Số | Không. |
| `SKIP` | Người kế tiếp mất lượt. `current_player` nhảy 2 bước. |
| `REVERSE` | Đảo `direction`. Bàn 2 người: hoạt động như `SKIP`, người đánh đi tiếp (tức là người đó đi lượt nữa). |
| `DRAW2` | Người kế tiếp rút `2` lá, mất lượt. Không cộng dồn. |
| `WILD` | Không. `active_color = chosen_color`. |
| `WILD4` | Người kế tiếp vào trạng thái `awaiting_challenge` (xem dưới). Không cộng dồn. |

7. `DRAW2`/`WILD4` đẩy người kế tiếp vào trạng thái chờ; cả hai đều **không** cộng dồn với lá trước.
8. Sau khi áp hiệu ứng, kiểm UNO (mục dưới), rồi kiểm thắng (mục dưới).
9. `turn_deadline` đặt lại cho người sắp đi, trừ khi ván kết thúc.

### `draw`

1. Rút `1` lá từ Cọc Rút.
   - Cọc Rút rỗng: xáo lại Cọc Bỏ **trừ lá trên cùng**, thành Cọc Rút mới (D12).
   - Cả hai cọc rỗng: không rút được, vẫn cho phép `pass`.
2. Lá vừa rút gửi **chỉ qua kênh riêng** của người rút. Event công khai của `draw` không có `card`.
3. Lượt không tự kết thúc. Người chơi chọn:
   - `play` đúng lá vừa rút nếu nó hợp lệ.
   - `pass` nếu không đánh.
4. Hết `20s` sau khi rút: chỉ mất lượt, không rút thêm (xem §Hết giờ).

### `pass`

Chỉ hợp lệ sau `draw` trong cùng lượt → `PASS_WITHOUT_DRAW` nếu không.

### Chuyển lượt

```
next(idx) = (idx + direction + n) % n
```

`n` = số người chơi còn sống (chưa `forfeit`). Người `forfeit` bị loại khỏi vòng hoàn toàn, không chỉ mất một lượt.

## `WILD4` và thách thức

### Trạng thái

Khi người A đánh `WILD4`:

- Ván sang `phase = awaiting_challenge`, `pending_challenger = người kế tiếp A`, `turn_deadline = now + 20s`.
- Snapshot lưu cùng nước đi: `had_matching_color` = tay bài A tại thời điểm đánh có ít nhất một lá cùng `active_color` trước khi đánh `WILD4` hay không. **Lưu tại thời điểm đó**, không kiểm lại sau.
- Chỉ người `pending_challenger` được gửi `challenge` hoặc `accept`. `play`/`draw`/`pass` từ họ → `CHALLENGE_PENDING`. Từ người khác → `NOT_YOUR_TURN`.

### Kết quả

| Tình huống | Hành vi |
|---|---|
| `challenge` và `had_matching_color == true` | Thách thức **đúng**. A rút `4` lá. Ván tiếp tục. `current_player = pending_challenger`, lượt bình thường, `20s` mới. A không thắng. |
| `challenge` và `had_matching_color == false` | Thách thức **sai**. Challenger rút `6`, mất lượt. `current_player` = người sau challenger. |
| `accept` | Challenger rút `4`, mất lượt. |
| Hết `20s` | Như `accept`. **Không** phạt thêm `1` lá hết giờ. |
| Challenger `forfeit` khi đang chờ | Như `accept`. |

Công bố chỉ `"correct"` / `"wrong"`, không gửi tay bài A cho ai, kể cả challenger. Luật giấy cho xem bài; online thì đó là rò bài.

### `WILD4` là lá cuối cùng của A

Thắng **chưa xác nhận**. `match_end` và `PAYOUT` chưa chạy.

- `challenge` đúng: A rút `4` (tay A thành `4` lá), ván tiếp tục, challenger đi lượt bình thường. A không thắng.
- `challenge` sai, `accept`, hết `20s`, hoặc challenger `forfeit`: thắng được xác nhận. A thắng pot. **Không ai rút `4`**, ván đã xong.

## UNO

### Cờ

`uno_called[player_id] bool`, mặc định `false`.

Đặt `true` khi người đó gọi `UNO` thành công. Tự xoá khi tay bài người đó **lên lại trên 1 lá** (ví dụ đã gọi UNO nhưng sau đó rút bài).

### `can_call_uno`

`true` khi và chỉ khi người đó vừa đánh xuống còn **đúng 1 lá** mà chưa gọi, và cửa sổ bắt vẫn mở.

Còn `2` lá thì luôn `false`, kể cả đang là lượt của người đó: nút không render, `call_uno` bị từ chối (`CANNOT_CALL_UNO`).

### `can_catch_uno`

`player_id | null`. Chỉ một người tại một thời điểm.

`= victim_id`? Không — giá trị là người **được phép bắt**, luôn khác victim. Bằng `victim_id` không xảy ra: người còn `1` lá chưa gọi thì mọi người **khác** bắt được, nhưng `can_catch_uno` là cờ **per-viewer**: với viewer `v`, giá trị là `victim_id` nếu `victim_id != v`, ngược lại `null`.

Điều kiện cửa sổ bắt đang mở:

- Có victim với `hand_len == 1` và `uno_called[victim] == false`.
- Người kế tiếp victim (theo `direction`) chưa hành động.

Cửa sổ đóng khi (D13):

- Người kế tiếp victim đánh, rút, hoặc hết giờ.
- Victim bị `SKIP`/`DRAW2`/`WILD4` bỏ lượt → đóng khi lượt của người sau victim bắt đầu.
- Ván kết thúc.
- Có quyết định `challenge`/`accept`.

### `call_uno`

Hợp lệ khi `can_call_uno` cho người gửi. Không hợp lệ → `CANNOT_CALL_UNO`.

### `catch_uno`

Hợp lệ khi `can_catch_uno == user_id` của người gửi. Bắt đầu tiên thắng: server xử lý theo thứ tự message trong channel của goroutine ván, chỉ phạt một lần.

- Bị bắt: victim rút `2` lá, `uno_called[victim]` không đổi.
- Người kia đã gọi rồi: `UNO_ALREADY_CALLED`, không phạt ai.
- Cửa sổ đã đóng: `UNO_WINDOW_CLOSED`.

Người disconnected vẫn bị bắt như người thường. Server không gọi `UNO` hộ ai.

## Hết giờ

Timer `20s` chạy trong goroutine ván, gửi message vào cùng channel như hành động người chơi. Không có race giữa timer và `play`: một hành động mỗi lượt.

| Trạng thái khi hết giờ | Xử lý |
|---|---|
| Lượt thường, chưa `draw` | Phạt rút `1` lá, mất lượt. |
| Đã `draw` rồi | Chỉ mất lượt. Không rút thêm. |
| `awaiting_challenge` | Như `accept`: challenger rút `4`, mất lượt. |
| Cửa sổ bắt UNO đang mở | Không phạt riêng; cửa sổ chỉ đóng khi người kế tiếp hành động. |

Áp dụng như nhau cho người online và người disconnected. Đây là phạt cố định, **không phải auto-play**: server không chọn lá nào đánh thay.

## Rớt mạng và forfeit

Hai đồng hồ độc lập:

- Session TTL `15p` — hết thì login lại bằng password.
- Reconnect grace `90s` — gắn với `match_players`, không gắn session.

### Khi socket đóng giữa ván

1. `match_players.status = 'disconnected'`, ghi `disconnected_at`. Tay bài, ghế, pot giữ nguyên ở server.
2. Trong `90s`, client gọi `GET /api/matches/{id}/state` bằng session key hiện có → snapshot đầy đủ, không replay event.
3. Session hết TTL nhưng grace còn: login lại, gọi state, vào ván bình thường.
4. Hết `90s` không quay lại: `forfeit` — rời vòng lượt, bài trên tay đưa vào Cọc Bỏ **dưới lá trên cùng**, để lần xáo lại Cọc Rút sau này dùng được.
5. Nhiều người hết grace cùng lúc: xử lý theo `disconnected_at` tăng dần.

### Thắng khi người cuối cùng đang disconnected

Khi chỉ còn `1` người, người đó thắng pot **ngay**, kể cả khi đang disconnected. Không có chỗ nào trong code cần huỷ pot cho trường hợp `0` người; nhánh `BURN` chỉ là chốt chặn phòng lỗi logic.

### Rời phòng chủ động giữa ván

`forfeit` ngay, không chờ `90s` (D4). Cùng một đường xử lý.

### Tiền

**Không hoàn trong mọi trường hợp.**

- `BET` đã trừ lúc ván bắt đầu; theo sổ sách tiền đã rời tài khoản.
- Pot trả cho người thắng, kể cả khi chưa đánh lá nào.
- Lý do: hoàn tiền biến ngắt mạng lúc sắp thua thành undo miễn phí; server không phân biệt được rớt thật với cố ý. Luật một chiều nên không lách được.
- Không có `refund` ở v3. Key `match:{id}:refund` không tồn tại.

Pot khi còn `<= 0` người: `BURN` (pot → `System`, key `match:{match_id}:burn`). Không hoàn, không xoá dòng, để invariant vẫn khớp.

## Kết thúc ván

- `match_end` và `PAYOUT` chỉ chạy **sau khi thắng được xác nhận**, không chạy lúc tay bài về `0`.
- Lá cuối là số/`SKIP`/`REVERSE`/`WILD`/`DRAW2`: xác nhận ngay. Người kế tiếp không rút, vì ván đã xong và không tính điểm.
- Lá cuối là `WILD4`: xem §`WILD4` là lá cuối cùng.
- Không tính điểm, không chơi tới `500` điểm. Một ván một lần cược.
- `PAYOUT`: `Transfer` toàn bộ `matches.pot` → người thắng, reason `PAYOUT`, key `match:{match_id}:payout`. Amount = pot, không phải `n × k` (D7).
- Khi còn `1` người vì forfeit: người đó thắng, `PAYOUT` như trên.

## State ván

```go
type Phase uint8 // 0=playing 1=awaiting_challenge 2=ended

type PlayerState struct {
    UserID       int64
    Seat         int
    Hand         []Card
    UnoCalled    bool
    Status       Status // connected | disconnected | forfeit
    DisconnectedAt *time.Time
}

type MatchState struct {
    MatchID        string
    Seq            uint64
    Phase          Phase
    Deck           []Card  // không bao giờ serialize
    Discard        []Card
    ActiveColor    Color
    Direction      int
    Current        int     // index vào Players
    Players        []*PlayerState
    Pot            int64
    TurnDeadline   time.Time
    PendingChallenger *int64
    HadMatchingColor  bool
    DrawnThisTurn  *Card     // lá đã rút lượt này, nil nếu chưa
    UnoWindowVictim *int64   // victim đang bị bắt, nil nếu cửa sổ đóng
    Winner         *int64
}
```

Một ván do **đúng một goroutine** xử lý. Mọi hành động, timer lượt, timer forfeit gửi vào channel của goroutine đó và chạy lần lượt.

## Thứ tự verify khi nhận hành động

Đúng thứ tự, không bỏ bước. Sai ở bước nào trả lỗi riêng cho người gửi, **không broadcast**:

1. Session hợp lệ → `user_id`.
2. `user_id` là người chơi của ván và ghế còn sống.
3. Trạng thái phù hợp: `playing` → nhận `play`/`draw`/`pass`/`call_uno`/`catch_uno`; `awaiting_challenge` → chỉ nhận `challenge`/`accept` từ `pending_challenger`.
4. `now < turn_deadline`.
5. Hành động hợp lệ cho trạng thái (bảng §Hành động hợp lệ).
6. Lá nằm trong tay server đang giữ.
7. Lá hợp lệ trên đỉnh Cọc Bỏ; `WILD`/`WILD4` phải kèm `chosen_color`.
8. Đã rút lượt này → chỉ được đánh đúng lá vừa rút.
9. `WILD4`: lưu `had_matching_color` snapshot.
10. Chỉ sau đó mới mutate state, tăng `seq`, gửi event với dữ liệu tối thiểu.

## Mã lỗi của engine

| Mã | Khi nào |
|---|---|
| `NOT_IN_MATCH` | Bước 2. |
| `NOT_YOUR_TURN` | Bước 3/4. |
| `CHALLENGE_PENDING` | Gửi `play`/`draw`/`pass` khi đang chờ thách thức. |
| `TURN_EXPIRED` | Bước 4. |
| `CARD_NOT_IN_HAND` | Bước 6. |
| `INVALID_MOVE` | Bước 7. |
| `MISSING_COLOR` | `WILD`/`WILD4` không có `chosen_color`. |
| `MUST_PLAY_DRAWN_CARD` | Bước 8. |
| `ALREADY_DREW` | `draw` lần hai trong lượt. |
| `PASS_WITHOUT_DRAW` | `pass` khi chưa `draw`. |
| `CANNOT_CALL_UNO` | `call_uno` ngoài cửa sổ. |
| `UNO_ALREADY_CALLED` | `catch_uno` khi victim đã gọi. |
| `UNO_WINDOW_CLOSED` | `catch_uno` sau khi cửa sổ đóng. |
| `MATCH_ENDED` | Ván đã xong. |

Danh sách đầy đủ (gồm lỗi HTTP) ở 04 §Mã lỗi.

## Bảng test bắt buộc

Mỗi dòng là một test case, không phải gợi ý.

| # | Test |
|---|---|
| R1 | Bộ bài đúng `108` lá, đúng số lượng từng loại/màu. |
| R2 | Lá đầu tiên lật lên luôn là lá số, kể cả khi 20 lá đầu là chức năng. |
| R3 | `REVERSE` với `2` người: người đánh đi lượt tiếp. |
| R4 | `SKIP`/`DRAW2` với `2` người: người đánh đi lượt tiếp. |
| R5 | `DRAW2` không cộng dồn: người bị `+2` không được đánh `+2` đáp. |
| R6 | Đánh lá khác sau khi `draw` → `MUST_PLAY_DRAWN_CARD`. |
| R7 | `pass` khi chưa `draw` → `PASS_WITHOUT_DRAW`. |
| R8 | Cọc Rút rỗng: xáo lại Cọc Bỏ trừ lá trên cùng; lá trên cùng không đổi. |
| R9 | Cả hai cọc rỗng: `draw` không panic, lượt vẫn kết thúc được. |
| R10 | Hết `20s` chưa `draw`: rút `1`, mất lượt. |
| R11 | Hết `20s` đã `draw`: chỉ mất lượt, không rút thêm. |
| R12 | `challenge` đúng: người đánh rút `4`, ván tiếp tục. |
| R13 | `challenge` sai: challenger rút `6`, mất lượt. |
| R14 | Hết `20s` khi chờ thách thức = `accept`, không phạt thêm `1`. |
| R15 | `WILD4` là lá cuối + `accept`: người đánh thắng, không ai rút `4`. |
| R16 | `WILD4` là lá cuối + `challenge` đúng: không ai thắng, ván tiếp tục. |
| R17 | `call_uno` khi còn `2` lá: `CANNOT_CALL_UNO`. Vừa đánh xuống còn `1` lá: hợp lệ. |
| R18 | Bắt UNO hết hạn khi người kế tiếp hành động → `UNO_WINDOW_CLOSED`. |
| R19 | `catch_uno` hai lần: chỉ phạt một lần, lần hai `UNO_ALREADY_CALLED`. |
| R20 | `uno_called` xoá khi tay bài lên lại trên `1`. |
| R21 | Forfeit khi có `3` người: bài vào Cọc Bỏ dưới lá trên cùng, người đó rời vòng. |
| R22 | Forfeit tới người cuối cùng: người cuối thắng pot, kể cả đang disconnected. |
| R23 | `play` đến đúng lúc hết `20s`: đúng một kết quả, không hai. Test bằng channel tuần tự, không bằng sleep. |
| R24 | `seq` tăng đơn điệu, không nhảy cóc, không lặp. |
| R25 | Không payload nào, kể cả log debug, chứa `Deck` hoặc bài người khác. |
