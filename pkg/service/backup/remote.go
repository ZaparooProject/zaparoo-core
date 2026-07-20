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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	inboxservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Remote snapshot types accepted by the server for Core-initiated commits.
const (
	RemoteBackupTypeManual    = "manual"
	RemoteBackupTypeScheduled = "scheduled"
)

const (
	// remoteSchemaVersion is the manifest compatibility version Core commits
	// with and the newest version it can restore. Bump it whenever a change
	// makes older Cores unable to apply a restored backup (e.g. a breaking
	// user.db schema change): restore refuses snapshots with a newer version.
	remoteSchemaVersion         = 1
	remoteCheckBatchSize        = 10000
	remotePackTargetBytes       = 8 << 20
	remoteMaxPackBytes          = 64 << 20
	remotePackFooterTrailerSize = 4

	// remoteEmptyContentSHA256 is the sha256 of zero bytes. Empty files stay
	// in the manifest but are never packed or downloaded: the server treats
	// the empty object as always-present and Core synthesizes it on restore.
	remoteEmptyContentSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// remoteRequestTimeout bounds JSON API calls. Byte transfers (pack
	// uploads, object downloads) scale on top of it via remoteTransferTimeout
	// so a 64 MiB pack on a slow uplink is not killed at the base timeout.
	remoteRequestTimeout        = 60 * time.Second
	remoteTransferBytesPerSec   = 128 << 10
	remoteManifestResponseLimit = 64 << 20
	// remoteAvailabilityTTL caches a confirmed-available subscription check.
	// Any other result (unavailable, unknown) is rechecked on the shorter
	// retry TTL so a user who just subscribed or fixed connectivity is not
	// stuck behind a stale negative result.
	remoteAvailabilityTTL      = 1 * time.Hour
	remoteAvailabilityRetryTTL = 5 * time.Minute
)

var (
	errRemoteNotAvailable   = errors.New("remote backup is not available for this account")
	errRemoteQuotaExceeded  = errors.New("remote backup quota exceeded")
	errRemoteUnlinked       = errors.New("remote backup is unlinked")
	errRemoteRateLimited    = errors.New("remote backup rate limited")
	errRemoteIntegrityRetry = errors.New("remote backup integrity mismatch")
	errRemoteMissingObjects = errors.New("remote backup snapshot references missing objects")
	errRemoteNewerSchema    = errors.New("backup requires a newer Core version")
)

// RemoteRunInfo describes one completed remote backup run.
//
//nolint:govet // JSON response shape is grouped for API readability.
type RemoteRunInfo struct {
	Backup            RemoteBackupInfo                 `json:"backup"`
	Categories        map[string]remoteCategorySummary `json:"categories"`
	Warnings          []models.BackupWarning           `json:"warnings,omitempty"`
	UploadedFiles     int                              `json:"uploadedFiles"`
	DedupedFiles      int                              `json:"dedupedFiles"`
	SkippedFiles      int                              `json:"skippedFiles,omitempty"`
	UploadedPacks     int                              `json:"uploadedPacks"`
	UploadedBytes     int64                            `json:"uploadedBytes"`
	StorageUsedBytes  int64                            `json:"storageUsedBytes,omitempty"`
	StorageQuotaBytes int64                            `json:"storageQuotaBytes,omitempty"`
}

// RemoteRestoreInfo describes one completed remote restore.
//
//nolint:govet // JSON response shape is grouped for API readability.
type RemoteRestoreInfo struct {
	PreRestoreBackup *Info            `json:"preRestoreBackup,omitempty"`
	RestoredFrom     RemoteBackupInfo `json:"restoredFrom"`
}

// RemoteListInfo contains remote backups and quota usage.
//
//nolint:govet // JSON response shape is grouped for API readability.
type RemoteListInfo struct {
	Items             []RemoteBackupInfo `json:"items"`
	StorageUsedBytes  int64              `json:"storageUsedBytes"`
	StorageQuotaBytes int64              `json:"storageQuotaBytes"`
}

// RemoteBackupInfo is remote snapshot metadata returned to Core clients.
//
//nolint:govet // JSON response shape is grouped for API readability.
type RemoteBackupInfo struct {
	CoreVersion   *string                          `json:"coreVersion,omitempty"`
	Platform      *string                          `json:"platform,omitempty"`
	RestoredAt    *time.Time                       `json:"restoredAt,omitempty"`
	SourceDevice  *RemoteBackupSourceDevice        `json:"sourceDevice,omitempty"`
	Categories    map[string]remoteCategorySummary `json:"categories"`
	ManifestHash  string                           `json:"manifestHash"`
	BackupType    string                           `json:"backupType"`
	CreatedAt     time.Time                        `json:"createdAt"`
	Manifest      json.RawMessage                  `json:"manifest,omitempty"`
	ID            string                           `json:"id"`
	SchemaVersion int                              `json:"schemaVersion"`
	SizeBytes     int64                            `json:"sizeBytes"`
	// Incompatible marks snapshots committed with a newer schema version
	// than this Core supports: they list fine but refuse to restore.
	Incompatible bool `json:"incompatible,omitempty"`
}

// RemoteBackupSourceDevice identifies the account device that created a
// snapshot. Current is relative to the device requesting the catalog.
type RemoteBackupSourceDevice struct {
	Platform *string `json:"platform,omitempty"`
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Linked   bool    `json:"linked"`
	Current  bool    `json:"current"`
}

type remoteCategorySummary struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// remoteManifestFormat is the canonical manifest format identifier shared
// with the server.
const remoteManifestFormat = "cas-v1"

type remoteManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

//nolint:govet // JSON wire shape is grouped for readability.
type remoteManifest struct {
	Format     string                           `json:"format"`
	Categories map[string][]remoteManifestEntry `json:"categories"`
}

//nolint:tagliatelle,govet // Remote API contract uses snake_case JSON fields.
type remoteSnapshotRequest struct {
	CoreVersion   *string                          `json:"core_version,omitempty"`
	Categories    map[string][]remoteManifestEntry `json:"categories"`
	BackupType    string                           `json:"backup_type"`
	SchemaVersion int                              `json:"schema_version"`
}

type remoteCheckRequest struct {
	Hashes []string `json:"hashes"`
}

type remoteCheckResponse struct {
	Missing []string `json:"missing"`
}

//nolint:tagliatelle,govet // Remote API contract uses snake_case JSON fields.
type remoteListResponse struct {
	Items             []remoteBackupResponse `json:"items"`
	StorageUsedBytes  int64                  `json:"storage_used_bytes"`
	StorageQuotaBytes int64                  `json:"storage_quota_bytes"`
}

//nolint:tagliatelle,govet // Remote API contract uses snake_case JSON fields.
type remoteBackupResponse struct {
	CoreVersion   *string                          `json:"core_version,omitempty"`
	Platform      *string                          `json:"platform,omitempty"`
	RestoredAt    *time.Time                       `json:"restored_at,omitempty"`
	SourceDevice  *remoteBackupSourceDevice        `json:"source_device,omitempty"`
	Categories    map[string]remoteCategorySummary `json:"categories"`
	Manifest      json.RawMessage                  `json:"manifest,omitempty"`
	ManifestHash  string                           `json:"manifest_hash"`
	BackupType    string                           `json:"backup_type"`
	CreatedAt     time.Time                        `json:"created_at"`
	ID            string                           `json:"id"`
	SchemaVersion int                              `json:"schema_version"`
	SizeBytes     int64                            `json:"size_bytes"`
}

//nolint:tagliatelle,govet // Remote API contract uses snake_case JSON fields.
type remoteBackupSourceDevice struct {
	Platform *string `json:"platform,omitempty"`
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Linked   bool    `json:"linked"`
	Current  bool    `json:"current"`
}

//nolint:tagliatelle // Remote API contract uses snake_case JSON fields.
type remoteRestoreCompleteRequest struct {
	RestoreID string `json:"restore_id"`
}

//nolint:tagliatelle // Remote API contract uses snake_case JSON fields.
type remotePackResponse struct {
	CreatedAt   time.Time `json:"created_at"`
	PackHash    string    `json:"pack_hash"`
	SizeBytes   int64     `json:"size_bytes"`
	ObjectCount int       `json:"object_count"`
}

