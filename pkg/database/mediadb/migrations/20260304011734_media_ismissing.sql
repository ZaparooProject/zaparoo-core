-- +goose Up

-- Add New IsMissing Field for rescan matching
ALTER TABLE Media
ADD IsMissing integer NOT NULL DEFAULT 0;

-- +goose Down

-- Remove New IsMissing Field for rescan matching
ALTER TABLE Media
DROP COLUMN IsMissing;