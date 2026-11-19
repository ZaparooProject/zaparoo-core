-- +goose Up
-- idx_media_parentdir indexed Media(ParentDir) alone, which
-- idx_media_parentdir_system (ParentDir, SystemDBID) and the browse sort index
-- both already lead with. It answered nothing they could not, and every media
-- row written had to update it: one more page to flush per row on storage
-- where each page write waits for the card.
DROP INDEX IF EXISTS idx_media_parentdir;

-- +goose Down
CREATE INDEX IF NOT EXISTS idx_media_parentdir ON Media(ParentDir);