//nolint:tagliatelle,govet // Remote API contract uses snake_case JSON fields.
type remoteDeviceMeResponse struct {
	Name         string    `json:"name"`
	LinkedAt     time.Time `json:"linked_at"`
	ID           string    `json:"id"`
	BackupActive bool      `json:"backup_active"`
}

type remoteAPIErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type remoteAPIError struct {
	Error remoteAPIErrorBody `json:"error"`
}

type packFooterEntry struct {
	Hash   string `json:"hash"`
	Offset int64  `json:"offset"`
	Length int64  `json:"length"`
}

type remoteClient struct {
	httpClient     *http.Client
	onUnauthorized func()
	baseURL        string
	bearer         string
	platform       string
}

func (m *Manager) RunRemote(ctx context.Context, backupType string) (RemoteRunInfo, error) {
	if backupType != RemoteBackupTypeManual && backupType != RemoteBackupTypeScheduled {
		return RemoteRunInfo{}, fmt.Errorf("invalid remote backup type: %s", backupType)
	}
	lease, err := m.begin(ctx, OperationRemoteUpload, OperationWrite)
	if err != nil {
		return RemoteRunInfo{}, err
	}
	defer lease.Release()
	ctx = lease.Context()

	started := time.Now().UTC()
	_ = m.writeRemoteStatus(&statusEntry{LastRunAt: formatTime(started), LastStatus: StatusRunning})

	info, err := m.createRemoteSnapshot(ctx, backupType)
	if errors.Is(err, errRemoteIntegrityRetry) {
		// A file changed between hashing and packing (e.g. a save written
		// mid-run). Re-collect and re-hash everything once; the second pass
		// sees the settled bytes.
		log.Warn().Msg("remote backup integrity mismatch, re-reading files and retrying once")
		info, err = m.createRemoteSnapshot(ctx, backupType)
	}
	if err != nil {
		failed := &statusEntry{
			LastRunAt:  formatTime(started),
			LastStatus: StatusFailed,
			LastError:  safeStatusError(err),
		}
		if errors.Is(err, errRemoteUnlinked) {
			failed.Unlinked = true
		}
		_ = m.writeRemoteStatus(failed)
		m.notifyRemoteFailure(err)
		return RemoteRunInfo{}, err
	}
	lastStatus := StatusSuccess
	if len(info.Warnings) > 0 || info.SkippedFiles > 0 {
		lastStatus = StatusPartial
	}
	_ = m.writeRemoteStatus(&statusEntry{
		LastRunAt:      formatTime(started),
		LastSuccessAt:  formatTime(info.Backup.CreatedAt),
		LastStatus:     lastStatus,
		LastBackupSize: info.Backup.SizeBytes,
		Categories:     remoteCategoriesToStatus(info.Categories),
		Warnings:       info.Warnings,
		SkippedFiles:   len(info.Warnings) + info.SkippedFiles,
	})
	return info, nil
}

func (m *Manager) ListRemote(ctx context.Context) (RemoteListInfo, error) {
	client, err := m.newRemoteClient()
	if err != nil {
		return RemoteListInfo{}, err
	}
	var resp remoteListResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/device/backups", nil, &resp); err != nil {
		return RemoteListInfo{}, err
	}
	items := make([]RemoteBackupInfo, 0, len(resp.Items))
	for i := range resp.Items {
		items = append(items, remoteBackupToInfo(&resp.Items[i]))
	}
	return RemoteListInfo{
		Items: items, StorageUsedBytes: resp.StorageUsedBytes, StorageQuotaBytes: resp.StorageQuotaBytes,
	}, nil
}

// RevokeRemoteLink invalidates the current device on the backup server before
// Core removes its local bearer. An already-missing or revoked credential is
// treated as success so local cleanup can finish.
func (m *Manager) RevokeRemoteLink(ctx context.Context) error {
	client, err := m.newRemoteClient()
	if errors.Is(err, errRemoteUnlinked) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := client.doJSON(ctx, http.MethodDelete, "/v1/device/me", nil, nil); err != nil &&
		!errors.Is(err, errRemoteUnlinked) {
		return err
	}
	return nil
}

func (m *Manager) RestoreRemote(ctx context.Context, id string) (RemoteRestoreInfo, error) {
	if id == "" {
		return RemoteRestoreInfo{}, errors.New("invalid remote backup id")
	}
	restoreID := uuid.NewString()
	lease, err := m.begin(ctx, OperationRemoteRestore, OperationWrite)
	if err != nil {
		return RemoteRestoreInfo{}, err
	}
	defer lease.Release()
	ctx = lease.Context()
	finishRestore, err := m.beginRestoreGate()
	if err != nil {
		return RemoteRestoreInfo{}, err
	}
	restoreSucceeded := false
	defer func() { finishRestore(restoreSucceeded) }()
	if idleErr := m.requireRestoreIdle(); idleErr != nil {
		return RemoteRestoreInfo{}, idleErr
	}
	if recoveryErr := m.recoverRestoreLocked(ctx); recoveryErr != nil {
		return RemoteRestoreInfo{}, recoveryErr
	}
	client, err := m.newRemoteClient()
	if err != nil {
		return RemoteRestoreInfo{}, err
	}
	var resp remoteBackupResponse
	backupPath := remoteBackupPath(id)
	getErr := client.doJSONLimit(ctx, http.MethodGet, backupPath, nil, &resp, remoteManifestResponseLimit)
	if getErr != nil {
		return RemoteRestoreInfo{}, getErr
	}
	if resp.ID == "" || resp.ID != id {
		return RemoteRestoreInfo{}, fmt.Errorf(
			"remote backup response ID %q does not match requested ID %q", resp.ID, id,
		)
	}
	if resp.SchemaVersion > remoteSchemaVersion {
		return RemoteRestoreInfo{}, fmt.Errorf(
			"%w: backup schema version %d is newer than supported version %d",
			errRemoteNewerSchema, resp.SchemaVersion, remoteSchemaVersion,
		)
	}
	manifest, err := validateRemoteManifestResponse(&resp, m.pl.ID())
	if err != nil {
		return RemoteRestoreInfo{}, err
	}
	files := remoteManifestFiles(manifest)
	if validateErr := validateFiles(files); validateErr != nil {
		return RemoteRestoreInfo{}, validateErr
	}
	if len(files) > maxArchiveEntries-1 {
		return RemoteRestoreInfo{}, fmt.Errorf("remote backup has too many files: %d", len(files))
	}
	if _, validateErr := validateLogicalSize(files, m.cfg.BackupMaxSizeBytes()); validateErr != nil {
		return RemoteRestoreInfo{}, validateErr
	}
	for _, file := range files {
		hash, decodeErr := hex.DecodeString(file.SHA256)
		if decodeErr != nil || len(hash) != sha256.Size {
			return RemoteRestoreInfo{}, fmt.Errorf("invalid remote backup SHA-256 for %s", file.RestorePath)
		}
	}
	if policyErr := m.validateManifestPolicy(&Manifest{Platform: *resp.Platform, Files: files}); policyErr != nil {
		return RemoteRestoreInfo{}, policyErr
	}

	// Download and verify every payload before touching the device: a
	// mid-restore network failure or hash mismatch must leave it unchanged.
	staged, cleanup, err := m.stageRemotePayloads(ctx, client, files)
	if err != nil {
		return RemoteRestoreInfo{}, err
	}
	defer cleanup()

	pre, err := m.createBackup(ctx, true)
	if err != nil {
		return RemoteRestoreInfo{}, fmt.Errorf("creating pre-restore backup: %w", err)
	}
	if idleErr := m.requireRestoreIdle(); idleErr != nil {
		return RemoteRestoreInfo{}, idleErr
	}
	finishPlatformRestore, err := m.preparePlatformRestore()
	if err != nil {
		return RemoteRestoreInfo{}, err
	}
	if err = m.applyRestore(ctx, &Manifest{Files: files}, func(file FileRef) (io.ReadCloser, error) {
		return staged.open(file.SHA256, file.Size)
	}); err != nil {
		return RemoteRestoreInfo{}, errors.Join(err, finishPlatformRestore(false))
	}
	if finishErr := finishPlatformRestore(true); finishErr != nil {
		log.Warn().Err(finishErr).Msg("committed remote restore profile cleanup deferred until restart")
	}
	restoreCompletePath := remoteBackupPath(id) + "/restore-complete"
	complete := remoteRestoreCompleteRequest{RestoreID: restoreID}
	if err := client.doJSON(ctx, http.MethodPost, restoreCompletePath, &complete, nil); err != nil {
		log.Warn().Err(err).Str("backup_id", id).Str("restore_id", restoreID).
			Msg("failed to mark remote backup restored")
	} else {
		completed := log.Info().Str("backup_id", id).Str("restore_id", restoreID).
			Str("target_device_hint", m.cfg.DeviceID())
		if resp.SourceDevice != nil {
			completed = completed.Str("source_device_id", resp.SourceDevice.ID)
		}
		completed.Msg("remote backup restore recorded")
	}
	preInfo := pre
	restoreSucceeded = true
	return RemoteRestoreInfo{PreRestoreBackup: &preInfo, RestoredFrom: remoteBackupToInfo(&resp)}, nil
}

