-- +goose Up
-- +goose StatementBegin

create table Profiles
(
    DBID          INTEGER PRIMARY KEY,
    ProfileID     text    not null unique,
    Name          text    not null,
    SwitchID      text    not null unique,
    PINHash       text,
    LimitsEnabled integer,
    DailyLimit    text,
    SessionLimit  text,
    CreatedAt     integer not null,
    UpdatedAt     integer not null
);

create table DeviceState
(
    Key       text    primary key,
    Value     text    not null,
    UpdatedAt integer not null
);

ALTER TABLE MediaHistory ADD COLUMN ProfileID TEXT;
CREATE INDEX idx_media_history_profile ON MediaHistory (ProfileID) WHERE ProfileID IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_media_history_profile;
ALTER TABLE MediaHistory DROP COLUMN ProfileID;
DROP TABLE IF EXISTS DeviceState;
DROP TABLE IF EXISTS Profiles;
-- +goose StatementEnd
