# UNOFA — bộ spec triển khai

Project: Uno Friend Arena (UNOFA).

Nguồn: `../web_game_uno_online.md` (v1, v2, v3) và `../luat_choi_uno_tieu_chuan.md`.
Bộ file trong thư mục này là bản đã chốt để code. Khi mâu thuẫn: spec > v3 > v2 > v1. Hai file nguồn giữ nguyên làm lịch sử.

## Đọc theo thứ tự

| File | Nội dung |
|---|---|
| [01-product.md](./01-product.md) | Phạm vi MVP, actor, trang, luồng người dùng, nội dung pháp lý (nguyên văn) |
| [02-rules-engine.md](./02-rules-engine.md) | Bộ bài, luật, state machine của ván, đồng hồ, UNO, thách thức `+4`, forfeit |
| [03-data-model.md](./03-data-model.md) | DDL Postgres, tiền, sổ giao dịch, `Transfer`/`Seize`, công thức loan, idempotency |
| [04-http-api.md](./04-http-api.md) | Danh sách endpoint, request/response, mã lỗi, rate limit tầng app |
| [05-realtime.md](./05-realtime.md) | Giao thức WebSocket, event, `seq`, snapshot, reconnect, tải dữ liệu |
| [06-frontend.md](./06-frontend.md) | Cấu trúc React, store, i18n, accessibility, chart, asset |
| [07-ops-admin.md](./07-ops-admin.md) | Config, nginx, admin app, job nền, deploy |
| [08-acceptance.md](./08-acceptance.md) | Định nghĩa "ready to code", checklist DoD, ma trận test |

Đọc 00 → 01 → 02 → 03 → 04 → 05 khi làm backend; 06 khi làm frontend; 07 trước khi deploy; 08 trước khi mở PR.

## Phạm vi MVP

Có:

- Đăng ký/đăng nhập bằng username + password, session Redis, một session một tài khoản.
- Ví tiền ảo `U`, chuyển khoản giữa người chơi, lịch sử giao dịch, chart số dư.
- Vay trong game một khoản active, lãi trong hạn và lãi quá hạn, siết nợ ở cap `2 × gốc`.
- Phòng chơi bằng mã 6 ký tự, host, 2–10 người, preset cược `10/25/50/100/500U`.
- Ván UNO 108 lá qua WebSocket, đồng hồ lượt `20s`, gọi/bắt `UNO`, thách thức `+4`.
- Rớt mạng có cửa sổ hồi `90s`, forfeit sau đó, không hoàn tiền.
- Trang admin riêng subdomain: metric request, HTTP code, quản lý tài khoản, cờ cảnh báo.
- Song ngữ `en`/`vi`, WCAG 2.1 A + 2 tiêu chí AA.

Không có ở MVP (xem 01 §Ngoài phạm vi):

- Khôi phục ván sau restart, Friends system, Google OAuth, load balancer, Kafka, green/blue, microservice, devtool detection, chơi tính điểm tới 500.

## Bảng quyết định bổ sung

Các điểm v1/v2/v3 để ngỏ hoặc chỉ nói một nửa. Mỗi điểm dưới đây là chốt của spec, kèm lý do. Không có điểm nào cần hỏi lại trước khi code.

