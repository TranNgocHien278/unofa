# 06 — Frontend

React 18 + TypeScript + Vite. Một SPA cho `uno.example.com`, một SPA riêng (hoặc cùng repo, entry khác) cho `admin.example.com`.

Lib đã chốt, không thay:

| Việc | Lib |
|---|---|
| Routing | `react-router-dom` |
| State | `zustand` |
| Animation | `framer-motion` |
| Chart | `recharts` (+ `d3-scale` nếu cần scale phức tạp cho 100% stacked area; chỉ package `scale`, không lấy cả D3) |
| i18n | `react-i18next` |
| Realtime | `WebSocket` gốc + wrapper tự viết |

Không thêm lib chart thứ hai (D: hai lib chart là hai bộ màu, hai tooltip, hai bug phải sửa hai lần).

## Cấu trúc thư mục

```
web/
  src/
    main.tsx
    app/
      router.tsx
      providers.tsx
    api/
      client.ts            # fetch wrapper, gắn cookie, parse lỗi
      auth.ts
      money.ts
      loan.ts
      rooms.ts
      matches.ts
      admin.ts
    realtime/
      socket.ts            # wrapper tự reconnect, backoff
      useMatchStream.ts    # hook: subscribe event + apply theo seq
      types.ts
    store/
      session.ts
      lobby.ts
      room.ts
      match.ts
      ui.ts                # toast, dialog, ngôn ngữ đang chọn
    features/
      auth/ register/ lobby/ room/ table/ account/ loan/ about/
    components/
      Card.tsx Hand.tsx Discard.tsx Table.tsx Timer.tsx
      ChartFrame.tsx Money.tsx Dialog.tsx Toast.tsx
    i18n/
      index.ts
      locales/en.json
      locales/vi.json
    styles/
      tokens.css           # màu, spacing, z-index
```

Admin ở `web/admin/` với `main.tsx` riêng, dùng lại `api/client.ts`, `components/ChartFrame.tsx`, không dùng lại store của game.

## API client

```ts
// api/client.ts
export class ApiError extends Error {
  constructor(public code: string, public status: number, public requestId?: string) { super(code) }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T>
```

- Luôn `credentials: 'include'`, `Content-Type: application/json`.
- Non-2xx → parse `{error, request_id}` → ném `ApiError`.
- `401 SESSION_EXPIRED` → gọi `ui.openRelogin()`, không tự logout.
- `429` → đọc `Retry-After`, toast "thử lại sau N giây".
- `503` → toast `SERVICE_UNAVAILABLE`.
- Không retry POST tự động (trừ khi body có `client_uuid`).

## Store

Zustand, mỗi slice một store nhỏ. Không Redux, không immer.

### `session`

```ts
{ user: {userId, username, balance, locale} | null,
  status: 'loading' | 'authenticated' | 'anonymous',
  termsVersion: number }
```

### `match`

```ts
{ matchId: string | null,
  seq: number,                 // last applied
  phase: 'playing' | 'awaiting_challenge' | 'ended' | null,
  hand: Card[],
  public: PublicState | null,
  you: { canCallUno: boolean, canCatchUno: number | null, isYourTurn: boolean },
  timerOffsetMs: number,       // server_now - Date.now(), cập nhật mỗi event
  pendingAction: { cardId: string } | null,
  applySnapshot(s), applyEvent(e) }
```

`applyEvent`:

1. `if (e.seq <= seq) return` — duplicate.
2. `if (e.seq > seq + 1) { refetchState(); return }` — mất event.
3. Apply, `seq = e.seq`.

## Realtime wrapper

```ts
// realtime/socket.ts
new Socket({ url: '/api/ws', onEvent, onOpen, onClose })
```

- Tự reconnect: `1s, 2s, 4s, 8s, 16s`, jitter ±20%, dừng sau `90s` (quá grace thì thôi).
- Nhận `1000` với reason `SESSION_REPLACED`: không reconnect, toast "tài khoản đăng nhập ở nơi khác".
- Nhận `1008`: chờ `Retry-After` rồi mới reconnect.
- `onOpen` → nếu store có `matchId` → gọi `GET /api/matches/{id}/state`, apply snapshot.
- Chỉ một socket toàn app. Không tạo socket riêng cho từng route.

