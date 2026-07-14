-- +goose Up
-- +goose StatementBegin

create table MediaUserData
(
    DBID             INTEGER PRIMARY KEY AUTOINCREMENT,
    SystemID         text    not null,
    Path             text    not null,
    IsFavorite       integer not null default 0,
    LauncherOverride text    not null default '',
    CreatedAt        integer not null,
    UpdatedAt        integer not null,
    unique (SystemID, Path)
);

create index mediauserdata_system_idx on MediaUserData (SystemID);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table MediaUserData;
-- +goose StatementEnd
