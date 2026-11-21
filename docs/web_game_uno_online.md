# Web game UNO online

Project Fullname: Uno Friend Arena
Project Shortname: UNOFA

## v1

- Tài khoản: lịch sử, static, balance
- Phòng chơi game online kết nối trực tiếp chơi trực tiếp. sync time on time
- Hệ thống tiền ảo để đặt cược, thua tự trả tiền, thắng tiền tự về tài khoảng. Có thể chuyển tiền, giao dịch ngoài ván chơi. Hệ thống loan, lãi suất, kỳ hạn thanh toán.
  - Transaction
  - Reliable, secure, safety: giao dịch đang thực hiện thì server off. client mất mạng, không thể kết nối. xác thực nghiêm ngặt
- web platform
- sub domain cho admin (admin.example.com). monitor, static, metric của toàn bộ hệ thống. tài khoảng đặc biệt chỉ có khi cấp trong config lúc startup.
- game ui sync, prediction (v3: không prediction bài ẩn, chỉ optimistic UI cho hành động của mình, xem "Dữ liệu gửi xuống client")

* accessibility
* timeout
* req limit
* cookie lưu email, tên đăng nhập (v3: không có email, cookie chỉ lưu username để điền sẵn form). password bắt nhập lại. mỗi session có ttl, gần hết thì kiểm tra client còn connect thì gia hạn, không thì revoke. nếu còn session thì không cần đăng nhập lại.
* dev tool detection. block inspector (v3: bỏ, xem "Đã bỏ ở v3")
* redis, nginx, load balancer, tìm hiểu kafka (v3: MVP chỉ redis + nginx, chưa load balancer, chưa kafka)
* be: mono first -> micro final. golang (v3: MVP một binary Go)
* deploy automatic. green blue server (v3: chưa làm ở MVP)

## v2

- Pages
  - Landing
    - Giới thiệu dịch vụ
    - Hướng dẫn sử dụng ngắn, kịch bản đẹp nhất
  - About
    - Giải thích mục đích tồn tại
      - Dự án nhỏ của sinh viên
      - Tập luyện những kỹ năng chuyên môn
        - System design
          - FE
          - BE
        - DB
        - CI/CD
          - Green blue server
        - Cyber security
          - Red blue team
    - Miễn trừ trách nhiệm
  - Register/Login
    - Register
      - Username
        - Username
        - Password
      - Google (v3: bỏ)
    - Login
      - Username
      - Google (v3: bỏ)
      - Session
        - TTL 60s (v3: 15p)
        - Heartbeat 10s gia hạn session (v3: 1p)
        - Session key lưu trong cookie
        - Login
          - Session alive
            - Gia hạn
            - Tiếp tục dùng key hiện tại
          - Session expire
            - Yêu cầu điền lại Password
            - Server cung cấp session key mới
        - Secure
          - Session key khác auth key
          - Mỗi tài khoảng chỉ cho phép 1 session key tồn tại cùng 1 thời điểm
          - Session key hợp lệ lưu trong redis expire thì giải phóng
  - Admin
    - Login
      - Tài khoản tạo từ config lúc startup (v3 thay cho "từ phía DB", xem mục "Admin account")
      - Username
      - Password
    - Metric
      - Req/time
        - Stacked Bar Chart
        - Label dữ liệu là req với từng endpoint
        - Khoảng thời gian
          - Today (default)
          - 24h
          - 3 days
          - 7 days
          - 30 days
          - 60 days
      - Graph log HTTP code
        - 100% Stacked Area Chart
        - Stacked Bar Chart
        - Label dữ liệu các HTTP code
        - Khoảng thời gian
          - Today (default)
          - 24h
          - 3 days
          - 7 days
          - 30 days
          - 60 days
    - Notify
      - Server status
        - Dựa vào log trong
    - Bảng tài khoản
      - Hiển thị các tài khoản trong BD
      - Add/Delete thủ công (v3: Delete là ẩn danh hoá dòng `users`, không xoá dòng, xem "Miễn trừ trách nhiệm")
      - Điều chiển số dư
        - Thông qua endpoint
        - Có hiển thị trong balance metric với label riêng
          - SYSTEM {ADD/GET} {amount}
- Core feat
  - Login/Register
  - Comunity
    - Friends system (v3: làm sau MVP, MVP dùng mã phòng)
      - Add
      - Delete
      - Invite
      - Status
        - Incativo
        - Active
  - Account
    - Balance
      - Số dư ví
      - Lịch sử
        - Chuyển
        - Nhận
        - Chi tiêu
      - Loan
      - Graph metric
    - Username
      - Đổi tên
    - Password
      - Đổi password
      - Yêu cầu nhập lại password hiện tại để đổi password mới
  - Game mechanic
    - Bài UNO
      - Reward
        - Mỗi bàn có config tiền cược
        - Mỗi người chơi đặt tiền cược khi bắt đầu trận
          - Giá trị giải thưởng là n\*k
            - n là số người chơi
            - k là tiền cược
      - Điều kiện dừng
        - Trong trận
          - Số lượng người chơi còn lại <= 1
            - Nếu còn 1
              - Dừng ván
              - Giải thưởng cho người còn lại
            - Nếu còn <= 0
              - Dừng ván
              - Giải thưởng không cho ai (v3: đốt vào `user_id = 0`, reason `BURN`)
        - Sau trận
          - All
            - Panel người chiến thắng
          - Host
            - Menu số người sẵn sàng
            - Có thể bắt đầu ván mới khi Số người sẵn sàng >= 2
          - Client
            - Số dư
              - > = tiền cược
                - Cho phép sẵn sàng
              - < tiền cược
                - Không cho sẵn sàng
                - Hiển thị nút Loan
                  - Cho phép loan vô hạn (v3: bỏ, chỉ 1 khoản active, xem mục "Loan")
                  - Lưu lịch sử
            - Sẵn sàng
              - Chỉ cho phép sẵn sàng khi đủ tiền cược
      - Luật chơi
        - UNO standard (v3: `luat_choi_uno_tieu_chuan.md` + mục "Luật chơi")
        - Điều kiện kết thúc
          - 1 người thắng - hết bài (v3: lá cuối là `+4` thì phải qua thách thức mới thắng, xem "Kết thúc ván")
        - Tối đa 10 người chơi cùng lúc
    - Dùng WebSocket/SSE (v3: chỉ WebSocket)

## v3 — quyết định đã chốt (2026-11-11) — Làm file này

Mục này chốt các điểm mở từ v1/v2. Nếu v1/v2 mâu thuẫn với v3, v3 thắng.
Chỉ mục "Còn thiếu" ở cuối là chưa chốt.

### Stack

- FE: React + TypeScript.
  - Animation: Framer Motion. Bài ra/vào tay, layout animation khi tay bài sắp lại.
  - Graph: Recharts. Đủ cho stacked bar, 100% stacked area (admin metric) và graph số dư (account). Chi tiết ở mục "Graph và metric".
  - State: Zustand. Store nhận event từ WS, không Redux.
  - Realtime: `WebSocket` gốc của trình duyệt + một wrapper tự reconnect. Không dùng socket.io vì BE là Go.
- BE: Go, mono repo, một binary. Chưa tách microservice.
- DB: Postgres. Nguồn sự thật duy nhất cho tiền, tài khoản, loan, transaction, audit.
- State ván: nằm trong memory của goroutine xử lý ván đó (xem "Dữ liệu gửi xuống client"). Không ghi xuống Redis, vì MVP không khôi phục ván sau restart nên bản ghi đó không có ai đọc.
- Session và bộ đếm rate limit: Redis. Redis không bao giờ là nguồn sự thật cho tiền.
- Reverse proxy: nginx. Chưa dùng kafka, chưa load balancer, chưa green/blue ở v3.
- RNG chia bài: `crypto/rand` + Fisher–Yates. Không seed tái sử dụng, không `math/rand`.
- Bộ bài: tự vẽ, tự đặt tên/màu/chữ. Không dùng art, logo, font của UNO — đó là thương hiệu đã đăng ký. Luật chơi thì dùng được.

