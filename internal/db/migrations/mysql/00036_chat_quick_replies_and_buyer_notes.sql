-- +goose Up
CREATE TABLE chat_quick_replies (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(191) NOT NULL,
    content TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    CONSTRAINT fk_chat_quick_replies_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    INDEX idx_chat_quick_replies_account_created (cookie_id, id DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE chat_buyer_notes (
    cookie_id VARCHAR(191) NOT NULL,
    buyer_id VARCHAR(191) NOT NULL,
    content TEXT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (cookie_id, buyer_id),
    CONSTRAINT fk_chat_buyer_notes_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS chat_buyer_notes;
DROP TABLE IF EXISTS chat_quick_replies;