// remoteStaging holds downloaded, hash-verified payloads on disk, keyed by
// content hash, so the apply phase never depends on the network.
type remoteStaging struct {
	dir string
}

func (s *remoteStaging) path(hash string) string { return filepath.Join(s.dir, hash) }

func (s *remoteStaging) open(hash string, wantSize int64) (io.ReadCloser, error) {
	if hash == remoteEmptyContentSHA256 {
		return io.NopCloser(strings.NewReader("")), nil
	}
	file, err := os.Open(s.path(hash))
	if err != nil {
		return nil, fmt.Errorf("opening staged restore payload %s: %w", hash, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stating staged restore payload %s: %w", hash, err)
	}
	if info.Size() != wantSize {
		_ = file.Close()
		return nil, fmt.Errorf("staged restore payload size mismatch: %s", hash)
	}
	return file, nil
}

// stageRemotePayloads downloads and verifies every unique non-empty object
// referenced by files into a temporary directory under the backup dir. The
// returned cleanup removes the staging directory; it is safe to call after a
// partial failure.
func (m *Manager) stageRemotePayloads(
	ctx context.Context,
	client *remoteClient,
	files []FileRef,
) (*remoteStaging, func(), error) {
	if err := os.MkdirAll(m.backupDir(), 0o750); err != nil {
		return nil, nil, fmt.Errorf("creating backup directory: %w", err)
	}
	var required int64
	sizes := make(map[string]int64, len(files))
	for _, file := range files {
		if file.SHA256 == remoteEmptyContentSHA256 {
			continue
		}
		if size, ok := sizes[file.SHA256]; ok {
			if size != file.Size {
				return nil, nil, fmt.Errorf("remote manifest has conflicting sizes for %s", file.SHA256)
			}
			continue
		}
		if file.Size < 0 || file.Size > m.cfg.BackupMaxSizeBytes()-required {
			return nil, nil, errors.New("remote restore exceeds backup size limit")
		}
		sizes[file.SHA256] = file.Size
		required += file.Size
	}
	free, err := helpers.FreeDiskSpace(m.backupDir())
	if err != nil {
		return nil, nil, fmt.Errorf("checking remote restore staging space: %w", err)
	}
	if uint64(required) > free {
		return nil, nil, fmt.Errorf("insufficient disk space to stage remote restore: need %d bytes", required)
	}
	dir, err := os.MkdirTemp(m.backupDir(), "remote-restore-")
	if err != nil {
		return nil, nil, fmt.Errorf("creating restore staging directory: %w", err)
	}
	staging := &remoteStaging{dir: dir}
	cleanup := func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			log.Debug().Err(removeErr).Str("dir", dir).Msg("failed to remove restore staging directory")
		}
	}

	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.SHA256 == remoteEmptyContentSHA256 {
			continue
		}
		if _, ok := seen[file.SHA256]; ok {
			continue
		}
		seen[file.SHA256] = struct{}{}
		downloadErr := client.downloadObject(
			ctx, file.SHA256, file.Size, staging.path(file.SHA256),
		)
		if downloadErr != nil {
			cleanup()
			return nil, nil, downloadErr
		}
	}
	return staging, cleanup, nil
}