`ref` cho mỗi lần gửi action: `crypto.randomUUID()`. Lưu vào `pendingAction` để rollback nếu server trả `error` với cùng `ref`.

## Bàn chơi (`/table/:matchId`)

### Layout

```
┌──────────────────────────────────────────┐
│ Room code · Pot 200U · [timer 00:14]     │
├──────────────────────────────────────────┤
│        [đối thủ trái]   [đối thủ phải]   │
│  (avatar, số lá, status)  (…)            │
│                                          │
│        [Cọc Rút]  [Cọc Bỏ: top card]     │
│                                          │
├──────────────────────────────────────────┤
│  [UNO] [Bắt UNO]        [Rút] [Pass]     │
├──────────────────────────────────────────┤
│   ← tay bài: 7 lá, cuộn ngang trên mobile│
└──────────────────────────────────────────┘
```

- Bàn tối đa `10` ghế: hiện danh sách ghế hai bên + trên, không dùng toạ độ vòng tròn với CSS transform (dễ vỡ khi 3 người). Dùng grid theo số người, `data-count` để chọn layout.
- Mobile: tay bài cuộn ngang, mỗi lá `min-width: 64px`.

### Component lá bài

Tự vẽ, không dùng asset của UNO: hình học/HUD riêng, tên riêng. Màu là token CSS, không phải mã màu rải rác:

```css
--card-red: #d7263d; --card-yellow: #f2b500; --card-green: #2a9d4a; --card-blue: #1b6ca8;
--card-ink: #101010; --card-paper: #fafafa;
```

Mỗi màu có **thêm ký hiệu hình riêng** (không chỉ màu) và `aria-label` đầy đủ: `"Red 7"`, `"Wild Draw Four"`. Người mù màu đỏ–xanh lá chơi được.

Lá:

```tsx
<button
  role="button"
  aria-label="Red 7"
  aria-selected={selected}
  className="card card--red"
  onClick={…}
/>
```

Không dùng `<div role="button">` khi `<button>` được.

### Timer

- Đếm ngược từ `turn_deadline - (Date.now() + offset)`, `requestAnimationFrame` hoặc `setInterval(250ms)`.
- `aria-live="off"` (đếm giây liên tục sẽ spam screen reader). Cảnh báo bằng `aria-live="polite"` khi còn `5s`, một lần.
- Dưới `5s`: thanh đổi màu + chữ "5s" — không thêm animation chớp.

### Nút theo state

| Nút | Hiện khi |
|---|---|
| `Rút` | `isYourTurn && !drewThisTurn` |
| `Pass` | `isYourTurn && drewThisTurn` |
| `UNO` | `canCallUno` |
| `Bắt UNO` | `canCatchUno !== null` |
| Dialog thách thức | `phase === 'awaiting_challenge' && you là pending_challenger` |

Nút **không hiện** thì không render — không render rồi `disabled` (v3 yêu cầu). `disabled` chỉ dùng cho trạng thái "đang gửi" trong lúc chờ server.

### Dialog thách thức `+4`

```tsx
<Dialog role="alertdialog" aria-modal="true" labelledBy="challenge-title" descripbedBy="challenge-desc">
  <h2 id="challenge-title">Thách thức lá +4?</h2>
  <p id="challenge-desc">Hết giờ tính là Chấp nhận.</p>
  <button onClick={accept} autoFocus>Chấp nhận</button>
  <button onClick={challenge}>Thách thức</button>
</Dialog>
```

- Tự nhận focus vào `Chấp nhận`.
- `Tab` chuyển giữa hai nút. `Enter` chọn.
- `Esc` **không** đóng: đóng dialog không phải là một lựa chọn.
- Kết quả công bố chỉ "đúng"/"sai", không hiện bài ai.

### Optimistic UI

