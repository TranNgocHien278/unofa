# 03 — Data model

Postgres là nguồn sự thật duy nhất cho tiền, tài khoản, loan, transaction, audit.
Redis chỉ giữ session + bộ đếm rate limit. Không bao giờ là nguồn sự thật cho tiền.
State ván nằm trong memory goroutine, không persist (xem 02 §State ván).

## DDL

Migration chạy một chiều, đánh số `0001_init.sql` trở đi.

```sql
CREATE TABLE users (
    user_id            BIGINT PRIMARY KEY,
    username           TEXT NOT NULL UNIQUE,
    password_hash      TEXT,                    -- NULL cho System và user đã xoá
    locale             TEXT,                    -- 'en' | 'vi', NULL cho System/deleted
    balance            BIGINT NOT NULL DEFAULT 0,
    terms_version      INT NOT NULL DEFAULT 0,
    terms_accepted_at  TIMESTAMPTZ,
    login_locked_until TIMESTAMPTZ,             -- khoá do rate limit username
    deleted_at         TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT balance_floor CHECK (user_id = 0 OR balance >= 0),
    CONSTRAINT username_lower CHECK (user_id = 0 OR username = lower(username))
);

CREATE SEQUENCE users_seq START 1;

-- Tài khoản hệ thống, tạo bằng migration, không qua đăng ký
INSERT INTO users (user_id, username, password_hash, balance)
VALUES (0, 'System', NULL, 0);

CREATE TABLE rooms (
    room_id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code         TEXT NOT NULL UNIQUE,          -- 6 ký tự A-Z2-9
    host_user_id BIGINT NOT NULL REFERENCES users(user_id),
    stake        BIGINT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open',  -- open | locked | in_match
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT stake_preset CHECK (stake IN (10, 25, 50, 100, 500)),
    CONSTRAINT code_shape   CHECK (code ~ '^[A-Z2-9]{6}$'),
    CONSTRAINT room_status  CHECK (status IN ('open', 'locked', 'in_match'))
);

CREATE TABLE room_members (
    room_id      UUID NOT NULL REFERENCES rooms(room_id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users(user_id),
    seat         INT NOT NULL,
    lobby_ready  BOOLEAN NOT NULL DEFAULT false,
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (room_id, user_id),
    UNIQUE (room_id, seat),
    CONSTRAINT seat_range CHECK (seat BETWEEN 0 AND 9)
);

CREATE TABLE matches (
    match_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id     UUID NOT NULL REFERENCES rooms(room_id),
    stake       BIGINT NOT NULL,
    pot         BIGINT NOT NULL DEFAULT 0,
    status      TEXT NOT NULL,                  -- running | ended | aborted
    winner_id   BIGINT REFERENCES users(user_id),
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at    TIMESTAMPTZ,

    CONSTRAINT pot_floor    CHECK (pot >= 0),
    CONSTRAINT stake_preset CHECK (stake IN (10, 25, 50, 100, 500)),
    CONSTRAINT match_status CHECK (status IN ('running', 'ended', 'aborted'))
);

CREATE TABLE match_players (
    match_id        UUID NOT NULL REFERENCES matches(match_id),
    user_id         BIGINT NOT NULL REFERENCES users(user_id),
    seat            INT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'connected',  -- connected | disconnected | forfeit
    disconnected_at TIMESTAMPTZ,
    forfeited_at    TIMESTAMPTZ,

    PRIMARY KEY (match_id, user_id),
    CONSTRAINT mp_status CHECK (status IN ('connected', 'disconnected', 'forfeit'))
);

CREATE TABLE transactions (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    from_user_id    BIGINT REFERENCES users(user_id),
    from_match_id   UUID   REFERENCES matches(match_id),
    to_user_id      BIGINT REFERENCES users(user_id),
    to_match_id     UUID   REFERENCES matches(match_id),
    amount          BIGINT NOT NULL,
    reason          TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT amount_positive CHECK (amount >= 1),
    CONSTRAINT from_one_side CHECK (num_nonnulls(from_user_id, from_match_id) = 1),
    CONSTRAINT to_one_side   CHECK (num_nonnulls(to_user_id,   to_match_id)   = 1),
    CONSTRAINT reason_known CHECK (reason IN (
        'SIGNUP_BONUS','BET','PAYOUT','BURN','TRANSFER','LOAN_DISBURSE',
        'LOAN_REPAY','SEIZE','SYSTEM_ADD','SYSTEM_GET','ACCOUNT_DELETE'
    ))
);

CREATE INDEX transactions_from_user ON transactions (from_user_id, created_at DESC);
CREATE INDEX transactions_to_user   ON transactions (to_user_id,   created_at DESC);
CREATE INDEX transactions_transfer  ON transactions (from_user_id, created_at)
    WHERE reason = 'TRANSFER';

CREATE TABLE balance_snapshot (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id  BIGINT NOT NULL REFERENCES users(user_id),
    balance  BIGINT NOT NULL,
    at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX balance_snapshot_user ON balance_snapshot (user_id, at);

CREATE TABLE loans (
    loan_id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id            BIGINT NOT NULL REFERENCES users(user_id),
    principal          BIGINT NOT NULL,
    term               TEXT NOT NULL,           -- '24h' | '3d' | '30d'
    rate_bps           INT NOT NULL,            -- 100 | 300 | 1000
    due_at             TIMESTAMPTZ NOT NULL,
    owed_at_settle     BIGINT,                  -- số thực trả lúc đóng
    forgiven_at_seize  BIGINT,                  -- phần không đòi được
    status             TEXT NOT NULL DEFAULT 'active',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    settled_at         TIMESTAMPTZ,

    CONSTRAINT principal_preset CHECK (principal IN (500, 1000, 2000)),
    CONSTRAINT term_known       CHECK (term IN ('24h', '3d', '30d')),
    CONSTRAINT rate_known       CHECK (rate_bps IN (100, 300, 1000)),
    CONSTRAINT loan_status      CHECK (status IN ('active', 'repaid', 'seized'))
);

CREATE UNIQUE INDEX one_active_loan ON loans(user_id) WHERE status = 'active';

CREATE TABLE http_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_id  TEXT NOT NULL,
    endpoint    TEXT NOT NULL,       -- route pattern, không phải URL thật
    status_code INT NOT NULL,
    ip          INET,
    user_id     BIGINT,              -- không FK: bị NULL hoá khi xoá tài khoản
    at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX http_log_at       ON http_log (at);
CREATE INDEX http_log_endpoint ON http_log (endpoint, at);

CREATE TABLE admin_audit (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    admin_username TEXT NOT NULL,
    action         TEXT NOT NULL,   -- SYSTEM_ADD | SYSTEM_GET | DELETE_USER | CLEAR_FLAG
    target_user_id BIGINT,
    amount         BIGINT,
    at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    ip             INET,
    request_id     TEXT NOT NULL
);

CREATE TABLE admin_flags (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind       TEXT NOT NULL,       -- MONEY_INVARIANT | LOGIN_LOCKED | REDIS_DOWN | LOAN_SEIZED
    detail     JSONB NOT NULL DEFAULT '{}',
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    cleared_at TIMESTAMPTZ
);
CREATE INDEX admin_flags_open ON admin_flags (at DESC) WHERE cleared_at IS NULL;
```

