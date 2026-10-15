-- +goose Up
-- +goose StatementBegin

-- How a deck rotates when it opens as a playlist, in milliseconds. A zero
-- AdvanceMs or IdleMs means the deck does not rotate that way; the two are
-- never both set. CountdownMs is NULL when the deck does not say, which opens
-- with the default notice, and an explicit 0 switches with no notice.
alter table Decks add column AdvanceMs integer not null default 0;
alter table Decks add column IdleMs integer not null default 0;
alter table Decks add column CountdownMs integer;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table Decks drop column CountdownMs;
alter table Decks drop column IdleMs;
alter table Decks drop column AdvanceMs;
-- +goose StatementEnd
