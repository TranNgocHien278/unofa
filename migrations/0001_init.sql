-- UNOFA initial schema. Verbatim from docs/spec/03-data-model.md §DDL.
-- One-way migrations: 0001_init.sql, 0002_*.sql, ...

CREATE TABLE users (
    user_id            BIGINT PRIMARY KEY,
    username           TEXT NOT NULL UNIQUE,
    password_hash      TEXT,
    locale             TEXT,
    balance            BIGINT NOT NULL DEFAULT 0,
    terms_version      INT NOT NULL DEFAULT 0,
    terms_accepted_at  TIMESTAMPTZ,
    login_locked_until TIMESTAMPTZ,
    deleted_at         TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT balance_floor CHECK (user_id = 0 OR balance >= 0),
    CONSTRAINT username_lower CHECK (username = lower(username))
);

CREATE SEQUENCE users_seq START 1;

INSERT INTO users (user_id, username, password_hash, balance)
VALUES (0, 'System', NULL, 0);

CREATE TABLE rooms (
    room_id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code         TEXT NOT NULL UNIQUE,
    host_user_id BIGINT NOT NULL REFERENCES users(user_id),
    stake        BIGINT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open',
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
    status      TEXT NOT NULL,
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
    status          TEXT NOT NULL DEFAULT 'connected',
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
    term               TEXT NOT NULL,
    rate_bps           INT NOT NULL,
    due_at             TIMESTAMPTZ NOT NULL,
    owed_at_settle     BIGINT,
    forgiven_at_seize  BIGINT,
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
    endpoint    TEXT NOT NULL,
    status_code INT NOT NULL,
    ip          INET,
    user_id     BIGINT,
    at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX http_log_at       ON http_log (at);
CREATE INDEX http_log_endpoint ON http_log (endpoint, at);

CREATE TABLE admin_audit (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    admin_username TEXT NOT NULL,
    action         TEXT NOT NULL,
    target_user_id BIGINT,
    amount         BIGINT,
    at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    ip             INET,
    request_id     TEXT NOT NULL
);

CREATE TABLE admin_flags (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind       TEXT NOT NULL,
    detail     JSONB NOT NULL DEFAULT '{}',
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    cleared_at TIMESTAMPTZ
);
CREATE INDEX admin_flags_open ON admin_flags (at DESC) WHERE cleared_at IS NULL;
