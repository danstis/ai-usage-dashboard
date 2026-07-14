-- +goose Up
CREATE TABLE usage_snapshots (
    provider_id  TEXT PRIMARY KEY,
    metrics      TEXT NOT NULL,
    collected_at DATETIME NOT NULL,
    last_error   TEXT
);

-- +goose Down
DROP TABLE usage_snapshots;
