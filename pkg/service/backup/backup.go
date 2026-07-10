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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

var statusMu syncutil.RWMutex

const (
	CategoryZaparoo    = "zaparoo"
	CategorySettings   = "settings"
	CategoryInputs     = "inputs"
	CategorySaves      = "saves"
	CategorySavestates = "savestates"

	StatusNever   = "never"
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"

	manifestName  = "manifest.json"
	filesRoot     = "files"
	zaparooRoot   = "zaparoo"
	platformRoot  = "platform"
	backupDirName = "files"
)

type Manager struct {
	cfg      *config.Instance
	pl       platforms.Platform
	database *database.Database
	inbox    *inboxservice.Service
}

type FileRef struct {
	SourcePath  string `json:"-"`
	ArchivePath string `json:"archivePath"`
	RestorePath string `json:"restorePath"`
	Category    string `json:"category"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
}

type Manifest struct {
	CreatedAt   time.Time                              `json:"createdAt"`
	Categories  map[string]models.BackupCategoryStatus `json:"categories"`
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
	Error      string                                 `json:"error,omitempty"`
	Size       int64                                  `json:"size"`
	Valid      bool                                   `json:"valid"`
}

type RestoreInfo struct {
	PreRestoreBackup *Info `json:"preRestoreBackup,omitempty"`
	RestoredFrom     Info  `json:"restoredFrom"`
}

type zipReadResult struct {
	Manifest Manifest
	Info     Info
}

type statusFile struct {
	Local  statusEntry `json:"local"`
	Remote statusEntry `json:"remote"`
}

type statusEntry struct {
	Categories     map[string]models.BackupCategoryStatus `json:"categories,omitempty"`
	LastRunAt      string                                 `json:"lastRunAt,omitempty"`
	LastSuccessAt  string                                 `json:"lastSuccessAt,omitempty"`
	LastError      string                                 `json:"lastError,omitempty"`
	LastStatus     string                                 `json:"lastStatus"`
	LastBackupSize int64                                  `json:"lastBackupSize"`
	// Unlinked records a 401 from the remote API: the token was revoked
	// server-side (the credential file still exists). Cleared by a
	// successful run or a fresh claim/link.
	Unlinked bool `json:"unlinked,omitempty"`
}

func NewManager(cfg *config.Instance, pl platforms.Platform, db *database.Database) *Manager {
	return &Manager{cfg: cfg, pl: pl, database: db}
}

func (m *Manager) WithInbox(inbox *inboxservice.Service) *Manager {
	m.inbox = inbox
	return m
}

func (m *Manager) Create() (Info, error) {
	started := time.Now().UTC()
	_ = m.writeLocalStatus(&statusEntry{LastRunAt: formatTime(started), LastStatus: StatusRunning})

	info, err := m.createBackup(false)
	if err != nil {
		_ = m.writeLocalStatus(&statusEntry{
			LastRunAt:  formatTime(started),
			LastStatus: StatusFailed,
			LastError:  safeStatusError(err),
		})
		return Info{}, err
	}
	_ = m.writeLocalStatus(&statusEntry{
		LastRunAt:      formatTime(started),
		LastSuccessAt:  formatTime(info.CreatedAt),
		LastStatus:     StatusSuccess,
		LastBackupSize: info.Size,
		Categories:     info.Categories,
	})
	return info, nil
}

func (m *Manager) createBackup(preRestore bool) (Info, error) {
	files, err := m.collectFiles()
	if err != nil {
		return Info{}, err
	}
	if validateErr := validateFiles(files); validateErr != nil {
		return Info{}, validateErr
	}
	now := time.Now().UTC()
	backupDir := m.backupDir()
	if mkdirErr := os.MkdirAll(backupDir, 0o750); mkdirErr != nil {
		return Info{}, fmt.Errorf("creating backup directory: %w", mkdirErr)
	}
	name := backupName(preRestore, now)
	finalPath := filepath.Join(backupDir, name)
	tmpPath := finalPath + ".tmp"
	manifest := m.newManifest(now, files)
	if writeErr := writeZip(tmpPath, files, &manifest); writeErr != nil {
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
	return info, nil
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

func (m *Manager) Inspect(name string) (Info, error) {
	backupPath, err := m.resolveBackupPath(name)
	if err != nil {
		return Info{}, err
	}
	info, err := inspectZipManifest(backupPath)
	if err != nil {
		return Info{}, fmt.Errorf("inspecting backup ZIP: %w", err)
	}
	return info, nil
}

func (m *Manager) Delete(name string) error {
	backupPath, err := m.resolveBackupPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("deleting backup ZIP: %w", err)
	}
	return nil
}

func (m *Manager) Restore(name string) (RestoreInfo, error) {
	backupPath, err := m.resolveBackupPath(name)
	if err != nil {
		return RestoreInfo{}, err
	}
	zipResult, err := readAndVerifyZip(backupPath)
	if err != nil {
		return RestoreInfo{}, err
	}
	if validateErr := validateFiles(zipResult.Manifest.Files); validateErr != nil {
		return RestoreInfo{}, validateErr
	}
	pre, err := m.createBackup(true)
	if err != nil {
		return RestoreInfo{}, fmt.Errorf("creating pre-restore backup: %w", err)
	}
	if err := m.applyRestoreFromZip(backupPath, &zipResult.Manifest); err != nil {
		return RestoreInfo{}, err
	}
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
	return models.BackupStatusResponse{Local: local, Remote: remote}
}

func (m *Manager) backupDir() string {
	if localDir := m.cfg.BackupLocalDir(); localDir != "" {
		return localDir
	}
	return filepath.Join(helpers.DataDir(m.pl), "backups", backupDirName)
}

func (m *Manager) statusPath() string {
	return filepath.Join(m.backupDir(), "status.json")
}

func (m *Manager) readStatus() statusFile {
	statusMu.RLock()
	defer statusMu.RUnlock()

	return m.readStatusLocked()
}

func (m *Manager) readStatusLocked() statusFile {
	var st statusFile
	data, err := os.ReadFile(m.statusPath())
	if err == nil {
		_ = json.Unmarshal(data, &st)
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
	if err := os.MkdirAll(m.backupDir(), 0o750); err != nil {
		return fmt.Errorf("creating backup status directory: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding backup status: %w", err)
	}
	if err := os.WriteFile(m.statusPath(), data, 0o600); err != nil {
		return fmt.Errorf("writing backup status: %w", err)
	}
	return nil
}

func (m *Manager) newManifest(createdAt time.Time, files []FileRef) Manifest {
	return Manifest{
		Version:     1,
		CreatedAt:   createdAt,
		Platform:    m.pl.ID(),
		CoreVersion: config.AppVersion,
		Categories:  summarize(files),
		Files:       files,
	}
}

func (m *Manager) collectFiles() ([]FileRef, error) {
	var files []FileRef
	dataDir := helpers.DataDir(m.pl)
	configDir := helpers.ConfigDir(m.pl)

	if m.database == nil || m.database.UserDB == nil {
		return nil, errors.New("database is not available")
	}
	// UserDB.Backup creates a managed auto backup with quick_check validation.
	// Auto-backup pruning in UserDB bounds retained snapshots.
	userBackup, err := m.database.UserDB.Backup("local-zip", false)
	if err != nil {
		return nil, fmt.Errorf("snapshotting user database: %w", err)
	}
	files = append(files, fileRef(userBackup.Path, CategoryZaparoo, zaparooArchive("user.db"), "user.db"))

	for _, item := range []struct{ root, rel string }{
		{configDir, config.CfgFile},
		{dataDir, "frontend.toml"},
		{dataDir, config.TUIFile},
	} {
		files = appendIfExists(
			files,
			filepath.Join(item.root, item.rel),
			CategoryZaparoo,
			zaparooArchive(item.rel),
			item.rel,
		)
	}
	files = appendWalk(
		files,
		filepath.Join(dataDir, config.LaunchersDir),
		CategoryZaparoo,
		config.LaunchersDir,
		zaparooArchive,
		includeTOML,
	)
	files = appendWalk(
		files,
		filepath.Join(dataDir, config.MappingsDir),
		CategoryZaparoo,
		config.MappingsDir,
		zaparooArchive,
		includeTOML,
	)

	if provider, ok := m.pl.(platforms.BackupProvider); ok {
		files = collectPlatformFiles(files, provider.BackupDefinitions())
	}
	return files, nil
}

func collectPlatformFiles(files []FileRef, definitions []platforms.BackupDefinition) []FileRef {
	for i := range definitions {
		files = appendBackupDefinition(files, &definitions[i])
	}
	return files
}

func appendBackupDefinition(files []FileRef, def *platforms.BackupDefinition) []FileRef {
	if def.NonRecursive {
		return appendNonRecursiveBackupDefinition(files, def)
	}
	walkErr := filepath.WalkDir(def.SourceRoot, func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		return appendBackupDefinitionFile(&files, def, filePath)
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		log.Debug().Err(walkErr).Str("root", def.SourceRoot).Msg("failed to walk platform backup source")
	}
	return files
}

func appendNonRecursiveBackupDefinition(files []FileRef, def *platforms.BackupDefinition) []FileRef {
	entries, err := os.ReadDir(def.SourceRoot)
	if errors.Is(err, os.ErrNotExist) {
		return files
	}
	if err != nil {
		log.Debug().Err(err).Str("root", def.SourceRoot).Msg("failed to list platform backup source")
		return files
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filePath := filepath.Join(def.SourceRoot, entry.Name())
		if appendErr := appendBackupDefinitionFile(&files, def, filePath); appendErr != nil {
			log.Debug().Err(appendErr).Str("path", filePath).Msg("failed to collect platform backup file")
		}
	}
	return files
}

func appendBackupDefinitionFile(files *[]FileRef, def *platforms.BackupDefinition, filePath string) error {
	rel, relErr := filepath.Rel(def.SourceRoot, filePath)
	if relErr != nil {
		return fmt.Errorf("computing platform backup source path: %w", relErr)
	}
	if !backupPatternsMatch(rel, def.Include) || backupPatternsMatch(rel, def.Exclude) {
		return nil
	}
	restorePath := filepath.Join(def.RestoreRoot, rel)
	*files = appendIfExists(*files, filePath, def.Category, platformArchive(restorePath), restorePath)
	return nil
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
		glob := filepath.ToSlash(pattern.Glob)
		matched, err := path.Match(strings.ToLower(glob), lowerRel)
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

func (m *Manager) applyRestoreFromZip(zipPath string, manifest *Manifest) error {
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
	return m.applyRestore(manifest, func(file FileRef) ([]byte, error) {
		entry, ok := entries[file.ArchivePath]
		if !ok {
			return nil, fmt.Errorf("backup ZIP missing payload for %s", file.ArchivePath)
		}
		return readZipEntry(entry)
	})
}

func (m *Manager) applyRestore(manifest *Manifest, readPayload func(FileRef) ([]byte, error)) error {
	dataDir := helpers.DataDir(m.pl)
	configDir := helpers.ConfigDir(m.pl)
	platformBase := m.platformRestoreRoot(dataDir)
	var userDBPayload []byte
	var userDBFound bool
	for _, file := range manifest.Files {
		payload, err := readPayload(file)
		if err != nil {
			return err
		}
		if file.Category == CategoryZaparoo && file.RestorePath == "user.db" {
			userDBPayload = payload
			userDBFound = true
			continue
		}
		root := dataDir
		if file.Category == CategoryZaparoo && file.RestorePath == config.CfgFile {
			root = configDir
		}
		if strings.HasPrefix(file.ArchivePath, path.Join(filesRoot, platformRoot)+"/") {
			root = platformBase
		}
		dst, err := safeJoin(root, file.RestorePath)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return fmt.Errorf("creating restore directory: %w", err)
		}
		if err = os.WriteFile(dst, payload, 0o600); err != nil {
			return fmt.Errorf("restoring %s: %w", file.RestorePath, err)
		}
	}
	if userDBFound {
		name := databaseBackupName(time.Now().UTC())
		backupDir := filepath.Join(filepath.Dir(m.database.UserDB.GetDBPath()), "backups")
		if err := os.MkdirAll(backupDir, 0o750); err != nil {
			return fmt.Errorf("creating user database restore staging directory: %w", err)
		}
		staged := filepath.Join(backupDir, name)
		if err := os.WriteFile(staged, userDBPayload, 0o600); err != nil {
			return fmt.Errorf("staging user database restore: %w", err)
		}
		defer func() {
			if removeErr := os.Remove(staged); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				log.Debug().Err(removeErr).Str("path", staged).Msg("failed to remove staged user database restore")
			}
		}()
		if _, err := m.database.UserDB.RestoreBackup(name); err != nil {
			return fmt.Errorf("restoring user database: %w", err)
		}
	}
	return nil
}

func (m *Manager) platformRestoreRoot(dataDir string) string {
	if provider, ok := m.pl.(platforms.BackupRestoreRootProvider); ok {
		return provider.BackupRestoreRoot()
	}
	return filepath.Dir(dataDir)
}

func appendIfExists(files []FileRef, sourcePath, category, archivePath, restorePath string) []FileRef {
	info, err := os.Stat(sourcePath)
	if err != nil || info.IsDir() {
		return files
	}
	files = append(files, fileRef(sourcePath, category, archivePath, filepath.ToSlash(restorePath)))
	return files
}

func appendWalk(
	files []FileRef,
	root, category, restorePrefix string,
	archiveFn func(string) string,
	include func(string) bool,
) []FileRef {
	walkErr := filepath.WalkDir(root, func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, filePath)
		if relErr != nil {
			return fmt.Errorf("computing backup source path: %w", relErr)
		}
		if !include(rel) {
			return nil
		}
		restorePath := filepath.Join(restorePrefix, rel)
		files = appendIfExists(files, filePath, category, archiveFn(restorePath), restorePath)
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		log.Debug().Err(walkErr).Str("root", root).Msg("failed to walk backup source")
	}
	return files
}

func fileRef(sourcePath, category, archivePath, restorePath string) FileRef {
	return FileRef{
		SourcePath:  sourcePath,
		Category:    category,
		ArchivePath: filepath.ToSlash(archivePath),
		RestorePath: filepath.ToSlash(restorePath),
	}
}

func zaparooArchive(rel string) string {
	return path.Join(filesRoot, zaparooRoot, filepath.ToSlash(rel))
}

func platformArchive(rel string) string {
	return path.Join(filesRoot, platformRoot, filepath.ToSlash(rel))
}

func includeTOML(rel string) bool { return strings.EqualFold(filepath.Ext(rel), ".toml") }

func validateFiles(files []FileRef) error {
	archiveSeen := make(map[string]struct{}, len(files))
	restoreSeen := make(map[string]struct{}, len(files))
	for _, file := range files {
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

func writeZip(zipPath string, files []FileRef, manifest *Manifest) error {
	// #nosec G304 -- zipPath is resolved from configured backup directory and generated filename.
	out, err := os.OpenFile(zipPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("creating backup ZIP: %w", err)
	}
	zw := zip.NewWriter(out)
	for i := range files {
		if err = writeZipFile(zw, &files[i]); err != nil {
			_ = zw.Close()
			_ = out.Close()
			return err
		}
	}
	manifest.Files = files
	manifest.Categories = summarize(files)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return fmt.Errorf("marshalling backup manifest: %w", err)
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
	if err = out.Close(); err != nil {
		return fmt.Errorf("closing backup ZIP file: %w", err)
	}
	return nil
}

func writeZipFile(zw *zip.Writer, file *FileRef) error {
	in, err := os.Open(file.SourcePath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", file.SourcePath, err)
	}
	defer func() {
		if closeErr := in.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", file.SourcePath).Msg("failed to close backup source")
		}
	}()

	hdr := &zip.FileHeader{Name: file.ArchivePath, Method: zip.Deflate, Modified: time.Now().UTC()}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("creating ZIP entry %s: %w", file.ArchivePath, err)
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(w, hash), in)
	if err != nil {
		return fmt.Errorf("writing ZIP entry %s: %w", file.ArchivePath, err)
	}
	file.SHA256 = hex.EncodeToString(hash.Sum(nil))
	file.Size = size
	return nil
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
	return Info{
		Name:       filepath.Base(zipPath),
		Path:       zipPath,
		CreatedAt:  manifest.CreatedAt,
		Size:       st.Size(),
		Status:     StatusSuccess,
		Valid:      true,
		Categories: manifest.Categories,
	}, nil
}

func inspectZipManifest(zipPath string) (Info, error) {
	zr, openErr := zip.OpenReader(zipPath)
	if openErr != nil {
		return Info{}, fmt.Errorf("opening backup ZIP: %w", openErr)
	}
	defer func() {
		if closeErr := zr.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", zipPath).Msg("failed to close backup ZIP")
		}
	}()
	manifest, err := readManifestFromZip(zr.File)
	if err != nil {
		return Info{}, err
	}
	return infoFromManifest(zipPath, manifest)
}

func readManifestFromZip(files []*zip.File) (*Manifest, error) {
	var manifest Manifest
	found := false
	for _, f := range files {
		if validateErr := validateArchivePath(f.Name); validateErr != nil && f.Name != manifestName {
			return nil, validateErr
		}
		if f.Name != manifestName {
			continue
		}
		body, readErr := readZipEntry(f)
		if readErr != nil {
			return nil, readErr
		}
		if decodeErr := json.Unmarshal(body, &manifest); decodeErr != nil {
			return nil, fmt.Errorf("decoding backup manifest: %w", decodeErr)
		}
		found = true
	}
	if !found {
		return nil, errors.New("backup ZIP missing manifest")
	}
	if manifest.Version != 1 {
		return nil, fmt.Errorf("unsupported backup manifest version: %d", manifest.Version)
	}
	if validateErr := validateFiles(manifest.Files); validateErr != nil {
		return nil, validateErr
	}
	return &manifest, nil
}

func readAndVerifyZip(zipPath string) (*zipReadResult, error) {
	zr, openErr := zip.OpenReader(zipPath)
	if openErr != nil {
		return nil, fmt.Errorf("opening backup ZIP: %w", openErr)
	}
	defer func() {
		if closeErr := zr.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", zipPath).Msg("failed to close backup ZIP")
		}
	}()
	entries := zipEntriesByName(zr.File)
	manifest, err := readManifestFromZip(zr.File)
	if err != nil {
		return nil, err
	}
	for _, file := range manifest.Files {
		entry, ok := entries[file.ArchivePath]
		if !ok {
			return nil, fmt.Errorf("missing ZIP entry: %s", file.ArchivePath)
		}
		payload, readErr := readZipEntry(entry)
		if readErr != nil {
			return nil, readErr
		}
		sum := sha256.Sum256(payload)
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			return nil, fmt.Errorf("backup ZIP hash mismatch: %s", file.ArchivePath)
		}
	}
	st, err := os.Stat(zipPath)
	if err != nil {
		return nil, fmt.Errorf("stating backup ZIP: %w", err)
	}
	return &zipReadResult{Manifest: *manifest, Info: Info{
		Name:       filepath.Base(zipPath),
		Path:       zipPath,
		CreatedAt:  manifest.CreatedAt,
		Size:       st.Size(),
		Status:     StatusSuccess,
		Valid:      true,
		Categories: manifest.Categories,
	}}, nil
}

func zipEntriesByName(files []*zip.File) map[string]*zip.File {
	entries := make(map[string]*zip.File, len(files))
	for _, file := range files {
		entries[file.Name] = file
	}
	return entries
}

func readZipEntry(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("opening ZIP entry %s: %w", f.Name, err)
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("name", f.Name).Msg("failed to close ZIP entry")
		}
	}()
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading ZIP entry %s: %w", f.Name, err)
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
	return models.BackupStatusEntry{
		LastRunAt:      optionalString(st.LastRunAt),
		LastSuccessAt:  optionalString(st.LastSuccessAt),
		LastError:      safeStatusErrorString(st.LastError),
		LastStatus:     lastStatus,
		LastBackupSize: st.LastBackupSize,
		Categories:     st.Categories,
		Enabled:        enabled,
		Schedule:       schedule,
	}
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func safeJoin(root, rel string) (string, error) {
	if err := validateRestorePath(rel); err != nil {
		return "", err
	}
	joined := filepath.Join(root, filepath.FromSlash(rel))
	cleanRoot := filepath.Clean(root)
	cleanJoined := filepath.Clean(joined)
	if cleanJoined != cleanRoot && !strings.HasPrefix(cleanJoined, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("restore path escapes root: %s", rel)
	}
	return cleanJoined, nil
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
	case errors.Is(err, errRemoteBackupRunning):
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
