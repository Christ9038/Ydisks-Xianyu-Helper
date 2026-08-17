-- +goose Up
-- Messages already stored before message-level read tracking was introduced
-- are history snapshots, not newly received unread messages.
UPDATE chat_messages SET read_status=2, read_at=CASE WHEN read_at=0 THEN strftime('%s','now') * 1000 ELSE read_at END
WHERE direction='incoming' AND read_status=0;

-- +goose Down
-- No safe reverse: the previous schema had no per-message read boundary.