func (m *Manager) createRemoteSnapshot(ctx context.Context, backupType string) (result RemoteRunInfo, err error) {
	client, err := m.newRemoteClient()
	if err != nil {
		return RemoteRunInfo{}, err
	}
	if waitErr := m.pauser.Wait(ctx); waitErr != nil {
		return RemoteRunInfo{}, fmt.Errorf("creating remote backup snapshot: %w", waitErr)
	}
	heartbeatErr := client.heartbeat(ctx)
	if heartbeatErr != nil {
		return RemoteRunInfo{}, heartbeatErr
	}
	availability, err := m.updateRemoteAvailability(ctx, client)
	if err != nil {
		return RemoteRunInfo{}, err
	}
	if availability != RemoteAvailabilityAvailable {
		return RemoteRunInfo{}, errRemoteNotAvailable
	}
	collection, err := m.collectFiles(ctx, "remote-"+backupType, m.cfg.BackupScope())
	if err != nil {
		return RemoteRunInfo{}, err
	}
	defer func() {
		if cleanupErr := collection.Cleanup(); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	files := collection.Files
	validateErr := validateFiles(files)
	if validateErr != nil {
		return RemoteRunInfo{}, validateErr
	}
	if len(files) > maxArchiveEntries-1 {
		return RemoteRunInfo{}, fmt.Errorf(
			"backup has too many entries: %d exceeds %d", len(files), maxArchiveEntries-1,
		)
	}
	if _, sizeErr := validateLogicalSize(files, m.cfg.BackupMaxSizeBytes()); sizeErr != nil {
		return RemoteRunInfo{}, sizeErr
	}
	files, unreadableWarnings, err := prepareSourceFiles(ctx, files, m.sourceOpener, m.pauser)
	if err != nil {
		if errors.Is(err, errSourceIdentityChanged) {
			return RemoteRunInfo{}, fmt.Errorf("%w: %w", errRemoteIntegrityRetry, err)
		}
		return RemoteRunInfo{}, err
	}
	collection.Warnings, err = appendBackupWarnings(collection.Warnings, unreadableWarnings)
	if err != nil {
		return RemoteRunInfo{}, err
	}
	missing, err := client.checkMissing(ctx, uniqueHashes(files))
	if err != nil {
		return RemoteRunInfo{}, err
	}
	missingSet := stringSet(missing)
	uploaded, err := client.uploadMissing(ctx, files, missingSet, m.pauser)
	if err != nil {
		return RemoteRunInfo{}, err
	}
	if len(uploaded.skipped) > 0 {
		// Files too large to fit in a single pack cannot be stored under
		// this protocol: surface them and back up everything else.
		files = withoutSkippedFiles(files, uploaded.skipped)
		for _, skipped := range uploaded.skipped {
			delete(missingSet, skipped.SHA256)
		}
		m.notifyRemoteSkipped(uploaded.skipped)
	}
	request := remoteSnapshotRequest{
		BackupType:    backupType,
		SchemaVersion: remoteSchemaVersion,
		CoreVersion:   &config.AppVersion,
		Categories:    remoteCategories(files),
	}
	if verifyErr := verifyRemoteSources(ctx, files, m.pauser); verifyErr != nil {
		return RemoteRunInfo{}, verifyErr
	}
	var backup remoteBackupResponse
	commit := func() error {
		backup = remoteBackupResponse{}
		return client.doJSONLimit(
			ctx, http.MethodPost, "/v1/device/backups", &request, &backup, remoteManifestResponseLimit,
		)
	}
	commitErr := commit()
	if errors.Is(commitErr, errRemoteMissingObjects) {
		if verifyErr := verifyRemoteSources(ctx, files, m.pauser); verifyErr != nil {
			return RemoteRunInfo{}, verifyErr
		}
		repairMissing, checkErr := client.checkMissing(ctx, uniqueHashes(files))
		if checkErr != nil {
			return RemoteRunInfo{}, checkErr
		}
		repairSet := stringSet(repairMissing)
		repair, repairErr := client.uploadMissing(ctx, files, repairSet, m.pauser)
		if repairErr != nil {
			return RemoteRunInfo{}, repairErr
		}
		if len(repair.skipped) > 0 {
			return RemoteRunInfo{}, errors.New("remote backup could not repair missing oversized objects")
		}
		uploaded.packs += repair.packs
		uploaded.bytesUploaded += repair.bytesUploaded
		for hash := range repairSet {
			missingSet[hash] = struct{}{}
		}
		if verifyErr := verifyRemoteSources(ctx, files, m.pauser); verifyErr != nil {
			return RemoteRunInfo{}, verifyErr
		}
		commitErr = commit()
	}
	if commitErr != nil {
		return RemoteRunInfo{}, commitErr
	}
	if validateErr := validateCommittedRemoteBackup(
		&backup, files, m.pl.ID(), backupType,
	); validateErr != nil {
		return RemoteRunInfo{}, validateErr
	}
	list, listErr := m.ListRemote(ctx)
	if listErr != nil {
		log.Debug().Err(listErr).Msg("failed to refresh remote backup quota after upload")
	}
	m.notifyRemoteWarnings(collection.Warnings)
	uploadedFiles := 0
	for _, hash := range uniqueHashes(files) {
		if hash == remoteEmptyContentSHA256 {
			continue
		}
		if _, ok := missingSet[hash]; ok {
			uploadedFiles++
		}
	}
	return RemoteRunInfo{
		Backup:            remoteBackupToInfo(&backup),
		Categories:        remoteCategorySummaries(files),
		Warnings:          collection.Warnings,
		UploadedFiles:     uploadedFiles,
		DedupedFiles:      len(uniqueHashes(files)) - uploadedFiles,
		SkippedFiles:      len(uploaded.skipped),
		UploadedPacks:     uploaded.packs,
		UploadedBytes:     uploaded.bytesUploaded,
		StorageUsedBytes:  list.StorageUsedBytes,
		StorageQuotaBytes: list.StorageQuotaBytes,
	}, nil
}

// expandSkippedFiles widens a skipped set to every file sharing a skipped
// content hash, so warnings and manifest filtering cover deduplicated
// duplicates of an unstorable file too.
func expandSkippedFiles(files, skipped []FileRef) []FileRef {
	skippedHashes := make(map[string]struct{}, len(skipped))
	for i := range skipped {
		skippedHashes[skipped[i].SHA256] = struct{}{}
	}
	expanded := make([]FileRef, 0, len(skipped))
	for _, file := range files {
		if _, ok := skippedHashes[file.SHA256]; ok {
			expanded = append(expanded, file)
		}
	}
	return expanded
}

// withoutSkippedFiles drops every file whose content hash was skipped at
// upload time, so the committed manifest never references bytes the server
// does not have.
func withoutSkippedFiles(files, skipped []FileRef) []FileRef {
	skippedHashes := make(map[string]struct{}, len(skipped))
	for i := range skipped {
		skippedHashes[skipped[i].SHA256] = struct{}{}
	}
	kept := make([]FileRef, 0, len(files))
	for _, file := range files {
		if _, ok := skippedHashes[file.SHA256]; ok {
			continue
		}
		kept = append(kept, file)
	}
	return kept
}

func (m *Manager) newRemoteClient() (*remoteClient, error) {
	if m.coordinator != nil && m.coordinator.RemoteUnlinked() {
		return nil, errRemoteUnlinked
	}
	if m.readStatus().Remote.Unlinked {
		if m.coordinator != nil {
			m.coordinator.SetRemoteUnlinked(true)
		}
		return nil, errRemoteUnlinked
	}
	baseURL := strings.TrimRight(m.cfg.BackupRemoteBaseURL(), "/")
	lookupURL := config.BackupAuthLookupURL(baseURL)
	entry := config.LookupAuth(config.GetAuthCfg(), lookupURL)
	if entry == nil || entry.Bearer == "" {
		return nil, errRemoteUnlinked
	}
	bearer := entry.Bearer
	return &remoteClient{
		// Timeouts are applied per request (scaled to transfer size for
		// uploads/downloads), not on the client, so a large pack on a slow
		// uplink is not killed at the base timeout.
		httpClient: &http.Client{},
		onUnauthorized: func() {
			m.markRemoteUnlinkedIfCurrent(bearer)
		},
		baseURL:  baseURL,
		bearer:   bearer,
		platform: m.pl.ID(),
	}, nil
}

// remoteTransferTimeout returns the request timeout for a transfer of the
// given size: the base request timeout plus time for the bytes at a
// deliberately pessimistic throughput floor.
func remoteTransferTimeout(sizeBytes int64) time.Duration {
	timeout := remoteRequestTimeout
	if sizeBytes > 0 {
		timeout += time.Duration(sizeBytes/remoteTransferBytesPerSec) * time.Second
	}
	return timeout
}

func (m *Manager) notifyRemoteFailure(err error) {
	if m.inbox == nil {
		return
	}

	var busy *BusyError
	var title, body, category string
	switch {
	case errors.Is(err, errRemoteNotAvailable):
		title = "Remote backup not available"
		body = "Remote backup is not available for this account. Local backups still work."
		category = inboxservice.CategoryBackupRemoteNotAvailable
	case errors.Is(err, errRemoteQuotaExceeded):
		title = "Remote backup storage full"
		body = "Remote backup could not run because storage quota was reached. " +
			"Delete remote backups or reduce backup size."
		category = inboxservice.CategoryBackupRemoteQuotaExceeded
	case errors.Is(err, errRemoteUnlinked):
		title = "Remote backup needs relinking"
		body = "Remote backup could not run because this device is not linked. " +
			"Relink Zaparoo Online to resume remote backups."
		category = inboxservice.CategoryBackupRemoteUnlinked
	case errors.As(err, &busy):
		// Not a failure worth an inbox message: another run is in flight.
		return
	default:
		title = "Remote backup failed"
		body = "Remote backup did not complete. It will be retried automatically; " +
			"check the backup status screen for details."
		category = inboxservice.CategoryBackupRemoteFailed
	}

	if addErr := m.inbox.Add(
		title,
		inboxservice.WithBody(body),
		inboxservice.WithSeverity(inboxservice.SeverityError),
		inboxservice.WithCategory(category),
	); addErr != nil {
		log.Warn().Err(addErr).Str("category", category).Msg("failed to add remote backup inbox message")
	}
}

// notifyRemoteSkipped surfaces files dropped from a remote backup because
// they cannot fit inside a single pack.
func (m *Manager) notifyRemoteSkipped(skipped []FileRef) {
	if m.inbox == nil || len(skipped) == 0 {
		return
	}
	names := make([]string, 0, len(skipped))
	for i := range skipped {
		names = append(names, skipped[i].RestorePath)
	}
	body := fmt.Sprintf(
		"%d file(s) were too large for remote backup and were not uploaded: %s",
		len(skipped), strings.Join(names, ", "),
	)
	if len(body) > 500 {
		body = body[:500] + "…"
	}
	if addErr := m.inbox.Add(
		"Some files were not backed up remotely",
		inboxservice.WithBody(body),
		inboxservice.WithSeverity(inboxservice.SeverityWarning),
		inboxservice.WithCategory(inboxservice.CategoryBackupRemoteFilesSkipped),
	); addErr != nil {
		log.Warn().Err(addErr).Msg("failed to add remote backup skipped-files inbox message")
	}
}

func (m *Manager) notifyRemoteWarnings(warnings []models.BackupWarning) {
	if m.inbox == nil || len(warnings) == 0 {
		return
	}
	details := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		details = append(details, warning.Path+" ("+warning.Reason+")")
	}
	body := fmt.Sprintf(
		"%d path(s) could not be backed up: %s", len(warnings), strings.Join(details, ", "),
	)
	if len(body) > 500 {
		body = body[:500] + "…"
	}
	if addErr := m.inbox.Add(
		"Remote backup completed with warnings",
		inboxservice.WithBody(body),
		inboxservice.WithSeverity(inboxservice.SeverityWarning),
		inboxservice.WithCategory(inboxservice.CategoryBackupRemoteFilesSkipped),
	); addErr != nil {
		log.Warn().Err(addErr).Msg("failed to add remote backup warning inbox message")
	}
}

// SendHeartbeat reports liveness (Core version + capabilities) when the
// device is linked. Callers use it independently of backup runs so "last
// seen" stays fresh even with remote backup disabled.
func (m *Manager) SendHeartbeat(ctx context.Context) error {
	client, err := m.newRemoteClient()
	if err != nil {
		return err
	}
	if heartbeatErr := client.heartbeat(ctx); heartbeatErr != nil {
		return heartbeatErr
	}
	_, err = m.updateRemoteAvailability(ctx, client)
	return err
}

func (m *Manager) RefreshRemoteAvailability(ctx context.Context) (string, error) {
	client, err := m.newRemoteClient()
	if err != nil {
		return RemoteAvailabilityUnknown, err
	}
	return m.updateRemoteAvailability(ctx, client)
}

