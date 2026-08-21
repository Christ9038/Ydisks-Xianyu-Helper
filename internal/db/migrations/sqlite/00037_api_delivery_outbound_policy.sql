-- +goose Up
INSERT OR IGNORE INTO system_settings (key, value, description)
VALUES ('outbound_http_public_only', 'false', '是否限制用户配置的 HTTP 出站请求只能访问公网地址');
-- +goose Down
DELETE FROM system_settings WHERE key = 'outbound_http_public_only';
