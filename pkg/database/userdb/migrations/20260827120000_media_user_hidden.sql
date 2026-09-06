-- +goose Up
ALTER TABLE MediaUserData ADD COLUMN IsHidden INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE MediaUserData DROP COLUMN IsHidden;
