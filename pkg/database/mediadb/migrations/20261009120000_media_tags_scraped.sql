-- +goose Up
-- Scraped marks a media tag link a scraper wrote. The scanner deletes links it
-- can no longer derive from a file, and a link of a type both write, such as a
-- romset's region, is only the scanner's to delete when the scanner wrote it.
ALTER TABLE MediaTags ADD COLUMN Scraped INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE MediaTags DROP COLUMN Scraped;
