-- +goose Up
-- +goose StatementBegin

create table Inbox
(
    DBID      INTEGER PRIMARY KEY,
    Title     text    not null,
    Body      text,
    Severity  integer not null default 0,
    Category  text,
    ProfileID integer not null default 0,
    CreatedAt integer not null
);

create index idx_inbox_created_at on Inbox (CreatedAt);
create unique index idx_inbox_category_profile on Inbox (Category, ProfileID) where Category is not null;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table Inbox;
-- +goose StatementEnd
