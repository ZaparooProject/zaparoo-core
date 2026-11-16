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

package romm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/installer"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/rs/zerolog/log"
)

const (
	// platformCacheTTL is how long one platform listing serves the scans of
	// every system in an index run, instead of one request per system.
	platformCacheTTL = time.Minute
	// lookupTimeout bounds the synchronous ROM lookup a launch check makes.
	lookupTimeout = 5 * time.Second
)

// errNeedsInstall is returned if a romm:// path reaches the launcher itself.
// Launches download the ROM first and start the local file with the system's
// own launcher, so this only happens when that step was skipped; failing here
// keeps the running media from being stopped for nothing.
var errNeedsInstall = errors.New("romm games are downloaded before launch")

type platformListing struct {
	at   time.Time
	err  error
	list []Platform
}

type platformCache struct {
	byBase map[string]platformListing
	mu     syncutil.Mutex
}

var cachedPlatforms platformCache

// now is the clock the platform cache reads; tests replace it.
var now = time.Now

// platformsFor lists the server's platforms, reusing a recent listing or a
// recent failure. An unreachable server would otherwise be retried, and
// waited on, once for every system in an index run.
func platformsFor(ctx context.Context, c *Client) ([]Platform, error) {
	cachedPlatforms.mu.Lock()
	defer cachedPlatforms.mu.Unlock()
	base := c.BaseURL()
	t := now()
	if entry, ok := cachedPlatforms.byBase[base]; ok && t.Sub(entry.at) < platformCacheTTL {
		return entry.list, entry.err
	}
	list, err := c.Platforms(ctx)
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	if cachedPlatforms.byBase == nil {
		cachedPlatforms.byBase = make(map[string]platformListing)
	}
	for k, entry := range cachedPlatforms.byBase {
		if t.Sub(entry.at) >= platformCacheTTL {
			delete(cachedPlatforms.byBase, k)
		}
	}
	cachedPlatforms.byBase[base] = platformListing{at: t, list: list, err: err}
	return list, err
}

// NewLauncher builds the RomM launcher. It has no system of its own: its
// scanner runs for each system and lists the server's ROMs for it. Launching
// one downloads it into the system's game folder, where the system's own
// launcher starts it.
func NewLauncher(pl platforms.Platform) platforms.Launcher {
	return platforms.Launcher{
		ID:      LauncherID,
		Schemes: []string{shared.SchemeRomM},
		Test: func(_ *config.Instance, p string) bool {
			_, err := ParseRomPath(p)
			return err == nil
		},
		Availability: func(cfg *config.Instance) error {
			if ServerURL(cfg) == "" {
				return ErrNotConfigured
			}
			return nil
		},
		VirtualPathExists: func(cfg *config.Instance, p string) bool {
			ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
			defer cancel()
			return romExists(ctx, cfg, p)
		},
		Preflight: func(*config.Instance, string, *platforms.LaunchOptions) error {
			return errNeedsInstall
		},
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			return nil, errNeedsInstall
		},
		Scanner: func(
			ctx context.Context, cfg *config.Instance, systemID string, results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			return Scan(ctx, cfg, pl, systemID, results)
		},
	}
}

func romExists(ctx context.Context, cfg *config.Instance, p string) bool {
	id, err := ParseRomPath(p)
	if err != nil {
		return false
	}
	c, err := ClientFromConfig(cfg)
	if err != nil {
		return false
	}
	rom, err := c.Rom(ctx, id)
	if err != nil {
		log.Debug().Err(err).Int("rom", id).Msg("romm rom lookup failed")
		return false
	}
	return rom.Downloadable()
}

// LaunchExtensions are the lowercase extensions the platform's file launchers
// for a system open. RomM ROMs a system cannot open are not listed for it,
// and an extracted multi-file game is started from a file of one of these.
func LaunchExtensions(cfg *config.Instance, pl platforms.Platform, systemID string) []string {
	var exts []string
	launchers := pl.Launchers(cfg)
	for i := range launchers {
		l := &launchers[i]
		if l.SystemID != systemID || l.ID == LauncherID || len(l.Schemes) > 0 {
			continue
		}
		for _, ext := range l.Extensions {
			ext = strings.ToLower(strings.TrimSpace(ext))
			if ext == "" {
				continue
			}
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			if !slices.Contains(exts, ext) {
				exts = append(exts, ext)
			}
		}
	}
	return exts
}

