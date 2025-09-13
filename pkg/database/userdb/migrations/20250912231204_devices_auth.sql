-- +goose Up
-- +goose StatementBegin
CREATE TABLE devices (
    device_id TEXT PRIMARY KEY,
    device_name TEXT NOT NULL,
    auth_token_hash TEXT NOT NULL UNIQUE,
    shared_secret BLOB NOT NULL,
    current_seq INTEGER DEFAULT 0,
    seq_window BLOB,
    nonce_cache TEXT DEFAULT '[]',
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    last_seen INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_devices_auth_token ON devices(auth_token_hash);
CREATE INDEX idx_devices_last_seen ON devices(last_seen);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE devices;
-- +goose StatementEnd