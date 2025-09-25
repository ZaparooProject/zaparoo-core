-- +goose Up
-- Ensure all indexes exist (for databases that may not have them from earlier migrations)
-- Using IF NOT EXISTS for safety
CREATE INDEX IF NOT EXISTS mediatitles_slug_idx ON MediaTitles(Slug);
CREATE INDEX IF NOT EXISTS mediatitles_system_idx ON MediaTitles(SystemDBID);
CREATE INDEX IF NOT EXISTS media_mediatitle_idx ON Media(MediaTitleDBID);
CREATE INDEX IF NOT EXISTS tags_tag_idx ON Tags(Tag);
CREATE INDEX IF NOT EXISTS tags_tagtype_idx ON Tags(TypeDBID);
CREATE INDEX IF NOT EXISTS mediatags_media_idx ON MediaTags(MediaDBID);
CREATE INDEX IF NOT EXISTS mediatags_tag_idx ON MediaTags(TagDBID);
CREATE INDEX IF NOT EXISTS mediatitletags_mediatitle_idx ON MediaTitleTags(MediaTitleDBID);
CREATE INDEX IF NOT EXISTS mediatitletags_tag_idx ON MediaTitleTags(TagDBID);
CREATE INDEX IF NOT EXISTS supportingmedia_mediatitle_idx ON SupportingMedia(MediaTitleDBID);
CREATE INDEX IF NOT EXISTS supportingmedia_typetag_idx ON SupportingMedia(TypeTagDBID);

-- Also ensure the optimized search index exists
CREATE INDEX IF NOT EXISTS mediatitles_system_slug_idx ON MediaTitles(SystemDBID, Slug);

-- +goose Down
-- We don't drop indexes on down migration as they may be needed by earlier migrations