### Tiền

- Kiểu lưu: `int64` ở mọi tầng. Postgres `BIGINT`, Go `int64`. Cấm float cho tiền.
- **Sàn cứng 0U.** Số dư người chơi không bao giờ âm. Enforce ở 2 lớp:
  - DB: `CHECK (user_id = 0 OR balance >= 0)` trên `users.balance`. Chỉ tài khoản `System` (`user_id = 0`) được âm. Vượt sàn là transaction fail, không phải bug âm thầm.
  - Go: mọi phép trừ đi qua `Transfer`/`Seize`, kiểm tra `amount > balance` **trước** khi trừ. Bỏ qua kiểm tra này chỉ khi `from = 0`.
  - Không dùng `uint64`. Lý do: Postgres không có `BIGINT UNSIGNED`, và ở Go `uint64` trừ quá tay không panic mà wrap thành số gần `2^64` — chia pot sai một lần là nhân tiền lên thay vì mất tiền. `int64` + `CHECK` cho cùng một đảm bảo (không âm) mà lỗi thì fail ồn.
- Đơn vị: `UNOd`, ký hiệu `U`. 1U là đơn vị nhỏ nhất, không có số lẻ.
- Số dư mặc định tài khoản mới: `200U`.
- JSON trả về FE là number; an toàn tới 2^53 nên đủ cho mọi giá trị của game.
- Client không bao giờ cộng tiền, chỉ hiển thị số server gửi.
- Pot là cột DB, không giữ trong memory: `matches.pot BIGINT NOT NULL DEFAULT 0 CHECK (pot >= 0)`. Không tách bảng `pots` riêng, vì mỗi ván đúng một pot.
- Một "tài khoản" trong `Transfer`/`Seize` là một trong hai thứ: một dòng `users` hoặc pot của một dòng `matches`.
- Cược trừ **ngay khi ván bắt đầu**, tức là ngay sau khi mọi người qua handshake sẵn sàng (xem "Bắt đầu ván"), không phải khi vào phòng: một `Transfer` từ mỗi người chơi vào pot, label `BET`. Tiền đã rời tài khoản người chơi từ lúc đó, nên mọi trường hợp rớt mạng về sau đều không có gì để hoàn.
- Hai hàm chuyển tiền, không thêm hàm thứ ba:
  - `Transfer(ctx, tx, from, to, amount, reason, idempotency_key)` — strict. `from` không đủ tiền thì fail cả transaction. Dùng cho mọi chuyển tiền trừ siết nợ.
  - `Seize(ctx, tx, from, to, owed, reason, idempotency_key)` — siết nợ. Trừ `min(owed, balance)`, sàn 0U, phần nợ dư ra không đòi được thì ghi vào `loans.forgiven_at_seize` cho audit rồi đóng khoản vay. Không bao giờ tạo số dư âm. Số dư đang là `0U` thì không ghi dòng `transactions` nào (vì `amount >= 1`), chỉ đóng khoản vay.
  - Cả hai: một Postgres transaction duy nhất, `SELECT ... FOR UPDATE` dòng của cả hai bên, lock theo thứ tự cố định (`users` theo `user_id` tăng dần, rồi tới `matches`) để tránh deadlock, `INSERT INTO transactions` với `UNIQUE(idempotency_key)`.
  - Server chết giữa transaction thì Postgres rollback, số dư không đổi.
- Sổ giao dịch, một dòng cho mỗi lần chuyển:
  `transactions(id, from_user_id, from_match_id, to_user_id, to_match_id, amount CHECK (amount >= 1), reason, idempotency_key UNIQUE, created_at)`. Mỗi bên đúng một trong hai cột `_user_id`/`_match_id` có giá trị.
  - "Chuyển" và "Nhận" trong lịch sử của v2 tính theo chiều của dòng, không lưu hai dòng.
- Danh sách `reason` đầy đủ, dùng chung cho DB, API và FE:
  | reason | từ | tới |
  |---|---|---|
  | `SIGNUP_BONUS` | `0` | user mới (`200U`) |
  | `BET` | user | pot |
  | `PAYOUT` | pot | người thắng |
  | `BURN` | pot | `0` |
  | `TRANSFER` | user | user |
  | `LOAN_DISBURSE` | `0` | user |
  | `LOAN_REPAY` | user | `0` |
  | `SEIZE` | user | `0` |
  | `SYSTEM_ADD` | `0` | user do admin chọn |
  | `SYSTEM_GET` | user do admin chọn | `0` |
  | `ACCOUNT_DELETE` | user | `0` (xoá tài khoản) |
- Cột "`0`" trong bảng trên là tài khoản hệ thống: một dòng `users` thật, `user_id = 0`, username `System`.
  - Mọi tiền sinh ra đi từ `System`, mọi tiền mất đi về `System`. Số dư của nó âm, và độ âm bằng tổng số tiền đang lưu hành.
  - Tạo bằng migration, không qua đăng ký. `password_hash = NULL`, nên login vào `System` luôn thất bại.
  - Username `system` bị chặn khi đăng ký, so sánh không phân biệt hoa thường.
  - Hiện trong lịch sử giao dịch của người chơi với tên `System`, ví dụ "System → you: 200U (SIGNUP_BONUS)".
- Kiểm tra tiền có lệch sổ không (invariant), một câu SQL, tính cả `System`:
  `SELECT (SELECT SUM(balance) FROM users) + (SELECT COALESCE(SUM(pot), 0) FROM matches) = 0`
  - Khác 0 tức là có tiền sinh ra hoặc mất đi ngoài sổ.
  - Chạy trong test sau mỗi test case có chuyển tiền, và trong job mỗi giờ. Lệch thì báo lên trang admin.
- Idempotency key: `signup:{user_id}`, `match:{match_id}:bet:{user_id}`, `match:{match_id}:payout`, `match:{match_id}:burn`, `transfer:{client_uuid}`, `loan:{loan_id}:disburse`, `loan:{loan_id}:repay`, `loan:{loan_id}:seize`, `admin:{request_id}`, `delete:{user_id}`.

### Chuyển tiền giữa người chơi

- Phí `0U`. Min `1U`. Max `1000U/ngày` cho mỗi người gửi.
- "Ngày" là cửa sổ 24h trượt, không phải ngày lịch: `SELECT COALESCE(SUM(amount), 0) FROM transactions WHERE reason = 'TRANSFER' AND from_user_id = $1 AND created_at > now() - interval '24 hours'`. Cửa sổ trượt khó lách hơn (không reset lúc nửa đêm để gửi gấp đôi) và chỉ cần một câu SUM, không cần bảng đếm theo ngày.
- Chặn ở DB chứ không chỉ ở code: `CHECK (amount >= 1)` và limit kiểm trong cùng transaction với `Transfer`, sau khi đã lock dòng người gửi.
- Không cho chuyển tiền khi người gửi có khoản vay quá hạn (`status = 'active' AND now() > due_at`). Nếu cho thì vay 2000U rồi chuyển đi 1000U/ngày là cách trốn siết nợ hợp lệ.

### Rớt mạng, hết giờ, server chết

Hai đồng hồ độc lập, không gộp làm một:

