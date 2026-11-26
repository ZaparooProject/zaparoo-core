-- +goose Up
-- +goose StatementBegin

-- The deck items the account refused on the last rejected push, as a JSON
-- array of {"itemId", "code"} naming local deck item IDs. Empty when the
-- rejection was about the deck as a whole, or there is none.
alter table DeckSync add column RejectedItems text not null default '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table DeckSync drop column RejectedItems;
-- +goose StatementEnd