// Scan returns results with the server's ROMs for systemID added. ROMs the
// device already has a file of, by name, are left out, as are ROMs none of
// the system's launchers could open once downloaded.
func Scan(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	systemID string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	c, err := ClientFromConfig(cfg)
	if errors.Is(err, ErrNotConfigured) {
		return results, nil
	}
	if err != nil {
		return results, fmt.Errorf("%w: %w", platforms.ErrScannerSourceUnreachable, err)
	}

	exts := LaunchExtensions(cfg, pl, systemID)
	if len(exts) == 0 {
		return results, nil
	}

	roms, err := SystemRoms(ctx, c, systemID)
	if err != nil {
		return results, sourceError(err)
	}

	local := indexedNames(results)
	added := 0
	for i := range roms {
		entry, ok := scanEntry(&roms[i], exts, local)
		if !ok {
			continue
		}
		results = append(results, entry)
		added++
	}
	log.Debug().Str("system", systemID).Int("roms", added).Msg("romm scan complete")
	return results, nil
}

// sourceError marks a failure to reach or use the server as the source being
// unavailable, which indexing logs and moves past rather than reporting as a
// fault in Core.
func sourceError(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	if IsUnreachable(err) || errors.Is(err, ErrUnauthorized) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", platforms.ErrScannerSourceUnreachable, err)
	}
	return err
}

// indexedNames are the lowercase base names of the files already indexed for
// the system.
func indexedNames(results []platforms.ScanResult) map[string]struct{} {
	names := make(map[string]struct{}, len(results))
	for i := range results {
		p := results[i].Path
		if strings.Contains(p, "://") {
			continue
		}
		// Both separators: the index holds paths in the host's form.
		if j := strings.LastIndexAny(p, `/\`); j >= 0 {
			p = p[j+1:]
		}
		if p != "" {
			names[strings.ToLower(p)] = struct{}{}
		}
	}
	return names
}

func scanEntry(rom *Rom, exts []string, local map[string]struct{}) (platforms.ScanResult, bool) {
	if !rom.Downloadable() {
		return platforms.ScanResult{}, false
	}
	layout, err := LayoutFor(rom)
	if err != nil {
		log.Debug().Err(err).Msg("skipping romm rom")
		return platforms.ScanResult{}, false
	}
	if !slices.ContainsFunc(extensions(rom, layout), func(e string) bool { return slices.Contains(exts, e) }) {
		return platforms.ScanResult{}, false
	}
	for _, name := range LocalNames(rom, layout) {
		if _, ok := local[name]; ok {
			return platforms.ScanResult{}, false
		}
	}
	title := Title(layout)
	return platforms.ScanResult{
		Path:  RomPath(rom.ID, title),
		Name:  title,
		NoExt: true,
	}, true
}

// Install downloads the ROM a romm:// path names into its system's game
// folder, unless it is there already, and returns the local file to launch
// and the system it belongs to.
func Install(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	ui *uievents.Service,
	p string,
) (localPath, systemID string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	id, err := ParseRomPath(p)
	if err != nil {
		return "", "", err
	}
	c, err := ClientFromConfig(cfg)
	if err != nil {
		return "", "", err
	}
	rom, err := c.Rom(ctx, id)
	if err != nil {
		return "", "", err
	}
	if !rom.Downloadable() {
		return "", "", fmt.Errorf("romm rom %d has no files on the server", id)
	}
	systemID, ok := SystemForPlatform(rom.PlatformSlug, rom.PlatformFSSlug)
	if !ok {
		return "", "", fmt.Errorf("romm platform %q has no matching system", rom.PlatformSlug)
	}
	layout, err := LayoutFor(rom)
	if err != nil {
		return "", "", err
	}
	display := rom.Name
	if display == "" {
		display = Title(layout)
	}
	localPath, err = installer.InstallRemoteMedia(ctx, cfg, pl, ui, &installer.RemoteMedia{
		URL:         c.ContentURL(id, layout.Name),
		SystemID:    systemID,
		FileName:    layout.Name,
		DisplayName: display,
		LaunchExts:  LaunchExtensions(cfg, pl, systemID),
		Size:        rom.FSSizeBytes,
		Archive:     layout.Archive,
	}, installer.DownloadHTTPFile)
	if err != nil {
		return "", "", fmt.Errorf("download romm rom %d: %w", id, err)
	}
	return localPath, systemID, nil
}
