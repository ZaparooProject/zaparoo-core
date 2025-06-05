-- +goose NO TRANSACTION
-- +goose Up
PRAGMA
journal_mode = OFF;
PRAGMA
synchronous = OFF;

drop table if exists DBConfig;
create table DBConfig
(
    Name  text PRIMARY KEY,
    Value text
);

-- ROWID is an internal subject to change on vacuum
-- DBID INTEGER PRIMARY KEY aliases ROWID and makes it
-- persistent between vacuums

drop table if exists Systems;
create table Systems
(
    DBID     INTEGER PRIMARY KEY,
    SystemID text unique not null,
    Name     text        not null
);

drop table if exists MediaTitles;
create table MediaTitles
(
    DBID       INTEGER PRIMARY KEY,
    SystemDBID integer not null,
    Slug       text    not null,
    Name       text    not null
);

drop table if exists Media;
create table Media
(
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    Path           text    not null
);

drop table if exists TagTypes;
create table TagTypes
(
    DBID INTEGER PRIMARY KEY,
    Type text unique not null
);

drop table if exists Tags;
create table Tags
(
    DBID     INTEGER PRIMARY KEY,
    TypeDBID integer not null,
    Tag      text    not null
);

drop table if exists MediaTags;
create table MediaTags
(
    DBID      INTEGER PRIMARY KEY,
    MediaDBID integer not null,
    TagDBID   integer not null
);

drop table if exists MediaTitleTags;
create table MediaTitleTags
(
    DBID           INTEGER PRIMARY KEY,
    TagDBID        integer not null,
    MediaTitleDBID integer not null
);

drop table if exists SupportingMedia;
create table SupportingMedia
(
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    TypeTagDBID    integer not null,
    Path           string  not null,
    ContentType    text    not null,
    Binary         blob
);

-- +goose Down
drop table SupportingMedia;
drop table MediaTitleTags;
drop table MediaTags;
drop table Tags;
drop table TagTypes;
drop table Media;
drop table MediaTitles;
drop table Systems;
drop table DBConfig;