func RemoteAvailabilityNeedsRefresh(now time.Time, status *models.BackupStatusEntry) bool {
	if status == nil || status.AvailabilityCheckedAt == nil {
		return true
	}
	checkedAt, err := time.Parse(time.RFC3339Nano, *status.AvailabilityCheckedAt)
	if err != nil || now.Before(checkedAt) {
		return true
	}
	ttl := remoteAvailabilityTTL
	if status.Availability != RemoteAvailabilityAvailable {
		ttl = remoteAvailabilityRetryTTL
	}
	return now.Sub(checkedAt) >= ttl
}

// cachedRemoteAvailability returns the stored availability and whether it is
// still within its TTL.
func cachedRemoteAvailability(remote *statusEntry) (string, bool) {
	availability := remote.Availability
	if availability == "" {
		availability = RemoteAvailabilityUnknown
	}
	checkedAt, err := time.Parse(time.RFC3339Nano, remote.AvailabilityCheckedAt)
	if err != nil {
		return availability, false
	}
	ttl := remoteAvailabilityTTL
	if availability != RemoteAvailabilityAvailable {
		ttl = remoteAvailabilityRetryTTL
	}
	age := time.Since(checkedAt)
	return availability, age >= 0 && age < ttl
}

func (m *Manager) RefreshRemoteAvailabilityIfStale(ctx context.Context) (string, error) {
	remote := m.readStatus().Remote
	if availability, fresh := cachedRemoteAvailability(&remote); fresh {
		return availability, nil
	}
	return m.RefreshRemoteAvailability(ctx)
}

// availabilityRefreshInFlight dedupes background availability refreshes.
// Managers are constructed per request, so the flag is package-level like
// the status file lock.
var availabilityRefreshInFlight atomic.Bool

// RefreshRemoteAvailabilityIfStaleAsync refreshes remote availability in the
// background when the cached value is past its TTL, so status requests
// return immediately instead of blocking on a network round trip.
func (m *Manager) RefreshRemoteAvailabilityIfStaleAsync() {
	if !m.Status().Remote.Linked {
		return
	}
	remote := m.readStatus().Remote
	if _, fresh := cachedRemoteAvailability(&remote); fresh {
		return
	}
	if !availabilityRefreshInFlight.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer availabilityRefreshInFlight.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), remoteRequestTimeout)
		defer cancel()
		if _, err := m.RefreshRemoteAvailability(ctx); err != nil {
			log.Debug().Err(err).Msg("background remote availability refresh failed")
		}
	}()
}

func (m *Manager) updateRemoteAvailability(
	ctx context.Context, client *remoteClient,
) (string, error) {
	me, err := client.deviceMe(ctx)
	if err != nil {
		if !errors.Is(err, errRemoteUnlinked) {
			m.setRemoteAvailability(RemoteAvailabilityUnknown, time.Now().UTC(), nil)
		}
		return RemoteAvailabilityUnknown, err
	}
	availability := RemoteAvailabilityUnavailable
	if me.BackupActive {
		availability = RemoteAvailabilityAvailable
	}
	m.setRemoteAvailability(availability, time.Now().UTC(), me)
	return availability, nil
}

// setRemoteAvailability persists the availability check result. When the
// check reached the server, the device identity it reported (name, link
// time) is recorded alongside so the UI can show what this device is
// linked as.
func (m *Manager) setRemoteAvailability(
	availability string, checkedAt time.Time, me *remoteDeviceMeResponse,
) {
	statusMu.Lock()
	defer statusMu.Unlock()
	st := m.readStatusLocked()
	st.Remote.Availability = availability
	st.Remote.AvailabilityCheckedAt = formatTime(checkedAt)
	if me != nil {
		st.Remote.DeviceName = me.Name
		st.Remote.LinkedAt = ""
		if !me.LinkedAt.IsZero() {
			st.Remote.LinkedAt = formatTime(me.LinkedAt)
		}
	}
	if err := m.writeStatusLocked(&st); err != nil {
		log.Warn().Err(err).Msg("failed to persist remote backup availability")
	}
}

func (m *Manager) markRemoteUnlinkedIfCurrent(rejectedBearer string) {
	baseURL := strings.TrimRight(m.cfg.BackupRemoteBaseURL(), "/")
	lookupURL := config.BackupAuthLookupURL(baseURL)
	current := config.LookupAuth(config.GetAuthCfg(), lookupURL)
	if current == nil || current.Bearer == "" || current.Bearer != rejectedBearer {
		log.Debug().Msg("ignoring unauthorized response for superseded remote backup credential")
		return
	}
	m.MarkRemoteUnlinked()
}

// MarkRemoteUnlinked records that no valid remote credential exists (the
// token was revoked server-side or removed by logout), so the status UI
// prompts a re-link and the scheduler stops attempting remote backups.
func (m *Manager) MarkRemoteUnlinked() {
	if m.coordinator != nil {
		m.coordinator.SetRemoteUnlinked(true)
	}
	statusMu.Lock()
	defer statusMu.Unlock()

	st := m.readStatusLocked()
	st.Remote.Unlinked = true
	st.Remote.Availability = RemoteAvailabilityUnknown
	st.Remote.AvailabilityCheckedAt = ""
	st.Remote.DeviceName = ""
	st.Remote.LinkedAt = ""
	if err := m.writeStatusLocked(&st); err != nil {
		log.Warn().Err(err).Msg("failed to persist remote backup revocation")
	}
}

// MarkRemoteLinked clears a persisted unlinked marker after a successful
// claim/link, so the status UI reflects the fresh credential immediately.
func (m *Manager) MarkRemoteLinked() {
	if m.coordinator != nil {
		m.coordinator.SetRemoteUnlinked(false)
	}
	statusMu.Lock()
	defer statusMu.Unlock()

	st := m.readStatusLocked()
	st.Remote.Unlinked = false
	st.Remote.Availability = RemoteAvailabilityUnknown
	st.Remote.AvailabilityCheckedAt = ""
	st.Remote.DeviceName = ""
	st.Remote.LinkedAt = ""
	if err := m.writeStatusLocked(&st); err != nil {
		log.Warn().Err(err).Msg("failed to clear remote backup unlinked marker")
	}
}

func (m *Manager) writeRemoteStatus(remote *statusEntry) error {
	statusMu.Lock()
	defer statusMu.Unlock()

	st := m.readStatusLocked()
	if remote.Categories == nil && st.Remote.Categories != nil {
		remote.Categories = st.Remote.Categories
	}
	if remote.LastSuccessAt == "" {
		remote.LastSuccessAt = st.Remote.LastSuccessAt
	}
	if remote.Availability == "" {
		remote.Availability = st.Remote.Availability
	}
	if remote.AvailabilityCheckedAt == "" {
		remote.AvailabilityCheckedAt = st.Remote.AvailabilityCheckedAt
	}
	if remote.ScheduleEnabledSince == "" {
		remote.ScheduleEnabledSince = st.Remote.ScheduleEnabledSince
	}
	if remote.DeviceName == "" {
		remote.DeviceName = st.Remote.DeviceName
	}
	if remote.LinkedAt == "" {
		remote.LinkedAt = st.Remote.LinkedAt
	}
	remote.Unlinked = remote.Unlinked || st.Remote.Unlinked
	st.Remote = *remote
	return m.writeStatusLocked(&st)
}

// parseReliableTime parses a persisted RFC 3339 timestamp, rejecting
// values recorded against an unreliable (pre-2024) clock.
func parseReliableTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || !helpers.IsClockReliable(parsed) {
		return time.Time{}, false
	}
	return parsed, true
}

