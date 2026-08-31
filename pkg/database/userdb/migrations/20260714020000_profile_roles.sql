-- +goose Up
-- +goose StatementBegin

ALTER TABLE Profiles ADD COLUMN Role text not null default 'member';
CREATE INDEX idx_profiles_role ON Profiles (Role);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_profiles_role;
ALTER TABLE Profiles DROP COLUMN Role;

-- +goose StatementEnd
