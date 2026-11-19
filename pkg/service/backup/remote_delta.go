// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package backup

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

const (
	remoteDeltaStateName    = "remote-index.json"
	remoteDeltaStateVersion = 1

	// A delta commit carries at most this many changed paths in a request
	// body of at most this many bytes. Larger change sets go out as a full
	// commit instead.
	remoteDeltaMaxChanges   = 2000
	remoteDeltaMaxBodyBytes = 1 << 20

	// remoteFullCommitInterval is the longest a device goes on delta commits
	// alone. A full commit re-sends the whole file list, so it corrects an
	// index that no longer matches the server and re-verifies every file.
	remoteFullCommitInterval = 24 * time.Hour

	remoteErrorBaseMismatch  = "backup_base_mismatch"
	remoteErrorDeltaTooLarge = "backup_delta_too_large"
)

// remoteDeltaRequest commits a snapshot as the changes against the device's
// latest snapshot, named by its manifest root hash. Upsert and Remove are
// always sent, empty or not, and entries are sorted so a retried request has
// the same body.
//
//nolint:tagliatelle,govet // Remote API contract uses snake_case JSON fields.
type remoteDeltaRequest struct {
	CoreVersion          *string                          `json:"core_version,omitempty"`
	Upsert               map[string][]remoteManifestEntry `json:"upsert"`
	Remove               map[string][]string              `json:"remove"`
	BaseManifestRootHash string                           `json:"base_manifest_root_hash"`
	BackupType           string                           `json:"backup_type"`
	SchemaVersion        int                              `json:"schema_version"`
}

// remoteDeltaState is the persisted record of the last snapshot this device
// committed: the root hash the server returned for it and every file it
// held. The next run diffs the collected files against Files and sends only
// the difference. It is bound to the link it was committed under, so a
// device linked to another account never reuses it.
//
//nolint:govet // JSON file shape is grouped for readability.
type remoteDeltaState struct {
	Files             map[string][]remoteManifestEntry `json:"files"`
	Backup            remoteBackupResponse             `json:"backup"`
	BaseURL           string                           `json:"baseUrl"`
	DeviceID          string                           `json:"deviceId"`
	Platform          string                           `json:"platform"`
	ManifestRootHash  string                           `json:"manifestRootHash"`
	LastFullCommitAt  string                           `json:"lastFullCommitAt"`
	StorageUsedBytes  int64                            `json:"storageUsedBytes"`
	StorageQuotaBytes int64                            `json:"storageQuotaBytes"`
	Version           int                              `json:"version"`
}

// remoteDeltaChanges is the difference between the index and the files
// collected for this run.
type remoteDeltaChanges struct {
	upsert      map[string][]remoteManifestEntry
	remove      map[string][]string
	upsertFiles []FileRef
	count       int
}

func (m *Manager) remoteDeltaStatePath() string {
	return filepath.Join(helpers.DataDir(m.pl), "backups", remoteDeltaStateName)
}

// readRemoteDeltaState returns the persisted delta state, or nil when there
// is none or it cannot be read. An unreadable state only costs a full commit.
func (m *Manager) readRemoteDeltaState() *remoteDeltaState {
	statusMu.RLock()
	defer statusMu.RUnlock()

	data, err := os.ReadFile(m.remoteDeltaStatePath())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Msg("remote backup index is unreadable; sending a full commit")
		}
		return nil
	}
	var state remoteDeltaState
	if decodeErr := json.Unmarshal(data, &state); decodeErr != nil {
		log.Warn().Err(decodeErr).Msg("remote backup index is corrupt; sending a full commit")
		return nil
	}
	return &state
}

func (m *Manager) writeRemoteDeltaState(state *remoteDeltaState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encoding remote backup index: %w", err)
	}
	statusMu.Lock()
	defer statusMu.Unlock()
	return m.writeStateFileLocked(m.remoteDeltaStatePath(), data)
}

// clearRemoteDeltaState forgets the last committed snapshot, so the next
// remote backup is a full commit.
func (m *Manager) clearRemoteDeltaState() {
	statusMu.Lock()
	defer statusMu.Unlock()
	m.clearRemoteDeltaStateLocked()
}

// clearRemoteDeltaStateLocked is clearRemoteDeltaState for callers that
// already hold statusMu.
func (m *Manager) clearRemoteDeltaStateLocked() {
	if err := os.Remove(m.remoteDeltaStatePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn().Err(err).Msg("failed to clear remote backup index")
	}
}

