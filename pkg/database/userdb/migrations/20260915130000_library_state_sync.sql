-- +goose Up
-- +goose StatementBegin

-- The last personal state row agreed with the linked Zaparoo Online account
-- for one game, keyed the way both sides name a game: media type, system,
-- Core's slug and the tags that make a file a different game. The local
-- favorites, likes and play-later flags stay in MediaUserData; this is only
-- the base Library sync compares them against, so an edit on either side is
-- told apart from an edit on both. Unmatched marks a row the device holds no
-- copy of yet.
create table LibraryStateSync
(
    IdentityKey   text    primary key,
    MediaType     text    not null,
    SystemID      text    not null,
    CoreSlug      text    not null,
    VariantTags   text    not null default '[]',
    Title         text    not null default '',
    Favorite      integer not null default 0,
    Intent        text    not null default 'none',
    Reaction      text    not null default 'none',
    PreferredTags text    not null default '[]',
    Revision      integer not null default 0,
    Deleted       integer not null default 0,
    Unmatched     integer not null default 0,
    RejectedCode  text    not null default '',
    RejectedHash  text    not null default '',
    UpdatedAt     integer not null
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table LibraryStateSync;
-- +goose StatementEnd
