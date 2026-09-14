-- +goose Up
ALTER TABLE MediaUserData ADD COLUMN IsLiked INTEGER NOT NULL DEFAULT 0;
ALTER TABLE MediaUserData ADD COLUMN IsDisliked INTEGER NOT NULL DEFAULT 0;
ALTER TABLE MediaUserData ADD COLUMN IsPlayLater INTEGER NOT NULL DEFAULT 0;
-- The title slug the scanner indexed the path under, snapshotted beside
-- MediaName and Tags so a row names its game the way a title launch does
-- without re-deriving the slug from the display name later.
ALTER TABLE MediaUserData ADD COLUMN Slug TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE MediaUserData DROP COLUMN Slug;
ALTER TABLE MediaUserData DROP COLUMN IsPlayLater;
ALTER TABLE MediaUserData DROP COLUMN IsDisliked;
ALTER TABLE MediaUserData DROP COLUMN IsLiked;
