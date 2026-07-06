-- +goose Up
-- +goose StatementBegin
-- 闲鱼管家初始 schema（MySQL 版）。
-- 与 SQLite 00001 对齐，差异：
--   - INTEGER PRIMARY KEY AUTOINCREMENT → BIGINT AUTO_INCREMENT PRIMARY KEY
--   - TEXT 主键/唯一键 → VARCHAR(255)（MySQL 索引长度限制）
--   - BOOLEAN → TINYINT(1)
--   - INSERT OR IGNORE → INSERT IGNORE
--   - CREATE INDEX IF NOT EXISTS → CREATE INDEX（MySQL 不支持 IF NOT EXISTS，靠 goose 版本表防重复）

-- 认证与会话
CREATE TABLE IF NOT EXISTS users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(255) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_active TINYINT(1) DEFAULT 1,
    is_admin TINYINT(1) DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sessions (
    session_id VARCHAR(255) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    username VARCHAR(255) NOT NULL,
    is_admin TINYINT(1) DEFAULT 0,
    expires_at BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

CREATE TABLE IF NOT EXISTS captcha_codes (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    session_id VARCHAR(255) NOT NULL,
    code VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 账号（闲鱼 cookie）
CREATE TABLE IF NOT EXISTS cookies (
    id VARCHAR(255) PRIMARY KEY,
    value TEXT NOT NULL,
    user_id BIGINT NOT NULL,
    auto_confirm TINYINT(1) DEFAULT 1,
    remark TEXT,
    pause_duration INTEGER DEFAULT 10,
    username VARCHAR(255),
    password TEXT,
    show_browser TINYINT(1) DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_cookies_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_cookies_user_id ON cookies(user_id);

CREATE TABLE IF NOT EXISTS cookie_status (
    cookie_id VARCHAR(255) PRIMARY KEY,
    enabled TINYINT(1) DEFAULT 1,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_cookie_status_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 关键字回复
CREATE TABLE IF NOT EXISTS keywords (
    cookie_id VARCHAR(255),
    keyword TEXT,
    reply TEXT,
    item_id VARCHAR(255),
    type VARCHAR(32) DEFAULT 'text',
    image_url TEXT,
    CONSTRAINT fk_keywords_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_keywords_cookie_id ON keywords(cookie_id);

-- AI 回复
CREATE TABLE IF NOT EXISTS ai_reply_settings (
    cookie_id VARCHAR(255) PRIMARY KEY,
    ai_enabled TINYINT(1) DEFAULT 0,
    model_name VARCHAR(255) DEFAULT 'qwen-plus',
    api_key TEXT,
    base_url VARCHAR(512) DEFAULT 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    max_discount_percent INTEGER DEFAULT 10,
    max_discount_amount INTEGER DEFAULT 100,
    max_bargain_rounds INTEGER DEFAULT 3,
    custom_prompts TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_ai_reply_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ai_conversations (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(255) NOT NULL,
    chat_id VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    item_id VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL,
    content TEXT NOT NULL,
    intent VARCHAR(255),
    bargain_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_ai_conv_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 卡券
CREATE TABLE IF NOT EXISTS cards (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(32) NOT NULL,
    api_config TEXT,
    text_content TEXT,
    data_content LONGTEXT,
    image_url TEXT,
    description TEXT,
    enabled TINYINT(1) DEFAULT 1,
    delay_seconds INTEGER DEFAULT 0,
    is_multi_spec TINYINT(1) DEFAULT 0,
    spec_name VARCHAR(255),
    spec_value VARCHAR(255),
    user_id BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_cards_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_cards_user_id ON cards(user_id);

CREATE TABLE IF NOT EXISTS delivery_rules (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    keyword VARCHAR(255) NOT NULL,
    card_id BIGINT NOT NULL,
    delivery_count INTEGER DEFAULT 1,
    enabled TINYINT(1) DEFAULT 1,
    description TEXT,
    delivery_times INTEGER DEFAULT 0,
    user_id BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_delivery_rules_card FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE,
    CONSTRAINT fk_delivery_rules_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_delivery_rules_user_id ON delivery_rules(user_id);
CREATE INDEX idx_delivery_rules_card_id ON delivery_rules(card_id);

-- 订单
CREATE TABLE IF NOT EXISTS orders (
    order_id VARCHAR(255) PRIMARY KEY,
    item_id VARCHAR(255),
    buyer_id VARCHAR(255),
    spec_name TEXT,
    spec_value TEXT,
    quantity VARCHAR(64),
    amount VARCHAR(64),
    order_status VARCHAR(64) DEFAULT 'unknown',
    cookie_id VARCHAR(255),
    is_bargain INTEGER DEFAULT 0,
    receiver_name TEXT,
    receiver_phone TEXT,
    receiver_address TEXT,
    receiver_city TEXT,
    version INTEGER DEFAULT 1,
    chat_id VARCHAR(255),
    system_shipped INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_orders_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_orders_cookie_id ON orders(cookie_id);

-- 商品信息
CREATE TABLE IF NOT EXISTS item_info (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(255) NOT NULL,
    item_id VARCHAR(255) NOT NULL,
    item_title TEXT,
    item_description TEXT,
    item_category VARCHAR(255),
    item_price VARCHAR(64),
    item_detail LONGTEXT,
    is_multi_spec TINYINT(1) DEFAULT 0,
    multi_quantity_delivery TINYINT(1) DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_item_info_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    UNIQUE KEY uk_item_info_cookie_item (cookie_id, item_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 默认回复
CREATE TABLE IF NOT EXISTS default_replies (
    cookie_id VARCHAR(255) PRIMARY KEY,
    enabled TINYINT(1) DEFAULT 0,
    reply_content TEXT,
    reply_image_url TEXT,
    reply_once TINYINT(1) DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_default_replies_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- WS 原始消息记录
CREATE TABLE IF NOT EXISTS ws_messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(255) NOT NULL,
    direction VARCHAR(16) NOT NULL DEFAULT 'in',
    raw_text LONGTEXT,
    parsed_json LONGTEXT,
    message_kind VARCHAR(64) NOT NULL DEFAULT '',
    parse_status VARCHAR(32) NOT NULL DEFAULT 'raw',
    error TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_ws_messages_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_ws_messages_cookie_id_created_at ON ws_messages(cookie_id, created_at DESC);

-- 指定商品回复
CREATE TABLE IF NOT EXISTS item_replay (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    item_id VARCHAR(255) NOT NULL,
    cookie_id VARCHAR(255) NOT NULL,
    reply_content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_item_replay_cookie_item ON item_replay(cookie_id, item_id);

-- 默认回复记录（防重复）
CREATE TABLE IF NOT EXISTS default_reply_records (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(255) NOT NULL,
    chat_id VARCHAR(255) NOT NULL,
    replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_default_reply_records (cookie_id, chat_id),
    CONSTRAINT fk_default_reply_records_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 通知
CREATE TABLE IF NOT EXISTS notification_channels (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(32) NOT NULL,
    config LONGTEXT NOT NULL,
    enabled TINYINT(1) DEFAULT 1,
    user_id BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_notif_channels_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS message_notifications (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(255) NOT NULL,
    channel_id BIGINT NOT NULL,
    enabled TINYINT(1) DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_message_notifications (cookie_id, channel_id),
    CONSTRAINT fk_msg_notif_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    CONSTRAINT fk_msg_notif_channel FOREIGN KEY (channel_id) REFERENCES notification_channels(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 系统设置
CREATE TABLE IF NOT EXISTS system_settings (
    `key` VARCHAR(255) PRIMARY KEY,
    value LONGTEXT NOT NULL,
    description TEXT,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 用户设置
CREATE TABLE IF NOT EXISTS user_settings (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    `key` VARCHAR(255) NOT NULL,
    value LONGTEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_user_settings (user_id, `key`),
    CONSTRAINT fk_user_settings_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 默认系统设置（不含管理员口令——由 init-admin CLI 初始化）
INSERT IGNORE INTO system_settings (`key`, value, description) VALUES
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
