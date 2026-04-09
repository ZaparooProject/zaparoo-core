-- +goose Up
-- +goose StatementBegin

create table Clients
(
    DBID       INTEGER PRIMARY KEY,
    ClientID   text    not null unique,
    ClientName text    not null,
    AuthToken  text    not null unique
        check (instr(AuthToken, ':') = 0),
    PairingKey blob    not null,
    CreatedAt  integer not null,
    LastSeenAt integer not null
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table Clients;
-- +goose StatementEnd