// saveRemoteDeltaState persists state, dropping whatever was stored before
// when the write fails: a stale index must never outlive a newer commit.
func (m *Manager) saveRemoteDeltaState(state *remoteDeltaState) {
	if err := m.writeRemoteDeltaState(state); err != nil {
		log.Warn().Err(err).Msg("failed to persist remote backup index")
		m.clearRemoteDeltaState()
	}
}

// recordRemoteFullCommit resets the delta state from a full commit. A server
// that returns no manifest root hash does not take delta commits, so nothing
// is kept and every run stays a full commit.
func (m *Manager) recordRemoteFullCommit(
	client *remoteClient,
	deviceID string,
	backup *remoteBackupResponse,
	files []FileRef,
	list *RemoteListInfo,
) {
	if deviceID == "" || !validRemoteRootHash(backup.ManifestRootHash) {
		m.clearRemoteDeltaState()
		return
	}
	stored := *backup
	stored.Manifest = nil
	m.saveRemoteDeltaState(&remoteDeltaState{
		Version:           remoteDeltaStateVersion,
		BaseURL:           client.baseURL,
		DeviceID:          deviceID,
		Platform:          m.pl.ID(),
		ManifestRootHash:  backup.ManifestRootHash,
		LastFullCommitAt:  formatTime(time.Now().UTC()),
		Backup:            stored,
		Files:             remoteCategories(files),
		StorageUsedBytes:  list.StorageUsedBytes,
		StorageQuotaBytes: list.StorageQuotaBytes,
	})
}

// remoteDeltaBase returns the state a delta commit can build on, or nil when
// this run must be a full commit: no usable state for the current link, the
// server is known not to take delta commits, or a full commit is due.
func (m *Manager) remoteDeltaBase(client *remoteClient, deviceID string) *remoteDeltaState {
	if deviceID == "" || (m.coordinator != nil && m.coordinator.RemoteDeltaUnsupported()) {
		return nil
	}
	state := m.readRemoteDeltaState()
	if state == nil {
		return nil
	}
	if state.Version != remoteDeltaStateVersion ||
		state.BaseURL != client.baseURL ||
		state.DeviceID != deviceID ||
		state.Platform != m.pl.ID() ||
		state.Backup.ID == "" ||
		state.Backup.SchemaVersion != remoteSchemaVersion ||
		!validRemoteRootHash(state.ManifestRootHash) {
		return nil
	}
	if remoteFullCommitDue(state, time.Now().UTC()) {
		return nil
	}
	return state
}

// remoteFullCommitDue reports whether the last full commit is older than the
// full commit interval. A timestamp that is missing, unreliable or in the
// future counts as due.
func remoteFullCommitDue(state *remoteDeltaState, now time.Time) bool {
	last, ok := parseReliableTime(state.LastFullCommitAt)
	if !ok || now.Before(last) {
		return true
	}
	return now.Sub(last) >= remoteFullCommitInterval
}

func validRemoteRootHash(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == 32
}

func remoteDeltaKey(category, restorePath string) string {
	return category + "\x00" + restorePath
}

// diffRemoteIndex compares the indexed snapshot with the files collected for
// this run. ok is false when either side names a path twice: the index
// cannot be trusted then, and the caller sends a full commit.
func diffRemoteIndex(
	indexed map[string][]remoteManifestEntry, files []FileRef,
) (changes remoteDeltaChanges, ok bool) {
	index := make(map[string]remoteManifestEntry)
	for category, entries := range indexed {
		for _, entry := range entries {
			key := remoteDeltaKey(category, entry.Path)
			if _, dup := index[key]; dup {
				return remoteDeltaChanges{}, false
			}
			index[key] = entry
		}
	}
	changes.remove = make(map[string][]string)
	current := make(map[string]struct{}, len(files))
	for i := range files {
		file := &files[i]
		key := remoteDeltaKey(file.Category, file.RestorePath)
		if _, dup := current[key]; dup {
			return remoteDeltaChanges{}, false
		}
		current[key] = struct{}{}
		if entry, known := index[key]; known && entry.SHA256 == file.SHA256 && entry.Size == file.Size {
			continue
		}
		changes.upsertFiles = append(changes.upsertFiles, *file)
		changes.count++
	}
	changes.upsert = remoteCategories(changes.upsertFiles)
	for category, entries := range indexed {
		for _, entry := range entries {
			if _, kept := current[remoteDeltaKey(category, entry.Path)]; kept {
				continue
			}
			changes.remove[category] = append(changes.remove[category], entry.Path)
			changes.count++
		}
		sort.Strings(changes.remove[category])
	}
	return changes, true
}