// TrackScheduleStale maintains the persisted record of when remote backup
// scheduling became active and reports whether scheduled backups are
// stale: scheduling active for at least staleAfter with no successful run
// inside that window. Staleness is only judged against a reliable clock.
func (m *Manager) TrackScheduleStale(now time.Time, active bool, staleAfter time.Duration) bool {
	if !helpers.IsClockReliable(now) {
		return false
	}
	statusMu.Lock()
	st := m.readStatusLocked()
	if !active {
		if st.Remote.ScheduleEnabledSince != "" {
			st.Remote.ScheduleEnabledSince = ""
			if err := m.writeStatusLocked(&st); err != nil {
				log.Warn().Err(err).Msg("failed to clear backup schedule activity marker")
			}
		}
		statusMu.Unlock()
		return false
	}
	if _, ok := parseReliableTime(st.Remote.ScheduleEnabledSince); !ok {
		st.Remote.ScheduleEnabledSince = formatTime(now)
		if err := m.writeStatusLocked(&st); err != nil {
			log.Warn().Err(err).Msg("failed to persist backup schedule activity marker")
		}
	}
	remote := st.Remote
	statusMu.Unlock()

	anchor, ok := parseReliableTime(remote.ScheduleEnabledSince)
	if !ok {
		return false
	}
	if lastSuccess, ok := parseReliableTime(remote.LastSuccessAt); ok && lastSuccess.After(anchor) {
		anchor = lastSuccess
	}
	return now.Sub(anchor) >= staleAfter
}

// NotifyScheduleStale posts the deduplicated overdue-backup inbox notice.
func (m *Manager) NotifyScheduleStale() {
	if m.inbox == nil {
		return
	}
	if addErr := m.inbox.Add(
		"Remote backup is overdue",
		inboxservice.WithBody(
			"Scheduled remote backup has not completed in over a week. "+
				"Check the backup status screen for connectivity or linking problems.",
		),
		inboxservice.WithSeverity(inboxservice.SeverityWarning),
		inboxservice.WithCategory(inboxservice.CategoryBackupRemoteStale),
	); addErr != nil {
		log.Warn().Err(addErr).Msg("failed to add stale remote backup inbox message")
	}
}

func (c *remoteClient) heartbeat(ctx context.Context) error {
	body := map[string]any{
		"core_version": config.AppVersion,
		"capabilities": map[string]any{"backup": 1},
	}
	return c.doJSON(ctx, http.MethodPost, "/v1/device/heartbeat", body, nil)
}

func (c *remoteClient) deviceMe(ctx context.Context) (*remoteDeviceMeResponse, error) {
	var resp remoteDeviceMeResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/device/me", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *remoteClient) checkMissing(ctx context.Context, hashes []string) ([]string, error) {
	missing := make([]string, 0, len(hashes))
	for start := 0; start < len(hashes); start += remoteCheckBatchSize {
		end := start + remoteCheckBatchSize
		if end > len(hashes) {
			end = len(hashes)
		}
		var resp remoteCheckResponse
		req := remoteCheckRequest{Hashes: hashes[start:end]}
		if err := c.doJSON(
			ctx, http.MethodPost, "/v1/device/backup-objects/check", &req, &resp,
		); err != nil {
			return nil, err
		}
		missing = append(missing, resp.Missing...)
	}
	return missing, nil
}

// uploadResult summarizes one uploadMissing pass: how many packs and bytes
// went over the wire, and which files were skipped as unstorable.
type uploadResult struct {
	skipped       []FileRef
	packs         int
	bytesUploaded int64
}

func (c *remoteClient) uploadMissing(
	ctx context.Context,
	files []FileRef,
	missing map[string]struct{},
	pauser *syncutil.Pauser,
) (uploadResult, error) {
	var result uploadResult
	if len(missing) == 0 {
		return result, nil
	}
	candidates := make([]FileRef, 0, len(files))
	for _, file := range files {
		if _, ok := missing[file.SHA256]; !ok {
			continue
		}
		// Empty files are never packed: a zero-length range cannot live in
		// a pack, and the server treats the empty object as always-present.
		// (A current server never reports it missing; this also covers one
		// that predates that rule.)
		if file.Size == 0 || file.SHA256 == remoteEmptyContentSHA256 {
			continue
		}
		candidates = append(candidates, file)
	}
	sortRemotePackFiles(candidates)

	unique := make([]FileRef, 0, len(missing))
	seenHashes := make(map[string]struct{}, len(missing))
	for _, file := range candidates {
		if _, seen := seenHashes[file.SHA256]; seen {
			continue
		}
		seenHashes[file.SHA256] = struct{}{}
		unique = append(unique, file)
	}

	var current []FileRef
	var currentBytes int64
	flush := func() error {
		if len(current) == 0 {
			return nil
		}
		if waitErr := pauser.Wait(ctx); waitErr != nil {
			return fmt.Errorf("uploading remote backup pack: %w", waitErr)
		}
		body, packHash, buildErr := buildRemotePack(ctx, current, pauser)
		if buildErr != nil {
			return buildErr
		}
		if len(body) > remoteMaxPackBytes {
			return fmt.Errorf("remote backup pack exceeds maximum size: %d bytes", len(body))
		}
		var resp remotePackResponse
		uploadPath := "/v1/device/backup-packs/" + packHash
		if uploadErr := c.doBytes(ctx, http.MethodPut, uploadPath, body, &resp); uploadErr != nil {
			return uploadErr
		}
		result.packs++
		result.bytesUploaded += int64(len(body))
		current = nil
		currentBytes = 0
		return nil
	}

	for i := range unique {
		file := &unique[i]
		if remoteSingleFilePackExceedsMax(file) {
			// There is no way to store a file that cannot fit inside one
			// pack: skip it and let the caller drop it from the manifest.
			log.Warn().Str("path", file.RestorePath).Int64("size", file.Size).
				Msg("skipping file too large for remote backup")
			result.skipped = append(result.skipped, *file)
			continue
		}
		categoryChanged := len(current) > 0 && current[0].Category != file.Category
		packFull := len(current) > 0 && currentBytes+file.Size > remotePackTargetBytes
		if categoryChanged || packFull {
			if err := flush(); err != nil {
				return result, err
			}
		}
		current = append(current, *file)
		currentBytes += file.Size
	}
	if err := flush(); err != nil {
		return result, err
	}
	result.skipped = expandSkippedFiles(files, result.skipped)
	return result, nil
}

func (c *remoteClient) downloadObject(
	ctx context.Context,
	hash string,
	wantSize int64,
	destination string,
) error {
	ctx, cancel := context.WithTimeout(ctx, remoteTransferTimeout(wantSize))
	defer cancel()
	downloadPath := "/v1/device/backup-objects/" + hash
	return c.doRaw(ctx, http.MethodGet, downloadPath, nil, "", func(resp *http.Response) (err error) {
		// #nosec G304 -- destination is a validated hash inside a private staging directory.
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("creating remote restore staging file: %w", err)
		}
		defer func() {
			if closeErr := out.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("closing remote restore staging file: %w", closeErr))
			}
		}()

		hasher := sha256.New()
		limited := &io.LimitedReader{R: resp.Body, N: wantSize + 1}
		written, copyErr := io.Copy(io.MultiWriter(out, hasher), limited)
		if copyErr != nil {
			return fmt.Errorf("reading remote backup object: %w", copyErr)
		}
		if written != wantSize {
			return fmt.Errorf("remote backup object size mismatch: %s", hash)
		}
		if hex.EncodeToString(hasher.Sum(nil)) != hash {
			return fmt.Errorf("remote backup object hash mismatch: %s", hash)
		}
		if syncErr := out.Sync(); syncErr != nil {
			return fmt.Errorf("syncing remote restore staging file: %w", syncErr)
		}
		return nil
	})
}

func (c *remoteClient) doJSON(ctx context.Context, method, path string, body, out any) error {
	return c.doJSONLimit(ctx, method, path, body, out, helpers.MaxResponseBodySize)
}

// doJSONLimit is doJSON with an explicit response-size limit, for the calls
// whose responses carry a full snapshot manifest (commit and snapshot GET) —
// those can far exceed the default cap on file-heavy devices.
func (c *remoteClient) doJSONLimit(
	ctx context.Context,
	method, path string,
	body, out any,
	responseLimit int64,
) error {
	var reader io.Reader
	var requestBytes int64
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding remote backup request: %w", err)
		}
		requestBytes = int64(len(data))
		reader = bytes.NewReader(data)
	}
	ctx, cancel := context.WithTimeout(ctx, remoteTransferTimeout(requestBytes))
	defer cancel()
	return c.doRaw(ctx, method, path, reader, "application/json", func(resp *http.Response) error {
		if out == nil {
			return nil
		}
		limitedBody := io.LimitReader(resp.Body, responseLimit)
		if err := json.NewDecoder(limitedBody).Decode(out); err != nil {
			return fmt.Errorf("decoding remote backup response: %w", err)
		}
		return nil
	})
}

