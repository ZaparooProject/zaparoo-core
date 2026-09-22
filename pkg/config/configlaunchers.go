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
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

type Launchers struct {
	IndexRoot        []string `toml:"index_root,omitempty,multiline"`
	Preference       []string `toml:"preference,omitempty,multiline"`
	AllowFile        []string `toml:"allow_file,omitempty,multiline"`
	allowFileRe      []*regexp.Regexp
	MediaDir         string `toml:"media_dir,omitempty"`
	BeforeMediaStart string `toml:"before_media_start,omitempty"`
	OnMediaStart     string `toml:"on_media_start,omitempty"`
	// BeforeExit is the fallback before_exit script for outgoing media that no
	// [[systems.default]] or [[launchers.default]] entry claims. Declared before
	// Default because go-toml marshals in field order and a scalar written after
	// an array of tables reads back as a key of the last table.
	BeforeExit string             `toml:"before_exit,omitempty"`
	Default    []LaunchersDefault `toml:"default,omitempty"`
	Custom     []LaunchersCustom  `toml:"custom,omitempty"`
}

type LaunchersDefault struct {
	RenderScale *int `toml:"render_scale,omitempty"`
	// ScanDuplicates makes a launcher index the media it normally skips as a
	// duplicate of media it already indexes: directories it excludes as alias
	// trees, and symlinks resolving back inside its own folders. Excludes for
	// files that are not media at all, such as MiSTer's boot.rom, still apply.
	// A pointer so an entry that omits the key leaves an earlier entry alone
	// and an explicit false can override a group-wide true.
	ScanDuplicates   *bool  `toml:"scan_duplicates,omitempty"`
	Launcher         string `toml:"launcher"`
	InstallDir       string `toml:"install_dir,omitempty"`
	ServerURL        string `toml:"server_url,omitempty"`
	RenderResolution string `toml:"render_resolution,omitempty"`
	// Action specifies the default launch action. Common values:
	// - "" or "run": Default behavior (launch/play the media)
	// - "details": Show media details/info page instead of launching
	Action string `toml:"action,omitempty"`
	// LoadPath specifies the implementation file the launcher should load.
	// Format is launcher-specific. For MiSTer, this is an MGL-form RBF path
	// like "_Unstable/SNES" (no extension, relative to /media/fat). Launchers
	// that do not load an implementation file ignore this field.
	LoadPath string `toml:"load_path,omitempty"`
	// BeforeExit is a ZapScript run just before media started by a matching
	// launcher stops or is replaced.
	BeforeExit string `toml:"before_exit,omitempty"`
}

// ScanDuplicatesEnabled reports the resolved scan_duplicates setting, treating
// an unset key as off.
func (d *LaunchersDefault) ScanDuplicatesEnabled() bool {
	return d.ScanDuplicates != nil && *d.ScanDuplicates
}

const (
	CustomLauncherKindLauncher      = "launcher"
	CustomLauncherKindVirtualSystem = "virtual_system"
	// CustomLauncherBackendCommand and CustomLauncherBackendMisterCore alias
	// the shared backend vocabulary in pkg/api/models so config and the API
	// response never drift apart.
	CustomLauncherBackendCommand    = models.LauncherBackendCommand
	CustomLauncherBackendMisterCore = models.LauncherBackendMisterCore
)

type LaunchersCustom struct {
	Controls   map[string]string `toml:"controls"`
	ID         string            `toml:"id"`
	Kind       string            `toml:"kind,omitempty"`
	Backend    string            `toml:"backend,omitempty"`
	System     string            `toml:"system,omitempty"`
	Name       string            `toml:"name,omitempty"`
	Category   string            `toml:"category,omitempty"`
	Categories []string          `toml:"categories,omitempty"`
	Execute    string            `toml:"execute,omitempty"`
	Lifecycle  string            `toml:"lifecycle,omitempty"`
	LoadPath   string            `toml:"load_path,omitempty"`
	MediaDirs  []string          `toml:"media_dirs,omitempty"`
	FileExts   []string          `toml:"file_exts,omitempty"`
	Groups     []string          `toml:"groups,omitempty"`
	Schemes    []string          `toml:"schemes,omitempty"`
	Restricted bool              `toml:"restricted,omitempty"`
}

func (c *Instance) LauncherPreference() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.vals.Launchers.Preference...)
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

func (c *Instance) LaunchersBeforeExit() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Launchers.BeforeExit
}

