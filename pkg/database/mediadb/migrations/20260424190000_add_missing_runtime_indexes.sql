-- +goose Up
CREATE INDEX IF NOT EXISTS media_path_idx ON Media(Path);
CREATE INDEX IF NOT EXISTS idx_media_parentdir ON Media(ParentDir);

-- +goose Down
DROP INDEX IF EXISTS idx_media_parentdir;
DROP INDEX IF EXISTS media_path_idx;