- **Session TTL 15p** (auth). Hết TTL thì phải login lại bằng password.
- **Reconnect grace 90s** (ván). Gắn với dòng `match_players`, không gắn với session. Còn grace thì còn ghế trong ván và còn cơ hội thắng pot.

Xử lý khi socket đóng giữa ván:

1. Đánh dấu `match_players.status = disconnected`, ghi `disconnected_at`. Tay bài, ghế, pot giữ nguyên ở server.
2. Trong 90s, client gọi `GET /api/matches/{match_id}/state` bằng session key hiện có. Server trả snapshot riêng tư đầy đủ: tay bài của mình, số lá của từng người khác, lá đỉnh chồng bỏ, số lá còn trong chồng rút, lượt hiện tại, `turn_deadline`, pot, `seq` mới nhất. Client không phải replay event cũ, nên F5 giữa ván vẫn chơi tiếp được.
3. Session đã hết TTL nhưng grace còn: client login lại bằng password, rồi gọi lại endpoint state và vào ván bình thường. Đây là lý do hai đồng hồ phải tách nhau.
4. Hết 90s không trở lại thì `forfeit`: người đó rời khỏi vòng lượt, bài trên tay đưa vào Cọc Bỏ (dưới lá trên cùng) để lần xáo lại Cọc Rút sau này dùng được.
5. Nhiều người hết grace cùng lúc: xử lý lần lượt theo `disconnected_at` tăng dần. Khi chỉ còn 1 người thì người đó thắng pot ngay, **kể cả khi người đó cũng đang disconnected**. Vì vậy trường hợp `<= 0` người thực tế không xảy ra; luật `BURN` bên dưới giữ lại làm chốt chặn.

**Không có auto-play.** Server không bao giờ đánh bài thay người chơi.

- Đồng hồ lượt `20s` vẫn chạy khi người chơi đang disconnected.
- Hết `20s` mà người chơi chưa hành động: **phạt rút 1 lá và mất lượt.** Áp dụng như nhau cho người đang online và người đang disconnected.
  - Đây là luật phạt cố định, không phải auto-play: server không chọn lá nào để đánh thay.
  - Đã rút bài trong lượt đó rồi mới hết giờ: chỉ mất lượt, không rút thêm.
  - Đang chờ quyết định thách thức `+4`: xem mục "Luật chơi".
  - Người disconnected hết grace sau `90s`, tức bị phạt tối đa khoảng 4 lượt trước khi bị `forfeit`.

Tiền khi rớt mạng hoặc forfeit (đã chốt): **không hoàn trong mọi trường hợp.**

- Tiền cược đã bị trừ bằng `Transfer` label `BET` ngay lúc ván bắt đầu, nên về mặt sổ sách nó đã rời tài khoản người chơi. Không còn gì để trả lại.
- Tiền nằm lại pot và trả cho người thắng, kể cả khi ván chưa đánh lá nào.
- Lý do giữ luật này: nếu hoàn tiền thì ngắt mạng lúc sắp thua thành một cách undo miễn phí, và server không phân biệt được rớt mạng thật với cố ý. Luật một chiều nên không thể lách.
- Hệ quả phải hiển thị cho người chơi **trước khi** ván bắt đầu, ngay ở nút bắt đầu: "Tiền cược trừ ngay. Rớt mạng không được hoàn." Nói sau thì người chơi mất tiền mà không biết luật.
- Vì không có đường hoàn tiền, `refund` không tồn tại ở v3. Idempotency key `match:{id}:refund` đã bỏ.

Pot khi còn `<= 0` người (đã chốt): **mất hết, không cho ai, không hoàn lại.**

- Tiền cược ở lại pot và bị đốt: `Transfer` từ pot vào `System`, reason `BURN`, key `match:{match_id}:burn`.
- Không hoàn lại, vì hoàn lại thì ngắt mạng hàng loạt trở thành cách lấy lại tiền cược khi ván đang bất lợi.
- Đốt phải ghi sổ, không phải xóa dòng, để kiểm tra lệch sổ ở mục "Tiền" vẫn đúng.

Server restart (MVP): **như mới hoàn toàn.**

- Ván đang chơi mất luôn. Không khôi phục, không hoàn tiền. Tiền cược nằm lại trong `matches.pot` của ván đó và coi như mất.
- Lúc startup chạy đúng một câu: `UPDATE matches SET status = 'aborted' WHERE status = 'running'`. Không có câu này thì người chơi của ván cũ bị kẹt ở trạng thái "đang trong ván": không vào được phòng mới, không xoá được tài khoản.
- Tiền vẫn an toàn vì số dư chỉ nằm ở Postgres. Không để số dư ở trạng thái "chờ" trong memory hay Redis.
- Khôi phục ván sau restart: làm sau, xem "Làm sau".

### Loan

Người chơi tự chọn gói và kỳ hạn:

- Gói: `500U`, `1000U`, `2000U`.
- Kỳ hạn: `24h`, `3 ngày`, `30 ngày`.
- Lãi trong hạn (lãi tốt), lãi kép:
  - `24h`: 1% / 24h. Hết hạn trả `1.01 × gốc`.
  - `3 ngày`: 3% cho cả kỳ hạn. Trả `1.03 × gốc`.
  - `30 ngày`: 10% cho cả kỳ hạn. Trả `1.10 × gốc`.
- Lãi quá hạn (lãi xấu): 5% / 24h, lãi kép, tính trên **toàn bộ số đang nợ** gồm gốc cộng lãi trong hạn đã dồn.
- Chỉ một khoản vay active tại một thời điểm. Không vay tiếp khi chưa trả đủ gốc lẫn lãi.
  - Chặn ở DB, không chặn ở code: `CREATE UNIQUE INDEX one_active_loan ON loans(user_id) WHERE status = 'active';` Bấm hai lần liên tiếp không tạo được hai khoản vay.
- Bảng: `loans(id, user_id, principal, term, rate_bps, due_at, owed_at_settle, forgiven_at_seize, status, created_at, settled_at)`.
  - `status` chỉ có 3 giá trị: `active`, `repaid`, `seized`. `repaid` và `seized` đều là đã đóng.
  - `rate_bps` là lãi trong hạn của gói (`100`, `300`, `1000`). Lãi quá hạn cố định `500` bps, không lưu theo từng khoản.
- Tiền vay vào tài khoản bằng `Transfer` với reason `LOAN_DISBURSE`.
- Số phải trả tính **tại thời điểm thanh toán**, không lưu sẵn:
  - `rate` lưu dạng basis point integer, `100` = 1%.
  - Trong hạn: `owed = (principal * (10000 + rate_bps) + 5000) / 10000`. Trả sớm vẫn trả đủ lãi của cả kỳ hạn, không tính theo giờ đã dùng.
  - Quá hạn, mỗi kỳ 24h: `owed = (owed * (10000 + 500) + 5000) / 10000`. Làm tròn nửa lên, không dùng float.
  - Số kỳ quá hạn: `floor((now - due_at) / 24h)`.
  - Cap: `owed = min(owed, 2 * principal)`.
