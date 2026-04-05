-- +goose Up
ALTER TABLE TestTable ADD COLUMN Email TEXT;

-- +goose Down
ALTER TABLE TestTable DROP COLUMN Email;
