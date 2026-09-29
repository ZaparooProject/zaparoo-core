-- +goose Up
-- The image type a media row's cover resolved to when its thumbnail was
-- first built, and the average colour of that thumbnail as 0xRRGGBB. It lets
-- media.image find a cached thumbnail without reading the original artwork,
-- and gives list results a placeholder colour. The table is disposable: it is
-- cleared with the thumbnail cache and refilled as thumbnails are built.
CREATE TABLE IF NOT EXISTS MediaCoverThumbs (
    MediaDBID INTEGER PRIMARY KEY REFERENCES Media (DBID) ON DELETE CASCADE,
    TypeTag   TEXT NOT NULL,
    Color     INTEGER
);

-- +goose Down
DROP TABLE IF EXISTS MediaCoverThumbs;
