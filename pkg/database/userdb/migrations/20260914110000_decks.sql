-- +goose Up
-- +goose StatementBegin

-- A deck is the user's persistent list of games and cards. It is a local
-- feature first: the ID is minted on the device, and a copy of somebody
-- else's deck reached through a ZapLink is cached here too, flagged as not
-- owned so it is never edited.
create table Decks
(
    DBID        INTEGER PRIMARY KEY AUTOINCREMENT,
    DeckID      text    not null unique,
    Name        text    not null,
    Description text    not null default '',
    Owned       integer not null default 1,
    Metadata    text    not null default '',
    SourceURL   text    not null default '',
    FetchedAt   integer not null default 0,
    CreatedAt   integer not null,
    UpdatedAt   integer not null
);

-- One member of a deck in position order. A card item carries the card's
-- scripts; a script item carries its own ZapScript. A game added on this
-- device also keeps the file it came from (the anchor), so this device
-- launches exactly that file and the row can be re-linked after the media
-- database is rebuilt. UserDB runs without foreign keys: DeleteDeck removes
-- a deck's items explicitly.
create table DeckItems
(
    DBID      INTEGER PRIMARY KEY AUTOINCREMENT,
    DeckDBID  integer not null,
    Position  integer not null,
    Kind      text    not null,
    Name      text    not null default '',
    ZapScript text    not null default '',
    CardID    text    not null default '',
    Scripts   text    not null default '',
    Metadata  text    not null default '',
    SystemID  text    not null default '',
    Path      text    not null default '',
    MediaName text    not null default '',
    Tags      text    not null default '',
    CreatedAt integer not null,
    UpdatedAt integer not null,
    unique (DeckDBID, Position)
);

create index deckitems_deck_idx on DeckItems (DeckDBID);
create index deckitems_anchor_idx on DeckItems (SystemID, Path) where Path != '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table DeckItems;
drop table Decks;
-- +goose StatementEnd
