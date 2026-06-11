-- +goose Up
ALTER TABLE Media ADD COLUMN SortName TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_media_browse_sort ON Media(ParentDir, IsMissing, SortName, DBID);

-- +goose Down
DROP INDEX IF EXISTS idx_media_browse_sort;
ALTER TABLE Media DROP COLUMN SortName;
