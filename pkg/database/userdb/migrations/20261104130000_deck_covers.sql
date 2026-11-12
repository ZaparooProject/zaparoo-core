-- +goose Up
-- +goose StatementBegin

-- A deck's cover. CoverFile names the image this device stores for the deck
-- in its deck covers folder, empty when it has none. ImageURL is the image
-- the linked account or the deck's source holds for it. CoverPending marks a
-- cover change made here that is still to reach the account: 'set' to send
-- CoverFile, 'remove' to clear the account's image.
alter table Decks add column CoverFile text not null default '';
alter table Decks add column ImageURL text not null default '';
alter table Decks add column CoverPending text not null default '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table Decks drop column CoverPending;
alter table Decks drop column ImageURL;
alter table Decks drop column CoverFile;
-- +goose StatementEnd