- Trả nợ: trừ toàn bộ số đang nợ một lần, không cho trả từng phần. Trả từng phần buộc phải tính lại lãi trên phần gốc đã trả, thêm logic mà không thêm giá trị ở v3.
- Siết nợ:
  - Trigger: tổng nợ đạt `2 × gốc`. Đây là cap cứng. Quá cap thì ngừng dồn lãi 5%/24h, khoản vay sang `status = 'seized'`.
  - Thời điểm siết tính được ngay lúc vay, vì lãi cố định. Số kỳ `n` tính bằng đúng vòng lặp làm tròn nguyên ở trên, không bằng công thức thực. Ví dụ gói `500U`, `24h`: `505 → 530 → 557 → ... → 952 → 1000` tại `n = 14` (công thức thực cho `n = 15`, lệch 1 kỳ vì làm tròn). Test phải dùng cùng vòng lặp với code. Một job chạy mỗi giờ tìm các khoản `active` đã tới mốc này và gọi `Seize`. Key `loan:{loan_id}:seize` chặn siết hai lần.
  - Hành động: `Seize` trừ `min(owed, balance)`, sàn `0U`. Nợ nhiều hơn số dư thì cũng chỉ trừ tới `0U`, không sinh số dư âm.
  - Phần nợ không đòi được ghi vào `loans.forgiven_at_seize` để audit, rồi đóng khoản vay. Không treo nợ vô hạn.
  - Sau siết: tài khoản còn `0U`, khoản vay đã đóng, được phép vay lại. Không đóng băng vĩnh viễn, vì đóng băng vĩnh viễn thì người chơi chỉ có đường bỏ tài khoản và tạo tài khoản mới.
  - Cap `2 × gốc` làm nợ tối đa đoán trước được: vay `2000U` thì mất nhiều nhất `4000U`, không phải `8644U` như lãi kép không cap sau 30 ngày.

### Admin account

- Nguồn danh tính: **config đọc lúc startup**, không phải DB. Đổi admin bắt buộc restart server, nên không thể đổi khi server đang chạy.
- Config chỉ chứa `username` và `bcrypt hash`. Cấm password plaintext. File config quyền `0600`, không commit file thật, chỉ commit `config.example.yaml`.
- Admin không nằm trong bảng `users` của người chơi.
- Session admin tách hẳn session người chơi: tên cookie khác, scope riêng cho `admin.example.com`, key Redis namespace `admin:session:*`. Session người chơi không có đường nào escalate thành admin. Session admin dùng cùng TTL `15p` và heartbeat `1p` như người chơi.
- Redis chết thì admin không đăng nhập được và cờ "Redis chết" chỉ hiện sau khi Redis sống lại. Chấp nhận ở MVP.
- Cả hai cookie dùng tiền tố `__Host-` (ví dụ `__Host-unofa_sid`, `__Host-unofa_admin_sid`). Tiền tố này buộc cookie không có thuộc tính `Domain`, nên cookie của game không bao giờ bị gửi sang `admin.example.com` và ngược lại.
- `admin.example.com` là origin khác. CORS của game API không nhận origin đó, và API admin không nhận origin game.
- Danh tính ở config nhưng **hành động phải log vào DB**: `admin_audit(admin_username, action, target_user_id, amount, at, ip, request_id)`. Yêu cầu ở v2 về chỉnh số dư qua endpoint và label `SYSTEM {ADD|GET} {amount}` đều đọc từ bảng này.

### Dữ liệu gửi xuống client

Nguyên tắc: server chỉ gửi những gì đúng người chơi đó được nhìn thấy.

- Kênh riêng, per player: tay bài của chính họ, số dư, khoản vay của họ.
- Kênh chung, per room: `player_id`, **số lá** của từng người chứ không phải lá gì, lá đỉnh chồng bỏ, số lá còn trong chồng rút, lượt hiện tại, `turn_deadline`, pot, danh sách action.
- Không bao giờ gửi: bài của người khác, thứ tự chồng rút. Deck array chỉ tồn tại trong memory của goroutine ván, không serialize vào payload nào, kể cả payload debug và log.
- Shape event action:
  `{match_id, seq, player_id, action: "play"|"draw"|"pass"|"call_uno"|"catch_uno"|"challenge"|"accept", card?: {color, rank}, chosen_color?, target_player_id?, result?: "success"|"fail", at}`
  - `draw` gửi cho cả phòng **không có** `card`. Lá vừa rút chỉ gửi qua kênh riêng của người rút.
- Event ngoài nước đi: `ready_check`, `match_start`, `player_status` (`connected`/`disconnected`/`forfeit`), `match_end` (`winner_id`, `pot`; chỉ gửi sau khi thắng được xác nhận), `host_changed`. Message từ client ngoài nước đi: `ready_ack`.
- Mọi event và mọi snapshot state kèm `server_now`. Client tính `offset = server_now - Date.now()` và đếm ngược theo `turn_deadline - (Date.now() + offset)`. Không có `server_now` thì đồng hồ máy người chơi lệch vài giây là đếm ngược sai.
- `seq` tăng đơn điệu theo ván. Client apply theo `seq`; phát hiện thiếu seq thì gọi lại `GET /api/matches/{id}/state` thay vì đoán.
- Không làm client prediction cho bài ẩn. Chỉ optimistic UI cho hành động của chính mình, và phải rollback khi server trả lỗi. Đây là sửa lại mục "game ui sync, prediction" của v1.
- Mỗi ván do **đúng một goroutine** xử lý, và state ván nằm trong memory của goroutine đó. Mọi hành động của người chơi, timer hết lượt và timer forfeit đều gửi vào một channel của goroutine và chạy lần lượt.
  - Lý do: không có bước tuần tự này thì `play` đến đúng lúc timer `20s` hết có thể bị xử lý cùng lúc với việc hết giờ, thành hai kết quả cho một lượt. `catch_uno` "ai bấm trước thắng" cũng cần đúng thứ tự này.
  - Server một binary nên không cần lock phân tán. Khi nào chạy nhiều instance thì mới cần.
- Thứ tự verify khi nhận `play`, đúng thứ tự, không bỏ bước:
  1. Session key hợp lệ, ra `user_id`.
  2. `user_id` là người chơi của `match_id` và ghế còn sống.
  3. Trạng thái ván là `awaiting_input` và `current_player = user_id`. Đang chờ quyết định thách thức `+4` thì chỉ nhận `challenge` hoặc `accept`, từ chối `play`/`draw`/`pass`.
  4. `now < turn_deadline`.
  5. Lá bài nằm trong tay bài mà server đang giữ cho người đó.
  6. Lá bài hợp lệ trên lá đỉnh chồng bỏ: cùng màu (hoặc màu đã chọn sau wild), cùng số/ký hiệu, hoặc wild. Wild phải kèm `chosen_color`.
  7. Vừa rút bài trong lượt này thì chỉ được đánh đúng lá vừa rút.
  8. Đánh `+4`: lưu snapshot "tay bài có lá cùng màu không" để xử lý thách thức. Không chặn ở bước này, vì luật cho phép đánh sai rồi bị thách thức.
  9. Chỉ sau đó mới mutate state, rồi gửi event với data tối thiểu.
  - Sai ở bước nào thì trả lỗi riêng cho người gửi, không broadcast.
- `draw` chỉ hợp lệ một lần mỗi lượt. `pass` chỉ hợp lệ sau khi đã `draw` trong lượt đó.

### Session

- TTL `15p`, heartbeat `1p`. Mỗi heartbeat `EXPIRE` lại key trong Redis, sliding expiry.
- Một tài khoản một session key. Login mới thì `DEL` key cũ trước khi cấp key mới, và đóng kết nối WebSocket của session cũ. Không đóng thì session cũ vẫn chơi tiếp qua socket đang mở.
- Session key khác auth key. Password chỉ dùng lúc login để sinh session key, không bao giờ gửi lại phía client.
- Session key sinh bằng `crypto/rand`, 32 byte.
- Cookie session: `__Host-unofa_sid; HttpOnly; Secure; SameSite=Strict; Path=/`.
- Cookie lưu username theo v1 chỉ để điền sẵn form, không phải credential. Hệ thống không có email nên không lưu email.
- Mở WebSocket: server kiểm tra header `Origin` đúng domain game, sai thì từ chối. Trình duyệt không áp CORS cho WebSocket, nên đây là lớp chặn trang lạ mở socket bằng cookie của người chơi.
- Thời gian gửi xuống client là **timestamp tuyệt đối của server**, ví dụ `turn_deadline`, `due_at`, kèm `server_now` để client bù lệch đồng hồ. Không gửi "còn X giây" vì countdown sẽ trôi khi lag.

