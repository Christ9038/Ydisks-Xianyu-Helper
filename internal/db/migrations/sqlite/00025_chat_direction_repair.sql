-- +goose Up
UPDATE chat_messages SET direction='outgoing', sender_name='我'
WHERE sender_id=cookie_id AND direction='incoming';
UPDATE chat_sessions SET buyer_id=COALESCE((
    SELECT m.sender_id FROM chat_messages m
    WHERE m.cookie_id=chat_sessions.cookie_id AND m.chat_id=chat_sessions.chat_id
      AND m.direction='incoming' AND m.sender_id<>chat_sessions.cookie_id
    ORDER BY m.sent_at DESC,m.id DESC LIMIT 1
), buyer_id);

-- +goose Down
-- Data repair is intentionally irreversible.
