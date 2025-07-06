-- +goose Up
-- +goose StatementBegin
create table ZapLinkHosts
(
    DBID      INTEGER PRIMARY KEY,
    Host      text not null unique,
    CheckedAt text    default current_timestamp,
    ZapScript integer not null
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE ZapLinkHosts;
-- +goose StatementEnd
