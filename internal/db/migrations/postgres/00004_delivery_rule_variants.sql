-- +goose Up
-- +goose StatementBegin
ALTER TABLE delivery_rules ADD COLUMN cookie_id TEXT NOT NULL DEFAULT '';
ALTER TABLE delivery_rules ADD COLUMN item_id TEXT NOT NULL DEFAULT '';

CREATE TABLE delivery_rule_variants (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL REFERENCES delivery_rules(id) ON DELETE CASCADE,
    spec_name TEXT NOT NULL DEFAULT '',
    spec_value TEXT NOT NULL DEFAULT '',
    card_id BIGINT NOT NULL REFERENCES cards(id) ON DELETE RESTRICT,
    delivery_count INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(rule_id, spec_name, spec_value)
);

CREATE INDEX IF NOT EXISTS idx_delivery_rules_cookie_item ON delivery_rules(cookie_id, item_id);
CREATE INDEX IF NOT EXISTS idx_delivery_rule_variants_rule ON delivery_rule_variants(rule_id);
CREATE INDEX IF NOT EXISTS idx_delivery_rule_variants_card ON delivery_rule_variants(card_id);

-- 旧规则迁移为一条默认或指定规格映射。cards 上的规格字段仅保留兼容读取。
INSERT INTO delivery_rule_variants
    (rule_id, spec_name, spec_value, card_id, delivery_count, enabled)
SELECT dr.id,
       CASE WHEN COALESCE(c.is_multi_spec, FALSE) = TRUE THEN COALESCE(c.spec_name, '') ELSE '' END,
       CASE WHEN COALESCE(c.is_multi_spec, FALSE) = TRUE THEN COALESCE(c.spec_value, '') ELSE '' END,
       dr.card_id,
       CASE WHEN dr.delivery_count > 0 THEN dr.delivery_count ELSE 1 END,
       dr.enabled
FROM delivery_rules dr
JOIN cards c ON c.id = dr.card_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE delivery_rule_variants;
DROP INDEX IF EXISTS idx_delivery_rules_cookie_item;
ALTER TABLE delivery_rules DROP COLUMN item_id;
ALTER TABLE delivery_rules DROP COLUMN cookie_id;
-- +goose StatementEnd
