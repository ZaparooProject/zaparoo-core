-- +goose Up
-- +goose StatementBegin

ALTER TABLE Clients ADD COLUMN Role text not null default 'member';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE Clients DROP COLUMN Role;
-- +goose StatementEnd
