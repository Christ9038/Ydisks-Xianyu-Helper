-- +goose Up
-- +goose StatementBegin
-- 闲鱼管家初始 schema（PostgreSQL 版）。
-- 与 SQLite 00001 对齐，差异：
--   - INTEGER PRIMARY KEY AUTOINCREMENT → BIGSERIAL PRIMARY KEY
--   - BOOLEAN 原生支持（无需 boolToInt，但 Go 代码传 0/1 也兼容）
--   - INSERT OR IGNORE → ON CONFLICT DO NOTHING（业务代码用方言适配器）
--   - CURRENT_TIMESTAMP 一致；updated_at 不设 ON UPDATE（业务代码手动 SET）

-- 认证与会话
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    is_admin BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    username TEXT NOT NULL,
    is_admin BOOLEAN DEFAULT FALSE,
    expires_at BIGINT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);

CREATE TABLE IF NOT EXISTS captcha_codes (
    id BIGSERIAL PRIMARY KEY,
    session_id TEXT NOT NULL,
    code TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 账号（闲鱼 cookie）
CREATE TABLE IF NOT EXISTS cookies (
    id TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    auto_confirm BOOLEAN DEFAULT TRUE,
    remark TEXT DEFAULT '',
    pause_duration INTEGER DEFAULT 10,
    username TEXT DEFAULT '',
    password TEXT DEFAULT '',
    show_browser BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_cookies_user_id ON cookies(user_id);

CREATE TABLE IF NOT EXISTS cookie_status (
    cookie_id TEXT PRIMARY KEY REFERENCES cookies(id) ON DELETE CASCADE,
    enabled BOOLEAN DEFAULT TRUE,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 关键字回复
CREATE TABLE IF NOT EXISTS keywords (
    cookie_id TEXT REFERENCES cookies(id) ON DELETE CASCADE,
    keyword TEXT,
    reply TEXT,
    item_id TEXT,
    type TEXT DEFAULT 'text',
    image_url TEXT
);
CREATE INDEX IF NOT EXISTS idx_keywords_cookie_id ON keywords(cookie_id);

-- AI 回复
CREATE TABLE IF NOT EXISTS ai_reply_settings (
    cookie_id TEXT PRIMARY KEY REFERENCES cookies(id) ON DELETE CASCADE,
    ai_enabled BOOLEAN DEFAULT FALSE,
    model_name TEXT DEFAULT 'qwen-plus',
    api_key TEXT,
    base_url TEXT DEFAULT 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    max_discount_percent INTEGER DEFAULT 10,
    max_discount_amount INTEGER DEFAULT 100,
    max_bargain_rounds INTEGER DEFAULT 3,
    custom_prompts TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ai_conversations (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    intent TEXT,
    bargain_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 卡券
CREATE TABLE IF NOT EXISTS cards (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('api', 'text', 'data', 'image')),
    api_config TEXT,
    text_content TEXT,
    data_content TEXT,
    image_url TEXT,
    description TEXT,
    enabled BOOLEAN DEFAULT TRUE,
    delay_seconds INTEGER DEFAULT 0,
    is_multi_spec BOOLEAN DEFAULT FALSE,
    spec_name TEXT,
    spec_value TEXT,
    user_id BIGINT NOT NULL DEFAULT 1 REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_cards_user_id ON cards(user_id);

CREATE TABLE IF NOT EXISTS delivery_rules (
    id BIGSERIAL PRIMARY KEY,
    keyword TEXT NOT NULL,
    card_id INTEGER NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    delivery_count INTEGER DEFAULT 1,
    enabled BOOLEAN DEFAULT TRUE,
    description TEXT,
    delivery_times INTEGER DEFAULT 0,
    user_id BIGINT NOT NULL DEFAULT 1 REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_delivery_rules_user_id ON delivery_rules(user_id);
CREATE INDEX IF NOT EXISTS idx_delivery_rules_card_id ON delivery_rules(card_id);

-- 订单
CREATE TABLE IF NOT EXISTS orders (
    order_id TEXT PRIMARY KEY,
    item_id TEXT,
    buyer_id TEXT,
    spec_name TEXT,
    spec_value TEXT,
    quantity TEXT,
    amount TEXT,
    order_status TEXT DEFAULT 'unknown',
    cookie_id TEXT REFERENCES cookies(id) ON DELETE CASCADE,
    is_bargain INTEGER DEFAULT 0,
    receiver_name TEXT DEFAULT '',
    receiver_phone TEXT DEFAULT '',
    receiver_address TEXT DEFAULT '',
    receiver_city TEXT DEFAULT '',
    version INTEGER DEFAULT 1,
    chat_id TEXT DEFAULT '',
    system_shipped INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_orders_cookie_id ON orders(cookie_id);

-- 商品信息
CREATE TABLE IF NOT EXISTS item_info (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    item_id TEXT NOT NULL,
    item_title TEXT,
    item_description TEXT,
    item_category TEXT,
    item_price TEXT,
    item_detail TEXT,
    is_multi_spec BOOLEAN DEFAULT FALSE,
    multi_quantity_delivery BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cookie_id, item_id)
);

-- 默认回复
CREATE TABLE IF NOT EXISTS default_replies (
    cookie_id TEXT PRIMARY KEY REFERENCES cookies(id) ON DELETE CASCADE,
    enabled BOOLEAN DEFAULT FALSE,
    reply_content TEXT,
    reply_image_url TEXT,
    reply_once BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- WS 原始消息记录
CREATE TABLE IF NOT EXISTS ws_messages (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    direction TEXT NOT NULL DEFAULT 'in',
    raw_text TEXT NOT NULL DEFAULT '',
    parsed_json TEXT NOT NULL DEFAULT '',
    message_kind TEXT NOT NULL DEFAULT '',
    parse_status TEXT NOT NULL DEFAULT 'raw',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_ws_messages_cookie_id_created_at ON ws_messages(cookie_id, created_at DESC);

-- 指定商品回复
CREATE TABLE IF NOT EXISTS item_replay (
    id BIGSERIAL PRIMARY KEY,
    item_id TEXT NOT NULL,
    cookie_id TEXT NOT NULL,
    reply_content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_item_replay_cookie_item ON item_replay(cookie_id, item_id);

-- 默认回复记录（防重复）
CREATE TABLE IF NOT EXISTS default_reply_records (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL,
    replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cookie_id, chat_id)
);

-- 通知
CREATE TABLE IF NOT EXISTS notification_channels (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('qq','ding_talk','dingtalk','feishu','lark','bark','email','webhook','wechat','telegram')),
    config TEXT NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    user_id BIGINT NOT NULL DEFAULT 1 REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS message_notifications (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cookie_id, channel_id)
);

-- 系统设置（key 是 PG 保留字，需双引号）
CREATE TABLE IF NOT EXISTS system_settings (
    "key" TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 用户设置
CREATE TABLE IF NOT EXISTS user_settings (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "key" TEXT NOT NULL,
    value TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, "key")
);

-- 默认系统设置（不含管理员口令——由 init-admin CLI 初始化）
INSERT INTO system_settings ("key", value, description) VALUES
    ('theme_color', 'blue', '主题颜色'),
    ('registration_enabled', 'true', '是否开启用户注册'),
    ('show_default_login_info', 'true', '是否显示默认登录信息'),
    ('login_captcha_enabled', 'true', '登录滑动验证码开关'),
    ('smtp_server', '', 'SMTP服务器地址'),
    ('smtp_port', '587', 'SMTP端口'),
    ('smtp_user', '', 'SMTP登录用户名（发件邮箱）'),
    ('smtp_password', '', 'SMTP登录密码/授权码'),
    ('smtp_from', '', '发件人显示名（留空则使用用户名）'),
    ('smtp_use_tls', 'true', '是否启用TLS'),
    ('smtp_use_ssl', 'false', '是否启用SSL'),
    ('qq_reply_secret_key', '', 'QQ回复消息API秘钥（必须显式配置，无默认值）'),
    ('item_sync_enabled', 'true', '是否启用定时自动同步商品'),
    ('item_sync_interval', '600', '商品同步间隔时间（秒）'),
    ('item_sync_max_pages', '5', '每次最多同步的页数')
ON CONFLICT ("key") DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_settings;
DROP TABLE IF EXISTS system_settings;
DROP TABLE IF EXISTS message_notifications;
DROP TABLE IF EXISTS notification_channels;
DROP TABLE IF EXISTS default_reply_records;
DROP TABLE IF EXISTS item_replay;
DROP TABLE IF EXISTS default_replies;
DROP TABLE IF EXISTS item_info;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS delivery_rules;
DROP TABLE IF EXISTS cards;
DROP TABLE IF EXISTS ai_conversations;
DROP TABLE IF EXISTS ai_reply_settings;
DROP TABLE IF EXISTS keywords;
DROP TABLE IF EXISTS cookie_status;
DROP TABLE IF EXISTS cookies;
DROP TABLE IF EXISTS captcha_codes;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