// commitRemoteDelta commits this run as the changes against base. done is
// false with a nil error when the caller must send a full commit instead:
// the index is inconsistent, the change set is too large for one delta, or
// the server refused the base or does not take delta commits.
//
// A run with nothing changed sends no commit at all. The full commit that
// remoteFullCommitInterval forces keeps the snapshot verified in that case.
func (m *Manager) commitRemoteDelta(
	ctx context.Context,
	client *remoteClient,
	backupType string,
	files []FileRef,
	base *remoteDeltaState,
	warnings []models.BackupWarning,
) (result RemoteRunInfo, done bool, err error) {
	changes, ok := diffRemoteIndex(base.Files, files)
	if !ok || changes.count > remoteDeltaMaxChanges {
		return RemoteRunInfo{}, false, nil
	}
	var uploaded uploadResult
	missingSet := map[string]struct{}{}
	if len(changes.upsertFiles) > 0 {
		missing, checkErr := client.checkMissing(ctx, uniqueHashes(changes.upsertFiles))
		if checkErr != nil {
			return RemoteRunInfo{}, false, checkErr
		}
		missingSet = stringSet(missing)
		uploaded, err = m.uploadMissingWithQuotaPreflight(ctx, client, changes.upsertFiles, missingSet)
		if err != nil {
			return RemoteRunInfo{}, false, err
		}
		// Files that could not be stored leave the snapshot, exactly as they
		// leave a full commit, so the changes are worked out again without
		// them.
		for _, skipped := range [][]FileRef{uploaded.skipped, uploaded.quotaSkipped} {
			if len(skipped) == 0 {
				continue
			}
			files = withoutSkippedFiles(files, skipped)
			for i := range skipped {
				delete(missingSet, skipped[i].SHA256)
			}
		}
		if changes, ok = diffRemoteIndex(base.Files, files); !ok {
			return RemoteRunInfo{}, false, nil
		}
	}
	if len(files) == 0 {
		// The server refuses a delta that leaves no files. The full commit
		// path handles an empty run as it always has.
		return RemoteRunInfo{}, false, nil
	}

	backup := base.Backup
	list := RemoteListInfo{
		StorageUsedBytes: base.StorageUsedBytes, StorageQuotaBytes: base.StorageQuotaBytes,
	}
	if changes.count == 0 {
		backup.Deduplicated = true
	} else {
		committed, accepted, commitErr := m.sendRemoteDelta(ctx, client, backupType, files, base, &changes)
		if commitErr != nil || !accepted {
			return RemoteRunInfo{}, false, commitErr
		}
		backup = committed
		if fresh, listErr := m.ListRemote(ctx); listErr != nil {
			log.Warn().Err(listErr).Msg("failed to refresh remote backup quota after upload")
		} else {
			list = fresh
		}
		next := *base
		next.ManifestRootHash = backup.ManifestRootHash
		next.Backup = backup
		next.Files = remoteCategories(files)
		next.StorageUsedBytes = list.StorageUsedBytes
		next.StorageQuotaBytes = list.StorageQuotaBytes
		m.saveRemoteDeltaState(&next)
	}
	if len(uploaded.skipped) > 0 {
		m.notifyRemoteSkipped(uploaded.skipped)
	}
	if len(uploaded.quotaSkipped) > 0 {
		m.notifyRemoteQuotaSkipped(uploaded.quotaSkipped, uploaded.quotaRemains)
	}
	m.notifyRemoteWarnings(warnings)
	return newRemoteRunInfo(&backup, files, warnings, &uploaded, missingSet, &list), true, nil
}

