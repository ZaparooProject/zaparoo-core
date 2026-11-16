-- +goose Up
-- +goose StatementBegin

-- Nothing reads History by CreatedAt, but every token scan had to update this
-- index, and each page a scan writes is flushed to the card before the scan's
-- insert returns.
drop index if exists idx_history_created;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
create index if not exists idx_history_created on History (CreatedAt);
-- +goose StatementEnd
