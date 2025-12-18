-- +goose Up
-- +goose StatementBegin

create table Inbox
(
    DBID      INTEGER PRIMARY KEY,
    Title     text    not null,
    Body      text,
    CreatedAt integer not null default (cast((julianday('now') - 2440587.5)*86400000 as INTEGER))
);

create index idx_inbox_created_at on Inbox (CreatedAt);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table Inbox;
-- +goose StatementEnd
