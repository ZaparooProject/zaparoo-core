-- +goose Up
-- +goose StatementBegin
create table ZapLinkCache
(
    DBID      INTEGER PRIMARY KEY,
    URL       text not null unique,
    UpdatedAt text default current_timestamp,
    ZapScript text not null
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE ZapLinkCache;
-- +goose StatementEnd
