-- +goose Up
-- +goose StatementBegin
CREATE TABLE RemoteCommands (
    CommandID TEXT PRIMARY KEY NOT NULL,
    OperationID TEXT NOT NULL,
    OperationType TEXT NOT NULL,
    ProtocolVersion INTEGER NOT NULL,
    ParamsDigest TEXT NOT NULL,
    Origin TEXT NOT NULL,
    DeadlineAt INTEGER NOT NULL,
    ExecutionExpiresAt INTEGER,
    State TEXT NOT NULL CHECK (State IN (
        'recorded', 'accepted', 'executing', 'terminal', 'void', 'expired'
    )),
    ResultStatus TEXT,
    Result TEXT,
    ErrorCode TEXT,
    ResultReported INTEGER NOT NULL DEFAULT 0 CHECK (ResultReported IN (0, 1)),
    CreatedAt INTEGER NOT NULL,
    UpdatedAt INTEGER NOT NULL
);
CREATE INDEX RemoteCommandsPruneIdx ON RemoteCommands (DeadlineAt);
CREATE INDEX RemoteCommandsUnreportedIdx ON RemoteCommands (ResultReported, State);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE RemoteCommands;
-- +goose StatementEnd
