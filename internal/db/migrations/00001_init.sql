-- +goose Up
-- +goose StatementBegin
-- 闲鱼管家初始 schema。
-- 把历史上 12+ 处运行时 ALTER TABLE 补齐到 CREATE，并修复 schema 不一致：
--   - orders.system_shipped：原 CREATE 缺失却被 insert_or_update_order 引用 → 补齐
--   - orders.receiver_city：原为运行时补齐 → 补齐
--   - delivery_rules.user_id：原 CREATE 缺失却被 create_delivery_rule 引用 → 补齐
--   - cards.user_id / delay_seconds / image_url：历史 ALTER → 补齐
--   - keywords.item_id / type / image_url：历史 ALTER → 补齐
--   - item_info.multi_quantity_delivery：历史 ALTER → 补齐
--   - default_replies.reply_once / reply_image_url：历史 ALTER → 补齐
--   - users.is_admin：历史 ALTER → 补齐
-- 注：老库导入对齐在 Phase 6 单独脚本处理；本迁移面向全新库。

-- 认证与会话
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
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
    user_id INTEGER NOT NULL,
    username TEXT NOT NULL,
    is_admin INTEGER DEFAULT 0,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);

CREATE TABLE IF NOT EXISTS email_verifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL,
    code TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    used BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS captcha_codes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    code TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 账号（闲鱼 cookie）
CREATE TABLE IF NOT EXISTS cookies (
    id TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    auto_confirm INTEGER DEFAULT 1,
    remark TEXT DEFAULT '',
    pause_duration INTEGER DEFAULT 10,
    username TEXT DEFAULT '',
    password TEXT DEFAULT '',
    show_browser INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_cookies_user_id ON cookies(user_id);

CREATE TABLE IF NOT EXISTS cookie_status (
    cookie_id TEXT PRIMARY KEY,
    enabled BOOLEAN DEFAULT TRUE,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

-- 关键字回复
CREATE TABLE IF NOT EXISTS keywords (
    cookie_id TEXT,
    keyword TEXT,
    reply TEXT,
    item_id TEXT,
    type TEXT DEFAULT 'text',
    image_url TEXT,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_keywords_cookie_id ON keywords(cookie_id);

-- AI 回复
CREATE TABLE IF NOT EXISTS ai_reply_settings (
    cookie_id TEXT PRIMARY KEY,
    ai_enabled BOOLEAN DEFAULT FALSE,
    model_name TEXT DEFAULT 'qwen-plus',
    api_key TEXT,
    base_url TEXT DEFAULT 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    max_discount_percent INTEGER DEFAULT 10,
    max_discount_amount INTEGER DEFAULT 100,
    max_bargain_rounds INTEGER DEFAULT 3,
    custom_prompts TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS ai_conversations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    intent TEXT,
    bargain_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS ai_item_cache (
    item_id TEXT PRIMARY KEY,
    data TEXT NOT NULL,
    price REAL,
    description TEXT,
    last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 卡券与发货规则
CREATE TABLE IF NOT EXISTS cards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
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
    user_id INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users (id)
);
CREATE INDEX IF NOT EXISTS idx_cards_user_id ON cards(user_id);

CREATE TABLE IF NOT EXISTS delivery_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    keyword TEXT NOT NULL,
    card_id INTEGER NOT NULL,
    delivery_count INTEGER DEFAULT 1,
    enabled BOOLEAN DEFAULT TRUE,
    description TEXT,
    delivery_times INTEGER DEFAULT 0,
    user_id INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id)
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
    cookie_id TEXT,
    is_bargain INTEGER DEFAULT 0,
    receiver_name TEXT DEFAULT '',
    receiver_phone TEXT DEFAULT '',
    receiver_address TEXT DEFAULT '',
    receiver_city TEXT DEFAULT '',
    version INTEGER DEFAULT 1,
    chat_id TEXT DEFAULT '',
    system_shipped INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_orders_cookie_id ON orders(cookie_id);

-- 商品信息
CREATE TABLE IF NOT EXISTS item_info (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
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
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    UNIQUE(cookie_id, item_id)
);

-- 默认回复
CREATE TABLE IF NOT EXISTS default_replies (
    cookie_id TEXT PRIMARY KEY,
    enabled BOOLEAN DEFAULT FALSE,
    reply_content TEXT,
    reply_image_url TEXT,
    reply_once BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

-- WS 原始消息记录
CREATE TABLE IF NOT EXISTS ws_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    direction TEXT NOT NULL DEFAULT 'in',
    raw_text TEXT NOT NULL DEFAULT '',
    parsed_json TEXT NOT NULL DEFAULT '',
    message_kind TEXT NOT NULL DEFAULT '',
    parse_status TEXT NOT NULL DEFAULT 'raw',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_ws_messages_cookie_id_created_at ON ws_messages(cookie_id, created_at DESC);

-- 指定商品回复
CREATE TABLE IF NOT EXISTS item_replay (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id TEXT NOT NULL,
    cookie_id TEXT NOT NULL,
    reply_content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_item_replay_cookie_item ON item_replay(cookie_id, item_id);

-- 默认回复记录（防重复）
CREATE TABLE IF NOT EXISTS default_reply_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cookie_id, chat_id),
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

-- 通知
CREATE TABLE IF NOT EXISTS notification_channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('qq','ding_talk','dingtalk','feishu','lark','bark','email','webhook','wechat','telegram')),
    config TEXT NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    user_id INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS message_notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    channel_id INTEGER NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    FOREIGN KEY (channel_id) REFERENCES notification_channels(id) ON DELETE CASCADE,
    UNIQUE(cookie_id, channel_id)
);

-- 系统设置
CREATE TABLE IF NOT EXISTS system_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 用户设置
CREATE TABLE IF NOT EXISTS user_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE(user_id, key)
);

-- 风控日志
CREATE TABLE IF NOT EXISTS risk_control_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    event_type TEXT NOT NULL DEFAULT 'slider_captcha',
    event_description TEXT,
    processing_result TEXT,
    processing_status TEXT DEFAULT 'processing',
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

-- 默认系统设置（不含管理员口令——由 init-admin CLI 初始化）
INSERT OR IGNORE INTO system_settings (key, value, description) VALUES
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
    ('item_sync_max_pages', '5', '每次最多同步的页数');
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS risk_control_logs;
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
DROP TABLE IF EXISTS ai_item_cache;
DROP TABLE IF EXISTS ai_conversations;
DROP TABLE IF EXISTS ai_reply_settings;
DROP TABLE IF EXISTS keywords;
DROP TABLE IF EXISTS cookie_status;
DROP TABLE IF EXISTS cookies;
DROP TABLE IF EXISTS captcha_codes;
DROP TABLE IF EXISTS email_verifications;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
