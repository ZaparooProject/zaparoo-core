-- +goose Up

-- Add per-(system,tag) usage counts to SystemTagsCache so the Tags API
-- can sort by popularity and cap long-tail categories like credit:.
ALTER TABLE SystemTagsCache ADD COLUMN Count INTEGER NOT NULL DEFAULT 0;

-- Reverse index enabling efficient COUNT(*) by TagDBID on MediaTitleTags.
-- The table's PK is (MediaTitleDBID, TagDBID), so counting by TagDBID
-- previously required a full PK btree scan.
CREATE INDEX IF NOT EXISTS mediatitletags_tag_idx ON MediaTitleTags(TagDBID);

-- +goose Down
DROP INDEX IF EXISTS mediatitletags_tag_idx;
ALTER TABLE SystemTagsCache DROP COLUMN Count;
