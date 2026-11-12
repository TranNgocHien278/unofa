# 01 — Sản phẩm

## Actor

| Actor | Quyền |
|---|---|
| Khách | Đăng ký, đăng nhập, đọc `/about`. Không thấy gì khác. |
| Người chơi | Mọi thứ của khách, cộng: ví, chuyển khoản, vay, tạo/vào phòng, chơi ván, đổi username/password/ngôn ngữ. |
| Host | Người chơi trong một phòng, có thêm: đổi mức cược, kick người, bấm `Play`. Quyền host chỉ có hiệu lực **ngoài ván**. |
| Admin | Đăng nhập riêng ở `admin.example.com`. Xem metric, xem/sửa số dư, xoá tài khoản, xem và xoá cờ cảnh báo. Không nằm trong bảng `users`. |

## Trang

Hai origin tách biệt: không share cookie, không share CORS.

### `uno.example.com`

| Route | Nội dung |
|---|---|
| `/` | Landing. Chưa đăng nhập: form đăng nhập + link đăng ký. Đã đăng nhập: chuyển `/lobby`. |
| `/register` | Username, password, xác nhận password, checkbox đồng ý điều khoản, link `/about`. |
| `/login` | Username (điền sẵn từ cookie nếu có), password. |
| `/lobby` | Ô nhập mã phòng, nút tạo phòng + chọn mức cược. **Không có danh sách phòng** (D10). |
| `/room/:code` | Phòng chờ: danh sách ghế, cờ sẵn sàng, mức cược, nút `Play` (host), rời phòng, kick (host). |
| `/table/:matchId` | Bàn chơi. 403 nếu người dùng không phải người chơi của ván. |
| `/account` | Số dư, chart số dư, lịch sử giao dịch có lọc, đổi username, đổi password, chọn ngôn ngữ. |
| `/loan` | Gói vay, kỳ hạn, lãi, khoản đang active, nút trả nợ. |
| `/play` | Hướng dẫn luật + phím tắt. |
| `/about` | Mục tiêu dự án, tóm tắt luật, miễn trừ trách nhiệm nguyên văn. |

Footer mọi trang: `UNOFA is a student project. U has no real-world value.` + link `/about`.

### `admin.example.com`

| Route | Nội dung |
|---|---|
| `/login` | Username + password admin. |
| `/` | Dashboard: 4 cờ cảnh báo, tổng tài khoản, tổng `U` lưu hành, số ván đang chạy. |
| `/metrics` | Chart request/time theo endpoint; chart HTTP code. Bộ lọc thời gian. |
| `/accounts` | Bảng tài khoản: username, số dư, trạng thái khoá login, ngày tạo. Tìm theo username. |
| `/accounts/:id` | Chi tiết: số dư, khoản vay, lịch sử giao dịch, nút cộng/trừ `U`, nút xoá tài khoản. |
| `/audit` | `admin_audit` gần nhất, mới nhất trước. |

## Luồng người dùng

### Đăng ký

1. Nhập username, password, xác nhận password, tick điều khoản.
2. Server kiểm: username `3`–`20` ký tự `[a-z0-9_]`, không phải `system`, không bắt đầu `deleted_`; password `8`–`72` byte; username chưa tồn tại.
3. Tạo user, ghi `terms_version`, `terms_accepted_at`, `Transfer` `SIGNUP_BONUS` `200U` từ `System`, ghi `balance_snapshot`.
4. Cấp session, set cookie, chuyển `/lobby`.

### Đăng nhập

1. Username + password. Kiểm rate limit theo IP và theo username (04 §Rate limit).
2. `bcrypt.CompareHashAndPassword`.
3. Nếu `users.terms_version < CURRENT_TERMS_VERSION`: trả `TERMS_OUTDATED`, FE bắt tick lại, gọi lại login kèm cờ.
4. `DEL` session cũ, sinh session key mới 32 byte, set cookie, đóng WS của session cũ.

### Chơi một ván

```
/lobby → tạo phòng hoặc nhập mã
      → /room/:code: chọn ghế, bấm Sẵn sàng (lobby_ready)
      → host bấm Play  [cần >=2 lobby_ready, host cũng phải ready]
      → ready_check/ready_ack (5s, tải asset)
      → trừ cược vào pot, chia bài, match_start
      → /table/:matchId
      → match_end: panel người thắng, pot đã về ví
      → về /room/:code, mọi lobby_ready bị xoá
```