func (c *Instance) IsLauncherFileAllowed(s string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return checkAllow(c.vals.Launchers.AllowFile, c.vals.Launchers.allowFileRe, s)
}

// LookupLauncherDefaults merges configuration defaults for a launcher. An entry
// matches when its launcher field equals the launcher's own ID or one of the
// groups it belongs to, compared case-insensitively.
//
// Group entries merge first, then exact-ID entries, so a launcher's own entry
// always beats an entry for a family it belongs to no matter where the two sit
// in the file. Within each pass later entries override earlier ones, which is
// the only tie-break available between two groups: Groups is a flat list whose
// order means different things per platform, so nothing declares that "Kodi" is
// broader than "KodiTV".
func (c *Instance) LookupLauncherDefaults(launcherID string, groups []string) LaunchersDefault {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := LaunchersDefault{Launcher: launcherID}

	log.Debug().
		Str("launcherID", launcherID).
		Strs("groups", groups).
		Int("defaultsCount", len(c.vals.Launchers.Default)).
		Msg("LookupLauncherDefaults: resolving launcher defaults")

	mergeMatching := func(matchedOn string, matches func(entryLauncher string) bool) {
		// Indexed rather than ranged by value: LaunchersDefault is large enough
		// that copying one per iteration trips gocritic's rangeValCopy.
		for i := range c.vals.Launchers.Default {
			entry := &c.vals.Launchers.Default[i]
			// An entry with no launcher field names nothing. Custom launchers
			// take their groups straight from user TOML, which does not reject a
			// blank one, so without this such an entry becomes a wildcard for
			// every launcher carrying it.
			if entry.Launcher == "" || !matches(entry.Launcher) {
				continue
			}
			log.Debug().
				Str("configLauncher", entry.Launcher).
				Str("launcherID", launcherID).
				Str("matchedOn", matchedOn).
				Msg("LookupLauncherDefaults: merging matching entry")
			mergeLauncherDefault(&result, entry)
		}
	}
	mergeMatching("group", func(entryLauncher string) bool {
		return matchesAnyLauncherGroup(entryLauncher, groups)
	})
	mergeMatching("launcher", func(entryLauncher string) bool {
		return strings.EqualFold(entryLauncher, launcherID)
	})

	log.Debug().
		Str("launcherID", launcherID).
		Str("resolvedServerURL", result.ServerURL).
		Str("resolvedAction", result.Action).
		Str("resolvedInstallDir", result.InstallDir).
		Str("resolvedLoadPath", result.LoadPath).
		Bool("resolvedBeforeExit", result.BeforeExit != "").
		Bool("resolvedScanDuplicates", result.ScanDuplicatesEnabled()).
		Msg("LookupLauncherDefaults: resolution complete")

	return result
}

// matchesAnyLauncherGroup reports whether a config entry's launcher field names
// one of the groups a launcher belongs to. Callers pass only a named entry, so a
// blank group in the list matches nothing.
func matchesAnyLauncherGroup(entryLauncher string, groups []string) bool {
	for _, group := range groups {
		if strings.EqualFold(entryLauncher, group) {
			return true
		}
	}
	return false
}

// mergeLauncherDefault copies entry's set fields over dst. An empty string never
// clears an already-resolved value, so a narrower entry that omits a field
// inherits it rather than blanking it. render_scale and render_resolution are
// mutually exclusive and each clears the other.
//
// An entry whose launcher field happens to name both the launcher ID and one of
// its groups merges twice; that is harmless because every field is set-if-set.
func mergeLauncherDefault(dst, entry *LaunchersDefault) {
	if entry.InstallDir != "" {
		dst.InstallDir = entry.InstallDir
	}
	if entry.ServerURL != "" {
		dst.ServerURL = entry.ServerURL
	}
	if entry.Action != "" {
		dst.Action = entry.Action
	}
	if entry.LoadPath != "" {
		dst.LoadPath = entry.LoadPath
	}
	if entry.BeforeExit != "" {
		dst.BeforeExit = entry.BeforeExit
	}
	if entry.RenderScale != nil {
		renderScale := *entry.RenderScale
		dst.RenderScale = &renderScale
		dst.RenderResolution = ""
	}
	if entry.RenderResolution != "" {
		dst.RenderScale = nil
		dst.RenderResolution = entry.RenderResolution
	}
	if entry.ScanDuplicates != nil {
		scanDuplicates := *entry.ScanDuplicates
		dst.ScanDuplicates = &scanDuplicates
	}
}

