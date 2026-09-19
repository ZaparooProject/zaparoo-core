-- +goose Up
-- +goose StatementBegin

-- A deck kept from a ZapLink is known by the link it came from, and opens as
-- the playlist ID its source served, so the copy and the served playlist are
-- the same playlist. A deck made on this device leaves PlaylistID empty.
alter table Decks add column PlaylistID text not null default '';

create unique index decks_fetched_source_idx on Decks (SourceURL) where Owned = 0 and SourceURL != '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop index decks_fetched_source_idx;
alter table Decks drop column PlaylistID;
-- +goose StatementEnd
