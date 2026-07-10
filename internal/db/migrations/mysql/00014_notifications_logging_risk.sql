-- +goose Up
DROP PROCEDURE IF EXISTS add_xianyu_column_if_missing;
-- +goose StatementBegin
CREATE PROCEDURE add_xianyu_column_if_missing(
    IN p_table_name VARCHAR(64),
    IN p_column_name VARCHAR(64),
    IN p_column_definition TEXT
)
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM information_schema.COLUMNS
         WHERE TABLE_SCHEMA = DATABASE()
           AND TABLE_NAME = p_table_name
           AND COLUMN_NAME = p_column_name
    ) THEN
        SET @ddl = CONCAT('ALTER TABLE `', p_table_name, '` ADD COLUMN ', p_column_definition);
        PREPARE stmt FROM @ddl;
        EXECUTE stmt;
        DEALLOCATE PREPARE stmt;
    END IF;
END;
-- +goose StatementEnd

CALL add_xianyu_column_if_missing('notification_channels', 'event_types', CONCAT('`event_types` VARCHAR(512) NOT NULL DEFAULT ', QUOTE('')));
CALL add_xianyu_column_if_missing('message_notifications', 'event_types', CONCAT('`event_types` VARCHAR(512) NOT NULL DEFAULT ', QUOTE('')));

CALL add_xianyu_column_if_missing('scheduled_cookies_refresh_log', 'step_details', '`step_details` TEXT');
CALL add_xianyu_column_if_missing('scheduled_cookies_refresh_log', 'renew_method', CONCAT('`renew_method` VARCHAR(64) NOT NULL DEFAULT ', QUOTE('')));
CALL add_xianyu_column_if_missing('scheduled_cookies_refresh_log', 'duration_ms', '`duration_ms` BIGINT NOT NULL DEFAULT 0');
CALL add_xianyu_column_if_missing('scheduled_cookies_refresh_log', 'request_count', '`request_count` INT NOT NULL DEFAULT 0');
CALL add_xianyu_column_if_missing('scheduled_cookies_refresh_log', 'response_content', '`response_content` MEDIUMTEXT');

CALL add_xianyu_column_if_missing('scheduled_login_renew_log', 'step_details', '`step_details` TEXT');
CALL add_xianyu_column_if_missing('scheduled_login_renew_log', 'renew_method', CONCAT('`renew_method` VARCHAR(64) NOT NULL DEFAULT ', QUOTE('')));
CALL add_xianyu_column_if_missing('scheduled_login_renew_log', 'duration_ms', '`duration_ms` BIGINT NOT NULL DEFAULT 0');
CALL add_xianyu_column_if_missing('scheduled_login_renew_log', 'request_count', '`request_count` INT NOT NULL DEFAULT 0');
CALL add_xianyu_column_if_missing('scheduled_login_renew_log', 'response_content', '`response_content` MEDIUMTEXT');
CALL add_xianyu_column_if_missing('scheduled_login_renew_log', 'updated_cookie_count', '`updated_cookie_count` INT NOT NULL DEFAULT 0');

CALL add_xianyu_column_if_missing('scheduled_api_cookie_renew_log', 'step_details', '`step_details` TEXT');
CALL add_xianyu_column_if_missing('scheduled_api_cookie_renew_log', 'renew_method', CONCAT('`renew_method` VARCHAR(64) NOT NULL DEFAULT ', QUOTE('')));
CALL add_xianyu_column_if_missing('scheduled_api_cookie_renew_log', 'duration_ms', '`duration_ms` BIGINT NOT NULL DEFAULT 0');
CALL add_xianyu_column_if_missing('scheduled_api_cookie_renew_log', 'request_count', '`request_count` INT NOT NULL DEFAULT 0');

DROP PROCEDURE IF EXISTS add_xianyu_column_if_missing;

CREATE TABLE IF NOT EXISTS risk_control_logs (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    cookie_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(64) NOT NULL DEFAULT 'slider_captcha',
    event_description TEXT,
    processing_result TEXT,
    processing_status VARCHAR(32) NOT NULL DEFAULT 'processing',
    captcha_engine VARCHAR(32) NOT NULL DEFAULT '',
    error_message TEXT,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_risk_control_logs_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO system_settings (`key`, value, description) VALUES
    ('log_level', 'info', '日志输出等级：debug/info/warn/error'),
    ('log_format', 'text', '日志输出格式：text/json'),
    ('renewal_log_retention_days', '10', '续期日志保留天数');

-- +goose Down
DROP TABLE IF EXISTS risk_control_logs;
DELETE FROM system_settings WHERE `key` IN ('log_level', 'log_format', 'renewal_log_retention_days');

DROP PROCEDURE IF EXISTS drop_xianyu_column_if_exists;
-- +goose StatementBegin
CREATE PROCEDURE drop_xianyu_column_if_exists(
    IN p_table_name VARCHAR(64),
    IN p_column_name VARCHAR(64)
)
BEGIN
    IF EXISTS (
        SELECT 1
          FROM information_schema.COLUMNS
         WHERE TABLE_SCHEMA = DATABASE()
           AND TABLE_NAME = p_table_name
           AND COLUMN_NAME = p_column_name
    ) THEN
        SET @ddl = CONCAT('ALTER TABLE `', p_table_name, '` DROP COLUMN `', p_column_name, '`');
        PREPARE stmt FROM @ddl;
        EXECUTE stmt;
        DEALLOCATE PREPARE stmt;
    END IF;
END;
-- +goose StatementEnd

CALL drop_xianyu_column_if_exists('notification_channels', 'event_types');
CALL drop_xianyu_column_if_exists('message_notifications', 'event_types');
CALL drop_xianyu_column_if_exists('scheduled_cookies_refresh_log', 'step_details');
CALL drop_xianyu_column_if_exists('scheduled_cookies_refresh_log', 'renew_method');
CALL drop_xianyu_column_if_exists('scheduled_cookies_refresh_log', 'duration_ms');
CALL drop_xianyu_column_if_exists('scheduled_cookies_refresh_log', 'request_count');
CALL drop_xianyu_column_if_exists('scheduled_cookies_refresh_log', 'response_content');
CALL drop_xianyu_column_if_exists('scheduled_login_renew_log', 'step_details');
CALL drop_xianyu_column_if_exists('scheduled_login_renew_log', 'renew_method');
CALL drop_xianyu_column_if_exists('scheduled_login_renew_log', 'duration_ms');
CALL drop_xianyu_column_if_exists('scheduled_login_renew_log', 'request_count');
CALL drop_xianyu_column_if_exists('scheduled_login_renew_log', 'response_content');
CALL drop_xianyu_column_if_exists('scheduled_login_renew_log', 'updated_cookie_count');
CALL drop_xianyu_column_if_exists('scheduled_api_cookie_renew_log', 'step_details');
CALL drop_xianyu_column_if_exists('scheduled_api_cookie_renew_log', 'renew_method');
CALL drop_xianyu_column_if_exists('scheduled_api_cookie_renew_log', 'duration_ms');
CALL drop_xianyu_column_if_exists('scheduled_api_cookie_renew_log', 'request_count');

DROP PROCEDURE IF EXISTS drop_xianyu_column_if_exists;
