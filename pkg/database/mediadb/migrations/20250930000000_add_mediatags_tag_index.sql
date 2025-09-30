-- +goose Up
-- Add index for efficient tag-based filtering queries
-- This index dramatically improves performance for queries like:
--   WHERE Media.DBID IN (SELECT MediaDBID FROM MediaTags WHERE TagDBID IN (...))
-- The PRIMARY KEY (MediaDBID, TagDBID) cannot efficiently seek by TagDBID alone,
-- so this reverse index is essential for tag filtering operations.
--
-- Note: This index adds write cost during initial indexing. For optimal bulk load
-- performance, this could be created AFTER the initial media scan completes.
CREATE INDEX IF NOT EXISTS mediatags_tag_media_idx ON MediaTags(TagDBID, MediaDBID);

-- +goose Down
DROP INDEX IF EXISTS mediatags_tag_media_idx;