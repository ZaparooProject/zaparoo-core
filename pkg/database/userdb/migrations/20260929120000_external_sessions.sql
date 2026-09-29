-- +goose Up
-- +goose StatementBegin

-- A launcher with LifecycleExternal (Android today) leaves Core's process, so
-- the active-media tracker never sees it end. A launch is durable before its
-- host intent is dispatched: a successful dispatch is not proof of play, only
-- host foreground evidence - or, without it, the host's own launcher return -
-- can confirm and time one. See docs/decisions/0002-android-playtime.md and
-- docs/platforms/android/session-reconciliation.md (Zaparoo Go).
CREATE TABLE ExternalSessions (
    LaunchID TEXT PRIMARY KEY,
    MediaHistoryDBID INTEGER REFERENCES MediaHistory(DBID),
    SystemID TEXT NOT NULL,
    SystemName TEXT NOT NULL,
    MediaPath TEXT NOT NULL,
    MediaName TEXT NOT NULL,
    MediaIdentity TEXT NOT NULL DEFAULT '',
    LauncherID TEXT NOT NULL,
    ProfileID TEXT,
    Target TEXT NOT NULL,
    BootID TEXT NOT NULL,
    Status TEXT NOT NULL CHECK (Status IN (
        'pending', 'confirmed', 'active', 'suspended', 'closed', 'abandoned', 'stale'
    )),
    Source TEXT NOT NULL CHECK (Source IN ('foreground_events', 'host_return')),
    RequestedMs INTEGER NOT NULL,
    RequestedElapsedMs INTEGER NOT NULL DEFAULT 0,
    InitialInteractive INTEGER NOT NULL DEFAULT 0,
    InitialUnlocked INTEGER NOT NULL DEFAULT 0,
    DispatchedMs INTEGER,
    ConfirmedMs INTEGER,
    EndedMs INTEGER,
    -- SupersededMs is set on an older unresolved session of the same boot
    -- once a later one is durably recorded, so a return or evidence batch
    -- never extends the wrong launch's time past the one that replaced it.
    SupersededMs INTEGER,
    CursorMs INTEGER,
    CursorElapsedMs INTEGER,
    UpdatedMs INTEGER NOT NULL
);
CREATE INDEX idx_external_sessions_unresolved ON ExternalSessions (UpdatedMs)
    WHERE Status IN ('pending', 'confirmed', 'active', 'suspended');
CREATE INDEX idx_external_sessions_boot ON ExternalSessions (BootID, RequestedElapsedMs)
    WHERE Status IN ('pending', 'confirmed', 'active', 'suspended');

-- The host strips every unrelated package/activity before evidence reaches
-- Core. A replayed overlap has the same tuple and cannot create a second row.
CREATE TABLE ExternalSessionEvidence (
    EvidenceID INTEGER PRIMARY KEY,
    LaunchID TEXT NOT NULL REFERENCES ExternalSessions(LaunchID) ON DELETE CASCADE,
    TimestampMs INTEGER NOT NULL,
    Kind TEXT NOT NULL CHECK (Kind IN (
        'target_resumed', 'target_paused', 'target_stopped', 'other_resumed',
        'screen_interactive', 'screen_non_interactive', 'keyguard_shown', 'keyguard_hidden'
    )),
    TargetActivity TEXT NOT NULL DEFAULT '',
    UNIQUE (LaunchID, TimestampMs, Kind, TargetActivity)
);
CREATE INDEX idx_external_evidence_order ON ExternalSessionEvidence (LaunchID, TimestampMs, EvidenceID);

CREATE TABLE ExternalSessionSegments (
    LaunchID TEXT NOT NULL REFERENCES ExternalSessions(LaunchID) ON DELETE CASCADE,
    Ordinal INTEGER NOT NULL,
    StartedMs INTEGER NOT NULL,
    EndedMs INTEGER NOT NULL CHECK (EndedMs >= StartedMs),
    PolicyVersion INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (LaunchID, Ordinal)
);

-- Every existing row is Core's own launch lifecycle. Unknown pre-existing
-- durations are never presented as newly verified foreground time.
ALTER TABLE MediaHistory ADD COLUMN SessionSource TEXT NOT NULL DEFAULT 'active_media';
ALTER TABLE MediaHistory ADD COLUMN SessionConfidence TEXT NOT NULL DEFAULT 'unspecified';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_external_evidence_order;
DROP TABLE IF EXISTS ExternalSessionSegments;
DROP TABLE IF EXISTS ExternalSessionEvidence;
DROP INDEX IF EXISTS idx_external_sessions_boot;
DROP INDEX IF EXISTS idx_external_sessions_unresolved;
DROP TABLE IF EXISTS ExternalSessions;
ALTER TABLE MediaHistory DROP COLUMN SessionConfidence;
ALTER TABLE MediaHistory DROP COLUMN SessionSource;
-- +goose StatementEnd
