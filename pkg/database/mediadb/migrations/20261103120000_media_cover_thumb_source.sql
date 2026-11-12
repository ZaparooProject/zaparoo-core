-- +goose Up
-- The source image a recorded cover thumbnail was built from, as the key the
-- thumbnail cache names its file by. media.image serves a recorded cover only
-- while that key still matches the image the media row resolves to, so the
-- records no longer have to be cleared when an index or scrape completes. Rows
-- recorded before this column existed have no key and are re-recorded the
-- next time their cover is built.
ALTER TABLE MediaCoverThumbs ADD COLUMN SourceKey TEXT;

-- +goose Down
ALTER TABLE MediaCoverThumbs DROP COLUMN SourceKey;
