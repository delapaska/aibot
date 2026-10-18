-- Семейные пользователи: все, кто писал боту (панель показывает их всех).
CREATE TABLE users (
    telegram_id BIGINT      PRIMARY KEY,
    first_name  TEXT        NOT NULL DEFAULT '',
    username    TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Траты. Деньги хранятся в копейках (BIGINT), чтобы исключить ошибки
-- округления. CHECK-ограничения дублируют валидацию домена и служат
-- последней линией защиты, если её обойти.
CREATE TABLE expenses (
    id         UUID        PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (telegram_id) ON DELETE CASCADE,
    amount     BIGINT      NOT NULL CHECK (amount > 0),
    category   TEXT        NOT NULL CHECK (category IN ('food', 'transport', 'housing', 'entertainment', 'health', 'shopping', 'other')),
    spent_at   DATE        NOT NULL,
    comment    TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX expenses_user_spent_idx    ON expenses (user_id, spent_at DESC);
CREATE INDEX expenses_user_category_idx ON expenses (user_id, category);