### Vay → trả / bị siết

```
/loan → chọn gói + kỳ hạn → Transfer LOAN_DISBURSE
      → trước hạn: trả owed_in_term, status=repaid
      → quá hạn: owed dồn 5%/24h, cap 2×gốc
      → chạm cap: job mỗi giờ Seize, trừ tới sàn 0U, status=seized
```

### Xoá tài khoản

Người chơi liên hệ admin (email trong `/about`). Admin chạy endpoint xoá. Xem 03 §Xoá tài khoản.

## Nội dung pháp lý

### Vị trí

- `/about`: bản đầy đủ, cả `en` và `vi`, chọn theo `users.locale`.
- Footer mọi trang: một dòng tóm tắt.
- `/register`: checkbox bắt buộc, không tick thì nút submit disabled.

### Lưu trữ

`users.terms_version INT NOT NULL`, `users.terms_accepted_at TIMESTAMPTZ NOT NULL`.
`CURRENT_TERMS_VERSION` là hằng số trong code. Tăng số là bắt mọi người tick lại ở lần login sau.
`CURRENT_TERMS_VERSION = 1` ở lần deploy đầu.

### Thông báo trước khi cược

Nút `Play` (host) và màn hình phòng chờ hiện cố định:

> Tiền cược trừ ngay khi ván bắt đầu. Rớt mạng không được hoàn.

Không có hộp thoại xác nhận riêng: dòng chữ nằm cạnh nút, luôn thấy trước khi bấm.

### Bản `en` (nguyên văn, dùng nguyên trên `/about`)

> **Disclaimer**
>
> **1. About this project.** UNOFA (Uno Friend Arena) is a non-commercial student project built to practise system design, web development, databases, CI/CD and security. It is not a business and does not make money.
>
> **2. Not affiliated with UNO or Mattel.** UNO is a registered trademark of Mattel, Inc. UNOFA is not affiliated with, endorsed by, or sponsored by Mattel. UNOFA uses only the publicly known rules of the game, with its own card design and artwork.
>
> **3. U is not money.** UNOd (U) is a play currency that exists only inside UNOFA.
>
> - U cannot be bought. There is no deposit and no payment of any kind.
> - U cannot be sold, withdrawn, or exchanged for real money, goods, or services.
> - U has no value outside UNOFA.
> - Buying, selling, or trading U or UNOFA accounts for anything of real value is forbidden. Accounts that do so will be closed.
>
> **4. Not gambling.** Because U cannot be bought or cashed out, betting U is part of the game, not gambling. Nothing of real value can be won or lost.
>
> **5. Loans are a game mechanic.** In-game loans, interest and seizure apply only to U. They are not real credit and create no real debt.
>
> **6. U can be lost.** U may be lost through normal play, through loan seizure, through bugs, or when a match is cancelled after a server restart. Bets are deducted when a match starts and are not refunded on disconnection. The project may reset balances, accounts or all data at any time, with or without notice. No compensation is given for lost U.
>
> **7. No warranty.** UNOFA is provided "as is". It may be unavailable, slow, or contain bugs. Use it at your own risk.
>
> **8. Data we store.** Username, a hashed password (never the password itself), language setting, your balance and transaction history, match history, and request logs including IP address and time. Request logs are deleted after 60 days. This data is used only to run and secure UNOFA. It is not sold or shared. To delete your account and data, contact the project owner.
>
> **9. Fair play.** Do not use scripts, bots, or exploits, and do not attack the service. Security testing is allowed only with written permission from the project owner. Accounts that break these rules may be closed and their U removed.
>
> **10. Contact.** unofa.contact@gmail.com

### Bản `vi` (nguyên văn)

