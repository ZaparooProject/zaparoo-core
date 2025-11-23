-- +goose Up
-- +goose StatementBegin
-- Add index to existing History table for faster cleanup queries
CREATE INDEX IF NOT EXISTS idx_history_time ON History (Time);

-- Create new MediaHistory table
create table MediaHistory
(
    DBID       INTEGER PRIMARY KEY,
    StartTime  integer not null,
    EndTime    integer,
    SystemID   text    not null,
    SystemName text    not null,
    MediaPath  text    not null,
    MediaName  text    not null,
    LauncherID text    not null,
    PlayTime   integer default 0
);

-- Index for efficient cleanup queries by start time
CREATE INDEX idx_media_history_start_time ON MediaHistory (StartTime);

-- Index for detecting hanging entries (EndTime IS NULL)
CREATE INDEX idx_media_history_open ON MediaHistory (EndTime) WHERE EndTime IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_media_history_open;
DROP INDEX IF EXISTS idx_media_history_start_time;
DROP TABLE IF EXISTS MediaHistory;
DROP INDEX IF EXISTS idx_history_time;
-- +goose StatementEnd
