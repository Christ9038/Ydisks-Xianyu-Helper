-- +goose Up
-- 扩展通知渠道类型约束，允许扫码创建的飞书应用机器人渠道。
ALTER TABLE notification_channels DROP CONSTRAINT IF EXISTS notification_channels_type_check;
ALTER TABLE notification_channels ADD CONSTRAINT notification_channels_type_check CHECK (type IN ('qq','ding_talk','dingtalk','feishu','feishu_app','lark','bark','email','webhook','wechat','telegram'));

-- +goose Down
DELETE FROM notification_channels WHERE type = 'feishu_app';
ALTER TABLE notification_channels DROP CONSTRAINT IF EXISTS notification_channels_type_check;
ALTER TABLE notification_channels ADD CONSTRAINT notification_channels_type_check CHECK (type IN ('qq','ding_talk','dingtalk','feishu','lark','bark','email','webhook','wechat','telegram'));
