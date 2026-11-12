# 08 — Acceptance và thứ tự build

## "Ready to code" nghĩa là gì

Bộ spec đạt chuẩn này khi cả 10 điều dưới đây đúng. Kiểm trước khi mở PR đầu tiên.

| # | Điều kiện | Ở đâu |
|---|---|---|
| 1 | Mọi bảng DB có DDL đầy đủ, mọi cột có kiểu, mọi constraint viết ra | 03 §DDL |
| 2 | Mọi endpoint có path, method, body, response, mã lỗi | 04 |
| 3 | Mọi frame WS có shape và điều kiện gửi | 05 |
| 4 | Mọi luật game có câu trả lời cho: ai làm, khi nào, kết quả, lỗi | 02 |
| 5 | Mọi công thức tiền là integer, có golden test | 03 §Loan |
| 6 | Mọi hành động có idempotency key | 03 §Idempotency key |
| 7 | Mọi trạng thái UI có component và điều kiện render | 06 |
| 8 | Mọi con số magic nằm trong config | 07 §Config |
| 9 | Mọi test bắt buộc có mã (`R#`, `T#`, `W#`, `F#`) | 02, 03, 05, 06 |
| 10 | Không còn câu "cần hỏi lại" nào trong 9 file | — |

Nếu một câu hỏi lúc code không trả lời được từ spec: **sửa spec trước, rồi code.** Không tự quyết trong code.

## Thứ tự build

Mỗi bước là một PR. Không bắt đầu bước N+1 khi bước N chưa pass CI.

| Bước | Việc | Ước lượng | Done khi |
|---|---|---|---|
| 1 | Repo scaffold, config loader, hai listener, `/healthz`, log JSON, Docker compose, CI skeleton | 0.5 ngày | `docker compose up` → `/healthz` `200` |
| 2 | Migration `0001_init.sql` toàn bộ DDL + seed `System` | 0.5 ngày | Up/down/up pass trong CI |
| 3 | `Transfer`/`Seize`, `transactions`, `balance_snapshot`, invariant SQL | 2 ngày | T1–T5, T13 pass |
| 4 | Auth: register, login, session Redis, heartbeat, cookie, logout, terms | 1.5 ngày | Đăng ký → login → heartbeat → logout; `System` không login được (T14, T15) |
| 5 | Rate limit middleware (HTTP) + `http_log` async writer | 1 ngày | 429 có `Retry-After`, dòng `http_log` xuất hiện |
| 6 | Money API: chuyển khoản, lịch sử, chart balance, `/api/me/matches` | 1.5 ngày | T6–T8, T11, T12 pass |
| 7 | Loan: products, borrow, repay, job siết | 2 ngày | T9, T10 pass; golden `505, 530, 557…` |
| 8 | Rooms: tạo, join, leave, ready, stake, kick, play, host transfer | 1.5 ngày | Mã 6 ký tự, ghế 2–10, `ROOM_LOCKED` đúng |
| 9 | Rules engine thuần (không network): deck, vòng lượt, action cards, UNO, challenge, forfeit | 4 ngày | R1–R22, R24, R25 pass. **Đây là bước dài nhất và dày test nhất.** |
| 10 | Goroutine ván + channel tuần tự + timer `20s`/`90s` + verify order | 2 ngày | R23 pass (race-free, `-race`) |
| 11 | WS: upgrade, auth, Origin check, router event, kênh riêng/chung, `seq`, close codes | 2 ngày | W1–W9 pass |
| 12 | Handshake `ready_check`/`ready_ack`, `match_start`, trừ cược, snapshot endpoint | 1.5 ngày | 2 người chơi được một ván đầy đủ qua curl + `websocat` |
| 13 | Reconnect + grace + snapshot resume + `forfeit` | 1 ngày | W6, W7, W8 pass |
| 14 | FE scaffold: router, store, API client, i18n, design tokens, `ChartFrame` | 2 ngày | `/`, `/login`, `/register` chạy thật |
| 15 | FE lobby + room | 1.5 ngày | Tạo/vào phòng, ready, host play |
| 16 | FE table: lá bài SVG, tay bài, timer, nút theo state, dialog thách thức | 3 ngày | Chơi được một ván đầy đủ bằng chuột |
| 17 | FE account + loan | 2 ngày | Chart, filter, vay, trả |
| 18 | Admin app: login, dashboard, metrics, accounts, audit, flags | 2.5 ngày | Sửa số dư có `admin_audit` |
| 19 | Accessibility: bàn phím, ARIA, focus trap, contrast, reduced motion | 1.5 ngày | F4–F7, F9 pass; `axe` sạch |
| 20 | Hardening: nginx config, CSP, backup, restore test, README vận hành | 1 ngày | Deploy thật, chơi được từ ngoài |

Tổng: khoảng **36 ngày** một người. Bước 9–13 chiếm gần một nửa; đừng cắt test ở đó.

Đường găng: **3 → 4 → 9 → 10 → 11 → 12**. Kế hoạch nào cũng phải qua bước 9.

## Definition of done cho từng PR

- [ ] Test của bước pass, chạy `-race` với Go.
- [ ] Invariant tiền vẫn `0` sau suite tích hợp.
- [ ] Không có `TODO` không kèm `shortcut:` comment.
- [ ] Mã lỗi mới đã có trong 04 §Mã lỗi **và** `locales/{en,vi}.json`.
- [ ] Migration mới up/down/up được.
- [ ] PR description nói rõ spec section nào được implement.