- Bấm `play`: lá `opacity: .5`, `aria-busy="true"`, không xoá khỏi mảng `hand`.
- Lỗi (`error` frame có `ref` khớp): rollback, lá sáng lại, toast mã lỗi đã dịch.
- Thành công (event `action` có `player_id === you`): xoá lá khỏi tay, chạy animation bay về Cọc Bỏ.
- Không optimistic cho `draw`, `challenge`, `catch_uno`: chờ server.

### Animation

Framer Motion. `layout` cho tay bài, `AnimatePresence` cho lá ra/vào.

`useReducedMotion()` → fade ngắn `120ms`, không bay lá, không scale.

## Phòng chờ (`/room/:code`)

- Grid ghế, mỗi ghế: username, số dư, cờ `lobby_ready`.
- Host: chọn stake (5 nút preset), `Play` (disabled kèm giải thích nếu dưới `2` ready), `Kick` mỗi ghế.
- Mọi người: `Sẵn sàng`, `Rời phòng`.
- Cạnh nút `Play`, luôn hiện: **"Tiền cược trừ ngay khi ván bắt đầu. Rớt mạng không được hoàn."** (01 §Thông báo trước khi cược).
- Prefetch asset bàn chơi khi mount phòng chờ (ảnh lá, âm thanh, font), để `5s` handshake đủ cho mạng chậm.
- Nhận `ready_check` → chờ asset xong → gửi `ready_ack`. Hiện overlay "Đang chuẩn bị bàn…".

## Account (`/account`)

- Số dư lớn, kèm `ArrowUpRight/ArrowDownRight` khi vừa đổi (`balance` event).
- Chart số dư: line chart qua `ChartFrame`.
- Bảng giao dịch: filter theo `reason` (multi-select) và `direction` (in/out), phân trang cursor.
- Đổi username: mở dialog, bắt nhập lại password (D5).
- Đổi password: dialog, nhập password cũ + mới + xác nhận.
- Chọn ngôn ngữ: `en`/`vi`, lưu qua `PATCH /api/me`.

## Loan (`/loan`)

- Chọn gói (`500/1000/2000`) + kỳ hạn (`24h/3d/30d`), hiện `owed` dự kiến — lấy từ `GET /api/loans/products` + tính preview ở client **chỉ để hiển thị**, số thật do server trả.
- Có khoản active: hiện gốc, `due_at`, `owed_now`, đếm ngược tới hạn, `seize_at`, nút `Trả nợ`.
- Quá hạn: banner đỏ "đang bị tính lãi 5%/24h", hiện `forgiven` dự kiến nếu bị siết.

Preview lãi ở client dùng đúng công thức integer của 03 §Công thức. Sai lệch với server thì server thắng, nhưng test phải khớp golden case `500U/24h → 505, 530, 557…`.

## i18n

- `en` là bản chính, `vi` là bản dịch. Thiếu chuỗi `vi` → hiện `en`, không hiện key.
- Chuỗi ở `locales/en.json`, `locales/vi.json`. Không hard-code chữ trong component.
- Server trả **mã lỗi**, FE dịch: `errors.INSUFFICIENT_BALANCE` → "Không đủ U" / "Not enough U".
- Mã lỗi không có bản dịch → hiện mã thô + báo Sentry/console. Không hiện chuỗi rỗng.
- Ngôn ngữ: `users.locale`; chưa đăng nhập → `navigator.language`, không phải `vi` → `en`.
- Số tiền: `Intl.NumberFormat(locale)` + hậu tố `U`: `1,000U` (en), `1.000U` (vi).
- `<html lang>` cập nhật theo ngôn ngữ đang chọn.

## Accessibility

Mức: WCAG 2.1 Level A + 2 tiêu chí AA: tương phản chữ `4.5:1`, focus luôn nhìn thấy được.

### Bàn phím

