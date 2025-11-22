-- +goose Up
-- +goose StatementBegin

-- Add new columns to MediaHistory table
ALTER TABLE MediaHistory ADD COLUMN ID TEXT;
ALTER TABLE MediaHistory ADD COLUMN BootUUID TEXT NOT NULL DEFAULT '';
ALTER TABLE MediaHistory ADD COLUMN MonotonicStart INTEGER NOT NULL DEFAULT 0;
ALTER TABLE MediaHistory ADD COLUMN DurationSec INTEGER DEFAULT 0;
ALTER TABLE MediaHistory ADD COLUMN WallDuration INTEGER DEFAULT 0;
ALTER TABLE MediaHistory ADD COLUMN TimeSkewFlag INTEGER DEFAULT 0;
ALTER TABLE MediaHistory ADD COLUMN ClockReliable INTEGER DEFAULT 1;
ALTER TABLE MediaHistory ADD COLUMN ClockSource TEXT;
ALTER TABLE MediaHistory ADD COLUMN CreatedAt INTEGER NOT NULL DEFAULT 0;
ALTER TABLE MediaHistory ADD COLUMN UpdatedAt INTEGER NOT NULL DEFAULT 0;
ALTER TABLE MediaHistory ADD COLUMN DeviceID TEXT;
ALTER TABLE MediaHistory ADD COLUMN SyncedAt INTEGER;
ALTER TABLE MediaHistory ADD COLUMN IsDeleted INTEGER DEFAULT 0;

-- Create new indexes for MediaHistory
CREATE INDEX idx_media_history_boot ON MediaHistory (BootUUID);
CREATE INDEX idx_media_history_updated ON MediaHistory (UpdatedAt);
CREATE INDEX idx_media_history_device ON MediaHistory (DeviceID) WHERE DeviceID IS NOT NULL;

-- Add new columns to History table
ALTER TABLE History ADD COLUMN ID TEXT;
ALTER TABLE History ADD COLUMN ClockReliable INTEGER DEFAULT 1;
ALTER TABLE History ADD COLUMN BootUUID TEXT NOT NULL DEFAULT '';
ALTER TABLE History ADD COLUMN MonotonicStart INTEGER NOT NULL DEFAULT 0;
ALTER TABLE History ADD COLUMN CreatedAt INTEGER NOT NULL DEFAULT 0;
ALTER TABLE History ADD COLUMN DeviceID TEXT;
ALTER TABLE History ADD COLUMN SyncedAt INTEGER;
ALTER TABLE History ADD COLUMN IsDeleted INTEGER DEFAULT 0;

-- Create new indexes for History
CREATE INDEX idx_history_boot ON History (BootUUID);
CREATE INDEX idx_history_created ON History (CreatedAt);

-- Backfill existing records with default values

-- For MediaHistory: Set CreatedAt and UpdatedAt to StartTime for existing records
UPDATE MediaHistory
SET CreatedAt = StartTime,
    UpdatedAt = StartTime,
    ClockReliable = 1,
    BootUUID = 'legacy'
WHERE CreatedAt = 0;

-- For MediaHistory: Calculate DurationSec and WallDuration for closed sessions
UPDATE MediaHistory
SET DurationSec = PlayTime,
    WallDuration = CASE
        WHEN EndTime IS NOT NULL THEN EndTime - StartTime
        ELSE 0
    END
WHERE EndTime IS NOT NULL;

-- For History: Set CreatedAt to Time for existing records
UPDATE History
SET CreatedAt = Time,
    ClockReliable = 1,
    BootUUID = 'legacy'
WHERE CreatedAt = 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop indexes for MediaHistory
DROP INDEX IF EXISTS idx_media_history_device;
DROP INDEX IF EXISTS idx_media_history_updated;
DROP INDEX IF EXISTS idx_media_history_boot;

-- Drop indexes for History
DROP INDEX IF EXISTS idx_history_created;
DROP INDEX IF EXISTS idx_history_boot;

-- Drop columns from MediaHistory
ALTER TABLE MediaHistory DROP COLUMN IsDeleted;
ALTER TABLE MediaHistory DROP COLUMN SyncedAt;
ALTER TABLE MediaHistory DROP COLUMN DeviceID;
ALTER TABLE MediaHistory DROP COLUMN UpdatedAt;
ALTER TABLE MediaHistory DROP COLUMN CreatedAt;
ALTER TABLE MediaHistory DROP COLUMN ClockSource;
ALTER TABLE MediaHistory DROP COLUMN ClockReliable;
ALTER TABLE MediaHistory DROP COLUMN TimeSkewFlag;
ALTER TABLE MediaHistory DROP COLUMN WallDuration;
ALTER TABLE MediaHistory DROP COLUMN DurationSec;
ALTER TABLE MediaHistory DROP COLUMN MonotonicStart;
ALTER TABLE MediaHistory DROP COLUMN BootUUID;
ALTER TABLE MediaHistory DROP COLUMN ID;

-- Drop columns from History
ALTER TABLE History DROP COLUMN IsDeleted;
ALTER TABLE History DROP COLUMN SyncedAt;
ALTER TABLE History DROP COLUMN DeviceID;
ALTER TABLE History DROP COLUMN CreatedAt;
ALTER TABLE History DROP COLUMN MonotonicStart;
ALTER TABLE History DROP COLUMN BootUUID;
ALTER TABLE History DROP COLUMN ClockReliable;
ALTER TABLE History DROP COLUMN ID;

-- +goose StatementEnd
