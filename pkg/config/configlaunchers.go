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

package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

type Launchers struct {
	IndexRoot        []string `toml:"index_root,omitempty,multiline"`
	AllowFile        []string `toml:"allow_file,omitempty,multiline"`
	allowFileRe      []*regexp.Regexp
	MediaDir         string             `toml:"media_dir,omitempty"`
	BeforeMediaStart string             `toml:"before_media_start,omitempty"`
	OnMediaStart     string             `toml:"on_media_start,omitempty"`
	Default          []LaunchersDefault `toml:"default,omitempty"`
	Custom           []LaunchersCustom  `toml:"custom,omitempty"`
}

type LaunchersDefault struct {
	Launcher   string `toml:"launcher"`
	InstallDir string `toml:"install_dir,omitempty"`
	ServerURL  string `toml:"server_url,omitempty"`
	// Action specifies the default launch action. Common values:
	// - "" or "run": Default behavior (launch/play the media)
	// - "details": Show media details/info page instead of launching
	Action string `toml:"action,omitempty"`
	// LoadPath specifies the implementation file the launcher should load.
	// Format is launcher-specific. For MiSTer, this is an MGL-form RBF path
	// like "_Unstable/SNES" (no extension, relative to /media/fat). Launchers
	// that do not load an implementation file ignore this field.
	LoadPath string `toml:"load_path,omitempty"`
}

const (
	CustomLauncherKindLauncher      = "launcher"
	CustomLauncherKindVirtualSystem = "virtual_system"
	CustomLauncherBackendCommand    = "command"
	CustomLauncherBackendMisterCore = "mister_core"
)

type LaunchersCustom struct {
	Controls   map[string]string `toml:"controls"`
	ID         string            `toml:"id"`
	Kind       string            `toml:"kind,omitempty"`
	Backend    string            `toml:"backend,omitempty"`
	System     string            `toml:"system,omitempty"`
	Name       string            `toml:"name,omitempty"`
	Category   string            `toml:"category,omitempty"`
	Execute    string            `toml:"execute,omitempty"`
	Lifecycle  string            `toml:"lifecycle,omitempty"`
	LoadPath   string            `toml:"load_path,omitempty"`
	MediaDirs  []string          `toml:"media_dirs,omitempty"`
	FileExts   []string          `toml:"file_exts,omitempty"`
	Groups     []string          `toml:"groups,omitempty"`
	Schemes    []string          `toml:"schemes,omitempty"`
	Restricted bool              `toml:"restricted,omitempty"`
}

func (c *Instance) DefaultMediaDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return pathutil.ResolveRelativePath(c.vals.Launchers.MediaDir)
}

func (c *Instance) LaunchersBeforeMediaStart() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Launchers.BeforeMediaStart
}

func (c *Instance) LaunchersOnMediaStart() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Launchers.OnMediaStart
}

func (c *Instance) IsLauncherFileAllowed(s string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return checkAllow(c.vals.Launchers.AllowFile, c.vals.Launchers.allowFileRe, s)
}