`rooms` và `room_members` tồn tại trong Postgres dù vòng đời phòng ngắn (D1): `CHECK` trên `stake` cần bảng thật, và `host_user_id`/`seat` cần FK.

## Tiền

- Kiểu `int64` ở mọi tầng. Postgres `BIGINT`, Go `int64`. **Cấm float** cho tiền.
- Đơn vị `UNOd`, ký hiệu `U`. `1U` là đơn vị nhỏ nhất, không số lẻ.
- Số dư mặc định: `200U`.
- JSON trả FE là number (an toàn tới `2^53`).
- Client không bao giờ cộng tiền, chỉ hiển thị số server gửi.

### Sàn cứng `0U`

Số dư người chơi không bao giờ âm. Enforce hai lớp:

1. DB: `CHECK (user_id = 0 OR balance >= 0)`. Chỉ `System` được âm. Vượt sàn → transaction fail, ồn ào, không âm thầm.
2. Go: mọi phép trừ đi qua `Transfer`/`Seize`, kiểm `amount > balance` **trước** khi trừ. Bỏ qua kiểm tra chỉ khi `from = 0`.

Không dùng `uint64`: Postgres không có `BIGINT UNSIGNED`, và Go `uint64` trừ quá tay wrap thành `~2^64` — chia pot sai một lần là nhân tiền thay vì mất tiền.

### Tài khoản `System`

