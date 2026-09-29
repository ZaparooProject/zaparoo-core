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

// Package android is the Android platform. It is ordinary Go: everything that
// needs the Android framework goes through the Host the embedding host supplies.
package android

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/libretrothumbs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

// errFolderUnavailable reports media in a folder the host no longer lists.
var errFolderUnavailable = errors.New("media folder is no longer granted")

// errNotAFile reports a source path with no segments below its root, which
// names a folder, not a file whose content could be read.
var errNotAFile = errors.New("source path names no file")

// Platform implements platforms.Platform for Android. Token readers, input
// and screenshots have no host capability yet and report ErrNotSupported.
type Platform struct {
	host             Host
	launcherContexts platforms.LauncherContextManager
	db               *database.Database
	entryByID        map[string]*catalogEntry
	// folders maps each source root ID to the host reference it was made
	// from, as of the last SourceRoots call.
	folders      map[string]string
	settings     platforms.Settings
	historyHooks platforms.MediaHistoryHooks
	entries      []catalogEntry
	mu           syncutil.RWMutex
}

var (
	_ platforms.Platform             = (*Platform)(nil)
	_ platforms.SourceRootReader     = (*Platform)(nil)
	_ platforms.MediaHistoryRecorder = (*Platform)(nil)
)

// New builds the platform over the directories the host owns. A nil host
// yields a platform with no launchers and no media.
func New(settings platforms.Settings, host Host) (*Platform, error) {
	entries, err := loadCatalog()
	if err != nil {
		return nil, fmt.Errorf("load launcher catalog: %w", err)
	}
	entryByID := make(map[string]*catalogEntry, len(entries))
	folders := make(map[string][]string)
	for i := range entries {
		system := entries[i].definition.System
		if _, ok := folders[system]; !ok {
			folders[system] = systemFolders(system)
		}
		entries[i].folders = folders[system]
		entryByID[entries[i].definition.ID] = &entries[i]
	}
	if err := validateBuiltEntries(entryByID); err != nil {
		return nil, err
	}
	return &Platform{
		host:      host,
		settings:  settings,
		entries:   entries,
		entryByID: entryByID,
	}, nil
}

// systemFolders is every folder name a system's media may sit in, directly
// below a media folder: its EmulationStation folders, its ID and its aliases.
// Matching is case-insensitive, so names differing only in case are listed
// once.
func systemFolders(systemID string) []string {
	names := esde.GetFoldersForSystemID(systemID)
	names = append(names, systemID)
	if system, err := systemdefs.GetSystem(systemID); err == nil {
		names = append(names, system.Aliases...)
	}
	slices.Sort(names)
	folders := make([]string, 0, len(names))
	for _, name := range names {
		if !slices.ContainsFunc(folders, func(folder string) bool { return strings.EqualFold(folder, name) }) {
			folders = append(folders, name)
		}
	}
	return folders
}

// validateBuiltEntries checks the templates of the launchers Core builds
// rather than reads from the catalog, and that none shares an ID with a
// catalog launcher or the generic installed-apps launcher, as loadCatalog
// does for its own.
func validateBuiltEntries(catalog map[string]*catalogEntry) error {
	entries := make([]*catalogEntry, 0, 1+len(gameNativeEntries))
	entries = append(entries, &scummVMEntry)
	for i := range gameNativeEntries {
		entries = append(entries, &gameNativeEntries[i].catalogEntry)
	}
	for _, entry := range entries {
		id := entry.definition.ID
		if _, clash := catalog[id]; clash || id == installedAppsID {
			return fmt.Errorf("built launcher %s clashes with a registered launcher: %w", id, ErrLaunchDefinition)
		}
		if err := entry.definition.Validate(); err != nil {
			return fmt.Errorf("built launcher %s: %w", id, err)
		}
	}
	return nil
}

func (*Platform) ID() string { return platformids.Android }

func (*Platform) StartPre(*config.Instance) error { return nil }

func (p *Platform) StartPost(
	ctx context.Context,
	_ *config.Instance,
	launcherContexts platforms.LauncherContextManager,
	_ func() *models.ActiveMedia,
	_ func(*models.ActiveMedia),
	db *database.Database,
	_ *idle.Scheduler,
) error {
	p.mu.Lock()
	p.launcherContexts = launcherContexts
	p.db = db
	p.mu.Unlock()
	go func() {
		if err := p.ReconcileExternalSessions(ctx); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("Android startup session reconciliation deferred")
		}
	}()
	return nil
}

// Stop drops the launcher contexts StartPost supplied. A host reuses one
// Platform across starts, and the manager from the previous run holds a
// cancelled context: leaving it in place makes launcherContext's readiness
// check pass with a dead context instead of refusing the launch.
func (p *Platform) Stop() error {
	p.mu.Lock()
	p.launcherContexts = nil
	p.db = nil
	p.mu.Unlock()
	return nil
}

// SetMediaHistoryHooks receives the service's profile and history
// notification hooks. Android history for a LifecycleExternal launcher comes
// from session reconciliation, not the active-media tracker.
func (p *Platform) SetMediaHistoryHooks(hooks platforms.MediaHistoryHooks) {
	p.mu.Lock()
	p.historyHooks = hooks
	p.mu.Unlock()
}

func (p *Platform) mediaHistoryHooks() platforms.MediaHistoryHooks {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.historyHooks
}

