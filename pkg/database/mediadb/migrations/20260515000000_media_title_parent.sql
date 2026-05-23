-- +goose Up
ALTER TABLE MediaTitles ADD COLUMN ParentDBID INTEGER NOT NULL DEFAULT 0;
CREATE INDEX mediatitles_parent_idx ON MediaTitles(ParentDBID);

-- +goose Down
DROP INDEX mediatitles_parent_idx;
ALTER TABLE MediaTitles DROP COLUMN ParentDBID;