| ID | Quyết định | Lý do |
|---|---|---|
| D1 | `rooms` và `room_members` nằm trong Postgres; lúc startup xoá sạch cả hai, song song với `UPDATE matches ... 'aborted'`. | v3 yêu cầu `CHECK` trên `rooms.stake` nên bảng phải tồn tại. Socket chết hết sau restart nên phòng cũ không còn ai, xoá là đúng trạng thái thật. |
| D2 | Một endpoint WS duy nhất `GET /api/ws`, không có frame `subscribe`. Server route event theo membership đọc từ DB. | Membership đã có trong `room_members`/`match_players`; thêm frame subscribe là thêm một nguồn sự thật thứ hai có thể lệch. |
| D3 | Heartbeat là HTTP `POST /api/session/heartbeat`. Traffic WS không gia hạn session. | Một cơ chế duy nhất, đếm được bằng `INCR` như mọi endpoint khác. Nếu WS cũng gia hạn thì một socket treo vẫn giữ session sống ngoài ý muốn. |
| D4 | Người chơi chủ động rời phòng khi ván đang chạy = `forfeit` ngay, không chờ `90s`. | Người chơi đã nói rõ ý định; bắt chờ `90s` chỉ làm chậm ván của người khác. Cùng một đường xử lý với hết grace. |
| D5 | Đổi username cần nhập lại password hiện tại, giống đổi password. | Session key nằm trong cookie; không có bước này thì kẻ chiếm được cookie đổi được tên hiển thị của nạn nhân. |
| D6 | `GET /api/loans/products` trả gói, kỳ hạn, lãi từ config. | FE không hard-code lãi suất; đổi config không phải sửa và build lại FE. |
| D7 | `matches.stake` lưu lại mức cược của ván; `payout = pot`. Không tính `n × k` lúc trả thưởng. | Pot là nguồn sự thật duy nhất; `n × k` là công thức suy ra và có thể lệch nếu ai đó bị loại ở bước 5 của handshake. |
| D8 | Một binary Go phục vụ cả `uno.example.com` và `admin.example.com`; nginx route theo `Host`. | v3 chốt một binary, chưa tách service. Hai listener trong cùng process là đủ tách cookie và CORS. |
| D9 | Bộ mã lỗi cố định trong 04 §Mã lỗi, dùng cho cả HTTP body và frame `error` của WS. | FE dịch mã lỗi; mã không cố định thì mỗi endpoint lại sinh một chuỗi mới và bản dịch không theo kịp. |
| D10 | Không có endpoint liệt kê phòng. Vào phòng chỉ bằng mã. | Mã phòng là cách mời ở MVP; liệt kê công khai làm lộ mã của phòng người khác. |
| D11 | Kick chỉ hợp lệ khi phòng không có ván đang chạy và không trong khoảng khoá handshake. | Host rời giữa ván không ảnh hưởng ván (v3), nên quyền kick giữa ván cũng không nên tồn tại. |
| D12 | Cọc Rút hết: xáo lại Cọc Bỏ **trừ lá trên cùng**, dùng `crypto/rand`. | Giữ lại lá trên cùng để ván còn lá so sánh. v3 đã chốt luật, spec chỉ chốt RNG. |
| D13 | Cửa sổ bắt `UNO` đóng khi người kế tiếp hành động, khi ván kết thúc, hoặc khi có quyết định `challenge`/`accept`. | v3 chốt "đóng khi người kế tiếp hành động"; `challenge`/`accept` chính là hành động của người đó. |
| D14 | Ván `aborted` giữ nguyên tiền trong `matches.pot` (không đốt, không hoàn). | v3 chốt pot mất khi restart. Đốt là thêm một đường ghi sổ không ai yêu cầu. Câu kiểm tra lệch sổ tính cả pot nên vẫn khớp. |
| D15 | Danh sách ván đã chơi của người chơi có endpoint riêng `GET /api/me/matches`. | Bản miễn trừ §8 cam kết lưu "match history", nên phải có đường đọc ra. |

## Từ điển

| Thuật ngữ | Nghĩa chính xác trong spec |
|---|---|
| `U`, UNOd | Đơn vị tiền trong game. Số nguyên, nhỏ nhất `1U`. Không có số lẻ. |
| `System` | Dòng `users` có `user_id = 0`. Mọi tiền sinh ra từ đây, mọi tiền mất đi về đây. Số dư âm. |
| pot | Tiền cược của một ván, cột `matches.pot`. |
| `Transfer` | Hàm chuyển tiền strict, fail nếu thiếu tiền. |
| `Seize` | Hàm siết nợ, trừ tới sàn `0U`. |
| `lobby_ready` | Cờ sẵn sàng ở phòng chờ, trước khi host bấm `Play`. |
| `ready_check`/`ready_ack` | Handshake tải asset sau khi host bấm `Play`. |
| grace | Cửa sổ `90s` để reconnect giữa ván. |
| `forfeit` | Rời ván vĩnh viễn: mất ghế, bài vào Cọc Bỏ, không hoàn tiền. |
| `seq` | Số thứ tự event trong một ván, tăng đơn điệu, bắt đầu từ 1 ở `match_start`. |
| `server_now` | Timestamp tuyệt đối của server kèm mọi snapshot và event, để client bù lệch đồng hồ. |
| `can_call_uno` | Cờ server tính, cho biết nút `UNO` có hiện không. |
| `can_catch_uno` | `player_id` duy nhất mà người nhận được phép bắt `UNO`, hoặc `null`. |
