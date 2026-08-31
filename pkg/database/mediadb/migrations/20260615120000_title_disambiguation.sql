-- +goose Up
ALTER TABLE MediaTitles ADD COLUMN DisambiguationTypes TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE MediaTitles DROP COLUMN DisambiguationTypes;