## Ma trận test end-to-end

Kịch bản chạy tay trước khi coi MVP là xong. Mỗi dòng một lần chạy, ghi pass/fail.

| # | Kịch bản | Kỳ vọng |
|---|---|---|
| E1 | Đăng ký → số dư `200U` → lịch sử có `SIGNUP_BONUS` | Chart có 1 điểm |
| E2 | Đăng ký 2 tài khoản, chuyển `50U` | Bên gửi `150U`, bên nhận `250U`, hai chiều lịch sử đúng |
| E3 | Chuyển `1001U` trong 24h | Request thứ 2 fail `TRANSFER_LIMIT` |
| E4 | Vay `500U/24h`, trả ngay | Trả `505U`, `status = repaid` |
| E5 | Vay `500U/24h`, chờ siết | Job trừ về sàn, `status = seized`, cờ `LOAN_SEIZED` |
| E6 | Vay rồi thử chuyển tiền khi quá hạn | `LOAN_OVERDUE` |
| E7 | Tạo phòng `50U`, 2 người ready, host play | Cả hai bị trừ `50U`, pot `100U` |
| E8 | Chơi hết một ván | Người thắng `+100U`, `match_end` có `pot: 100` |
| E9 | Giữa ván, một người F5 | Snapshot đầy đủ, chơi tiếp, không mất ghế |
| E10 | Giữa ván, đóng tab, chờ `95s` | `forfeit`, bài vào Cọc Bỏ, người kia thắng pot |
| E11 | Đánh `WILD4`, người sau bấm `Thách thức` khi tay người đánh có màu khớp | Người đánh rút `4`, ván tiếp tục |
| E12 | Còn 2 lá, không bấm UNO, người khác bấm `Bắt UNO` | Rút `2` |
| E13 | Đổi password | Session cũ chết, dialog login lại hiện |
| E14 | Admin cộng `500U` cho user | Số dư đổi, `admin_audit` có dòng, chart có label `SYSTEM ADD 500` |
| E15 | Admin xoá tài khoản có số dư | Username thành `deleted_{id}`, invariant vẫn `0` |
| E16 | Restart app giữa ván | Ván `aborted`, cược ở lại pot, invariant `0`, cờ `SERVER_RESTARTED` |
| E17 | Chơi trọn một ván chỉ bằng bàn phím | Được, focus luôn thấy |
| E18 | Đổi sang `vi` | Mọi chữ đổi, tiền `1.000U` |
| E19 | Screen reader: lượt mới, lá mới, nút UNO hiện | Đều được thông báo |
| E20 | Mở `admin.example.com` với cookie game | `401`, không vào được |

## Rủi ro đã biết

Ghi lại để người code không tưởng là bug.

| Rủi ro | Ảnh hưởng | Giảm thiểu |
|---|---|---|
| Restart mất ván, mất cược | Người chơi mất `U` | Đã ghi trong bản miễn trừ §6. Không sửa ở MVP. |
| `429` từ nginx không vào `http_log` | Chart admin thiếu một phần lượt bị chặn | Ghi trong README. |
| Redis chết → mọi người login lại | Downtime ngắn | Redis không giữ tiền, không mất dữ liệu. |
| Fixed window cho phép `2×` ở ranh giới | Bypass rate limit một chút | `shortcut:` comment + chuyển sliding khi cần. |
| Ghi `http_log` đồng bộ | Chậm khi tải cao | Batch + drop counter; xem lại ở `1000` người online. |
| Một instance Go | Không scale ngang | Đủ cho `100` người. Chuyển khi cần. |
| Không chặn được mua bán `U` thật | Vi phạm §9 | Hạn mức + đăng ký giới hạn + admin đóng tài khoản tay. |

## Backlog (ngoài MVP)

| Việc | Ước lượng |
|---|---|
| Khôi phục ván sau restart | 2–3 ngày |
| Friends system (add, delete, invite, status) | 2–3 ngày |
| Sliding window rate limit | 0.5 ngày |
| Scale ngang: state ván ra Redis + lock phân tán | 5 ngày |
| Green/blue deploy | 1 ngày |
| Spectator mode | 2 ngày |

## Định nghĩa "MVP xong"

Cả 5 điều:

1. E1–E20 pass trên môi trường deploy thật.
2. Invariant tiền `0` sau 24 giờ chạy liên tục không có `MONEY_INVARIANT`.
3. `go test -race ./...` pass, có test cho mọi `R#`, `T#`, `W#`.
4. `axe` sạch trên 5 route chính.
5. README có: cách chạy local, cách deploy, cách restore backup, bảng mã lỗi, và ba rủi ro ở trên.

## Còn thiếu (không chặn code)

Ba điểm dưới đây spec đã chọn mặc định. Đổi được sau mà không sửa schema.

| Điểm | Mặc định đã chọn | Đổi thì ảnh hưởng gì |
|---|---|---|
| Bao nhiêu admin | `2` (root + risk) trong config | Chỉ config |
| Stake preset cho bàn mới | cả `5` mức `10/25/50/100/500` | Chỉ config, miễn là tập con của `CHECK` |
| Có hiện `U` của người khác trong phòng chờ không | Có (cần để biết ai vào được bàn `500U`) | Bỏ được nếu thấy lộ thông tin |
