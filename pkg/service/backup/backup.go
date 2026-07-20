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
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	inboxservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/rs/zerolog/log"
)

var (
	ErrPlatformBackupUnsupported = errors.New("platform does not support full-device backup")
	statusMu                     syncutil.RWMutex
)

const (
	CategoryZaparoo    = "zaparoo"
	CategorySettings   = "settings"
	CategoryInputs     = "inputs"
	CategorySaves      = "saves"
	CategorySavestates = "savestates"

	StatusNever   = "never"
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusPartial = "partial"
	StatusFailed  = "failed"

	IntegrityUnchecked = "unchecked"
	IntegrityValid     = "valid"

	RemoteAvailabilityUnknown     = "unknown"
	RemoteAvailabilityAvailable   = "available"
	RemoteAvailabilityUnavailable = "unavailable"

	maxManifestBytes     = int64(32 << 20)
	maxRestoreConfigSize = int64(4 << 20)
	maxArchiveEntries    = 100_000
	maxArchivePathLen    = 512

	manifestName  = "manifest.json"
	filesRoot     = "files"
	zaparooRoot   = "zaparoo"
	platformRoot  = "platform"
	backupDirName = "files"
)

type sourceOpener func(context.Context, *FileRef) (io.ReadCloser, error)

type Manager struct {
	cfg           *config.Instance
	pl            platforms.Platform
	database      *database.Database
	inbox         *inboxservice.Service
	coordinator   *Coordinator
	activeMedia   func() *models.ActiveMedia
	restoreGate   func() (func(bool), error)
	directorySync func(string) error
	sourceOpener  sourceOpener
}

type sourceIdentity struct {
	info               os.FileInfo
	excludedIdentities []os.FileInfo
}