### Mức cược

- Preset cố định: `10U / 25U / 50U / 100U / 500U`. Host không nhập số tự do.
- Chặn ở DB: `rooms.stake` có `CHECK (stake IN (10, 25, 50, 100, 500))`. Sửa config không vượt được ràng buộc này.
- Preset thay vì số tự do vì: số tự do cho phép host đặt cược 1U để câu người mới vào rồi không có cách nào thắng, và buộc phải validate khoảng min/max cùng giới hạn theo số dư. Năm mức cố định thì cả hai vấn đề biến mất.
- `500U` cao hơn số dư mặc định `200U`, nên bàn 500U phải vay hoặc thắng trước mới vào được. Đây là hệ quả chấp nhận được, không phải bug: nó là mức cược của người chơi có tiền.
- Vào phòng lúc ván đang chạy: ngồi ghế chờ, chỉ thấy event kênh chung, chơi từ ván sau. Ngoại lệ: phòng khoá từ lúc host bấm `Play` tới lúc gửi `match_start` (xem "Bắt đầu ván"); hết khoảng đó thì mở lại.
- Phòng tối đa `10` người, kể cả ghế chờ.

### Bắt đầu ván

Có hai loại "sẵn sàng", dùng hai tên khác nhau để không lẫn:

- **Sẵn sàng chờ** (`lobby_ready`): người chơi bấm ở phòng chờ, trước khi host bấm `Play`. Đã có ở v2. Sau mỗi trận (`match_end`) server xoá `lobby_ready` của mọi người, ai cũng phải bấm lại, vì số dư đã đổi.
- **Sẵn sàng tải** (`ready_check` / `ready_ack`): handshake giữa server và client **sau khi** host bấm `Play`. Client chỉ trả lời khi đã tải đủ asset. Bước này server chưa gửi state nào.

Thứ tự, không đổi, không bỏ bước:

1. Host bấm `Play`. Cần ít nhất 2 người `lobby_ready`, **host cũng phải `lobby_ready`**; host chưa `lobby_ready` thì server từ chối `Play`. Phòng khoá lại tới khi gửi `match_start`: không ai vào, không ai đổi `lobby_ready`. Người ack trễ hoặc bị loại cũng không đổi được `lobby_ready` trong khoảng này.
2. Server gửi `ready_check {match_id}` cho từng người. **Không gửi thông tin ván nào**: chưa chia bài, chưa có tay bài, chưa có state.
3. Client tải và lazyload asset cần cho bàn chơi (ảnh lá bài, âm thanh, font, component bàn chơi). Tải xong mới gửi `ready_ack {match_id}`.
   - `match_id` không khớp ván đang chờ thì server từ chối ack đó.
   - Asset chưa tải xong thì client không gửi ack. Không có ack "tải một phần".
4. Server chờ ack tối đa **`5s`** tính từ lúc gửi `ready_check`. Đủ ack thì đi tiếp ngay, không chờ hết `5s`.
   - Hết `5s`, ai chưa ack thì về ghế chờ, không bị trừ tiền.
   - **Không handshake lại** với những người còn lại. Mọi người đã sẵn sàng từ phòng chờ; bước này chỉ để client tải asset, nên không được kéo dài.
   - Asset nên được prefetch từ lúc vào phòng chờ, để `5s` đủ cho cả mạng chậm.
5. Một Postgres transaction cho những người đã ack:
   1. Lock dòng `users` của họ, theo `user_id` tăng dần.
   2. Ai có `balance < stake` thì loại ra, về ghế chờ. Số dư có thể đã đổi từ lúc bấm `lobby_ready`, vì người đó có thể đã chuyển tiền đi.
   3. Còn dưới 2 người: rollback, huỷ ván, phòng mở lại. Không ai bị trừ tiền.
   4. Còn từ 2 người trở lên: `Transfer` `BET` cho từng người vào `matches.pot`, rồi commit.
   - Một transaction cho cả bàn để không có trường hợp nửa bàn bị trừ tiền mà ván không chạy.
6. Commit xong, server mới xáo bài bằng `crypto/rand`, chia `7` lá cho những người còn lại, lật lá đầu Cọc Bỏ, chọn người đi đầu.
7. Server gửi thông tin ván, lần đầu tiên:
   - Kênh riêng, từng người: `match_start {match_id, seq: 1, hand, ...state công khai, server_now}`. `hand` là tay bài của **chính người đó**.
   - State công khai: `players` (id, số lá), `top_card`, `draw_pile_count`, `current_player`, `direction`, `turn_deadline`, `pot`.
   - **Đồng hồ lượt đầu tiên bắt đầu chạy từ đây**, không phải từ bước 1.

- Tiền chỉ bị trừ ở bước 5. Người rớt mạng hay tải chậm trong lúc handshake không mất tiền, vì lúc đó ván chưa bắt đầu.
- Bài chỉ được chia sau khi biết chắc ai chơi. Người bị loại ở bước 4–5 chưa từng thấy lá bài nào, nên không cần trộn lại hay chia lại.

### Graph và metric

Giữ toàn bộ graph ở v1/v2, không cắt cái nào. Thêm framework/lib được phép.

- Lib: Recharts cho mọi chart. Nó đủ cho cả 4 loại cần dùng: stacked bar, 100% stacked area, line, area.
- Thêm D3-scale nếu cần scale phức tạp cho 100% stacked area; chỉ lấy package scale, không lấy cả D3.
- Không thêm lib chart thứ hai. Hai lib chart là hai bộ màu, hai cách tooltip, hai bug giống nhau phải sửa hai lần.
- Chart của admin (`admin.example.com`):
  - Req/time theo endpoint: stacked bar. Khoảng thời gian `Today` (default), `24h`, `3 days`, `7 days`, `30 days`, `60 days`.
  - Log HTTP code: 100% stacked area và stacked bar. Cùng bộ khoảng thời gian.
  - Cả hai đọc từ bảng `http_log(request_id, endpoint, status_code, ip, user_id, at)` ghi bằng middleware. Không parse log text, vì parse log là việc phải sửa mỗi lần đổi format.
  - `endpoint` lưu theo route pattern (`/api/matches/{id}/state`), không lưu URL thật. Lưu URL thật thì mỗi `match_id` thành một nhóm riêng trên chart.
  - Giữ `60` ngày, bằng khoảng dài nhất của chart. Job hằng ngày xoá dòng cũ hơn. Không xoá thì bảng lớn mãi.
- Chart của người chơi (`/account`):
  - Số dư theo thời gian: line chart. Nguồn là bảng `balance_snapshot(user_id, balance, at)`, ghi mỗi lần số dư đổi, trong cùng transaction với `Transfer`/`Seize`. Ghi cho cả `System` (`user_id = 0`), không loại trừ ai. Bảng lớn theo số giao dịch và chưa có job dọn; chấp nhận ở MVP.
  - Lịch sử giao dịch: bảng, lọc theo `reason` trong danh sách ở mục "Tiền", và theo chiều chuyển / nhận.
  - Không tính chart số dư bằng cách cộng ngược `transactions` từ hiện tại về quá khứ. Cách đó chậm dần theo độ dài lịch sử.