func (c *remoteClient) doBytes(
	ctx context.Context,
	method, path string,
	body []byte,
	out any,
) error {
	ctx, cancel := context.WithTimeout(ctx, remoteTransferTimeout(int64(len(body))))
	defer cancel()
	reader := bytes.NewReader(body)
	contentType := "application/octet-stream"
	return c.doRaw(ctx, method, path, reader, contentType, func(resp *http.Response) error {
		if out == nil {
			return nil
		}
		limitedBody := io.LimitReader(resp.Body, helpers.MaxResponseBodySize)
		if err := json.NewDecoder(limitedBody).Decode(out); err != nil {
			return fmt.Errorf("decoding remote backup response: %w", err)
		}
		return nil
	})
}

func (c *remoteClient) doRaw(
	ctx context.Context,
	method, requestPath string,
	body io.Reader,
	contentType string,
	onOK func(*http.Response) error,
) error {
	endpoint, err := remoteEndpoint(c.baseURL, requestPath)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("creating remote backup request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set(zapscript.HeaderZaparooOS, runtime.GOOS)
	req.Header.Set(zapscript.HeaderZaparooArch, runtime.GOARCH)
	req.Header.Set(zapscript.HeaderZaparooPlatform, c.platform)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	//nolint:gosec // URL is validated backup.remote.base_url or HTTPS default.
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("contacting remote backup server: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("failed to close remote backup response")
		}
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized && c.onUnauthorized != nil {
			c.onUnauthorized()
		}
		return remoteStatusError(resp)
	}
	return onOK(resp)
}

func remoteBackupPath(id string) string {
	escapedID := url.PathEscape(id)
	if id == "." || id == ".." {
		escapedID = strings.ReplaceAll(escapedID, ".", "%2E")
	}
	return "/v1/device/backups/" + escapedID
}

func remoteEndpoint(baseURL, requestPath string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid remote backup base URL: %w", err)
	}
	decodedRequestPath, err := url.PathUnescape(requestPath)
	if err != nil {
		return "", fmt.Errorf("invalid remote backup request path: %w", err)
	}
	basePath := strings.TrimRight(base.Path, "/")
	baseEscapedPath := strings.TrimRight(base.EscapedPath(), "/")
	base.Path = basePath + decodedRequestPath
	base.RawPath = baseEscapedPath + requestPath
	return base.String(), nil
}

func remoteStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, helpers.MaxResponseBodySize))
	var apiErr remoteAPIError
	_ = json.Unmarshal(body, &apiErr)
	switch apiErr.Error.Code {
	case "not_available":
		return errRemoteNotAvailable
	case "quota_exceeded":
		return errRemoteQuotaExceeded
	case "payload_too_large":
		return errors.New("remote backup payload too large")
	case "rate_limited":
		return errRemoteRateLimited
	case "missing_objects":
		return errRemoteMissingObjects
	case "backup_too_large":
		return errors.New("remote backup has too many files")
	case "integrity_mismatch":
		return errRemoteIntegrityRetry
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return errRemoteUnlinked
	}
	if apiErr.Error.Message != "" {
		return fmt.Errorf(
			"remote backup server returned status %d: %s",
			resp.StatusCode,
			apiErr.Error.Message,
		)
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("remote backup server returned status %d: %s", resp.StatusCode, msg)
}

func hashRemoteFiles(ctx context.Context, files []FileRef, pauser *syncutil.Pauser) ([]FileRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("hashing remote backup sources: %w", err)
	}
	out := make([]FileRef, len(files))
	copy(out, files)
	for i := range out {
		if err := pauser.Wait(ctx); err != nil {
			return nil, fmt.Errorf("hashing remote backup sources: %w", err)
		}
		f, err := openSourceContext(ctx, &out[i])
		if err != nil {
			if errors.Is(err, errSourceIdentityChanged) {
				return nil, fmt.Errorf("%w: %w", errRemoteIntegrityRetry, err)
			}
			return nil, err
		}
		hash := sha256.New()
		limited := &io.LimitedReader{R: &contextReader{ctx: ctx, reader: f}, N: out[i].Size + 1}
		size, copyErr := io.Copy(hash, limited)
		closeErr := f.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("hashing %s: %w", out[i].RestorePath, copyErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("closing %s: %w", out[i].RestorePath, closeErr)
		}
		if size != out[i].Size {
			return nil, fmt.Errorf("%w: source size changed for %s", errRemoteIntegrityRetry, out[i].RestorePath)
		}
		out[i].SHA256 = hex.EncodeToString(hash.Sum(nil))
	}
	return out, nil
}

func verifyRemoteSources(ctx context.Context, files []FileRef, pauser *syncutil.Pauser) error {
	verified, err := hashRemoteFiles(ctx, files, pauser)
	if err != nil {
		return err
	}
	for i := range files {
		if verified[i].Size != files[i].Size || verified[i].SHA256 != files[i].SHA256 {
			return fmt.Errorf("%w: source changed for %s", errRemoteIntegrityRetry, files[i].RestorePath)
		}
	}
	return nil
}

func sortRemotePackFiles(files []FileRef) {
	sort.Slice(files, func(i, j int) bool {
		leftRank := remoteCategoryRank(files[i].Category)
		rightRank := remoteCategoryRank(files[j].Category)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if files[i].RestorePath != files[j].RestorePath {
			return files[i].RestorePath < files[j].RestorePath
		}
		return files[i].SHA256 < files[j].SHA256
	})
}

func remoteCategoryRank(category string) int {
	switch category {
	case CategoryZaparoo:
		return 0
	case CategorySettings:
		return 1
	case CategoryInputs:
		return 2
	case CategorySaves:
		return 3
	case CategorySavestates:
		return 4
	default:
		return 100
	}
}

func remoteSingleFilePackExceedsMax(file *FileRef) bool {
	footer := []packFooterEntry{{Hash: file.SHA256, Offset: 0, Length: file.Size}}
	footerData, err := json.Marshal(footer)
	if err != nil {
		return true
	}
	return file.Size+int64(len(footerData))+remotePackFooterTrailerSize > remoteMaxPackBytes
}

func buildRemotePack(
	ctx context.Context, files []FileRef, pauser *syncutil.Pauser,
) (body []byte, packHash string, err error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, "", fmt.Errorf("building remote backup pack: %w", ctxErr)
	}
	var buf bytes.Buffer
	footer := make([]packFooterEntry, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if waitErr := pauser.Wait(ctx); waitErr != nil {
			return nil, "", fmt.Errorf("building remote backup pack: %w", waitErr)
		}
		if _, ok := seen[file.SHA256]; ok {
			continue
		}
		seen[file.SHA256] = struct{}{}
		opened, openErr := openSourceContext(ctx, &file)
		if openErr != nil {
			if errors.Is(openErr, errSourceIdentityChanged) {
				return nil, "", fmt.Errorf("%w: %w", errRemoteIntegrityRetry, openErr)
			}
			return nil, "", openErr
		}
		payload, readErr := io.ReadAll(io.LimitReader(
			&contextReader{ctx: ctx, reader: opened}, file.Size+1,
		))
		closeErr := opened.Close()
		if readErr != nil {
			return nil, "", fmt.Errorf("reading %s: %w", file.RestorePath, readErr)
		}
		if closeErr != nil {
			return nil, "", fmt.Errorf("closing %s: %w", file.RestorePath, closeErr)
		}
		payloadHash := sha256.Sum256(payload)
		if int64(len(payload)) != file.Size || hex.EncodeToString(payloadHash[:]) != file.SHA256 {
			return nil, "", fmt.Errorf("%w: source changed for %s", errRemoteIntegrityRetry, file.RestorePath)
		}
		footer = append(footer, packFooterEntry{
			Hash: file.SHA256, Offset: int64(buf.Len()), Length: int64(len(payload)),
		})
		_, _ = buf.Write(payload)
	}
	footerData, err := json.Marshal(footer)
	if err != nil {
		return nil, "", fmt.Errorf("encoding remote backup pack footer: %w", err)
	}
	_, _ = buf.Write(footerData)
	if len(footerData) > remoteMaxPackBytes {
		return nil, "", errors.New("remote backup pack footer exceeds maximum size")
	}
	var trailer [4]byte
	//nolint:gosec // len(footerData) is capped by remoteMaxPackBytes above.
	binary.BigEndian.PutUint32(trailer[:], uint32(len(footerData)))
	_, _ = buf.Write(trailer[:])
	body = buf.Bytes()
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:]), nil
}

