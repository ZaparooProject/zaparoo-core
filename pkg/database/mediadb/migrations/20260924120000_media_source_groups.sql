-- +goose Up
-- SourceGroup names the game a source belongs to when several media rows can
-- share one source directory, such as ScummVM's one target per language of a
-- multilingual disc. Rows sharing a directory and the same non-empty group are
-- variants of one game, so metadata for the directory describes all of them.
-- Existing rows have no group and stay ambiguous until the next index writes
-- one.

ALTER TABLE MediaSources ADD COLUMN SourceGroup TEXT NOT NULL DEFAULT '';
ALTER TABLE ScanStageSources ADD COLUMN SourceGroup TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE ScanStageSources DROP COLUMN SourceGroup;
ALTER TABLE MediaSources DROP COLUMN SourceGroup;
