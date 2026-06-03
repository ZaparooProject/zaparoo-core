-- +goose Up

ALTER TABLE Tags ADD COLUMN DisplayName text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE Tags DROP COLUMN DisplayName;
