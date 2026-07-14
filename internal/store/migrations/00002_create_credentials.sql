-- +goose Up
CREATE TABLE credentials (
    provider_id TEXT NOT NULL,
    field       TEXT NOT NULL,
    ciphertext  BLOB NOT NULL,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    PRIMARY KEY (provider_id, field)
);

-- +goose Down
DROP TABLE credentials;
