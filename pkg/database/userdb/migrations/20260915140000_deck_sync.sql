-- +goose Up
-- +goose StatementBegin

-- The last copy of each owned deck agreed with the linked Zaparoo Online
-- account. The deck itself stays in Decks; Library sync compares it against
-- this snapshot to tell an edit here from an edit there, and merges when both
-- changed. A row whose deck is gone is a delete still to be sent. Locked
-- marks a deck the account locked, which this device keeps read-only.
create table DeckSync
(
    DeckID       text    primary key,
    Snapshot     text    not null default '',
    Revision     integer not null default 0,
    Locked       integer not null default 0,
    Conflicts    integer not null default 0,
    RejectedCode text    not null default '',
    RejectedHash text    not null default '',
    UpdatedAt    integer not null
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table DeckSync;
-- +goose StatementEnd
