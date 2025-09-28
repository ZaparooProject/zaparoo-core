-- +goose Up
-- Add covering index to optimize search queries with ORDER BY Media.DBID
-- This eliminates the temporary B-tree creation during ORDER BY operations
-- by allowing SQLite to stream results directly from the index in sorted order
CREATE INDEX IF NOT EXISTS media_search_covering_idx ON Media(MediaTitleDBID, DBID);

-- +goose Down
DROP INDEX IF EXISTS media_search_covering_idx;