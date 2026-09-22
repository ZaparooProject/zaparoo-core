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
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/hostmedia"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/spf13/afero"
)

const sourceRegistryFile = "host-sources.json"

// Platform implements platforms.Platform for Android. Token readers, input
// and screenshots have no host capability yet and report ErrNotSupported.
type Platform struct {
	host             Host
	launcherContexts platforms.LauncherContextManager
	registry         *hostmedia.Registry
	entryByID        map[string]*catalogEntry
	settings         platforms.Settings
	entries          []catalogEntry
	mu               syncutil.RWMutex
}

var (
	_ platforms.Platform          = (*Platform)(nil)
	_ platforms.HostMediaProvider = (*Platform)(nil)
)

// New builds the platform over the directories the host owns. A nil host
// yields a platform with no launchers and no media.
func New(settings platforms.Settings, host Host) (*Platform, error) {
	return newPlatform(settings, host, afero.NewOsFs())
}

func newPlatform(settings platforms.Settings, host Host, fs afero.Fs) (*Platform, error) {
	entries, err := loadCatalog()
	if err != nil {
		return nil, fmt.Errorf("load launcher catalog: %w", err)
	}
	entryByID := make(map[string]*catalogEntry, len(entries))
	for i := range entries {
		entryByID[entries[i].definition.ID] = &entries[i]
	}
	return &Platform{
		host:      host,
		settings:  settings,
		entries:   entries,
		entryByID: entryByID,
		registry:  hostmedia.NewRegistry(fs, filepath.Join(settings.DataDir, sourceRegistryFile)),
	}, nil
}

func (*Platform) ID() string { return platformids.Android }

func (*Platform) StartPre(*config.Instance) error { return nil }

func (p *Platform) StartPost(
	_ context.Context,
	_ *config.Instance,
	launcherContexts platforms.LauncherContextManager,
	_ func() *models.ActiveMedia,
	_ func(*models.ActiveMedia),
	_ *database.Database,
	_ *idle.Scheduler,
) error {
	p.mu.Lock()
	p.launcherContexts = launcherContexts
	p.mu.Unlock()
	return nil
}

// Stop drops the launcher contexts StartPost supplied. A host reuses one
// Platform across starts, and the manager from the previous run holds a
// cancelled context: leaving it in place makes launcherContext's readiness
// check pass with a dead context instead of refusing the launch.
func (p *Platform) Stop() error {
	p.mu.Lock()
	p.launcherContexts = nil
	p.mu.Unlock()
	return nil
}

func (p *Platform) Settings() platforms.Settings { return p.settings }

func (*Platform) ScanHook(*tokens.Token) error { return nil }

func (*Platform) SupportedReaders(*config.Instance) []readers.Reader { return nil }

// RootDirs is empty: media comes from host document sources, not filesystem roots.
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

func (*Platform) Scrapers(*config.Instance) map[string]platforms.Scraper { return nil }

// OpenMediaScan indexes the document sources the user granted to the host.
func (p *Platform) OpenMediaScan(ctx context.Context) (platforms.HostMediaScan, error) {
	if p.host == nil {
		return nil, unsupported("media scan without a host")
	}
	session, err := p.host.OpenDocuments(ctx)
	if err != nil {
		return nil, fmt.Errorf("open host documents: %w", err)
	}
	scan, err := hostmedia.NewScan(ctx, session, p.registry)
	if err != nil {
		return nil, fmt.Errorf("start host media scan: %w", err)
	}
	return scan, nil
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
