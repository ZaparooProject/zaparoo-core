-- +goose Up
-- +goose StatementBegin

-- Records which build last migrated this database.
--
-- An older binary refuses a database migrated by a newer one, because user
-- history, mappings and profiles cannot be reconstructed. Until now the only
-- thing that refusal could name was a goose timestamp, which tells a user
-- nothing about what to reinstall. This is written on every successful
-- migration so the refusal can name the version that wrote the schema.
--
-- It lives in the database rather than beside it so it survives a cleared
-- cache and travels inside backup copies.
create table if not exists SchemaMeta (
    Key       text primary key,
    Value     text not null,
    UpdatedAt integer not null
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists SchemaMeta;
-- +goose StatementEnd
