-- +goose Up
-- +goose StatementBegin

-- Persist Core's complete versioned scanner identity observation atomically.
-- Existing fallback fields remain unchanged for backward compatibility.
ALTER TABLE MediaHistory ADD COLUMN MediaIdentity TEXT NOT NULL DEFAULT '';
ALTER TABLE MediaHistory ADD COLUMN MediaIdentityPolicyVersion INTEGER NOT NULL DEFAULT 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE MediaHistory DROP COLUMN MediaIdentityPolicyVersion;
ALTER TABLE MediaHistory DROP COLUMN MediaIdentity;
-- +goose StatementEnd
