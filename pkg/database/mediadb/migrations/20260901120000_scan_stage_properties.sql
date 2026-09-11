-- +goose Up
-- ScanStageProperties was added to the scanner staging set after the
-- 20260703080000 migration shipped, and only ever existed because
-- sqlClearScanStage failed on the missing table and called
-- sqlEnsureScanStagingTables to repair the scratch schema. Every fresh
-- media.db took that error path on its first system. Definitions match
-- sqlEnsureScanStagingTables so the repair path stays a no-op.

CREATE TABLE IF NOT EXISTS ScanStageProperties (
    Path         TEXT NOT NULL,
    PropertyType TEXT NOT NULL,
    Property     TEXT NOT NULL,
    Text         TEXT NOT NULL,
    PRIMARY KEY (Path, PropertyType, Property)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS scanstageproperties_property_idx
    ON ScanStageProperties(PropertyType, Property, Text);

-- +goose Down
DROP INDEX IF EXISTS scanstageproperties_property_idx;
DROP TABLE IF EXISTS ScanStageProperties;