func remoteCategories(files []FileRef) map[string][]remoteManifestEntry {
	categories := make(map[string][]remoteManifestEntry)
	for _, file := range files {
		categories[file.Category] = append(categories[file.Category], remoteManifestEntry{
			Path:   file.RestorePath,
			SHA256: file.SHA256,
			Size:   file.Size,
		})
	}
	for category := range categories {
		entries := categories[category]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		categories[category] = entries
	}
	return categories
}

func remoteCategorySummaries(files []FileRef) map[string]remoteCategorySummary {
	out := make(map[string]remoteCategorySummary)
	for _, file := range files {
		entry := out[file.Category]
		entry.Files++
		entry.Bytes += file.Size
		out[file.Category] = entry
	}
	return out
}

func remoteCategoriesToStatus(
	in map[string]remoteCategorySummary,
) map[string]models.BackupCategoryStatus {
	out := map[string]models.BackupCategoryStatus{
		CategoryZaparoo:    {Enabled: true},
		CategorySettings:   {Enabled: true},
		CategoryInputs:     {Enabled: true},
		CategorySaves:      {Enabled: true},
		CategorySavestates: {Enabled: true},
	}
	for category, summary := range in {
		out[category] = models.BackupCategoryStatus{
			Files: summary.Files, Bytes: summary.Bytes, Enabled: true,
		}
	}
	return out
}

func uniqueHashes(files []FileRef) []string {
	seen := make(map[string]struct{}, len(files))
	out := make([]string, 0, len(files))
	for _, file := range files {
		if _, ok := seen[file.SHA256]; ok {
			continue
		}
		seen[file.SHA256] = struct{}{}
		out = append(out, file.SHA256)
	}
	sort.Strings(out)
	return out
}

func stringSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}

func remoteBackupToInfo(resp *remoteBackupResponse) RemoteBackupInfo {
	info := RemoteBackupInfo{
		ID:            resp.ID,
		BackupType:    resp.BackupType,
		SchemaVersion: resp.SchemaVersion,
		Incompatible:  resp.SchemaVersion > remoteSchemaVersion,
		CoreVersion:   resp.CoreVersion,
		Platform:      resp.Platform,
		ManifestHash:  resp.ManifestHash,
		SizeBytes:     resp.SizeBytes,
		Categories:    resp.Categories,
		Manifest:      resp.Manifest,
		CreatedAt:     resp.CreatedAt,
		RestoredAt:    resp.RestoredAt,
	}
	if resp.SourceDevice != nil {
		info.SourceDevice = &RemoteBackupSourceDevice{
			ID:       resp.SourceDevice.ID,
			Name:     resp.SourceDevice.Name,
			Platform: resp.SourceDevice.Platform,
			Linked:   resp.SourceDevice.Linked,
			Current:  resp.SourceDevice.Current,
		}
	}
	return info
}

func validateCommittedRemoteBackup(
	resp *remoteBackupResponse,
	expectedFiles []FileRef,
	expectedPlatform, expectedType string,
) error {
	manifest, err := validateRemoteManifestResponse(resp, expectedPlatform)
	if err != nil {
		return err
	}
	if resp.ID == "" || resp.BackupType != expectedType || resp.SchemaVersion != remoteSchemaVersion {
		return errors.New("remote backup commit response metadata mismatch")
	}
	actualFiles := remoteManifestFiles(manifest)
	if !remoteFilesEqual(actualFiles, expectedFiles) {
		return errors.New("remote backup commit manifest does not match verified snapshot")
	}
	return nil
}

// canonicalRemoteManifest rebuilds the canonical manifest bytes and their
// sha256 hex exactly as the server does when computing manifest_hash: empty
// categories dropped, entries sorted by path, and the format/categories
// shape marshaled compactly (json.Marshal sorts map keys, so the result is
// deterministic).
func canonicalRemoteManifest(
	categories map[string][]remoteManifestEntry,
) (manifest []byte, manifestHash string, err error) {
	sorted := make(map[string][]remoteManifestEntry, len(categories))
	for category, entries := range categories {
		if len(entries) == 0 {
			continue
		}
		copied := make([]remoteManifestEntry, len(entries))
		copy(copied, entries)
		sort.Slice(copied, func(i, j int) bool { return copied[i].Path < copied[j].Path })
		sorted[category] = copied
	}
	data, err := json.Marshal(remoteManifest{Format: remoteManifestFormat, Categories: sorted})
	if err != nil {
		return nil, "", fmt.Errorf("marshaling canonical remote manifest: %w", err)
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

func validateRemoteManifestResponse(
	resp *remoteBackupResponse, expectedPlatform string,
) (*remoteManifest, error) {
	if resp.Platform == nil || *resp.Platform != expectedPlatform {
		return nil, fmt.Errorf("remote backup platform does not match %q", expectedPlatform)
	}
	manifest, err := remoteManifestFromResponse(resp)
	if err != nil {
		return nil, err
	}
	// The server stores manifests in a JSONB column, which does not
	// preserve the committed bytes (key order and whitespace change on the
	// way back out), so the hash must be recomputed over the canonical
	// form rather than the wire bytes.
	_, canonicalHash, err := canonicalRemoteManifest(manifest.Categories)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(canonicalHash, resp.ManifestHash) {
		return nil, errors.New("remote backup manifest hash mismatch")
	}
	files := remoteManifestFiles(manifest)
	if len(files) > maxArchiveEntries-1 {
		return nil, fmt.Errorf("remote backup has too many files: %d", len(files))
	}
	var total int64
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if !knownCategory(file.Category) {
			return nil, fmt.Errorf("unknown remote backup category: %s", file.Category)
		}
		key := file.Category + "\x00" + file.RestorePath
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate remote backup path: %s", file.RestorePath)
		}
		seen[key] = struct{}{}
		if file.Size < 0 || file.Size > math.MaxInt64-total {
			return nil, errors.New("remote backup manifest size overflow")
		}
		if file.Size > 0 && remoteSingleFilePackExceedsMax(&file) {
			return nil, fmt.Errorf("remote backup object exceeds supported pack size: %s", file.RestorePath)
		}
		total += file.Size
	}
	if total != resp.SizeBytes || !remoteSummariesEqual(resp.Categories, remoteCategorySummaries(files)) {
		return nil, errors.New("remote backup manifest summary mismatch")
	}
	return manifest, nil
}

func remoteFilesEqual(actual, expected []FileRef) bool {
	if len(actual) != len(expected) {
		return false
	}
	type identity struct {
		hash string
		size int64
	}
	expectedByPath := make(map[string]identity, len(expected))
	for _, file := range expected {
		expectedByPath[file.Category+"\x00"+file.RestorePath] = identity{hash: file.SHA256, size: file.Size}
	}
	for _, file := range actual {
		want, ok := expectedByPath[file.Category+"\x00"+file.RestorePath]
		if !ok || want.hash != file.SHA256 || want.size != file.Size {
			return false
		}
	}
	return true
}

func remoteSummariesEqual(
	actual, expected map[string]remoteCategorySummary,
) bool {
	if len(actual) != len(expected) {
		return false
	}
	for category, want := range expected {
		if got, ok := actual[category]; !ok || got != want {
			return false
		}
	}
	return true
}

func remoteManifestFromResponse(resp *remoteBackupResponse) (*remoteManifest, error) {
	if len(resp.Manifest) == 0 {
		return nil, errors.New("remote backup response missing manifest")
	}
	var manifest remoteManifest
	if err := json.Unmarshal(resp.Manifest, &manifest); err != nil {
		return nil, fmt.Errorf("decoding remote backup manifest: %w", err)
	}
	if manifest.Format != remoteManifestFormat {
		return nil, fmt.Errorf("unsupported remote backup manifest format: %s", manifest.Format)
	}
	return &manifest, nil
}

func remoteManifestFiles(manifest *remoteManifest) []FileRef {
	var files []FileRef
	for category, entries := range manifest.Categories {
		for _, entry := range entries {
			archivePath := platformArchive(entry.Path)
			if category == CategoryZaparoo {
				archivePath = zaparooArchive(entry.Path)
			}
			files = append(files, FileRef{
				Category:    category,
				ArchivePath: archivePath,
				RestorePath: entry.Path,
				SHA256:      entry.SHA256,
				Size:        entry.Size,
			})
		}
	}
	return files
}