- Một dòng `users` thật, `user_id = 0`, username `System`, `password_hash = NULL`.
- Mọi tiền sinh ra đi từ đây, mọi tiền mất đi về đây. Số dư âm; độ âm = tổng `U` lưu hành.
- Tạo bằng migration. Login vào `System` luôn thất bại (`password_hash IS NULL`).
- Username `system` bị chặn khi đăng ký, so sánh không phân biệt hoa thường.
- Hiện trong lịch sử người chơi là `System`, ví dụ `System → you: 200U (SIGNUP_BONUS)`.

### Hai hàm chuyển tiền. Không có hàm thứ ba.

```go
// Strict: fail cả transaction nếu from không đủ tiền.
func Transfer(ctx context.Context, tx pgx.Tx, from Acct, to Acct,
              amount int64, reason string, key string) error

// Siết nợ: trừ min(owed, balance), sàn 0U.
func Seize(ctx context.Context, tx pgx.Tx, from Acct, to Acct,
           owed int64, reason string, key string) (seized int64, forgiven int64, err error)
```

`Acct` là một trong hai: `user:{id}` hoặc `match:{id}` (pot).

Cả hai:

- Một Postgres transaction duy nhất.
- `SELECT ... FOR UPDATE` dòng cả hai bên, lock theo thứ tự cố định: `users` theo `user_id` tăng dần, rồi tới `matches`.
- `INSERT INTO transactions` với `UNIQUE(idempotency_key)`.
- `INSERT INTO balance_snapshot` cho cả hai phía bên `users` (kể cả `System`).
- Server chết giữa transaction → Postgres rollback, số dư không đổi.

`Seize` đặc biệt:

- `seized = min(owed, balance)`.
- `balance == 0` → không ghi dòng `transactions` nào (vì `amount >= 1`), chỉ đóng khoản vay.
- `forgiven = owed - seized`, ghi vào `loans.forgiven_at_seize`.

Idempotency: gọi lại với cùng key → trả kết quả cũ, không trừ lần hai. Bắt bằng `UNIQUE` violation + đọc dòng cũ, hoặc `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING` rồi kiểm rowcount.

### Reason

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
| `ACCOUNT_DELETE` | user | `0` |

### Idempotency key

| Key | Dùng ở |
|---|---|
| `signup:{user_id}` | Tạo tài khoản |
| `match:{match_id}:bet:{user_id}` | Trừ cược |
| `match:{match_id}:payout` | Trả thưởng |
| `match:{match_id}:burn` | Đốt pot |
| `transfer:{client_uuid}` | Chuyển khoản người chơi |
| `loan:{loan_id}:disburse` | Giải ngân |
| `loan:{loan_id}:repay` | Trả nợ |
| `loan:{loan_id}:seize` | Siết nợ |
| `admin:{request_id}` | Admin cộng/trừ tiền |
| `delete:{user_id}` | Xoá tài khoản |

### Invariant lệch sổ

```sql
SELECT (SELECT COALESCE(SUM(balance), 0) FROM users)
     + (SELECT COALESCE(SUM(pot), 0)     FROM matches) = 0;
```

- Khác `0` tức có tiền sinh ra hoặc mất đi ngoài sổ.
- Chạy trong test sau mỗi test case có chuyển tiền, và trong job mỗi giờ. Lệch → `admin_flags` kind `MONEY_INVARIANT`.
- Ván `aborted` giữ nguyên pot (D14) nên vẫn tính vào tổng.

## Chuyển khoản giữa người chơi

- Phí `0U`. Min `1U`. Max `1000U/24h` mỗi người gửi.
- "Ngày" là cửa sổ `24h` trượt, không phải ngày lịch:

```sql
SELECT COALESCE(SUM(amount), 0)
FROM transactions
WHERE reason = 'TRANSFER' AND from_user_id = $1
  AND created_at > now() - interval '24 hours';
```

Cửa sổ trượt khó lách hơn (không reset nửa đêm để gửi gấp đôi) và chỉ cần một câu SUM.

- Limit kiểm **trong cùng transaction** với `Transfer`, sau khi đã lock dòng người gửi.
- Chặn chuyển tiền khi người gửi có khoản vay quá hạn (`status = 'active' AND now() > due_at`) → `LOAN_OVERDUE`. Không chặn thì vay `2000U` rồi chuyển `1000U/ngày` là trốn siết nợ hợp lệ.
- Tự chuyển cho chính mình → `SELF_TRANSFER`.

## Loan

### Gói và kỳ hạn

