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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	inboxservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
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
)

var (
	errRemoteNotAvailable   = errors.New("remote backup is not available for this account")
	errRemoteQuotaExceeded  = errors.New("remote backup quota exceeded")
	errRemoteUnlinked       = errors.New("remote backup is unlinked")
	errRemoteRateLimited    = errors.New("remote backup rate limited")
	errRemoteIntegrityRetry = errors.New("remote backup integrity mismatch")
	errRemoteBackupRunning  = errors.New("a remote backup is already running")
	errRemoteNewerSchema    = errors.New("backup requires a newer Core version")
)

// remoteRunActive serializes remote snapshot runs across Manager instances so
// a manual trigger and the scheduler cannot collide into the server's commit
// floor mid-flight.
var remoteRunActive atomic.Bool

// RemoteRunInfo describes one completed remote backup run.
//
//nolint:govet // JSON response shape is grouped for API readability.
type RemoteRunInfo struct {
	Backup            RemoteBackupInfo                 `json:"backup"`
	Categories        map[string]remoteCategorySummary `json:"categories"`
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
	Categories    map[string]remoteCategorySummary `json:"categories"`
	ManifestHash  string                           `json:"manifestHash"`
	BackupType    string                           `json:"backupType"`
	CreatedAt     time.Time                        `json:"createdAt"`
	Manifest      json.RawMessage                  `json:"manifest,omitempty"`
	ID            int64                            `json:"id"`
	SchemaVersion int                              `json:"schemaVersion"`
	SizeBytes     int64                            `json:"sizeBytes"`
	// Incompatible marks snapshots committed with a newer schema version
	// than this Core supports: they list fine but refuse to restore.
	Incompatible bool `json:"incompatible,omitempty"`
}

type remoteCategorySummary struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

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
	Categories    map[string]remoteCategorySummary `json:"categories"`
	Manifest      json.RawMessage                  `json:"manifest,omitempty"`
	ManifestHash  string                           `json:"manifest_hash"`
	BackupType    string                           `json:"backup_type"`
	CreatedAt     time.Time                        `json:"created_at"`
	ID            int64                            `json:"id"`
	SchemaVersion int                              `json:"schema_version"`
	SizeBytes     int64                            `json:"size_bytes"`
}

//nolint:tagliatelle,govet // Remote API contract uses snake_case JSON fields.
type remotePackResponse struct {
	PackHash    string    `json:"pack_hash"`
	SizeBytes   int64     `json:"size_bytes"`
	ObjectCount int       `json:"object_count"`
	CreatedAt   time.Time `json:"created_at"`
}

//nolint:tagliatelle,govet // Remote API contract uses snake_case JSON fields.
type remoteDeviceMeResponse struct {
	Name         string    `json:"name"`
	LinkedAt     time.Time `json:"linked_at"`
	ID           int64     `json:"id"`
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
	httpClient *http.Client
	baseURL    string
	bearer     string
	platform   string
}