- Dữ liệu chart trả về dạng đã bucket sẵn theo khoảng thời gian phía server, ví dụ 30 ngày thành 30 điểm. Không gửi raw log xuống client rồi group bằng JavaScript: 60 ngày log là hàng chục nghìn dòng, và client không được thấy chi tiết hơn mức nó cần.
- Điểm polish UI: mọi chart dùng cùng một bộ màu và cùng một component tooltip. Tách `ChartFrame` bọc Recharts một lần, mọi chart đi qua đó. Sửa màu một chỗ thay vì tám chỗ.

### Đã bỏ ở v3

- `dev tool detection` và `block inspector`: bỏ. Không chặn được gì thật vì đó là code phía client, tắt JavaScript là qua. Nó còn phá accessibility mà v1 yêu cầu. Phòng thủ thật là mục trên: mọi thứ validate ở server, và client không bao giờ nhận dữ liệu nó không được thấy.

### Luật chơi

Nguồn: `luat_choi_uno_tieu_chuan.md`. Mục này chỉ ghi chỗ file đó để ngỏ hoặc chỗ game online buộc phải khác.

- Bộ bài 108 lá, 2–10 người, chia `7` lá. Người đi đầu và chiều chơi ban đầu chọn ngẫu nhiên bằng `crypto/rand`. Không mô phỏng bước "rút lá chọn người chia".
- Thắng: người đầu tiên hết bài thắng cả pot, trừ khi lá cuối là `+4` và bị thách thức đúng (xem "Kết thúc ván"). **Không tính điểm, không chơi tới 500 điểm** (mục 8 của file luật). Một ván là một lần cược.
- Lá đầu tiên lật lên Cọc Bỏ: nếu không phải lá số thì xáo lại vào Cọc Rút và lật lá khác cho tới khi ra lá số. File luật để ngỏ chỗ này; đây là cách xử lý đơn giản nhất, không cần luật riêng cho từng lá chức năng.
- `+2` và `+4` **không cộng dồn**. File luật ghi người bị phạt "rút và mất lượt", nên không có cửa đánh chồng `+2` lên `+2`.
- `Reverse` khi còn 2 người: hoạt động như `Skip`, người đánh đi tiếp.
- Không có lá nào đánh được, hoặc có nhưng không muốn đánh: rút 1 lá. Sau khi rút chỉ được đánh **đúng lá vừa rút** nếu nó hợp lệ, hoặc `pass`. Không được đánh lá khác trong tay.
- Cọc Rút hết: xáo lại Cọc Bỏ trừ lá trên cùng thành Cọc Rút mới. Cả hai cọc đều hết thì người cần rút bỏ qua phần rút còn thiếu.
- `Wild` và `+4`: chọn màu nằm trong cùng request `play`, không phải bước riêng. Server từ chối `play` lá wild không có màu.

Thách thức `+4` (theo file luật):

- Chỉ người bị rút 4 được thách thức, trong lượt của họ, trong `20s` của lượt đó. Hai nút: `Thách thức` và `Chấp nhận`.
- Server kiểm tra tay bài **tại thời điểm** người kia đánh `+4`. Server lưu snapshot màu của tay bài đó cùng nước đi, không kiểm tra tay bài hiện tại vì nó đã đổi.
- "Màu của lá trên cùng" là màu đang có hiệu lực. Lá trên cùng là `Wild` thì dùng màu đã chọn cho lá đó.
- Người đánh `+4` có lá cùng màu với lá trên cùng lúc đó: người đánh rút 4, người thách thức đi lượt bình thường.
- Không có: người thách thức rút 6 và mất lượt.
- Hết `20s` không chọn: tính là `Chấp nhận` (rút 4, mất lượt). Không phạt thêm 1 lá hết giờ, vì người đó đã bị rút 4 rồi.
- Trường hợp `+4` là lá cuối của người đánh: `Chấp nhận` (hoặc hết giờ) thì người đánh thắng và người chấp nhận **không** rút 4, vì ván đã xong. Xem "Kết thúc ván".
- Kết quả thách thức chỉ công bố **đúng hay sai**. Server không lộ tay bài của người đánh `+4` cho ai, kể cả người thách thức. File luật bản giấy cho người thách thức xem bài, nhưng online thì đó là rò bài.

Nút `UNO`:

- Nút chỉ **hiện ra** khi bấm được. Không hiện thì không render, không phải render rồi `disabled`.
- Nút `UNO` hiện cho người chơi chỉ khi vừa đánh xuống còn đúng 1 lá mà chưa bấm, cho tới khi người kế tiếp hành động. Còn 2 lá thì không hiện, server cũng từ chối `call_uno`.
- Nút `Bắt UNO` hiện cho **mọi người khác** khi có người còn 1 lá mà chưa bấm `UNO`. Nút biến mất khi lượt đi qua người kế tiếp: họ đánh, rút, hết giờ, hoặc bị `Skip`/`+2`/`+4` bỏ lượt (lúc đó nút biến mất khi bắt đầu lượt của người sau họ).
  - Người bắt đầu tiên thắng. Server xử lý theo thứ tự request đến, chỉ phạt một lần.
  - Bị bắt: rút 2 lá.
  - Bấm `Bắt UNO` khi người kia đã gọi rồi: server từ chối, không phạt ai.
- Server giữ cờ `uno_called` theo người chơi. Cờ tự xoá khi tay bài người đó lên lại trên 1 lá, ví dụ đã bấm `UNO` nhưng sau đó rút bài thay vì đánh.
- Client không tự quyết khi nào nút hiện: server gửi `can_call_uno: bool` và `can_catch_uno: player_id | null` trong state, client chỉ render theo đó. Nhờ vậy người chơi không gọi `UNO` qua console khi chưa tới lúc.
  - `can_catch_uno` là một người, không phải danh sách. Cửa sổ bắt của người trước luôn đóng khi người kế tiếp hành động, nên tại một thời điểm có nhiều nhất một người bắt được.
- Người đang disconnected vẫn bị bắt `UNO` như người thường. Server không bấm `UNO` hộ ai.

Kết thúc ván:

- Hết bài là thắng. `PAYOUT` và event `match_end` chỉ chạy **sau khi thắng được xác nhận**, không chạy ngay lúc tay bài về 0.
- Lá cuối là lá thường, `Skip`, `Reverse`, `Wild` hoặc `+2`: thắng được xác nhận ngay. Người kế tiếp không phải rút, vì ván đã xong và không tính điểm.
- Lá cuối là `+4`: thắng **chưa xác nhận** cho tới khi người kế tiếp quyết định thách thức.
  - Người đánh đang có 0 lá nhưng chưa thắng. Server chưa gửi `match_end`, chưa chạy `PAYOUT`.
  - Người kế tiếp có `20s` chọn `Thách thức` hoặc `Chấp nhận`. Trong lúc này server chỉ nhận `challenge` hoặc `accept` từ họ (cùng trạng thái chờ quyết định ở bước 3 của thứ tự verify).
  - **Thách thức đúng** (tay bài người đánh lúc đó có lá cùng màu với lá trên cùng, theo snapshot): người đánh phải rút 4, ván tiếp tục, người thách thức đi lượt bình thường. Người đánh không thắng.
  - **Thách thức sai**, `Chấp nhận`, hoặc hết `20s`: thắng được xác nhận, người đánh thắng pot. Ván đã xong nên không ai phải rút 4.
  - Kết quả thách thức vẫn chỉ công bố đúng hay sai, không lộ tay bài.
  - Người cần quyết định bị `forfeit` (hết grace `90s`) khi đang chờ: tính là `Chấp nhận`, người đánh thắng.

Thời gian:

