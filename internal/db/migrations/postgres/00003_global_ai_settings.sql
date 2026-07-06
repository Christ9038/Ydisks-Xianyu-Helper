-- +goose Up
INSERT INTO system_settings ("key", value, description) VALUES
    ('ai_api_url', 'https://dashscope.aliyuncs.com/compatible-mode/v1', 'AI OpenAI兼容API地址'),
    ('ai_api_key', '', 'AI API Key'),
    ('ai_model', 'qwen-plus', 'AI模型名称')
ON CONFLICT ("key") DO NOTHING;

-- +goose Down
DELETE FROM system_settings WHERE "key" IN ('ai_api_url', 'ai_api_key', 'ai_model');
