-- +goose Up
-- +goose StatementBegin

-- zaparoo:allow-unbounded play history is pruned to a year at every start, so
-- these builds cover thousands of rows, and the queries they serve cannot
-- run without them.

-- The recently played list keeps the newest session of each media. With this
-- index it can ask "is there a newer session for the same media" per row
-- instead of grouping the whole table for every page. MediaPath leads so that
-- queries filtering or grouping by system alone keep their existing plans.
create index if not exists idx_media_history_media
    on MediaHistory (MediaPath, SystemID, DBID);

-- Play time limits sum the sessions that ended today every 30 seconds while
-- media is running. idx_media_history_open only covers open sessions, so
-- that sum read every row ever recorded.
create index if not exists idx_media_history_end_time
    on MediaHistory (EndTime);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop index if exists idx_media_history_end_time;
drop index if exists idx_media_history_media;
-- +goose StatementEnd
