-- +goose Up
UPDATE chat_messages SET read_status=2, read_at=CASE WHEN read_at=0 THEN (EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) * 1000)::BIGINT ELSE read_at END
WHERE direction='incoming' AND read_status=0;

-- +goose Down
-- No safe reverse: the previous schema had no per-message read boundary.
