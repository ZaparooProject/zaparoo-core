-- +goose Up
-- When each listing flag was last set, as a Unix time in seconds. NULL when
-- the flag is clear, and for flags set before the time was recorded, which
-- sort after every dated row in a list ordered by when games were added.
ALTER TABLE MediaUserData ADD COLUMN FavoriteAt INTEGER;
ALTER TABLE MediaUserData ADD COLUMN LikedAt INTEGER;
ALTER TABLE MediaUserData ADD COLUMN DislikedAt INTEGER;
ALTER TABLE MediaUserData ADD COLUMN PlayLaterAt INTEGER;

-- +goose Down
ALTER TABLE MediaUserData DROP COLUMN PlayLaterAt;
ALTER TABLE MediaUserData DROP COLUMN DislikedAt;
ALTER TABLE MediaUserData DROP COLUMN LikedAt;
ALTER TABLE MediaUserData DROP COLUMN FavoriteAt;