// ValidateRenderResolution validates and parses a positive WIDTHxHEIGHT render target.
func ValidateRenderResolution(value string) (width, height int, err error) {
	widthText, heightText, ok := strings.Cut(strings.ToLower(value), "x")
	if !ok || widthText == "" || heightText == "" || strings.Contains(heightText, "x") {
		return 0, 0, fmt.Errorf("render resolution must use WIDTHxHEIGHT, got %q", value)
	}
	width, widthErr := strconv.Atoi(widthText)
	height, heightErr := strconv.Atoi(heightText)
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("render resolution must use positive dimensions, got %q", value)
	}
	return width, height, nil
}

func validateLauncherDefaults(defaults []LaunchersDefault) error {
	for i := range defaults {
		entry := &defaults[i]
		if entry.RenderScale != nil && entry.RenderResolution != "" {
			return fmt.Errorf("launcher default %d cannot set both render_scale and render_resolution", i)
		}
		if entry.RenderScale != nil && *entry.RenderScale <= 0 {
			return fmt.Errorf("launcher default %d render_scale must be positive", i)
		}
		if entry.RenderResolution != "" {
			if _, _, err := ValidateRenderResolution(entry.RenderResolution); err != nil {
				return fmt.Errorf("launcher default %d: %w", i, err)
			}
		}
	}
	return nil
}

// ErrCustomLauncherUnknownFields means every candidate file was rejected for
// unknown TOML fields. I/O and other decoding failures must not match it.
var ErrCustomLauncherUnknownFields = errors.New("failed to parse any custom launcher files")

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
				// One unreadable entry must not abort the walk and drop
				// every other launcher file.
				log.Warn().Err(err).Str("path", path).Msg("skipping unreadable path in launchers directory")
				if info != nil && info.IsDir() {
					return filepath.SkipDir
				}
				return nil
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
	unknownFieldFiles := 0
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
			var unknown *toml.StrictMissingError
			if errors.As(decodeErr, &unknown) {
				unknownFieldFiles++
				// Key/position only: String() includes source text and private values.
				fields := make([]string, 0, min(len(unknown.Errors), 16))
				for _, field := range unknown.Errors[:min(len(unknown.Errors), 16)] {
					row, column := field.Position()
					fields = append(fields, fmt.Sprintf("%s (line %d, column %d)",
						strings.Join(field.Key(), "."), row, column))
				}
				log.Warn().Str("file", launcherPath).Strs("unknownFields", fields).
					Int("unknownFieldCount", len(unknown.Errors)).
					Msg("custom launcher file skipped: unknown configuration fields")
			} else {
				log.Error().Err(decodeErr).Str("file", launcherPath).Msg("error parsing custom launcher")
			}
			continue
		}

		if len(newVals.Systems.Category) > 0 {
			log.Warn().Str("file", launcherPath).
				Msg("custom launcher file skipped: systems categories must be declared in config.toml")
			continue
		}

		rawLaunchers = append(rawLaunchers, newVals.Launchers.Custom...)
		filesCount++
	}

	if len(launcherFiles) > 0 && filesCount == 0 {
		if unknownFieldFiles == len(launcherFiles) {
			return ErrCustomLauncherUnknownFields
		}
		return errors.New("failed to parse any custom launcher files")
	}

	validated := validateCustomLaunchers(
		rawLaunchers,
		c.loaded.customLaunchersInline,
		"external launcher files",
		c.loaded.categoryResolver,
	)
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

	entries := make([]LaunchersCustom, 0, len(c.loaded.customLaunchersInline)+len(c.customLaunchersExternal))
	entries = append(entries, c.loaded.customLaunchersInline...)
	// A config reload can add an inline launcher with an ID that a launcher
	// file already uses. The inline entry wins, as it does when files load.
	inlineIDs := make(map[string]struct{}, len(c.loaded.customLaunchersInline))
	for i := range c.loaded.customLaunchersInline {
		inlineIDs[strings.ToLower(c.loaded.customLaunchersInline[i].ID)] = struct{}{}
	}
	for i := range c.customLaunchersExternal {
		if _, exists := inlineIDs[strings.ToLower(c.customLaunchersExternal[i].ID)]; !exists {
			entries = append(entries, c.customLaunchersExternal[i])
		}
	}
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
