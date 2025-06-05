-- +goose Up
-- +goose StatementBegin

-- ROWID is an internal subject to change on vacuum
-- DBID INTEGER PRIMARY KEY aliases ROWID and makes it
-- persistent between vacuums

create table History
(
    DBID       INTEGER PRIMARY KEY,
    Time       integer not null,
    Type       text    not null,
    TokenID    text    not null,
    TokenValue text    not null,
    TokenData  text    not null,
    Success    integer not null
);

create table Mappings
(
    DBID     INTEGER PRIMARY KEY,
    Added    integer not null,
    Label    text    not null,
    Enabled  integer not null,
    Type     text    not null,
    Match    text    not null,
    Pattern  text    not null,
    Override text    not null
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table Mappings;
drop table History;
-- +goose StatementEnd
