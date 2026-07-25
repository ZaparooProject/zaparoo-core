-- +goose Up
-- +goose StatementBegin

-- Snapshot disambiguating filename tags (type:value pairs from the media
-- scanner, e.g. "region:us", "rev:1") onto the durable game-identity rows.
-- MediaDB is disposable (rebuilt by rescan, never backed up), so tags for
-- played/favorited items must be captured at record time or they are lost
-- when the MediaDB entry disappears. Stored as a JSON string array; '' means
-- no snapshot was possible (no scanner entry at record time).
ALTER TABLE MediaHistory ADD COLUMN Tags TEXT NOT NULL DEFAULT '';

-- MediaUserData additionally snapshots the display name: favorites store
-- only the raw path today, which leaves future favorites sync re-parsing
-- filenames server-side for names too.
ALTER TABLE MediaUserData ADD COLUMN MediaName TEXT NOT NULL DEFAULT '';
ALTER TABLE MediaUserData ADD COLUMN Tags TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE MediaUserData DROP COLUMN Tags;
ALTER TABLE MediaUserData DROP COLUMN MediaName;
ALTER TABLE MediaHistory DROP COLUMN Tags;
-- +goose StatementEnd
