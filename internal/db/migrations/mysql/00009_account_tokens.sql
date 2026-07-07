-- +goose Up
-- +goose StatementBegin
-- 账号登录凭证缓存：跨进程重启复用 device_id（避免阿里端设备绑定/风控），
-- 缓存 accessToken 让短重启与瞬时 mtop 失败可回退到上次有效 token，不掉线。
CREATE TABLE IF NOT EXISTS account_tokens (
    cookie_id     VARCHAR(255) PRIMARY KEY,
    device_id     TEXT NOT NULL,
    access_token  TEXT NOT NULL,
    expire_at     BIGINT NOT NULL DEFAULT 0,
    updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_account_tokens_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS account_tokens;
-- +goose StatementEnd