// LookupLauncherDefaults merges configuration defaults for a launcher by iterating
// through config entries in order. Entries match if their launcher field equals
// either the launcher ID or any of the launcher's groups (case-insensitive).
// Later matching entries override earlier ones, allowing hierarchical configuration
// like: set defaults for all "Kodi" launchers, then override for specific "KodiTV" group.
func (c *Instance) LookupLauncherDefaults(launcherID string, groups []string) LaunchersDefault {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result LaunchersDefault
	result.Launcher = launcherID

	log.Debug().
		Str("launcherID", launcherID).
		Strs("groups", groups).
		Int("defaultsCount", len(c.vals.Launchers.Default)).
		Msg("LookupLauncherDefaults: resolving launcher defaults")

	for _, entry := range c.vals.Launchers.Default {
		matches := false

		// Check if entry matches the exact launcher ID
		if strings.EqualFold(entry.Launcher, launcherID) {
			matches = true
		}

		// Check if entry matches any of the launcher's groups
		if !matches {
			for _, group := range groups {
				if strings.EqualFold(entry.Launcher, group) {
					matches = true
					break
				}
			}
		}

		if matches {
			log.Debug().
				Str("configLauncher", entry.Launcher).
				Str("launcherID", launcherID).
				Msg("LookupLauncherDefaults: merging matching entry")

			// Merge non-empty fields (later entries override earlier ones)
			if entry.InstallDir != "" {
				result.InstallDir = entry.InstallDir
			}
			if entry.ServerURL != "" {
				result.ServerURL = entry.ServerURL
			}
			if entry.Action != "" {
				result.Action = entry.Action
			}
			if entry.LoadPath != "" {
				result.LoadPath = entry.LoadPath
			}
		}
	}

	log.Debug().
		Str("launcherID", launcherID).
		Str("resolvedServerURL", result.ServerURL).
		Str("resolvedAction", result.Action).
		Str("resolvedInstallDir", result.InstallDir).
		Str("resolvedLoadPath", result.LoadPath).
		Msg("LookupLauncherDefaults: resolution complete")

	return result
}

func (c *Instance) LoadCustomLaunchers(launchersDir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	fs := c.getFs()

	_, err := fs.Stat(launchersDir)
	if err != nil {
		return fmt.Errorf("failed to stat launchers directory: %w", err)
	}

	var launcherFiles []string

	err = afero.Walk(
		fs,
		launchersDir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			if strings.ToLower(filepath.Ext(info.Name())) != ".toml" {
				return nil
			}

			launcherFiles = append(launcherFiles, path)

			return nil
		},
	)
	if err != nil {
		return fmt.Errorf("failed to walk launchers directory: %w", err)
	}

	sort.Strings(launcherFiles)

	filesCount := 0
	rawLaunchers := make([]LaunchersCustom, 0)
	for _, launcherPath := range launcherFiles {
		log.Debug().Msgf("loading custom launcher: %s", launcherPath)

		data, readErr := afero.ReadFile(fs, launcherPath)
		if readErr != nil {
			log.Error().Err(readErr).Str("file", launcherPath).Msg("error reading custom launcher")
			continue
		}

		var newVals Values
		decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
		if decodeErr := decoder.Decode(&newVals); decodeErr != nil {
			log.Error().Err(decodeErr).Str("file", launcherPath).Msg("error parsing custom launcher")
			continue
		}

		rawLaunchers = append(rawLaunchers, newVals.Launchers.Custom...)
		filesCount++
	}

	if len(launcherFiles) > 0 && filesCount == 0 {
		return errors.New("failed to parse any custom launcher files")
	}

	validated := validateCustomLaunchers(rawLaunchers, c.vals.Launchers.Custom, "external launcher files")
	c.customLaunchersExternal = cloneCustomLaunchers(validated)

	for i := range validated {
		cl := &validated[i]
		log.Info().
			Str("id", cl.ID).
			Str("kind", effectiveCustomLauncherKind(cl)).
			Str("backend", effectiveCustomLauncherBackend(cl)).
			Str("system", cl.System).
			Strs("mediaDirs", cl.MediaDirs).
			Strs("fileExts", cl.FileExts).
			Msg("registered custom launcher from TOML")
	}

	log.Info().Int("files", filesCount).Int("launchers", len(validated)).Msg("loaded custom launchers")

	return nil
}

func (c *Instance) CustomLaunchers() []LaunchersCustom {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries := make([]LaunchersCustom, 0, len(c.vals.Launchers.Custom)+len(c.customLaunchersExternal))
	entries = append(entries, c.vals.Launchers.Custom...)
	entries = append(entries, c.customLaunchersExternal...)
	return cloneCustomLaunchers(entries)
}

func (c *Instance) IndexRoots() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var roots []string
	if c.vals.Launchers.MediaDir != "" {
		roots = append(roots, pathutil.ResolveRelativePath(c.vals.Launchers.MediaDir))
	}
	for _, r := range c.vals.Launchers.IndexRoot {
		roots = append(roots, pathutil.ResolveRelativePath(r))
	}
	return roots
}