Chọn độc lập mỗi chiều: gói `{500, 1000, 2000}U` × kỳ hạn `{24h, 3d, 30d}` = 9 tổ hợp.

| Kỳ hạn | `rate_bps` | Hệ số trong hạn |
|---|---|---|
| `24h` | `100` | `1.01 × gốc` |
| `3d` | `300` | `1.03 × gốc` |
| `30d` | `1000` | `1.10 × gốc` |

Lãi quá hạn: `500 bps = 5% / 24h`, lãi kép trên **toàn bộ số đang nợ** (gốc + lãi trong hạn đã dồn).

### Công thức (integer, làm tròn nửa lên, không float)

```go
const overdueBps = 500

func roundHalfUp(n int64) int64 { return (n + 5000) / 10000 }

// Trong hạn:
owed = (principal*(10000+rate_bps) + 5000) / 10000

// Quá hạn, mỗi kỳ 24h:
for i := 0; i < periods; i++ {
    owed = (owed*(10000+overdueBps) + 5000) / 10000
}

// periods = floor((now - due_at) / 24h)
// cap:
owed = min(owed, 2*principal)
```

Trả sớm vẫn trả đủ lãi cả kỳ hạn, không tính theo giờ đã dùng.

Cap `2 × gốc` = `seize` trigger. Ví dụ `2000U` mất tối đa `4000U`, không phải `8644U` như lãi kép không cap sau 30 ngày.

### Ví dụ số học (dùng làm golden test)

Gói `500U`, kỳ hạn `24h`, `rate_bps = 100`:

```
owed_in_term = (500 * 10100 + 5000) / 10000 = 505
n=1: (505*10500+5000)/10000 = 530
n=2: (530*10500+5000)/10000 = 557
...
n=14: 1000  ← chạm cap 2*500, seize tại đây
```

Số kỳ phải tính bằng **đúng vòng lặp làm tròn** trên, không bằng công thức `505 * 1.05^n`. Công thức thực cho `n = 15`; vòng lặp nguyên cho `n = 14`. Test dùng cùng vòng lặp với code, không viết lại công thức.

### Trạng thái

| `status` | Nghĩa |
|---|---|
| `active` | Đang vay, chưa đóng. |
| `repaid` | Trả đủ. `owed_at_settle` = số đã trả. |
| `seized` | Bị siết. `owed_at_settle` = số phải trả, `forgiven_at_seize` = phần tha. |

`repaid` và `seized` đều là đã đóng, được vay lại.

### Hành động

**Vay:**

1. Kiểm không có khoản `active` (DB index `one_active_loan` là lớp chặn cuối).
2. Tính `due_at = now + term`.
3. `INSERT loans`.
4. `Transfer` `LOAN_DISBURSE` `principal` từ `System` → user, key `loan:{loan_id}:disburse`.

**Trả nợ:** trừ **toàn bộ** số đang nợ một lần, không cho trả từng phần. Trả từng phần buộc tính lại lãi trên phần gốc đã trả, thêm logic không thêm giá trị ở v3.

1. Tính `owed` theo công thức trên.
2. `Transfer` `LOAN_REPAY` từ user → `System`. Thiếu tiền → `INSUFFICIENT_BALANCE`, khoản vay giữ nguyên.
3. `status = 'repaid'`, `owed_at_settle = owed`, `settled_at = now()`.

**Siết nợ** (job mỗi giờ):

1. Tìm khoản `active` có `owed >= 2 × principal`.
2. `Seize` user → `System`, reason `SEIZE`, key `loan:{loan_id}:seize`.
3. `forgiven_at_seize = owed - seized`, `status = 'seized'`, `settled_at = now()`.
4. Ghi `admin_flags` kind `LOAN_SEIZED`.
5. Sau siết: user còn `0U` (hoặc số lẻ không đủ), khoản đã đóng, được vay lại. Không đóng băng vĩnh viễn — đóng băng thì người chơi chỉ còn đường bỏ tài khoản và tạo tài khoản mới.

## Phòng và host

- Mã phòng: `6` ký tự, `crypto/rand`, bảng chữ `A-Z` + `2-9` (bỏ `O`, `I`, `0`, `1`). 32 ký tự → `32^6 ≈ 1.07e9` mã; đủ cho MVP, va chạm thì sinh lại.
- Mã chết cùng phòng.
- Host rời: chọn **ngẫu nhiên** một người còn lại bằng `crypto/rand`, gửi `host_changed`.
- Phòng không còn ai: xoá phòng.
- Host rời giữa ván: ván chạy bình thường. Host chỉ có quyền ngoài ván.
- Tối đa `10` người kể cả ghế chờ.

