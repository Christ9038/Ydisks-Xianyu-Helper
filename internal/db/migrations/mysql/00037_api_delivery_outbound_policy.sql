-- +goose Up
INSERT INTO system_settings (`key`, value, description)
VALUES ('outbound_http_public_only', 'false', '是否限制用户配置的 HTTP 出站请求只能访问公网地址')
ON DUPLICATE KEY UPDATE `key`=`key`;
-- +goose Down
DELETE FROM system_settings WHERE `key` = 'outbound_http_public_only';
