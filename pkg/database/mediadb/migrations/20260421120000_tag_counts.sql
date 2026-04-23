-- +goose Up

-- Add per-(system,tag) usage counts to SystemTagsCache so the Tags API
-- can sort by popularity and cap long-tail categories like credit:.
ALTER TABLE SystemTagsCache ADD COLUMN Count INTEGER NOT NULL DEFAULT 0;

-- Reverse index enabling efficient COUNT(*) by TagDBID on MediaTitleTags.
-- The table's PK is (MediaTitleDBID, TagDBID), so counting by TagDBID
-- previously required a full PK btree scan.
CREATE INDEX IF NOT EXISTS mediatitletags_tag_idx ON MediaTitleTags(TagDBID);

-- Mark the cache as stale by clearing it so the application rebuilds it on startup.
-- The Count column defaults to 0 for all new rows, but existing rows also get
-- cleared here so the self-healing mechanism in GetSystemTagsCached triggers
-- and populates correct counts on first access.
DELETE FROM SystemTagsCache;

-- +goose Down
DELETE FROM SystemTagsCache;
DROP INDEX IF EXISTS mediatitletags_tag_idx;
ALTER TABLE SystemTagsCache DROP COLUMN Count;