// sendRemoteDelta posts the delta commit. accepted is false with a nil error
// when the caller must fall back to a full commit.
func (m *Manager) sendRemoteDelta(
	ctx context.Context,
	client *remoteClient,
	backupType string,
	files []FileRef,
	base *remoteDeltaState,
	changes *remoteDeltaChanges,
) (backup remoteBackupResponse, accepted bool, err error) {
	request := remoteDeltaRequest{
		BaseManifestRootHash: base.ManifestRootHash,
		BackupType:           backupType,
		SchemaVersion:        remoteSchemaVersion,
		CoreVersion:          &config.AppVersion,
		Upsert:               changes.upsert,
		Remove:               changes.remove,
	}
	body, err := json.Marshal(&request)
	if err != nil {
		return remoteBackupResponse{}, false, fmt.Errorf("encoding remote backup delta: %w", err)
	}
	if len(body) > remoteDeltaMaxBodyBytes {
		return remoteBackupResponse{}, false, nil
	}
	if verifyErr := verifyRemoteSources(ctx, changes.upsertFiles, m.pauser); verifyErr != nil {
		return remoteBackupResponse{}, false, verifyErr
	}
	// The server takes a repeated delta with the same body as the same
	// commit, so a rate-limited request is waited out and sent again as is.
	commitErr := client.retryRateLimited(ctx, func() error {
		backup = remoteBackupResponse{}
		return client.doJSON(ctx, http.MethodPost, "/v1/device/backups/delta", &request, &backup)
	})
	if commitErr != nil {
		if !m.remoteDeltaNeedsFullCommit(commitErr) {
			return remoteBackupResponse{}, false, commitErr
		}
		log.Info().Err(commitErr).Msg("remote backup delta commit refused, sending a full commit")
		return remoteBackupResponse{}, false, nil
	}
	if validateErr := validateRemoteDeltaResponse(&backup, files, m.pl.ID()); validateErr != nil {
		// The server answered, but not with a snapshot this client can
		// build on. Stop sending deltas until the next start rather than
		// pay for a delta and a full commit on every run.
		log.Warn().Err(validateErr).Msg("remote backup delta response rejected, sending a full commit")
		m.setRemoteDeltaUnsupported()
		return remoteBackupResponse{}, false, nil
	}
	return backup, true, nil
}

// remoteDeltaNeedsFullCommit reports whether a failed delta commit is
// answered by a full commit. A refused base or an oversized delta is expected
// and only costs this run. A 404 or 405 means the server has no delta
// endpoint, and any other rejection of the request means the two sides
// disagree about it: both stop delta commits for the rest of this process
// run, so they are tried again after the next start.
//
// A file that changed during the run or a snapshot over the file limit is
// returned as is: a full commit of the same files would be refused the same
// way, and neither says anything about the delta endpoint.
func (m *Manager) remoteDeltaNeedsFullCommit(err error) bool {
	if errors.Is(err, errRemoteMissingObjects) {
		// The full commit path uploads the objects again and retries.
		return true
	}
	if errors.Is(err, errRemoteIntegrityRetry) || errors.Is(err, errRemoteBackupTooLarge) {
		return false
	}
	apiErr, ok := AsAPIError(err)
	if !ok {
		return false
	}
	switch apiErr.Status {
	case http.StatusConflict, http.StatusBadRequest:
		if apiErr.Code != remoteErrorBaseMismatch && apiErr.Code != remoteErrorDeltaTooLarge {
			m.setRemoteDeltaUnsupported()
		}
		return true
	case http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusRequestEntityTooLarge,
		http.StatusUnprocessableEntity:
		m.setRemoteDeltaUnsupported()
		return true
	default:
		return false
	}
}

func (m *Manager) setRemoteDeltaUnsupported() {
	if m.coordinator != nil {
		m.coordinator.SetRemoteDeltaUnsupported(true)
	}
}

// validateRemoteDeltaResponse checks a delta commit response against the
// files this run committed. The response carries no manifest, so the totals
// the server reports for the resulting snapshot are what can be compared.
func validateRemoteDeltaResponse(
	resp *remoteBackupResponse, files []FileRef, expectedPlatform string,
) error {
	if resp.ID == "" || resp.SchemaVersion != remoteSchemaVersion {
		return errors.New("remote backup delta response metadata mismatch")
	}
	if resp.Platform == nil || *resp.Platform != expectedPlatform {
		return fmt.Errorf("remote backup platform does not match %q", expectedPlatform)
	}
	if !validRemoteRootHash(resp.ManifestRootHash) {
		return errors.New("remote backup delta response missing manifest root hash")
	}
	total, err := sumLogicalSize(files)
	if err != nil {
		return err
	}
	if total != resp.SizeBytes || !remoteSummariesEqual(resp.Categories, remoteCategorySummaries(files)) {
		return errors.New("remote backup delta response summary mismatch")
	}
	return nil
}
