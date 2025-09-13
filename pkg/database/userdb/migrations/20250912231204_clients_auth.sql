-- +goose Up
-- +goose StatementBegin
CREATE TABLE clients (
    client_id TEXT PRIMARY KEY,
    client_name TEXT NOT NULL,
    auth_token_hash TEXT NOT NULL UNIQUE,
    shared_secret BLOB NOT NULL,
    current_seq INTEGER DEFAULT 0,
    seq_window BLOB,
    nonce_cache TEXT DEFAULT '[]',
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    last_seen INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_clients_auth_token ON clients(auth_token_hash);
CREATE INDEX idx_clients_last_seen ON clients(last_seen);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE clients;
-- +goose StatementEnd