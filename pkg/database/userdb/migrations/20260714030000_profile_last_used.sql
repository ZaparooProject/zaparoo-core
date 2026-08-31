-- +goose Up
-- +goose StatementBegin

ALTER TABLE Profiles ADD COLUMN LastUsedAt integer;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE Profiles DROP COLUMN LastUsedAt;

-- +goose StatementEnd
