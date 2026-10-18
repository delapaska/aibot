-- Долги: записи «кто кому сколько должен» глазами одного участника.
-- Контрагент — свободный текст: долг может быть и с человеком, который
-- никогда не писал боту, поэтому на user_id он не ссылается.
CREATE TABLE debts (
    id         UUID        PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (telegram_id) ON DELETE CASCADE,
    direction  TEXT        NOT NULL CHECK (direction IN ('owes_me', 'i_owe')),
    debtor     TEXT        NOT NULL CHECK (debtor <> ''),
    amount     BIGINT      NOT NULL CHECK (amount > 0),
    comment    TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    settled_at TIMESTAMPTZ
);

CREATE INDEX debts_user_created_idx ON debts (user_id, created_at DESC);

-- Месячные лимиты трат по категориям, один на пару (пользователь, категория).
CREATE TABLE budgets (
    user_id       BIGINT NOT NULL REFERENCES users (telegram_id) ON DELETE CASCADE,
    category      TEXT   NOT NULL CHECK (category IN ('food', 'transport', 'housing', 'entertainment', 'health', 'shopping', 'other')),
    monthly_limit BIGINT NOT NULL CHECK (monthly_limit > 0),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, category)
);
