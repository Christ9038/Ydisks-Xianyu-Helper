-- +goose Up
CREATE TABLE keywords_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT,
    keyword TEXT,
    reply TEXT,
    item_id TEXT,
    type TEXT DEFAULT 'text',
    image_url TEXT,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

INSERT INTO keywords_new (cookie_id, keyword, reply, item_id, type, image_url)
SELECT cookie_id, keyword, reply, item_id, type, image_url FROM keywords;

DROP TABLE keywords;
ALTER TABLE keywords_new RENAME TO keywords;
CREATE INDEX IF NOT EXISTS idx_keywords_cookie_id ON keywords(cookie_id);

-- +goose Down
CREATE TABLE keywords_old (
    cookie_id TEXT,
    keyword TEXT,
    reply TEXT,
    item_id TEXT,
    type TEXT DEFAULT 'text',
    image_url TEXT,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);

INSERT INTO keywords_old (cookie_id, keyword, reply, item_id, type, image_url)
SELECT cookie_id, keyword, reply, item_id, type, image_url FROM keywords;

DROP TABLE keywords;
ALTER TABLE keywords_old RENAME TO keywords;
CREATE INDEX IF NOT EXISTS idx_keywords_cookie_id ON keywords(cookie_id);
