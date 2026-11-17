-- +goose Up
-- +goose StatementBegin

-- Metadata the user edited by hand: one row per field of one file. Path is
-- the file the edit was made on, the identity MediaUserData uses, and Slug
-- snapshots that file's title so a title-level field still finds its title
-- when the file is gone. Field is "name", "description", "tag:<type>" or
-- "image:<type>". Value is the text, a JSON array of canonical tag values, or
-- the name of a file in the user images folder; image bytes are never stored
-- here.
create table MediaUserFields
(
    DBID      INTEGER PRIMARY KEY AUTOINCREMENT,
    SystemID  text    not null,
    Path      text    not null,
    Field     text    not null,
    Value     text    not null,
    Slug      text    not null default '',
    CreatedAt integer not null,
    UpdatedAt integer not null,
    unique (SystemID, Path, Field)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table MediaUserFields;
-- +goose StatementEnd