- Mỗi lượt `20s`. Thách thức `+4` và chọn màu `Wild` nằm trong cùng `20s` đó, không cộng thêm.
- Màu cho `Wild` gửi cùng request `play`, nên server không bao giờ phải tự chọn màu.
- Lượt đầu tiên tính giờ từ `match_start`, sau handshake (mục "Bắt đầu ván").

### Phòng và host

- Mỗi phòng có **mã phòng** 6 ký tự, sinh bằng `crypto/rand` khi tạo phòng. Bảng chữ gồm `A-Z` và `2-9`, bỏ `O`, `I`, `0`, `1` vì dễ nhìn nhầm.
- Vào phòng bằng mã: người tạo phòng gửi mã cho bạn bè qua kênh bên ngoài. Đây là cách mời bạn ở MVP, thay cho Friends system.
- Mã chỉ sống bằng phòng. Phòng bị xoá thì mã hết hiệu lực.
- Host rời phòng: chọn **ngẫu nhiên** 1 người còn lại làm host mới, bằng `crypto/rand`. Gửi event `host_changed` cho cả phòng.
- Phòng không còn ai: xoá phòng.
- Host rời giữa ván: ván vẫn chạy bình thường. Host chỉ có quyền ở ngoài ván (đổi mức cược, bắt đầu ván mới, kick), nên đổi host không ảnh hưởng ván đang chạy.

### Đăng nhập

- **Bỏ Google OAuth ở v3.** Chỉ username + password. Mục "Google" ở Register/Login của v2 không làm.
- Password hash bằng `bcrypt`, cost `12`.
- Password: tối thiểu `8` ký tự, tối đa `72` byte. `bcrypt` bỏ qua mọi byte sau byte thứ 72, nên không giới hạn thì hai password khác nhau ở đuôi sẽ đăng nhập lẫn nhau.
- Username: `3`–`20` ký tự, chỉ `a-z`, `0-9`, `_`. Lưu chữ thường, `UNIQUE`. Chặn trùng kiểu `Admin`/`admin` và chặn ký tự Unicode trông giống nhau.
- Chặn **đăng ký và đổi tên** thành `system` hoặc bất kỳ username bắt đầu bằng `deleted_`. Không chặn thì người dùng đặt tên `deleted_12` làm `UPDATE username = 'deleted_12'` lúc xoá user 12 vi phạm `UNIQUE`, và xoá tài khoản fail.

### Ngôn ngữ

- Hai ngôn ngữ: `en` là bản chính, `vi` là bản dịch. Thiếu chuỗi `vi` thì hiện `en`, không hiện key.
- Lib: `react-i18next`. Chuỗi nằm trong `locales/en.json`, `locales/vi.json`. Không hard-code chữ trong component.
- Server trả **mã lỗi**, ví dụ `INSUFFICIENT_BALANCE`, không trả câu chữ. FE dịch mã lỗi. Server không cần biết ngôn ngữ của client.
- Ngôn ngữ lưu trong `users.locale`. Chưa đăng nhập thì đọc `navigator.language`, không phải `vi` thì dùng `en`.
- Số tiền format theo locale nhưng ký hiệu luôn là `U`: `1,000U` (en), `1.000U` (vi).

### Accessibility

- Mức: WCAG 2.1 Level A, cộng 2 tiêu chí AA: tương phản chữ `4.5:1` và focus luôn nhìn thấy được.
- Chơi trọn ván bằng bàn phím, không cần chuột:
  - `←`/`→` chọn lá trong tay, `Enter` đánh lá đang chọn.
  - `D` rút bài, `P` pass, `U` gọi `UNO`, `C` bắt `UNO`.
  - Chọn màu `Wild`: `1`–`4` hoặc mũi tên + `Enter`.
  - Thách thức `+4`: dialog hiện ra và tự nhận focus vào nút `Chấp nhận`. `Tab` chuyển giữa hai nút, `Enter` chọn. `Esc` không đóng dialog này, vì đóng dialog không phải là một lựa chọn.
  - `Esc` đóng các dialog khác. Focus quay lại lá bài đã chọn.
- Phím tắt chỉ chạy khi nút tương ứng đang hiện. Nút `UNO` không hiện thì `U` không làm gì.
- Phím tắt một ký tự (`D`, `P`, `U`, `C`, `1`–`4`) chỉ chạy khi focus đang nằm trong khu vực bàn chơi. WCAG 2.1.4 (Level A) yêu cầu điều này: phím tắt một chữ chạy ở mọi nơi sẽ bị kích hoạt nhầm khi người dùng screen reader hay voice control gõ phím.
- Lá bài không phân biệt **chỉ bằng màu**: mỗi màu có thêm ký hiệu hình riêng và chữ ở `aria-label`, ví dụ `"Red 7"`, `"Wild Draw Four"`. Người mù màu đỏ–xanh lá chơi được.
- Nút `UNO` và `Bắt UNO` hiện ra là sự kiện quan trọng, nên phải thông báo qua `aria-live="assertive"`. Không thông báo thì người dùng screen reader không biết nút vừa xuất hiện, vì nút không tồn tại trước đó.
- Lượt mới, lá vừa bị đánh, số lá của người khác thông báo qua `aria-live="polite"`.
- Animation tôn trọng `prefers-reduced-motion`: Framer Motion chuyển sang fade ngắn, không bay bài.

### Rate limit

Hai lớp, mỗi lớp một việc:

- **nginx, theo IP:** chặn flood thô trước khi request tới Go. Đặt lỏng, vì sinh viên dùng chung Wi-Fi ký túc xá hay trường học sẽ chung một IP NAT. Đặt chặt thì cả phòng lab bị khoá vì một người.
- **Go + Redis, theo user (và theo IP với endpoint chưa đăng nhập):** đây là lớp chính, đặt số theo từng nhóm endpoint.

Lớp nginx (`limit_req`, `limit_conn`):
| Phạm vi | Giới hạn | Burst |
|---|---|---|
| Mọi request HTTP, theo IP | `30 r/s` | `60`, `nodelay` |
| Kết nối đồng thời, theo IP | `50` | — |
| Mở WebSocket mới, theo IP | `10 /phút` | `5` |

Lớp app, theo nhóm endpoint:
| Nhóm | Khoá đếm | Giới hạn | Lý do con số |
|---|---|---|---|
| `POST /login` | IP | `5 /phút` | Chặn dò password từ một máy. |
| `POST /login` | username | `10 /giờ` | Chặn dò password một tài khoản từ nhiều IP. Vượt thì khoá đăng nhập tài khoản đó `15p`, báo trên trang admin. |
| `POST /register` | IP | `10 /giờ` | Mỗi tài khoản mới nhận `200U` miễn phí. Không giới hạn thì tạo tài khoản rác để gom tiền bằng chuyển khoản. Không đặt `3`, vì cả nhóm bạn chung Wi-Fi trường sẽ chung IP và người thứ 4 không đăng ký được. |
| Heartbeat | session | `3 /phút` | Client chuẩn gửi `1 /phút`. Dư gấp 3 cho tab ngủ dậy gửi dồn. |
| Hành động trong ván qua WS (`play`, `draw`, `pass`, `challenge`, `accept`) | user | `5 /s` | Game theo lượt, một lượt tối đa 2–3 hành động. `5 /s` chỉ có tác dụng chặn script. |
| `call_uno`, `catch_uno` | user | `2 /s` | Hai nút này là cuộc đua ai bấm trước. Không giới hạn riêng thì script spam `catch_uno` luôn thắng người thật. |
| `GET /api/matches/{id}/state` | user | `20 /phút` | Chỉ dùng khi reconnect hoặc thiếu `seq`. Gọi nhiều hơn tức là client lỗi vòng lặp. |
| Tiền: chuyển khoản, vay, trả nợ | user | `10 /phút` | Ngoài hạn mức `1000U/24h` sẵn có. Giới hạn này chặn spam request, hạn mức kia chặn số tiền. |
| Đổi password, đổi username | user | `5 /giờ` | Đổi password cần password cũ, nên đây cũng là đường dò password. |
| Đọc: lịch sử, chart, profile, danh sách phòng | user | `60 /phút` | Đủ cho người chuyển tab liên tục. |
| Tạo phòng | user | `10 /giờ` | Chặn tạo phòng rác làm đầy danh sách. |
| Vào phòng bằng mã | user | `10 /phút` | Chặn dò mã phòng. Người thật nhập sai vài lần là cùng. |
| Admin login | IP | `3 /phút` | Tài khoản admin có quyền sửa số dư. |
| Admin API | admin | `60 /phút` | Một người dùng, không cần cao hơn. |