> **Miễn trừ trách nhiệm**
>
> **1. Về dự án.** UNOFA (Uno Friend Arena) là dự án sinh viên phi thương mại, làm ra để luyện system design, phát triển web, cơ sở dữ liệu, CI/CD và bảo mật. Đây không phải doanh nghiệp và không thu tiền.
>
> **2. Không liên kết với UNO hay Mattel.** UNO là nhãn hiệu đã đăng ký của Mattel, Inc. UNOFA không liên kết, không được Mattel bảo trợ hay tài trợ. UNOFA chỉ dùng luật chơi phổ biến công khai, với thiết kế lá bài và hình ảnh riêng.
>
> **3. U không phải tiền.** UNOd (U) là tiền chơi, chỉ tồn tại trong UNOFA.
>
> - Không mua được U. Không có nạp tiền hay thanh toán dưới bất kỳ hình thức nào.
> - Không bán, rút, hay đổi U ra tiền thật, hàng hoá hoặc dịch vụ.
> - U không có giá trị bên ngoài UNOFA.
> - Cấm mua, bán, trao đổi U hay tài khoản UNOFA lấy bất kỳ thứ gì có giá trị thật. Tài khoản vi phạm sẽ bị đóng.
>
> **4. Không phải cờ bạc.** Vì U không mua được và không rút ra được, đặt cược bằng U là một phần của trò chơi, không phải cờ bạc. Không thể thắng hay thua thứ gì có giá trị thật.
>
> **5. Khoản vay là cơ chế game.** Khoản vay, lãi suất và siết nợ trong game chỉ áp dụng cho U. Đây không phải tín dụng thật và không tạo ra khoản nợ thật.
>
> **6. U có thể mất.** U có thể mất do chơi thua, do bị siết nợ, do lỗi phần mềm, hoặc khi ván bị huỷ sau khi server khởi động lại. Tiền cược bị trừ lúc ván bắt đầu và không được hoàn khi rớt mạng. Dự án có thể reset số dư, tài khoản hoặc toàn bộ dữ liệu bất kỳ lúc nào, có hoặc không báo trước. Không bồi thường U đã mất.
>
> **7. Không bảo đảm.** UNOFA được cung cấp "nguyên trạng". Dịch vụ có thể ngừng, chậm hoặc có lỗi. Bạn tự chịu rủi ro khi sử dụng.
>
> **8. Dữ liệu được lưu.** Username, password đã hash (không bao giờ lưu password gốc), cài đặt ngôn ngữ, số dư và lịch sử giao dịch, lịch sử ván chơi, và log request gồm địa chỉ IP và thời gian. Log request bị xoá sau 60 ngày. Dữ liệu chỉ dùng để vận hành và bảo vệ UNOFA, không bán và không chia sẻ. Muốn xoá tài khoản và dữ liệu, liên hệ chủ dự án.
>
> **9. Chơi công bằng.** Không dùng script, bot hay lỗi hệ thống để trục lợi, không tấn công dịch vụ. Kiểm thử bảo mật chỉ được phép khi có đồng ý bằng văn bản của chủ dự án. Tài khoản vi phạm có thể bị đóng và mất U.
>
> **10. Liên hệ.** unofa.contact@gmail.com

### Ràng buộc kỹ thuật từ bản miễn trừ

| Mục | Việc code phải làm |
|---|---|
| §8 | Có đường xoá tài khoản + dữ liệu (admin endpoint). Xem 03 §Xoá tài khoản. |
| §8 | `http_log` xoá sau `60` ngày (job hằng ngày). |
| §6 | Ván mất sau restart, không hoàn tiền. Đúng thiết kế MVP. |
| §9 | Không chặn được chuyển `U` lấy tiền thật; hạn mức `1000U/24h` + `10` đăng ký/giờ/IP chỉ làm chậm. Chấp nhận, ghi rõ trong README. |

Lưu ý: bản này do người không phải luật sư viết, cho dự án học tập phi thương mại. Mở công khai rộng, nhận tiền hoặc quảng cáo: cần người có chuyên môn pháp lý đọc lại.

## Ngoài phạm vi MVP

Không cắt, chỉ dời sau (xem 08 §Backlog):

| Hạng mục | Ghi chú |
|---|---|
| Khôi phục ván sau restart | ~2–3 ngày. Lưu state + `turn_deadline` mỗi lượt. |
| Friends system | ~2–3 ngày. MVP dùng mã phòng. |
| Google OAuth | Bỏ hẳn ở v3, không dời. |
| Chơi tính điểm tới `500` | Không làm. Mỗi ván một lần cược. |
| Danh sách phòng công khai | Không làm (D10). |
| Load balancer, Kafka, green/blue, microservice | Không làm. Một binary Go + một Postgres + một Redis. |
| Devtool detection, block inspector | Bỏ. Không chặn được gì thật, phá accessibility. |
| Client prediction cho bài ẩn | Không làm. Chỉ optimistic UI cho hành động của mình. |
