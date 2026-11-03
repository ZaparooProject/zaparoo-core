-- +goose Up
-- +goose StatementBegin

-- A deck kept from a link is marked Saved while the linked account lists it
-- as one it saved from another account. A saved deck is never evicted to make
-- room for other fetched decks, and is removed when the account stops listing
-- it. OwnerName is the name the account showed for the deck's owner, empty
-- when it gave none.
alter table Decks add column Saved integer not null default 0;
alter table Decks add column OwnerName text not null default '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table Decks drop column OwnerName;
alter table Decks drop column Saved;
-- +goose StatementEnd