type FileRef struct {
	sourceIdentity *sourceIdentity
	SourceRoot     string `json:"-"`
	SourceRel      string `json:"-"`
	ArchivePath    string `json:"archivePath"`
	RestorePath    string `json:"restorePath"`
	Category       string `json:"category"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
}

type Manifest struct {
	CreatedAt   time.Time                              `json:"createdAt"`
	Categories  map[string]models.BackupCategoryStatus `json:"categories"`
	Warnings    []models.BackupWarning                 `json:"warnings,omitempty"`
	Platform    string                                 `json:"platform"`
	CoreVersion string                                 `json:"coreVersion"`
	Files       []FileRef                              `json:"files"`
	Version     int                                    `json:"version"`
}

type ListInfo struct {
	CreatedAt time.Time `json:"createdAt"`
	Name      string    `json:"name"`
	Path      string    `json:"path,omitempty"`
	Size      int64     `json:"size"`
}

type Info struct {
	CreatedAt  time.Time                              `json:"createdAt"`
	Categories map[string]models.BackupCategoryStatus `json:"categories,omitempty"`
	Name       string                                 `json:"name"`
	Path       string                                 `json:"path,omitempty"`
	Status     string                                 `json:"status"`
	Integrity  string                                 `json:"integrity"`
	Error      string                                 `json:"error,omitempty"`
	Warnings   []models.BackupWarning                 `json:"warnings,omitempty"`
	Size       int64                                  `json:"size"`
}

type RestoreInfo struct {
	PreRestoreBackup *Info `json:"preRestoreBackup,omitempty"`
	RestoredFrom     Info  `json:"restoredFrom"`
}

type zipReadResult struct {
	Info     Info
	Manifest Manifest
}

type fileCollection struct {
	Cleanup  func() error
	Files    []FileRef
	Warnings []models.BackupWarning
}

type statusFile struct {
	Local  statusEntry `json:"local"`
	Remote statusEntry `json:"remote"`
}

type statusEntry struct {
	Categories            map[string]models.BackupCategoryStatus `json:"categories,omitempty"`
	LastRunAt             string                                 `json:"lastRunAt,omitempty"`
	LastSuccessAt         string                                 `json:"lastSuccessAt,omitempty"`
	AvailabilityCheckedAt string                                 `json:"availabilityCheckedAt,omitempty"`
	ScheduleEnabledSince  string                                 `json:"scheduleEnabledSince,omitempty"`
	LastError             string                                 `json:"lastError,omitempty"`
	LastStatus            string                                 `json:"lastStatus"`
	Availability          string                                 `json:"availability,omitempty"`
	DeviceName            string                                 `json:"deviceName,omitempty"`
	LinkedAt              string                                 `json:"linkedAt,omitempty"`
	Warnings              []models.BackupWarning                 `json:"warnings,omitempty"`
	LastBackupSize        int64                                  `json:"lastBackupSize"`
	SkippedFiles          int                                    `json:"skippedFiles,omitempty"`
	Unlinked              bool                                   `json:"unlinked,omitempty"`
}

func NewManager(cfg *config.Instance, pl platforms.Platform, db *database.Database) *Manager {
	return &Manager{
		cfg: cfg, pl: pl, database: db, coordinator: NewCoordinator(), sourceOpener: openSourceContext,
	}
}

func (m *Manager) WithInbox(inbox *inboxservice.Service) *Manager {
	m.inbox = inbox
	return m
}

func (m *Manager) WithActiveMedia(activeMedia func() *models.ActiveMedia) *Manager {
	m.activeMedia = activeMedia
	return m
}

func (m *Manager) WithRestoreGate(restoreGate func() (func(bool), error)) *Manager {
	m.restoreGate = restoreGate
	return m
}

func (m *Manager) WithCoordinator(coordinator *Coordinator) *Manager {
	if coordinator != nil {
		m.coordinator = coordinator
	}
	return m
}

func (m *Manager) begin(ctx context.Context, kind OperationKind, mode OperationMode) (*Lease, error) {
	if m.coordinator == nil {
		m.coordinator = NewCoordinator()
	}
	lease, err := m.coordinator.Begin(ctx, kind, mode)
	if err != nil {
		return nil, fmt.Errorf("coordinating backup operation: %w", err)
	}
	return lease, nil
}

func (m *Manager) Create(ctx context.Context) (Info, error) {
	lease, err := m.begin(ctx, OperationLocalCreate, OperationWrite)
	if err != nil {
		return Info{}, err
	}
	defer lease.Release()
	ctx = lease.Context()
	if err = ctx.Err(); err != nil {
		return Info{}, fmt.Errorf("creating backup: %w", err)
	}
	started := time.Now().UTC()
	_ = m.writeLocalStatus(&statusEntry{LastRunAt: formatTime(started), LastStatus: StatusRunning})

	info, err := m.createBackup(ctx, false)
	if err != nil {
		_ = m.writeLocalStatus(&statusEntry{
			LastRunAt:  formatTime(started),
			LastStatus: StatusFailed,
			LastError:  safeStatusError(err),
		})
		return Info{}, err
	}
	lastStatus := StatusSuccess
	if len(info.Warnings) > 0 {
		lastStatus = StatusPartial
	}
	_ = m.writeLocalStatus(&statusEntry{
		LastRunAt:      formatTime(started),
		LastSuccessAt:  formatTime(info.CreatedAt),
		LastStatus:     lastStatus,
		LastBackupSize: info.Size,
		Categories:     info.Categories,
		Warnings:       info.Warnings,
		SkippedFiles:   len(info.Warnings),
	})
	return info, nil
}

func (m *Manager) createBackup(ctx context.Context, preRestore bool) (result Info, err error) {
	reason := "local-zip"
	scope := m.cfg.BackupScope()
	if preRestore {
		reason = "pre-restore-zip"
		scope = m.preRestoreScope()
	}
	collection, err := m.collectFiles(ctx, reason, scope)
	if err != nil {
		return Info{}, err
	}
	defer func() {
		if cleanupErr := collection.Cleanup(); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	files := collection.Files
	if validateErr := validateFiles(files); validateErr != nil {
		return Info{}, validateErr
	}
	files, unreadableWarnings, err := prepareSourceFiles(ctx, files, m.sourceOpener)
	if err != nil {
		return Info{}, err
	}
	collection.Warnings, err = appendBackupWarnings(collection.Warnings, unreadableWarnings)
	if err != nil {
		return Info{}, err
	}
	if err = ctx.Err(); err != nil {
		return Info{}, fmt.Errorf("creating backup: %w", err)
	}
	now := time.Now().UTC()
	backupDir := m.backupDir()
	if mkdirErr := os.MkdirAll(backupDir, 0o750); mkdirErr != nil {
		return Info{}, fmt.Errorf("creating backup directory: %w", mkdirErr)
	}
	name := backupName(preRestore, now)
	finalPath := filepath.Join(backupDir, name)
	tmpPath := finalPath + ".tmp"
	manifest := m.newManifest(now, files, collection.Warnings)
	// Self-check against the restore-side policy before writing anything:
	// a backup the validator would reject is unusable, and failing here
	// surfaces collector/validator drift at create time instead of leaving
	// backups that can never be inspected or restored.
	if policyErr := m.validateManifestPolicy(&manifest); policyErr != nil {
		return Info{}, fmt.Errorf("backup would fail restore policy: %w", policyErr)
	}
	if writeErr := writeZip(ctx, tmpPath, files, &manifest, m.cfg.BackupMaxSizeBytes()); writeErr != nil {
		_ = os.Remove(tmpPath)
		return Info{}, writeErr
	}
	if renameErr := os.Rename(tmpPath, finalPath); renameErr != nil {
		_ = os.Remove(tmpPath)
		return Info{}, fmt.Errorf("finalizing backup ZIP: %w", renameErr)
	}
	info, err := infoFromManifest(finalPath, &manifest)
	if err != nil {
		return Info{}, err
	}
	log.Info().Str("path", finalPath).Int64("size", info.Size).Msg("created local backup ZIP")
	if preRestore {
		// Never fail the backup that was just created over a prune error.
		if pruneErr := m.prunePreRestoreZips(); pruneErr != nil {
			log.Warn().Err(pruneErr).Msg("failed to prune pre-restore backup ZIPs")
		}
	}
	return info, nil
}

// preRestoreZipKeep is how many pre-restore safety ZIPs are retained. They
// are system-generated before every restore and their protective value
// decays immediately, so only the newest few matter. Manual backups are
// user-managed and never pruned automatically.
const preRestoreZipKeep = 3

func (m *Manager) prunePreRestoreZips() error {
	backups, err := m.List()
	if err != nil {
		return err
	}
	preRestore := make([]ListInfo, 0, len(backups))
	for _, backup := range backups {
		if strings.HasSuffix(backup.Name, "-pre-restore.zip") {
			preRestore = append(preRestore, backup)
		}
	}
	if len(preRestore) <= preRestoreZipKeep {
		return nil
	}
	// Sort by name, newest first: names embed a zero-padded
	// timestamp with nanoseconds, so lexical order is chronological even
	// when List's second-precision CreatedAt ties.
	sort.Slice(preRestore, func(i, j int) bool { return preRestore[i].Name > preRestore[j].Name })
	for _, backup := range preRestore[preRestoreZipKeep:] {
		if err := os.Remove(backup.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("pruning pre-restore backup %s: %w", backup.Name, err)
		}
		log.Info().Str("name", backup.Name).Msg("pruned old pre-restore backup ZIP")
	}
	return nil
}

func (m *Manager) List() ([]ListInfo, error) {
	entries, err := os.ReadDir(m.backupDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing backup ZIPs: %w", err)
	}
	infos := make([]ListInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isBackupZipName(entry.Name()) {
			continue
		}
		info, infoErr := fastInfoFromDirEntry(m.backupDir(), entry)
		if infoErr != nil {
			log.Debug().Err(infoErr).Str("name", entry.Name()).Msg("failed to read backup ZIP metadata")
			continue
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].CreatedAt.After(infos[j].CreatedAt) })
	return infos, nil
}

func (m *Manager) Inspect(ctx context.Context, name string) (Info, error) {
	lease, err := m.begin(ctx, OperationLocalInspect, OperationRead)
	if err != nil {
		return Info{}, err
	}
	defer lease.Release()
	backupPath, err := m.resolveBackupPath(name)
	if err != nil {
		return Info{}, err
	}
	if validateErr := m.validateLocalArchiveManifest(backupPath); validateErr != nil {
		return Info{}, validateErr
	}
	info, err := inspectZipManifest(backupPath, m.cfg.BackupMaxSizeBytes())
	if err != nil {
		return Info{}, fmt.Errorf("inspecting backup ZIP: %w", err)
	}
	return info, nil
}

func (m *Manager) Delete(ctx context.Context, name string) error {
	lease, err := m.begin(ctx, OperationLocalDelete, OperationWrite)
	if err != nil {
		return err
	}
	defer lease.Release()
	backupPath, err := m.resolveBackupPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("deleting backup ZIP: %w", err)
	}
	return nil
}

func (m *Manager) Restore(ctx context.Context, name string) (RestoreInfo, error) {
	lease, err := m.begin(ctx, OperationLocalRestore, OperationWrite)
	if err != nil {
		return RestoreInfo{}, err
	}
	defer lease.Release()
	ctx = lease.Context()
	finishRestore, err := m.beginRestoreGate()
	if err != nil {
		return RestoreInfo{}, err
	}
	restoreSucceeded := false
	defer func() { finishRestore(restoreSucceeded) }()
	if idleErr := m.requireRestoreIdle(); idleErr != nil {
		return RestoreInfo{}, idleErr
	}
	if recoveryErr := m.recoverRestoreLocked(ctx); recoveryErr != nil {
		return RestoreInfo{}, recoveryErr
	}
	backupPath, err := m.resolveBackupPath(name)
	if err != nil {
		return RestoreInfo{}, err
	}
	if validateErr := m.validateLocalArchiveManifest(backupPath); validateErr != nil {
		return RestoreInfo{}, validateErr
	}
	zipResult, err := readAndVerifyZipLimit(backupPath, m.cfg.BackupMaxSizeBytes())
	if err != nil {
		return RestoreInfo{}, err
	}
	if validateErr := validateFiles(zipResult.Manifest.Files); validateErr != nil {
		return RestoreInfo{}, validateErr
	}
	pre, err := m.createBackup(ctx, true)
	if err != nil {
		return RestoreInfo{}, fmt.Errorf("creating pre-restore backup: %w", err)
	}
	if idleErr := m.requireRestoreIdle(); idleErr != nil {
		return RestoreInfo{}, idleErr
	}
	finishPlatformRestore, err := m.preparePlatformRestore()
	if err != nil {
		return RestoreInfo{}, err
	}
	if err = m.applyRestoreFromZip(ctx, backupPath, &zipResult.Manifest); err != nil {
		return RestoreInfo{}, errors.Join(err, finishPlatformRestore(false))
	}
	if finishErr := finishPlatformRestore(true); finishErr != nil {
		log.Warn().Err(finishErr).Msg("committed restore profile cleanup deferred until restart")
	}
	restoreSucceeded = true
	return RestoreInfo{PreRestoreBackup: &pre, RestoredFrom: zipResult.Info}, nil
}

func (m *Manager) Status() models.BackupStatusResponse {
	stored := m.readStatus()
	remoteLinked := false
	lookupURL := config.BackupAuthLookupURL(m.cfg.BackupRemoteBaseURL())
	if entry := config.LookupAuth(config.GetAuthCfg(), lookupURL); entry != nil && entry.Bearer != "" {
		// A recorded 401 means the token was revoked server-side: report
		// unlinked so the UI prompts a re-link instead of silently failing.
		remoteLinked = !stored.Remote.Unlinked
	}
	local := toStatusEntry(&stored.Local, true, "")
	remote := toStatusEntry(&stored.Remote, m.cfg.BackupRemoteEnabled(), m.cfg.BackupRemoteSchedule())
	remote.Linked = remoteLinked
	status := models.BackupStatusResponse{Local: local, Remote: remote}
	if m.coordinator != nil {
		kind, startedAt, active := m.coordinator.Active()
		if active {
			status.ActiveOperation = string(kind)
			formatted := formatTime(startedAt)
			status.ActiveSince = &formatted
		}
	}
	return status
}

func (m *Manager) backupDir() string {
	if localDir := m.cfg.BackupLocalDir(); localDir != "" {
		return localDir
	}
	return filepath.Join(helpers.DataDir(m.pl), "backups", backupDirName)
}

func (m *Manager) statusPath() string {
	return filepath.Join(helpers.DataDir(m.pl), "backups", "status.json")
}

func (m *Manager) readStatus() statusFile {
	statusMu.RLock()
	defer statusMu.RUnlock()

	return m.readStatusLocked()
}

func (m *Manager) readStatusLocked() statusFile {
	var st statusFile
	data, err := os.ReadFile(m.statusPath())
	switch {
	case err == nil:
		if decodeErr := json.Unmarshal(data, &st); decodeErr != nil {
			log.Warn().Err(decodeErr).Msg("backup status is corrupt; remote access disabled until relink")
			st.Remote.Unlinked = true
			st.Remote.Availability = RemoteAvailabilityUnknown
			st.Remote.AvailabilityCheckedAt = ""
		}
	case errors.Is(err, os.ErrNotExist):
		// No status is normal before the first backup operation.
	default:
		log.Warn().Err(err).Msg("backup status is unreadable; remote access disabled until relink")
		st.Remote.Unlinked = true
		st.Remote.Availability = RemoteAvailabilityUnknown
	}
	return st
}

func (m *Manager) writeLocalStatus(local *statusEntry) error {
	statusMu.Lock()
	defer statusMu.Unlock()

	st := m.readStatusLocked()
	if local.Categories == nil && st.Local.Categories != nil {
		local.Categories = st.Local.Categories
	}
	if local.LastSuccessAt == "" {
		local.LastSuccessAt = st.Local.LastSuccessAt
	}
	st.Local = *local
	return m.writeStatusLocked(&st)
}

// writeStatusLocked persists the status file. Callers must hold statusMu.
func (m *Manager) writeStatusLocked(st *statusFile) error {
	statusPath := m.statusPath()
	dir := filepath.Dir(statusPath)
	_, statErr := os.Stat(dir)
	dirMissing := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !dirMissing {
		return fmt.Errorf("checking backup status directory: %w", statErr)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating backup status directory: %w", err)
	}
	if dirMissing {
		if err := m.syncRestoreDirectory(filepath.Dir(dir)); err != nil {
			return fmt.Errorf("syncing backup status parent directory: %w", err)
		}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding backup status: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".status-*")
	if err != nil {
		return fmt.Errorf("creating backup status temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return fmt.Errorf("writing backup status temp file: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("closing backup status temp file: %w", closeErr)
	}
	if err = os.Rename(tmpPath, statusPath); err != nil {
		return fmt.Errorf("installing backup status: %w", err)
	}
	if err = m.syncRestoreDirectory(dir); err != nil {
		return fmt.Errorf("syncing backup status directory: %w", err)
	}
	return nil
}

func (m *Manager) newManifest(
	createdAt time.Time, files []FileRef, warnings []models.BackupWarning,
) Manifest {
	return Manifest{
		Version:     1,
		CreatedAt:   createdAt,
		Platform:    m.pl.ID(),
		CoreVersion: config.AppVersion,
		Categories:  summarize(files),
		Warnings:    warnings,
		Files:       files,
	}
}

// preRestoreScope ignores the configured backup scope: the pre-restore
// ZIP exists to preserve whatever a restore may overwrite, so it always
// includes platform files when the platform can provide them.
func (m *Manager) preRestoreScope() string {
	if _, ok := m.pl.(platforms.BackupProvider); ok {
		return config.BackupScopePlatform
	}
	return config.BackupScopeZaparoo
}

func (m *Manager) collectFiles(ctx context.Context, reason, scope string) (fileCollection, error) {
	dataDir := helpers.DataDir(m.pl)
	configDir := helpers.ConfigDir(m.pl)
	var platformPlan platforms.BackupPlan
	if scope == config.BackupScopePlatform {
		provider, ok := m.pl.(platforms.BackupProvider)
		if !ok {
			return fileCollection{}, ErrPlatformBackupUnsupported
		}
		platformPlan = platforms.BackupPlan{Definitions: provider.BackupDefinitions()}
		if planner, planning := m.pl.(platforms.BackupPlanningProvider); planning {
			platformPlan = planner.BackupPlan()
		}
	}

	if m.database == nil || m.database.UserDB == nil {
		return fileCollection{}, errors.New("database is not available")
	}
	userBackup, cleanup, err := m.database.UserDB.BackupForTransfer(ctx, reason)
	if err != nil {
		return fileCollection{}, fmt.Errorf("snapshotting transfer user database: %w", err)
	}

	canonicalConfigRoots := canonicalCategoryRoots([]string{configDir}, "")
	excludedSources := make(map[string]struct{}, 1)
	for _, root := range canonicalConfigRoots {
		excludedSources[filepath.Join(root, config.AuthFile)] = struct{}{}
	}
	excludedIdentities, err := sourceIdentities(excludedSources)
	if err != nil {
		return fileCollection{}, errors.Join(err, cleanup())
	}
	collector := newSourceCollector(ctx, m.cfg.BackupMaxSizeBytes(), excludedSources)
	collector.excludedIdentities = excludedIdentities
	if err = collector.addTrustedFile(
		userBackup.Path, CategoryZaparoo, zaparooArchive("user.db"), "user.db",
	); err != nil {
		return fileCollection{}, errors.Join(err, cleanup())
	}
	zaparooDefs := zaparooBackupDefinitions(configDir, dataDir)
	zaparooDefinitions := make([]collectorDefinition, 0, len(zaparooDefs))
	for i := range zaparooDefs {
		def := &zaparooDefs[i]
		trusted := canonicalConfigRoots
		if def.SourceRoot != configDir {
			trusted = canonicalCategoryRoots([]string{dataDir}, def.RestoreRoot)
		}
		zaparooDefinitions = append(zaparooDefinitions, collectorDefinition{
			definition: *def, trustedRoots: trusted, archive: zaparooArchive,
		})
	}
	for i := range zaparooDefinitions {
		collector.collect(&zaparooDefinitions[i])
	}
	m.collectPlatformDefinitions(collector, platformPlan.Definitions)
	for _, warning := range platformPlan.Warnings {
		collector.warn(warning.Category, warning.Path, warning.Reason)
	}
	if collector.err != nil {
		return fileCollection{}, errors.Join(collector.err, cleanup())
	}
	return fileCollection{Files: collector.files, Warnings: collector.warnings, Cleanup: cleanup}, nil
}

// zaparooBackupDefinitions describes which Zaparoo-owned files backups
// collect. validateManifestPolicy checks manifests against these same
// definitions, so collection and restore policy cannot drift. user.db is
// deliberately absent: it is a constructed entry staged from a database
// snapshot, enforced separately as an exactly-once manifest payload.
func zaparooBackupDefinitions(configDir, dataDir string) []platforms.BackupDefinition {
	return []platforms.BackupDefinition{
		{
			SourceRoot: configDir, Category: CategoryZaparoo, NonRecursive: true,
			Include: []platforms.BackupPattern{{Glob: config.CfgFile}},
		},
		{
			SourceRoot: dataDir, Category: CategoryZaparoo, NonRecursive: true,
			Include: []platforms.BackupPattern{{Glob: "frontend.toml"}, {Glob: config.TUIFile}},
		},
		{
			SourceRoot:  filepath.Join(dataDir, config.LaunchersDir),
			RestoreRoot: config.LaunchersDir,
			Category:    CategoryZaparoo, Include: []platforms.BackupPattern{{Glob: "*.toml"}},
		},
		{
			SourceRoot:  filepath.Join(dataDir, config.MappingsDir),
			RestoreRoot: config.MappingsDir,
			Category:    CategoryZaparoo, Include: []platforms.BackupPattern{{Glob: "*.toml"}},
		},
	}
}

func backupPatternsMatch(rel string, patterns []platforms.BackupPattern) bool {
	if len(patterns) == 0 {
		return false
	}
	rel = filepath.ToSlash(rel)
	lowerRel := strings.ToLower(rel)
	for _, pattern := range patterns {
		if pattern.All {
			return true
		}
		if pattern.Contains != "" && strings.Contains(lowerRel, strings.ToLower(pattern.Contains)) {
			return true
		}
		if pattern.Glob == "" {
			continue
		}
		glob := strings.ToLower(filepath.ToSlash(pattern.Glob))
		target := lowerRel
		if !strings.Contains(glob, "/") {
			target = path.Base(lowerRel)
		}
		matched, err := path.Match(glob, target)
		if err == nil && matched {
			return true
		}
	}
	return false
}

func (m *Manager) resolveBackupPath(name string) (string, error) {
	base := filepath.Base(name)
	if base != name || !isBackupZipName(base) {
		return "", fmt.Errorf("invalid backup name: %s", name)
	}
	backupPath := filepath.Join(m.backupDir(), base)
	if _, err := os.Stat(backupPath); err != nil {
		return "", fmt.Errorf("finding backup ZIP: %w", err)
	}
	return backupPath, nil
}

func (m *Manager) applyRestoreFromZip(ctx context.Context, zipPath string, manifest *Manifest) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening backup ZIP: %w", err)
	}
	defer func() {
		if closeErr := zr.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", zipPath).Msg("failed to close backup ZIP")
		}
	}()
	entries := zipEntriesByName(zr.File)
	return m.applyRestore(ctx, manifest, func(file FileRef) (io.ReadCloser, error) {
		entry, ok := entries[file.ArchivePath]
		if !ok {
			return nil, fmt.Errorf("backup ZIP missing payload for %s", file.ArchivePath)
		}
		opened, openErr := entry.Open()
		if openErr != nil {
			return nil, fmt.Errorf("opening backup ZIP payload %s: %w", file.ArchivePath, openErr)
		}
		return opened, nil
	})
}

func readRestorePayload(
	file *FileRef, openPayload func(FileRef) (io.ReadCloser, error), limit int64,
) (data []byte, err error) {
	if file.Size < 0 || file.Size > limit {
		return nil, fmt.Errorf("restore payload %s exceeds %d bytes", file.RestorePath, limit)
	}
	payload, err := openPayload(*file)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := payload.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing restore payload %s: %w", file.RestorePath, closeErr))
		}
	}()
	data, err = io.ReadAll(io.LimitReader(payload, file.Size+1))
	if err != nil {
		return nil, fmt.Errorf("reading restore payload %s: %w", file.RestorePath, err)
	}
	if int64(len(data)) != file.Size {
		return nil, fmt.Errorf("restore payload size mismatch: %s", file.RestorePath)
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != file.SHA256 {
		return nil, fmt.Errorf("restore payload hash mismatch: %s", file.RestorePath)
	}
	return data, nil
}

func (m *Manager) preserveRestoreConfig(
	manifest *Manifest, openPayload func(FileRef) (io.ReadCloser, error),
) (*Manifest, func(FileRef) (io.ReadCloser, error), error) {
	prepared := *manifest
	prepared.Files = append([]FileRef(nil), manifest.Files...)
	for i := range prepared.Files {
		file := &prepared.Files[i]
		if file.Category != CategoryZaparoo || file.RestorePath != config.CfgFile {
			continue
		}
		data, err := readRestorePayload(file, openPayload, maxRestoreConfigSize)
		if err != nil {
			return nil, nil, err
		}
		data, err = config.PreserveRestoreOverrides(data, m.cfg.DeviceID(), m.cfg.EncryptionEnabled())
		if err != nil {
			return nil, nil, fmt.Errorf("preparing restored Core config: %w", err)
		}
		file.Size = int64(len(data))
		hash := sha256.Sum256(data)
		file.SHA256 = hex.EncodeToString(hash[:])
		archivePath := file.ArchivePath
		return &prepared, func(candidate FileRef) (io.ReadCloser, error) {
			if candidate.ArchivePath == archivePath {
				return io.NopCloser(bytes.NewReader(data)), nil
			}
			return openPayload(candidate)
		}, nil
	}
	return &prepared, openPayload, nil
}

func (m *Manager) applyRestore(
	ctx context.Context,
	manifest *Manifest,
	openPayload func(FileRef) (io.ReadCloser, error),
) error {
	prepared, preparedOpen, err := m.preserveRestoreConfig(manifest, openPayload)
	if err != nil {
		return err
	}
	return m.applyRestoreTransaction(ctx, prepared, preparedOpen)
}

func installVerifiedPayload(
	ctx context.Context,
	destination string,
	file *FileRef,
	payload io.ReadCloser,
) (err error) {
	defer func() {
		if closeErr := payload.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing restore payload %s: %w", file.RestorePath, closeErr))
		}
	}()
	if err = os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return fmt.Errorf("creating restore directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".backup-restore-*")
	if err != nil {
		return fmt.Errorf("creating staged restore file for %s: %w", file.RestorePath, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err = tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("securing staged restore file for %s: %w", file.RestorePath, err)
	}

	hash := sha256.New()
	limited := &io.LimitedReader{R: &contextReader{ctx: ctx, reader: payload}, N: file.Size + 1}
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), limited)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if copyErr != nil {
		return fmt.Errorf("staging restore payload %s: %w", file.RestorePath, copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("syncing restore payload %s: %w", file.RestorePath, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing restore payload %s: %w", file.RestorePath, closeErr)
	}
	if written != file.Size {
		return fmt.Errorf("restore payload size mismatch: %s", file.RestorePath)
	}
	if hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return fmt.Errorf("restore payload hash mismatch: %s", file.RestorePath)
	}
	if err = os.Rename(tmpPath, destination); err != nil {
		return fmt.Errorf("installing restore payload %s: %w", file.RestorePath, err)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("reading restore payload: %w", err)
	}
	return n, nil
}

func appendBackupWarnings(
	existing, additional []models.BackupWarning,
) ([]models.BackupWarning, error) {
	if len(additional) > maxArchiveEntries-len(existing) {
		return nil, fmt.Errorf("backup has too many warnings: exceeds %d", maxArchiveEntries)
	}
	return append(existing, additional...), nil
}

func prepareSourceFiles(
	ctx context.Context, files []FileRef, opener sourceOpener,
) ([]FileRef, []models.BackupWarning, error) {
	prepared := make([]FileRef, 0, len(files))
	warnings := make([]models.BackupWarning, 0)
	for i := range files {
		file := files[i]
		hash, err := hashSourceFile(ctx, &file, opener)
		if err == nil {
			file.SHA256 = hash
			prepared = append(prepared, file)
			continue
		}
		if isSkippableSourceError(err) {
			warnings = append(warnings, models.BackupWarning{
				Category: file.Category,
				Path:     portableWarningPath(file.RestorePath),
				Reason:   "source unreadable during backup",
			})
			continue
		}
		return nil, nil, err
	}
	return prepared, warnings, nil
}

func hashSourceFile(ctx context.Context, file *FileRef, opener sourceOpener) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("reading backup source: %w", err)
	}
	if opener == nil {
		opener = openSourceContext
	}
	source, err := opener(ctx, file)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	limited := &io.LimitedReader{R: &contextReader{ctx: ctx, reader: source}, N: file.Size + 1}
	size, readErr := io.Copy(hash, limited)
	closeErr := source.Close()
	if readErr != nil {
		return "", fmt.Errorf("reading backup source %s: %w", file.RestorePath, readErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("closing backup source %s: %w", file.RestorePath, closeErr)
	}
	if size != file.Size {
		return "", fmt.Errorf("%w: source size changed for %s", errSourceIdentityChanged, file.RestorePath)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func isSkippableSourceError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errSourceIdentityChanged) {
		return false
	}
	var pathErr *fs.PathError
	return errors.As(err, &pathErr)
}

func (m *Manager) platformRestoreRoot(dataDir string) string {
	if provider, ok := m.pl.(platforms.BackupRestoreRootProvider); ok {
		return provider.BackupRestoreRoot()
	}
	return filepath.Dir(dataDir)
}

func zaparooArchive(rel string) string {
	return path.Join(filesRoot, zaparooRoot, filepath.ToSlash(rel))
}

func platformArchive(rel string) string {
	return path.Join(filesRoot, platformRoot, filepath.ToSlash(rel))
}

func validateFiles(files []FileRef) error {
	archiveSeen := make(map[string]struct{}, len(files))
	restoreSeen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if !knownCategory(file.Category) {
			return fmt.Errorf("unknown backup category: %s", file.Category)
		}
		if len(file.ArchivePath) > maxArchivePathLen || len(file.RestorePath) > maxArchivePathLen {
			return fmt.Errorf("backup path exceeds %d bytes: %s", maxArchivePathLen, file.RestorePath)
		}
		if err := validateArchivePath(file.ArchivePath); err != nil {
			return fmt.Errorf("invalid archive path %q: %w", file.ArchivePath, err)
		}
		if err := validateRestorePath(file.RestorePath); err != nil {
			return fmt.Errorf("invalid restore path %q: %w", file.RestorePath, err)
		}
		if _, ok := archiveSeen[file.ArchivePath]; ok {
			return fmt.Errorf("duplicate archive path: %s", file.ArchivePath)
		}
		archiveSeen[file.ArchivePath] = struct{}{}
		restoreKey := file.Category + ":" + file.RestorePath
		if isPlatformCategory(file.Category) {
			restoreKey = "platform:" + file.RestorePath
		}
		if _, ok := restoreSeen[restoreKey]; ok {
			return fmt.Errorf("duplicate restore path: %s", file.RestorePath)
		}
		restoreSeen[restoreKey] = struct{}{}
	}
	return nil
}

func knownCategory(category string) bool {
	return category == CategoryZaparoo || isPlatformCategory(category)
}

func isPlatformCategory(category string) bool {
	return category == CategorySettings || category == CategoryInputs ||
		category == CategorySaves || category == CategorySavestates
}

func validateArchivePath(p string) error {
	if p == manifestName {
		return errors.New("reserved manifest path")
	}
	return validateSlashPath(p)
}

func validateRestorePath(p string) error { return validateSlashPath(p) }

func validateSlashPath(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || strings.Contains(p, "\x00") {
		return errors.New("path must be relative slash path")
	}
	cleaned := path.Clean(p)
	if cleaned != p || cleaned == "." {
		return errors.New("path must be clean")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return errors.New("path contains unsafe segment")
		}
	}
	return nil
}

func writeZip(
	ctx context.Context, zipPath string, files []FileRef, manifest *Manifest, maxLogicalSize int64,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("writing backup archive: %w", err)
	}
	if err := validateManifestMetadata(manifest, maxLogicalSize); err != nil {
		return err
	}
	if len(files)+1 > maxArchiveEntries {
		return fmt.Errorf("backup has too many entries: %d exceeds %d", len(files)+1, maxArchiveEntries)
	}
	expectedSize, err := validateLogicalSize(files, maxLogicalSize)
	if err != nil {
		return err
	}
	free, freeErr := helpers.FreeDiskSpace(filepath.Dir(zipPath))
	if freeErr != nil {
		return fmt.Errorf("checking disk space for backup: %w", freeErr)
	}
	required := uint64(expectedSize) + uint64(maxManifestBytes) //nolint:gosec // values are nonnegative and bounded.
	if free < required {
		return fmt.Errorf("insufficient disk space for backup: %d bytes available, need %d", free, required)
	}

	// #nosec G304 -- zipPath is resolved from configured backup directory and generated filename.
	out, err := os.OpenFile(zipPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("creating backup ZIP: %w", err)
	}
	zw := zip.NewWriter(out)
	written := int64(0)
	for i := range files {
		remaining := maxLogicalSize - written
		if err = writeZipFile(ctx, zw, &files[i], remaining); err != nil {
			_ = zw.Close()
			_ = out.Close()
			return err
		}
		written += files[i].Size
	}
	manifest.Files = files
	manifest.Categories = summarize(files)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return fmt.Errorf("marshalling backup manifest: %w", err)
	}
	if int64(len(manifestData)) > maxManifestBytes {
		_ = zw.Close()
		_ = out.Close()
		return fmt.Errorf("backup manifest exceeds %d bytes", maxManifestBytes)
	}
	if err = writeZipBytes(zw, manifestName, manifestData); err != nil {
		_ = zw.Close()
		_ = out.Close()
		return err
	}
	if err = zw.Close(); err != nil {
		_ = out.Close()
		return fmt.Errorf("closing backup ZIP: %w", err)
	}
	if err = out.Sync(); err != nil {
		_ = out.Close()
		return fmt.Errorf("syncing backup ZIP: %w", err)
	}
	if err = out.Close(); err != nil {
		return fmt.Errorf("closing backup ZIP file: %w", err)
	}
	return nil
}

func writeZipFile(ctx context.Context, zw *zip.Writer, file *FileRef, remaining int64) error {
	if remaining < 0 {
		return errors.New("backup exceeds logical size limit")
	}
	in, err := openSourceContext(ctx, file)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := in.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", file.RestorePath).Msg("failed to close backup source")
		}
	}()

	hdr := &zip.FileHeader{Name: file.ArchivePath, Method: zip.Deflate, Modified: time.Now().UTC()}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("creating ZIP entry %s: %w", file.ArchivePath, err)
	}
	hash := sha256.New()
	limited := &io.LimitedReader{R: &contextReader{ctx: ctx, reader: in}, N: remaining + 1}
	size, err := io.Copy(io.MultiWriter(w, hash), limited)
	if err != nil {
		return fmt.Errorf("writing ZIP entry %s: %w", file.ArchivePath, err)
	}
	if size > remaining {
		return fmt.Errorf("backup exceeds logical size limit while reading %s", file.RestorePath)
	}
	if file.Size != size {
		return fmt.Errorf("backup source changed size while reading %s", file.RestorePath)
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if file.SHA256 != "" && file.SHA256 != actualHash {
		return fmt.Errorf("%w: source content changed for %s", errSourceIdentityChanged, file.RestorePath)
	}
	file.SHA256 = actualHash
	file.Size = size
	return nil
}

func validateLogicalSize(files []FileRef, maxLogicalSize int64) (int64, error) {
	if maxLogicalSize <= 0 || maxLogicalSize == math.MaxInt64 {
		return 0, errors.New("backup logical size limit must be positive")
	}
	var total int64
	for _, file := range files {
		if file.Size < 0 || file.Size > maxLogicalSize-total {
			return 0, fmt.Errorf("backup exceeds logical size limit of %d bytes", maxLogicalSize)
		}
		total += file.Size
	}
	return total, nil
}

func writeZipBytes(zw *zip.Writer, name string, data []byte) error {
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now().UTC()}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("creating ZIP entry %s: %w", name, err)
	}
	if _, err = w.Write(data); err != nil {
		return fmt.Errorf("writing ZIP entry %s: %w", name, err)
	}
	return nil
}

func fastInfoFromDirEntry(backupDir string, entry os.DirEntry) (ListInfo, error) {
	fileInfo, err := entry.Info()
	if err != nil {
		return ListInfo{}, fmt.Errorf("stating backup ZIP: %w", err)
	}
	createdAt := fileInfo.ModTime().UTC()
	if parsed, ok := parseBackupNameTime(entry.Name()); ok {
		createdAt = parsed
	}
	return ListInfo{
		Name:      entry.Name(),
		Path:      filepath.Join(backupDir, entry.Name()),
		CreatedAt: createdAt,
		Size:      fileInfo.Size(),
	}, nil
}

func parseBackupNameTime(name string) (time.Time, bool) {
	trimmed := strings.TrimPrefix(name, "backup-")
	parts := strings.SplitN(trimmed, "-", 3)
	if len(parts) < 2 {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation("20060102150405", parts[0]+parts[1], time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func infoFromManifest(zipPath string, manifest *Manifest) (Info, error) {
	st, err := os.Stat(zipPath)
	if err != nil {
		return Info{}, fmt.Errorf("stating backup ZIP: %w", err)
	}
	status := StatusSuccess
	if len(manifest.Warnings) > 0 {
		status = StatusPartial
	}
	return Info{
		Name:       filepath.Base(zipPath),
		Path:       zipPath,
		CreatedAt:  manifest.CreatedAt,
		Size:       st.Size(),
		Status:     status,
		Integrity:  IntegrityValid,
		Categories: manifest.Categories,
		Warnings:   manifest.Warnings,
	}, nil
}

func inspectZipManifest(zipPath string, maxLogicalSize int64) (Info, error) {
	zr, openErr := zip.OpenReader(zipPath)
	if openErr != nil {
		return Info{}, fmt.Errorf("opening backup ZIP: %w", openErr)
	}
	defer func() {
		if closeErr := zr.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", zipPath).Msg("failed to close backup ZIP")
		}
	}()
	entries, err := validateZipHeaders(zr.File, maxLogicalSize)
	if err != nil {
		return Info{}, err
	}
	manifest, err := readManifestFromZipLimit(zr.File, entries, maxLogicalSize)
	if err != nil {
		return Info{}, err
	}
	info, err := infoFromManifest(zipPath, manifest)
	if err != nil {
		return Info{}, err
	}
	info.Integrity = IntegrityUnchecked
	return info, nil
}

func (m *Manager) validateLocalArchiveManifest(zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening backup ZIP: %w", err)
	}
	defer func() { _ = zr.Close() }()
	entries, err := validateZipHeaders(zr.File, m.cfg.BackupMaxSizeBytes())
	if err != nil {
		return err
	}
	manifest, err := readManifestFromZipLimit(zr.File, entries, m.cfg.BackupMaxSizeBytes())
	if err != nil {
		return err
	}
	return m.validateManifestPolicy(manifest)
}

func readManifestFromZipLimit(
	files []*zip.File,
	entries map[string]*zip.File,
	maxLogicalSize int64,
) (*Manifest, error) {
	manifestEntry, ok := entries[manifestName]
	if !ok {
		return nil, errors.New("backup ZIP missing manifest")
	}
	body, err := readZipEntryLimited(manifestEntry, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err = json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("decoding backup manifest: %w", err)
	}
	if manifest.Version != 1 {
		return nil, fmt.Errorf("unsupported backup manifest version: %d", manifest.Version)
	}
	if validateErr := validateManifest(&manifest, files, entries, maxLogicalSize); validateErr != nil {
		return nil, validateErr
	}
	return &manifest, nil
}

func readAndVerifyZipLimit(zipPath string, maxLogicalSize int64) (*zipReadResult, error) {
	zr, openErr := zip.OpenReader(zipPath)
	if openErr != nil {
		return nil, fmt.Errorf("opening backup ZIP: %w", openErr)
	}
	defer func() {
		if closeErr := zr.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", zipPath).Msg("failed to close backup ZIP")
		}
	}()
	entries, err := validateZipHeaders(zr.File, maxLogicalSize)
	if err != nil {
		return nil, err
	}
	manifest, err := readManifestFromZipLimit(zr.File, entries, maxLogicalSize)
	if err != nil {
		return nil, err
	}
	for i := range manifest.Files {
		file := &manifest.Files[i]
		entry := entries[file.ArchivePath]
		if verifyErr := verifyZipEntry(entry, file); verifyErr != nil {
			return nil, verifyErr
		}
	}
	info, err := infoFromManifest(zipPath, manifest)
	if err != nil {
		return nil, err
	}
	info.Integrity = IntegrityValid
	return &zipReadResult{Manifest: *manifest, Info: info}, nil
}

func validateZipHeaders(files []*zip.File, maxLogicalSize int64) (map[string]*zip.File, error) {
	if maxLogicalSize <= 0 || maxLogicalSize == math.MaxInt64 {
		return nil, errors.New("backup logical size limit must be positive")
	}
	if len(files) > maxArchiveEntries {
		return nil, fmt.Errorf("backup has too many entries: %d exceeds %d", len(files), maxArchiveEntries)
	}
	entries := make(map[string]*zip.File, len(files))
	var total int64
	for _, file := range files {
		if len(file.Name) > maxArchivePathLen {
			return nil, fmt.Errorf("ZIP entry path exceeds %d bytes", maxArchivePathLen)
		}
		if file.Name == manifestName {
			if file.UncompressedSize64 > uint64(maxManifestBytes) {
				return nil, fmt.Errorf("backup manifest exceeds %d bytes", maxManifestBytes)
			}
		} else if err := validateArchivePath(file.Name); err != nil {
			return nil, fmt.Errorf("invalid ZIP entry %q: %w", file.Name, err)
		}
		if !file.Mode().IsRegular() {
			return nil, fmt.Errorf("ZIP entry is not a regular file: %s", file.Name)
		}
		if _, exists := entries[file.Name]; exists {
			return nil, fmt.Errorf("duplicate ZIP entry: %s", file.Name)
		}
		entries[file.Name] = file
		if file.Name == manifestName {
			continue
		}
		if file.UncompressedSize64 > uint64(maxLogicalSize) {
			return nil, fmt.Errorf("backup exceeds logical size limit of %d bytes", maxLogicalSize)
		}
		size := int64(file.UncompressedSize64) //nolint:gosec // bounded by positive maxLogicalSize above.
		if size > maxLogicalSize-total {
			return nil, fmt.Errorf("backup exceeds logical size limit of %d bytes", maxLogicalSize)
		}
		total += size
	}
	return entries, nil
}

func validateManifestMetadata(manifest *Manifest, maxLogicalSize int64) error {
	if manifest.Version != 1 {
		return fmt.Errorf("unsupported backup manifest version: %d", manifest.Version)
	}
	if manifest.Platform == "" {
		return errors.New("backup manifest platform is required")
	}
	if manifest.CreatedAt.IsZero() {
		return errors.New("backup manifest creation time is required")
	}
	if err := validateFiles(manifest.Files); err != nil {
		return err
	}
	if _, err := validateLogicalSize(manifest.Files, maxLogicalSize); err != nil {
		return err
	}
	if !categorySummariesEqual(manifest.Categories, summarize(manifest.Files)) {
		return errors.New("backup manifest category summary mismatch")
	}
	for _, warning := range manifest.Warnings {
		if !knownCategory(warning.Category) || warning.Reason == "" ||
			len(warning.Path) > maxArchivePathLen || validateRestorePath(warning.Path) != nil {
			return errors.New("backup manifest contains invalid warning metadata")
		}
	}
	return nil
}

func validateManifest(
	manifest *Manifest,
	files []*zip.File,
	entries map[string]*zip.File,
	maxLogicalSize int64,
) error {
	if err := validateManifestMetadata(manifest, maxLogicalSize); err != nil {
		return err
	}
	if len(manifest.Files)+1 != len(files) {
		return errors.New("backup ZIP payload entries do not match manifest")
	}
	declared := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		entry, ok := entries[file.ArchivePath]
		if !ok {
			return fmt.Errorf("missing ZIP entry: %s", file.ArchivePath)
		}
		if entry.UncompressedSize64 > math.MaxInt64 || int64(entry.UncompressedSize64) != file.Size {
			return fmt.Errorf("backup ZIP size mismatch: %s", file.ArchivePath)
		}
		hash, err := hex.DecodeString(file.SHA256)
		if err != nil || len(hash) != sha256.Size {
			return fmt.Errorf("invalid SHA-256 for %s", file.ArchivePath)
		}
		expectedPrefix := path.Join(filesRoot, platformRoot) + "/"
		if file.Category == CategoryZaparoo {
			expectedPrefix = path.Join(filesRoot, zaparooRoot) + "/"
		}
		if !strings.HasPrefix(file.ArchivePath, expectedPrefix) {
			return fmt.Errorf("archive path does not match category %s: %s", file.Category, file.ArchivePath)
		}
		declared[file.ArchivePath] = struct{}{}
	}
	for name := range entries {
		if name == manifestName {
			continue
		}
		if _, ok := declared[name]; !ok {
			return fmt.Errorf("undeclared ZIP entry: %s", name)
		}
	}
	return nil
}

func categorySummariesEqual(
	actual, expected map[string]models.BackupCategoryStatus,
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

func verifyZipEntry(entry *zip.File, file *FileRef) error {
	r, err := entry.Open()
	if err != nil {
		return fmt.Errorf("opening ZIP entry %s: %w", entry.Name, err)
	}
	defer func() { _ = r.Close() }()

	hash := sha256.New()
	limited := &io.LimitedReader{R: r, N: file.Size + 1}
	size, err := io.Copy(hash, limited)
	if err != nil {
		return fmt.Errorf("reading ZIP entry %s: %w", entry.Name, err)
	}
	if size != file.Size {
		return fmt.Errorf("backup ZIP size mismatch: %s", entry.Name)
	}
	if hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return fmt.Errorf("backup ZIP hash mismatch: %s", entry.Name)
	}
	return nil
}

func zipEntriesByName(files []*zip.File) map[string]*zip.File {
	entries := make(map[string]*zip.File, len(files))
	for _, file := range files {
		entries[file.Name] = file
	}
	return entries
}

func readZipEntryLimited(f *zip.File, limit int64) ([]byte, error) {
	if limit < 0 || f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("ZIP entry %s exceeds %d bytes", f.Name, limit)
	}
	r, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("opening ZIP entry %s: %w", f.Name, err)
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("name", f.Name).Msg("failed to close ZIP entry")
		}
	}()
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading ZIP entry %s: %w", f.Name, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("ZIP entry %s exceeds %d bytes", f.Name, limit)
	}
	return body, nil
}

func summarize(files []FileRef) map[string]models.BackupCategoryStatus {
	out := map[string]models.BackupCategoryStatus{
		CategoryZaparoo:    {Enabled: true},
		CategorySettings:   {Enabled: true},
		CategoryInputs:     {Enabled: true},
		CategorySaves:      {Enabled: true},
		CategorySavestates: {Enabled: true},
	}
	for _, file := range files {
		entry := out[file.Category]
		entry.Files++
		entry.Bytes += file.Size
		entry.Enabled = true
		out[file.Category] = entry
	}
	return out
}

func toStatusEntry(st *statusEntry, enabled bool, schedule string) models.BackupStatusEntry {
	lastStatus := st.LastStatus
	if lastStatus == "" {
		lastStatus = StatusNever
	}
	availability := st.Availability
	if schedule != "" && availability == "" {
		availability = RemoteAvailabilityUnknown
	}
	return models.BackupStatusEntry{
		LastRunAt:             optionalString(st.LastRunAt),
		LastSuccessAt:         optionalString(st.LastSuccessAt),
		AvailabilityCheckedAt: optionalString(st.AvailabilityCheckedAt),
		DeviceName:            optionalString(st.DeviceName),
		LinkedAt:              optionalString(st.LinkedAt),
		Availability:          availability,
		LastError:             safeStatusErrorString(st.LastError),
		LastStatus:            lastStatus,
		LastBackupSize:        st.LastBackupSize,
		Categories:            st.Categories,
		Warnings:              st.Warnings,
		SkippedFiles:          st.SkippedFiles,
		Enabled:               enabled,
		Schedule:              schedule,
	}
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func backupName(preRestore bool, now time.Time) string {
	kind := "manual"
	if preRestore {
		kind = "pre-restore"
	}
	return fmt.Sprintf("backup-%s-%09d-%s.zip", now.Format("20060102-150405"), now.Nanosecond(), kind)
}

func isBackupZipName(name string) bool {
	return strings.HasPrefix(name, "backup-") && strings.HasSuffix(name, ".zip")
}

func databaseBackupName(now time.Time) string {
	return fmt.Sprintf("backup-%s-%09d-manual.db", now.Format("20060102-150405"), now.Nanosecond())
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// safeStatusReasons are the only failure strings ever persisted or shown:
// short, application-style reasons with no error internals.
var safeStatusReasons = map[string]struct{}{
	"not available for this account": {},
	"storage quota exceeded":         {},
	"device not linked":              {},
	"rate limited":                   {},
	"files changed during backup":    {},
	"requires newer Core version":    {},
	"another backup was running":     {},
	"backup failed":                  {},
}

func safeStatusError(err error) string {
	var busy *BusyError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errRemoteNotAvailable):
		return "not available for this account"
	case errors.Is(err, errRemoteQuotaExceeded):
		return "storage quota exceeded"
	case errors.Is(err, errRemoteUnlinked):
		return "device not linked"
	case errors.Is(err, errRemoteRateLimited):
		return "rate limited"
	case errors.Is(err, errRemoteIntegrityRetry):
		return "files changed during backup"
	case errors.Is(err, errRemoteNewerSchema):
		return "requires newer Core version"
	case errors.As(err, &busy):
		return "another backup was running"
	default:
		return "backup failed"
	}
}

func safeStatusErrorString(msg string) string {
	if msg == "" {
		return ""
	}
	if _, ok := safeStatusReasons[msg]; ok {
		return msg
	}
	return "backup failed"
}