WebSocket:

- Một session chỉ một kết nối WS. Mở kết nối thứ hai thì đóng kết nối cũ, giống luật một session một tài khoản.
- Message tối đa `4 KB`. Lớn hơn thì đóng kết nối. Message hợp lệ lớn nhất của game dưới `200 byte`.
- Tổng mọi message từ client: `10 /s`, cộng dồn mọi loại.

Khi vượt giới hạn:

- HTTP trả `429`, header `Retry-After` tính bằng giây, body `{"error": "RATE_LIMITED"}`. FE dịch mã lỗi như mọi lỗi khác.
- WS gửi frame lỗi `RATE_LIMITED`, không xử lý message đó. Vượt `3` lần trong `1 phút` thì đóng kết nối với close code `1008`. Người chơi vẫn còn `90s` reconnect grace, nên không mất ghế vì lỗi client.
- Mọi `429` đều ghi vào `http_log`, nên hiện trên chart HTTP code của admin mà không cần code thêm.

Cài đặt:

- Fixed window: `MULTI`, `INCR rl:{nhóm}:{khoá}:{window}`, `EXPIRE rl:{nhóm}:{khoá}:{window} {độ dài window} NX`, `EXEC`. Cờ `NX` (Redis 7+) chỉ đặt TTL khi key chưa có TTL. Không dùng được "lần đầu thì `EXPIRE`" trong `MULTI`, vì lệnh trong `MULTI` không đọc được kết quả `INCR` để rẽ nhánh.
  - `shortcut: fixed window cho phép tới 2× giới hạn ở ranh giới hai window, chuyển sang sliding window khi log admin cho thấy có người lợi dụng ranh giới.`
- Redis chết thì session cũng chết, nên mọi API cần đăng nhập đã trả `503`. Riêng `POST /login` và `POST /register` thì **từ chối** khi Redis chết, không cho qua, vì cho qua lúc đó là mở cửa cho dò password.
- Mọi con số nằm trong config, đọc lúc startup giống admin account. Đổi phải restart.

### Miễn trừ trách nhiệm

Hiện ở trang About, và tóm tắt một dòng ở footer mọi trang: "UNOFA is a student project. U has no real-world value."
Người dùng phải tick đồng ý khi đăng ký. Lưu `users.terms_version` và `users.terms_accepted_at`. Đổi nội dung thì tăng version và bắt đồng ý lại ở lần đăng nhập sau.

Lưu ý: đây là bản nháp do người không phải luật sư viết, cho một dự án học tập phi thương mại. Nếu dự án mở công khai rộng, nhận tiền hay quảng cáo, cần người có chuyên môn pháp lý đọc lại.

Bản chính (`en`):

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

Bản dịch (`vi`):

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

Hệ quả kỹ thuật của bản miễn trừ, phải có trong code:

- Mục 8 hứa xoá dữ liệu khi được yêu cầu. Admin cần endpoint xoá tài khoản, chạy trong một Postgres transaction:
  1. Từ chối xoá nếu người đó đang ở trong một ván `running`. Trả mã lỗi `USER_IN_MATCH`.
  2. Khoản vay `active` thì đóng bằng `Seize` như khi siết nợ.
  3. Số dư còn lại chuyển về `System` bằng `Transfer`, reason `ACCOUNT_DELETE`, key `delete:{user_id}`. Số dư `0U` thì bỏ qua bước này.
     - Lý do: không ghi dòng này thì số dư biến mất khỏi sổ, và câu kiểm tra lệch sổ báo sai mãi mãi. Không ai nhận số tiền đó; nó chỉ giữ sổ khớp.
  4. **Không xoá dòng `users`**, mà xoá dữ liệu cá nhân trên dòng đó: `username = 'deleted_{user_id}'`, `password_hash = NULL`, `locale = NULL`, `deleted_at = now()`. Dòng `transactions` cũ vẫn trỏ đúng vào dòng này, không cần đổi.
     - Lý do: xoá hẳn dòng `users` thì mọi dòng `transactions` trỏ tới nó bị lỗi khoá ngoại.
  5. Xoá `balance_snapshot` của người đó, xoá session trong Redis.
  - Đổi `user_id` trong `http_log` thành `NULL`; IP vẫn giữ tới hết 60 ngày theo mục 8.
  - Username cũ được dùng lại cho người đăng ký mới.
- Mục 6 nói ván có thể bị huỷ khi restart. Đúng với thiết kế MVP (ván mất, pot mất), nên đây là cam kết trung thực, không phải lời chối.
- Mục 9 cấm chuyển U lấy tiền thật. Không có cách kỹ thuật nào chặn hoàn toàn. Hạn mức chuyển `1000U/24h` và giới hạn đăng ký `10/giờ/IP` làm việc này chậm và tốn công, không chặn được.

### Job chạy nền

Mọi job chạy trong cùng binary Go. Mỗi job idempotent, nên chạy lại sau restart không gây hại.
| Job | Chu kỳ | Việc |
|---|---|---|
| Đánh dấu ván cũ | lúc startup | `UPDATE matches SET status = 'aborted' WHERE status = 'running'` (mục "Server restart"). |
| Kiểm tra invariant tiền | mỗi giờ | Chạy câu SQL `SUM = 0`. Lệch thì báo lên trang admin. |
| Siết nợ | mỗi giờ | `Seize` các khoản `active` đã tới mốc `2 × gốc`. |
| Dọn `http_log` | mỗi ngày | Xoá dòng cũ hơn 60 ngày. |

### Tải dự kiến

Giả định để chọn kiến trúc và rate limit: tối đa `100` người online cùng lúc, `10` ván chạy cùng lúc. Vượt khoảng `1000` người online thì xem lại: ghi `http_log` đồng bộ vào Postgres mỗi request, một instance Go, job chạy trong process.

### Làm sau (ngoài MVP)

Không cắt, chỉ dời sau MVP:

- **Khôi phục ván sau khi server restart:** lưu state ván và `turn_deadline` mỗi lượt, lúc startup dựng lại goroutine và timer. MVP thì restart là mất ván.
- **Friends system** (v2: add, delete, invite, status Active/Inactive). MVP mời bạn bằng mã phòng (mục "Phòng và host"). Ước lượng khoảng 2–3 ngày khi làm.

### Admin: Notify và Server status

Trang admin hiện **4 cờ**, không gửi email hay Slack:

1. Invariant tiền lệch (job mỗi giờ).
2. Tài khoản bị khoá login `15p` do vượt rate limit `username`.
3. Redis chết.
4. Khoản vay vừa bị siết.

Nguồn dữ liệu: `http_log` và kết quả các job. Cờ lưu trong Postgres: `admin_flags(id, kind, detail, at, cleared_at)`. Admin bấm xoá cờ thì ghi `cleared_at`.

### Còn thiếu

Không còn mục nào chưa chốt.