| Phím | Việc |
|---|---|
| `←` / `→` | Chọn lá trong tay (roving tabindex). |
| `Enter` | Đánh lá đang chọn. |
| `D` | Rút bài. |
| `P` | Pass. |
| `U` | Gọi `UNO`. |
| `C` | Bắt `UNO`. |
| `1`–`4` | Chọn màu `Wild`. |
| `Esc` | Đóng dialog (trừ dialog thách thức). |
| `Tab` / `Shift+Tab` | Chuyển vùng. |

- Phím tắt chỉ chạy khi nút tương ứng đang hiện. Nút `UNO` không hiện thì `U` không làm gì.
- Phím tắt một ký tự (`D`, `P`, `U`, `C`, `1`–`4`) **chỉ chạy khi focus nằm trong khu vực bàn chơi** (`<section ref>` với `onKeyDown`). WCAG 2.1.4 Level A: phím một chữ chạy toàn trang bị kích hoạt nhầm khi người dùng screen reader hay voice control gõ phím.
- `Tab` không bao giờ nhảy ra ngoài dialog đang mở (focus trap).
- Focus trả về lá đã chọn sau khi đóng dialog.

### ARIA

- Nút `UNO` và `Bắt UNO` hiện ra = sự kiện quan trọng → `aria-live="assertive"` cho vùng thông báo riêng. Nút không tồn tại trước đó nên screen reader không tự biết.
- Lượt mới, lá vừa bị đánh, số lá của người khác → `aria-live="polite"`.
- Mọi lá bài có `aria-label` mô tả màu + giá trị. Không dựa vào màu.
- Timer: như trên, không `aria-live` liên tục.

### Khác

- Focus ring: `outline: 2px solid var(--focus); outline-offset: 2px`, không `outline: none` ở bất kỳ đâu.
- Contrast: chữ trên lá bài và nút phải đạt `4.5:1`. Kiểm bằng `axe` trong CI.
- `prefers-reduced-motion` → Framer Motion fade ngắn.

## Chart

Mọi chart đi qua `ChartFrame`:

```tsx
<ChartFrame title="Balance" range="30d" onRangeChange={…}>
  <LineChart …/>
</ChartFrame>
```

- Một bộ màu, một component tooltip, một chỗ sửa.
- Range selector: `Today` (default cho admin), `24h`, `3d`, `7d`, `30d`, `60d`.
- Dữ liệu đã bucket sẵn từ server. Client **không** group raw log.
- Chart admin:
  - Request/time theo endpoint: stacked bar.
  - HTTP code: 100% stacked area + stacked bar.
- Chart người chơi: số dư theo thời gian, line.
- Không animate chart khi `prefers-reduced-motion`.

## Asset

- Lá bài: SVG tự vẽ, đóng gói theo lá (`rank-color.svg`) + một sprite cho 4 màu.
- Âm thanh: rút bài, đánh bài, thắng, cảnh báo `5s`. Có công tắc tắt, mặc định tắt. Không autoplay trước khi người dùng tương tác.
- Font: self-host, không Google Fonts (không chặn render vì GDPR/network).
- Tổng bundle bàn chơi mục tiêu `< 250 KB` gzip, lazy route `/table`.

## Test bắt buộc

| # | Test |
|---|---|
| F1 | `applyEvent` bỏ event duplicate (`seq <= last`), refetch khi thiếu (`seq > last+1`). |
| F2 | Rollback optimistic UI khi server trả lỗi. |
| F3 | Nút `UNO` không render khi `can_call_uno = false`; `U` không làm gì. |
| F4 | Dialog thách thức nhận focus vào `Chấp nhận`, `Esc` không đóng, `Tab` chỉ trong dialog. |
| F5 | Chơi trọn một ván chỉ bằng bàn phím (test Playwright). |
| F6 | `axe` không báo lỗi AA trên `/`, `/login`, `/register`, `/table`, `/account`. |
| F7 | Đổi `vi` → mọi chuỗi đổi, số tiền `1.000U`; thiếu key → hiện `en`. |
| F8 | Reconnect sau `60s`: socket tự nối, snapshot apply, không mất lượt. |
| F9 | `prefers-reduced-motion` → không có animation bay lá. |