func (p *Platform) database() *database.Database {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.db
}

func (p *Platform) Settings() platforms.Settings { return p.settings }

func (*Platform) ScanHook(*tokens.Token) error { return nil }

func (*Platform) SupportedReaders(*config.Instance) []readers.Reader { return nil }

// RootDirs is empty: media comes from the host's media folders, which Core
// indexes as source roots.
func (*Platform) RootDirs(*config.Instance) []string { return nil }

func (*Platform) StopActiveLauncher(platforms.StopIntent) error { return unsupported("stop launcher") }

func (*Platform) ReturnToMenu() error { return unsupported("return to menu") }

func (*Platform) SetTrackedProcess(*os.Process) {}

func (*Platform) LaunchSystem(*config.Instance, string) error { return unsupported("launch system") }

func (*Platform) KeyboardPress(string) error { return unsupported("keyboard input") }

func (*Platform) GamepadPress(string) error { return unsupported("gamepad input") }

func (*Platform) Screenshot() (*platforms.ScreenshotResult, error) {
	return nil, unsupported("screenshot")
}

func (*Platform) ForwardCmd(*platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, unsupported("platform command")
}

func (*Platform) LookupMapping(*tokens.Token) (string, bool) { return "", false }

func (*Platform) ConsoleManager() platforms.ConsoleManager { return platforms.NoOpConsoleManager{} }

// ManagedByPackageManager is true: the host's package is the only update path.
func (*Platform) ManagedByPackageManager() bool { return true }

// Scrapers offers the libretro thumbnail scraper (box art, screenshots and
// title screens for indexed media, matched by libretro's own sanitised name;
// it never runs automatically after indexing, since it is the only scraper
// that downloads) and, once a host is present, an app icon scraper for
// installed apps offered as media.
func (p *Platform) Scrapers(*config.Instance) map[string]platforms.Scraper {
	thumbnails := libretrothumbs.NewPlatformScraper()
	scrapers := map[string]platforms.Scraper{thumbnails.ID: thumbnails}
	if p.host != nil {
		apps := p.appScraper()
		scrapers[apps.ID] = apps
	}
	return scrapers
}

// SourceRoots lists the media folders the user granted to the host.
func (p *Platform) SourceRoots(ctx context.Context) ([]string, error) {
	if p.host == nil {
		return nil, nil
	}
	references, err := p.host.MediaFolders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list host media folders: %w", err)
	}
	folders := make(map[string]string, len(references))
	roots := make([]string, 0, len(references))
	for _, reference := range references {
		if reference == "" {
			continue
		}
		root := platforms.SourceRootPath(reference)
		id := strings.TrimPrefix(root, platforms.SourceScheme+"://")
		if _, duplicate := folders[id]; duplicate {
			continue
		}
		folders[id] = reference
		roots = append(roots, root)
	}
	p.mu.Lock()
	p.folders = folders
	p.mu.Unlock()
	return roots, nil
}

// ReadSourceDir lists a directory in one of the host's media folders.
func (p *Platform) ReadSourceDir(ctx context.Context, path string) ([]platforms.SourceEntry, error) {
	if p.host == nil {
		return nil, unsupported("read media without a host")
	}
	id, segments, err := platforms.SourceLocation(path)
	if err != nil {
		return nil, fmt.Errorf("read media directory: %w", err)
	}
	reference, err := p.folderReference(ctx, id)
	if err != nil {
		return nil, err
	}
	entries, err := p.host.ReadMediaDir(ctx, reference, segments)
	if err != nil {
		return nil, fmt.Errorf("read host media directory: %w", err)
	}
	return entries, nil
}

// readSourceFile returns up to limit+1 bytes of the file at path, so an
// oversized file is detectable. This is a launch-time capability only: unlike
// SourceRoots/ReadSourceDir, it is not part of platforms.SourceRootReader and
// indexing never calls it.
func (p *Platform) readSourceFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	if p.host == nil {
		return nil, unsupported("read media without a host")
	}
	id, segments, err := platforms.SourceLocation(path)
	if err != nil {
		return nil, fmt.Errorf("read media file: %w", err)
	}
	if len(segments) == 0 {
		return nil, errNotAFile
	}
	reference, err := p.folderReference(ctx, id)
	if err != nil {
		return nil, err
	}
	content, err := p.host.ReadFile(ctx, reference, segments, limit)
	if err != nil {
		return nil, fmt.Errorf("read host media file: %w", err)
	}
	return content, nil
}

// folderReference returns the host reference of the source root id. A root
// not seen since the last listing, as after a restart, lists the folders
// again before giving up.
func (p *Platform) folderReference(ctx context.Context, id string) (string, error) {
	p.mu.RLock()
	reference, ok := p.folders[id]
	p.mu.RUnlock()
	if ok {
		return reference, nil
	}
	if _, err := p.SourceRoots(ctx); err != nil {
		return "", err
	}
	p.mu.RLock()
	reference, ok = p.folders[id]
	p.mu.RUnlock()
	if !ok {
		return "", errFolderUnavailable
	}
	return reference, nil
}

func (p *Platform) launcherContext() context.Context {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.launcherContexts == nil {
		return nil
	}
	return p.launcherContexts.GetContext()
}

func unsupported(operation string) error {
	return fmt.Errorf("%s on Android: %w", operation, platforms.ErrNotSupported)
}