### State phòng

`rooms.status`:

| Giá trị | Nghĩa |
|---|---|
| `open` | Nhận người, nhận đổi `lobby_ready`. |
| `locked` | Từ lúc host bấm `Play` tới lúc gửi `match_start`. Không ai vào, không ai đổi `lobby_ready`. |
| `in_match` | Ván đang chạy. Người mới vào ngồi ghế chờ, xem event công khai, chơi từ ván sau. |

Lúc startup: `DELETE FROM room_members; DELETE FROM rooms;` (D1) vì socket đã chết hết và `host_user_id` FK có thể trỏ vào user không còn. Chạy trong cùng transaction với `UPDATE matches SET status='aborted' WHERE status='running'`.

## Số dư và snapshot

`balance_snapshot` ghi mỗi lần số dư đổi, trong cùng transaction với `Transfer`/`Seize`. Ghi cho **cả** `System`. Nguồn của chart số dư ở `/account`.

Không tính chart bằng cách cộng ngược `transactions`: chậm dần theo độ dài lịch sử.

Bảng lớn theo số giao dịch, chưa có job dọn. Chấp nhận ở MVP.

## Xoá tài khoản

Admin endpoint, một Postgres transaction:

1. Từ chối nếu đang ở trong ván `running` → `USER_IN_MATCH`.
2. Khoản vay `active`: đóng bằng `Seize` như khi siết.
3. Số dư còn lại: `Transfer` về `System`, reason `ACCOUNT_DELETE`, key `delete:{user_id}`. Số dư `0U` thì bỏ qua.
   - Lý do: không ghi dòng này thì số dư biến mất khỏi sổ và invariant báo sai mãi.
4. **Không xoá dòng `users`**, xoá dữ liệu cá nhân:
   `username = 'deleted_{user_id}'`, `password_hash = NULL`, `locale = NULL`, `deleted_at = now()`.
   - Lý do: xoá hẳn dòng thì mọi `transactions` trỏ tới nó bị lỗi FK.
5. Xoá `balance_snapshot` của user, xoá session trong Redis, đóng WS.
6. `UPDATE http_log SET user_id = NULL WHERE user_id = $1`. IP vẫn giữ tới hết `60` ngày theo §8.
7. Username cũ dùng lại được cho người đăng ký mới.

Ràng buộc bắt buộc: chặn đăng ký/đổi tên thành `system` hoặc bất kỳ chuỗi bắt đầu bằng `deleted_`. Không chặn thì `UPDATE users SET username='deleted_12'` lúc xoá user 12 vi phạm `UNIQUE`, và xoá tài khoản fail.

## Test bắt buộc

| # | Test |
|---|---|
| T1 | Tổng `users.balance` + tổng `matches.pot` = `0` sau mỗi test có chuyển tiền. |
| T2 | Trừ quá số dư → DB `CHECK`/Go fail, không âm, không wrap. |
| T3 | `Transfer` hai lần cùng idempotency key → chỉ một dòng `transactions`. |
| T4 | `Seize` khi balance `0` → không dòng `transactions`, khoản vay đóng, `forgiven = owed`. |
| T5 | `Seize` khi balance `< owed` → trừ hết về `0`, `forgiven = owed - balance`. |
| T6 | Hai request vay đồng thời → một thành công, một conflict từ `one_active_loan`. |
| T7 | Chuyển khoản thứ `N+1` vượt `1000U/24h` → `TRANSFER_LIMIT`. |
| T8 | Chuyển khoản khi có khoản vay quá hạn → `LOAN_OVERDUE`. |
| T9 | Golden test loan: `500U/24h` → dãy `505, 530, 557, …`, seize tại `n = 14`. |
| T10 | Cap `2 × gốc` chặn đúng, không vượt. |
| T11 | Xoá tài khoản: dòng `users` còn, `username = 'deleted_{id}'`, invariant vẫn `0`. |
| T12 | Xoá tài khoản khi đang trong ván `running` → `USER_IN_MATCH`. |
| T13 | Deadlock test: hai chuyển khoản ngược chiều đồng thời, không deadlock. |
| T14 | Login vào `System` luôn thất bại. |
| T15 | Đăng ký username `System`/`SYSTEM`/`deleted_1` → bị chặn. |