func (m *Manager) RunRemote(ctx context.Context, backupType string) (RemoteRunInfo, error) {
	if backupType != RemoteBackupTypeManual && backupType != RemoteBackupTypeScheduled {
		return RemoteRunInfo{}, fmt.Errorf("invalid remote backup type: %s", backupType)
	}
	if !remoteRunActive.CompareAndSwap(false, true) {
		return RemoteRunInfo{}, errRemoteBackupRunning
	}
	defer remoteRunActive.Store(false)

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
	_ = m.writeRemoteStatus(&statusEntry{
		LastRunAt:      formatTime(started),
		LastSuccessAt:  formatTime(info.Backup.CreatedAt),
		LastStatus:     StatusSuccess,
		LastBackupSize: info.Backup.SizeBytes,
		Categories:     remoteCategoriesToStatus(info.Categories),
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

func (m *Manager) RestoreRemote(ctx context.Context, id int64) (RemoteRestoreInfo, error) {
	if id <= 0 {
		return RemoteRestoreInfo{}, errors.New("invalid remote backup id")
	}
	client, err := m.newRemoteClient()
	if err != nil {
		return RemoteRestoreInfo{}, err
	}
	var resp remoteBackupResponse
	backupPath := "/v1/device/backups/" + strconv.FormatInt(id, 10)
	getErr := client.doJSONLimit(ctx, http.MethodGet, backupPath, nil, &resp, remoteManifestResponseLimit)
	if getErr != nil {
		return RemoteRestoreInfo{}, getErr
	}
	if resp.SchemaVersion > remoteSchemaVersion {
		return RemoteRestoreInfo{}, fmt.Errorf(
			"%w: backup schema version %d is newer than supported version %d",
			errRemoteNewerSchema, resp.SchemaVersion, remoteSchemaVersion,
		)
	}
	manifest, err := remoteManifestFromResponse(&resp)
	if err != nil {
		return RemoteRestoreInfo{}, err
	}
	files := remoteManifestFiles(manifest)
	if validateErr := validateFiles(files); validateErr != nil {
		return RemoteRestoreInfo{}, validateErr
	}

	// Download and verify every payload before touching the device: a
	// mid-restore network failure or hash mismatch must leave it unchanged.
	staged, cleanup, err := m.stageRemotePayloads(ctx, client, files)
	if err != nil {
		return RemoteRestoreInfo{}, err
	}
	defer cleanup()

	pre, err := m.createBackup(true)
	if err != nil {
		return RemoteRestoreInfo{}, fmt.Errorf("creating pre-restore backup: %w", err)
	}
	if err := m.applyRestore(&Manifest{Files: files}, func(file FileRef) ([]byte, error) {
		return staged.read(file.SHA256, file.Size)
	}); err != nil {
		return RemoteRestoreInfo{}, err
	}
	restoreCompletePath := "/v1/device/backups/" + strconv.FormatInt(id, 10) + "/restore-complete"
	if err := client.doJSON(ctx, http.MethodPost, restoreCompletePath, nil, nil); err != nil {
		log.Warn().Err(err).Int64("backup_id", id).Msg("failed to mark remote backup restored")
	}
	preInfo := pre
	return RemoteRestoreInfo{PreRestoreBackup: &preInfo, RestoredFrom: remoteBackupToInfo(&resp)}, nil
}

// remoteStaging holds downloaded, hash-verified payloads on disk, keyed by
// content hash, so the apply phase never depends on the network.
type remoteStaging struct {
	dir string
}

func (s *remoteStaging) path(hash string) string { return filepath.Join(s.dir, hash) }

func (s *remoteStaging) read(hash string, wantSize int64) ([]byte, error) {
	if hash == remoteEmptyContentSHA256 {
		return []byte{}, nil
	}
	payload, err := os.ReadFile(s.path(hash))
	if err != nil {
		return nil, fmt.Errorf("reading staged restore payload %s: %w", hash, err)
	}
	if int64(len(payload)) != wantSize {
		return nil, fmt.Errorf("staged restore payload size mismatch: %s", hash)
	}
	return payload, nil
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
		payload, downloadErr := client.downloadObject(ctx, file.SHA256, file.Size)
		if downloadErr != nil {
			cleanup()
			return nil, nil, downloadErr
		}
		if writeErr := os.WriteFile(staging.path(file.SHA256), payload, 0o600); writeErr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("staging restore payload %s: %w", file.SHA256, writeErr)
		}
	}
	return staging, cleanup, nil
}

func (m *Manager) createRemoteSnapshot(ctx context.Context, backupType string) (RemoteRunInfo, error) {
	client, err := m.newRemoteClient()
	if err != nil {
		return RemoteRunInfo{}, err
	}
	heartbeatErr := client.heartbeat(ctx)
	if heartbeatErr != nil {
		return RemoteRunInfo{}, heartbeatErr
	}
	me, err := client.deviceMe(ctx)
	if err != nil {
		return RemoteRunInfo{}, err
	}
	if !me.BackupActive {
		return RemoteRunInfo{}, errRemoteNotAvailable
	}
	files, err := m.collectFiles()
	if err != nil {
		return RemoteRunInfo{}, err
	}
	validateErr := validateFiles(files)
	if validateErr != nil {
		return RemoteRunInfo{}, validateErr
	}
	files, err = hashRemoteFiles(files)
	if err != nil {
		return RemoteRunInfo{}, err
	}
	missing, err := client.checkMissing(ctx, uniqueHashes(files))
	if err != nil {
		return RemoteRunInfo{}, err
	}
	missingSet := stringSet(missing)
	uploaded, err := client.uploadMissing(ctx, files, missingSet)
	if err != nil {
		return RemoteRunInfo{}, err
	}
	if len(uploaded.skipped) > 0 {
		// Files too large to fit in a single pack cannot be stored under
		// this protocol: surface them and back up everything else.
		files = withoutSkippedFiles(files, uploaded.skipped)
		m.notifyRemoteSkipped(uploaded.skipped)
	}
	var backup remoteBackupResponse
	request := remoteSnapshotRequest{
		BackupType:    backupType,
		SchemaVersion: remoteSchemaVersion,
		CoreVersion:   &config.AppVersion,
		Categories:    remoteCategories(files),
	}
	if err := client.doJSONLimit(
		ctx, http.MethodPost, "/v1/device/backups", &request, &backup, remoteManifestResponseLimit,
	); err != nil {
		return RemoteRunInfo{}, err
	}
	list, listErr := m.ListRemote(ctx)
	if listErr != nil {
		log.Debug().Err(listErr).Msg("failed to refresh remote backup quota after upload")
	}
	uploadedFiles := 0
	for _, hash := range uniqueHashes(files) {
		if _, ok := missingSet[hash]; ok {
			uploadedFiles++
		}
	}
	return RemoteRunInfo{
		Backup:            remoteBackupToInfo(&backup),
		Categories:        remoteCategorySummaries(files),
		UploadedFiles:     uploadedFiles,
		DedupedFiles:      len(uniqueHashes(files)) - uploadedFiles,
		SkippedFiles:      len(uploaded.skipped),
		UploadedPacks:     uploaded.packs,
		UploadedBytes:     uploaded.bytesUploaded,
		StorageUsedBytes:  list.StorageUsedBytes,
		StorageQuotaBytes: list.StorageQuotaBytes,
	}, nil
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
	baseURL := strings.TrimRight(m.cfg.BackupRemoteBaseURL(), "/")
	lookupURL := config.BackupAuthLookupURL(baseURL)
	entry := config.LookupAuth(config.GetAuthCfg(), lookupURL)
	if entry == nil || entry.Bearer == "" {
		return nil, errRemoteUnlinked
	}
	return &remoteClient{
		// Timeouts are applied per request (scaled to transfer size for
		// uploads/downloads), not on the client, so a large pack on a slow
		// uplink is not killed at the base timeout.
		httpClient: &http.Client{},
		baseURL:    baseURL,
		bearer:     entry.Bearer,
		platform:   m.pl.ID(),
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
	case errors.Is(err, errRemoteBackupRunning):
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

// SendHeartbeat reports liveness (Core version + capabilities) when the
// device is linked. Callers use it independently of backup runs so "last
// seen" stays fresh even with remote backup disabled.
func (m *Manager) SendHeartbeat(ctx context.Context) error {
	client, err := m.newRemoteClient()
	if err != nil {
		return err
	}
	return client.heartbeat(ctx)
}

// MarkRemoteLinked clears a persisted unlinked marker after a successful
// claim/link, so the status UI reflects the fresh credential immediately.
func (m *Manager) MarkRemoteLinked() {
	statusMu.Lock()
	defer statusMu.Unlock()

	st := m.readStatusLocked()
	if !st.Remote.Unlinked {
		return
	}
	st.Remote.Unlinked = false
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
	st.Remote = *remote
	return m.writeStatusLocked(&st)
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
		body, packHash, buildErr := buildRemotePack(current)
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
	return result, nil
}

func (c *remoteClient) downloadObject(
	ctx context.Context,
	hash string,
	wantSize int64,
) ([]byte, error) {
	if hash == remoteEmptyContentSHA256 {
		// The empty object is never stored server-side; synthesize it.
		if wantSize != 0 {
			return nil, fmt.Errorf("remote backup object size mismatch: %s", hash)
		}
		return []byte{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, remoteTransferTimeout(wantSize))
	defer cancel()
	var out []byte
	downloadPath := "/v1/device/backup-objects/" + hash
	if err := c.doRaw(ctx, http.MethodGet, downloadPath, nil, "", func(resp *http.Response) error {
		body, err := io.ReadAll(io.LimitReader(resp.Body, wantSize+1))
		if err != nil {
			return fmt.Errorf("reading remote backup object: %w", err)
		}
		if int64(len(body)) != wantSize {
			return fmt.Errorf("remote backup object size mismatch: %s", hash)
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != hash {
			return fmt.Errorf("remote backup object hash mismatch: %s", hash)
		}
		out = body
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
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
		return remoteStatusError(resp)
	}
	return onOK(resp)
}

func remoteEndpoint(baseURL, requestPath string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid remote backup base URL: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + requestPath
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
		return errors.New("remote backup snapshot references missing objects")
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

func hashRemoteFiles(files []FileRef) ([]FileRef, error) {
	out := make([]FileRef, len(files))
	copy(out, files)
	for i := range out {
		f, err := os.Open(out[i].SourcePath)
		if err != nil {
			return nil, fmt.Errorf("opening %s: %w", out[i].SourcePath, err)
		}
		hash := sha256.New()
		size, copyErr := io.Copy(hash, f)
		closeErr := f.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("hashing %s: %w", out[i].SourcePath, copyErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("closing %s: %w", out[i].SourcePath, closeErr)
		}
		out[i].SHA256 = hex.EncodeToString(hash.Sum(nil))
		out[i].Size = size
	}
	return out, nil
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

func buildRemotePack(files []FileRef) (body []byte, packHash string, err error) {
	var buf bytes.Buffer
	footer := make([]packFooterEntry, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if _, ok := seen[file.SHA256]; ok {
			continue
		}
		seen[file.SHA256] = struct{}{}
		// #nosec G304 -- source path comes from validated backup collector.
		payload, readErr := os.ReadFile(file.SourcePath)
		if readErr != nil {
			return nil, "", fmt.Errorf("reading %s: %w", file.SourcePath, readErr)
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
	return RemoteBackupInfo{
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
}

func remoteManifestFromResponse(resp *remoteBackupResponse) (*remoteManifest, error) {
	if len(resp.Manifest) == 0 {
		return nil, errors.New("remote backup response missing manifest")
	}
	var manifest remoteManifest
	if err := json.Unmarshal(resp.Manifest, &manifest); err != nil {
		return nil, fmt.Errorf("decoding remote backup manifest: %w", err)
	}
	if manifest.Format != "cas-v1" {
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
