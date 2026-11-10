-- +goose Up
-- The MiSTer docs scraper used to take macOS AppleDouble files ("._Game.pdf",
-- written beside every file macOS copies to a FAT or exFAT drive) as manuals
-- and artwork. The AppleDouble file sorts before the real one and matches the
-- same game, so it won, and media.asset then refused to serve it because it
-- is not a PDF. The scraper now skips these files; deleting the rows lets the
-- next docs scrape, which re-runs because its fingerprint version changed,
-- write the real files in their place.
DELETE FROM MediaTitleProperties WHERE instr(Text, '/._') > 0;
DELETE FROM MediaProperties WHERE instr(Text, '/._') > 0;

-- +goose Down
-- The next docs scrape writes whatever the packs hold, so there is nothing to
-- restore.
SELECT 1;
