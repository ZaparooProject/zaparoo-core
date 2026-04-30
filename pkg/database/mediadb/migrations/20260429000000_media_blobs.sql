-- +goose Up

-- Content-addressed blob store. Hash is the hex-encoded SHA-256 of Data;
-- UNIQUE enforces deduplication so identical binary content is stored once.
CREATE TABLE MediaBlobs (
    DBID        INTEGER PRIMARY KEY,
    Hash        text    NOT NULL UNIQUE,
    ContentType text    NOT NULL,
    Data        blob    NOT NULL
);

CREATE INDEX mediablobs_hash_idx ON MediaBlobs(Hash);

-- Add nullable FK column to each property table.
ALTER TABLE MediaTitleProperties ADD COLUMN BlobDBID integer
    REFERENCES MediaBlobs(DBID) ON DELETE SET NULL;

ALTER TABLE MediaProperties ADD COLUMN BlobDBID integer
    REFERENCES MediaBlobs(DBID) ON DELETE SET NULL;

-- No data migration needed: Binary was never populated by any scraper
-- (all existing property rows have Binary=NULL / ContentType='').

-- Remove the now-redundant inline binary columns.
ALTER TABLE MediaTitleProperties DROP COLUMN Binary;
ALTER TABLE MediaTitleProperties DROP COLUMN ContentType;

ALTER TABLE MediaProperties DROP COLUMN Binary;
ALTER TABLE MediaProperties DROP COLUMN ContentType;

-- +goose Down

ALTER TABLE MediaTitleProperties ADD COLUMN ContentType text NOT NULL DEFAULT '';
ALTER TABLE MediaTitleProperties ADD COLUMN Binary blob;

ALTER TABLE MediaProperties ADD COLUMN ContentType text NOT NULL DEFAULT '';
ALTER TABLE MediaProperties ADD COLUMN Binary blob;

-- Inline binary data is not recovered on rollback; BlobDBID values remain
-- in MediaBlobs but the FK columns are dropped from the property tables.
ALTER TABLE MediaTitleProperties DROP COLUMN BlobDBID;
ALTER TABLE MediaProperties DROP COLUMN BlobDBID;

DROP INDEX mediablobs_hash_idx;
DROP TABLE MediaBlobs;
