-- +goose Up
-- The MiSTer docs scraper used to take macOS AppleDouble files ("._Game.pdf",
-- written beside every file macOS copies to a FAT or exFAT drive) as manuals
-- and artwork. The AppleDouble file sorts before the real one and matches the
-- same game, so it won, and media.asset then refused to serve it because it
-- is not a PDF. The scraper now skips these files; deleting the rows lets the
-- next docs scrape, which re-runs because its fingerprint version changed,
-- write the real files in their place.
--
-- Only the property types the docs scraper writes are touched, and only when
-- the file name itself starts with "._". Paths are stored with forward
-- slashes; rtrim(Text, replace(Text, '/', '')) is everything up to and
-- including the last slash, so the substr after it is the file name.
DELETE FROM MediaTitleProperties
WHERE TypeTagDBID IN (
        SELECT t.DBID FROM Tags t JOIN TagTypes tt ON tt.DBID = t.TypeDBID
        WHERE tt.Type = 'property'
          AND t.Tag IN ('manual', 'image-boxart', 'image-screenshot', 'image-titleshot')
    )
    AND substr(Text, length(rtrim(Text, replace(Text, '/', ''))) + 1, 2) = '._';

DELETE FROM MediaProperties
WHERE TypeTagDBID IN (
        SELECT t.DBID FROM Tags t JOIN TagTypes tt ON tt.DBID = t.TypeDBID
        WHERE tt.Type = 'property'
          AND t.Tag IN ('manual', 'image-boxart', 'image-screenshot', 'image-titleshot')
    )
    AND substr(Text, length(rtrim(Text, replace(Text, '/', ''))) + 1, 2) = '._';

-- +goose Down
-- The next docs scrape writes whatever the packs hold, so there is nothing to
-- restore.
SELECT 1;
