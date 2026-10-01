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

package android

import (
	"os"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// GameNative runs PC games from several stores. Its exported MainActivity
// accepts the app.gamenative.LAUNCH_GAME action with an int app_id extra and a
// game_source extra naming the store. Its frontend export writes one file per
// installed game, named after the game with a per-store extension, holding
// the decimal app ID.
//
// An export file is ordinary source-backed media, matched by Folders and
// Extensions exactly like any other system's files: its identity is the
// plain source:// path. The app ID lives only in the file's content, so it is
// read once, at dispatch, and never becomes part of the identity or reaches
// the indexer.
const (
	gameNativePackage   = "app.gamenative"
	gameNativeActivity  = "app.gamenative.MainActivity"
	gameNativeAction    = "app.gamenative.LAUNCH_GAME"
	gameNativeGroup     = "GameNative"
	gameNativeSteamID   = "GameNative.Steam"
	gameNativeWindowsID = "GameNative.Windows"
	gameNativeAppID     = "app_id"
	gameNativeSourceKey = "game_source"
	// maxGameNativeExportBytes bounds the file read. An app ID is at most 10
	// digits; the rest allows for a line ending and surrounding space.
	maxGameNativeExportBytes = 64
	gameNativeRepair         = "Install GameNative and install this game in it."
)

// gameNativeSource is one GameNative store: its GameSource name, the export
// extension its frontend writes and the system its games index as.
type gameNativeSource struct {
	name      string
	extension string
}

// gameNativeEntry is one GameNative launcher: the system it serves and the
// stores whose export extensions match it. Steam games index as PC, the
// system ES-DE's steam folder already maps to; every other store runs
// Windows games, so they share one launcher and one system.
type gameNativeEntry struct {
	sources []gameNativeSource
	catalogEntry
}

// gameNativeEntries are the launch templates, one per system. The game is
// added per launch, from the file, so the templates carry no extras.
var gameNativeEntries = []gameNativeEntry{
	buildGameNativeEntry(gameNativeSteamID, systemdefs.SystemPC, gameNativeSource{name: "STEAM", extension: ".steam"}),
	buildGameNativeEntry(gameNativeWindowsID, systemdefs.SystemWindows,
		gameNativeSource{name: "EPIC", extension: ".epic"},
		gameNativeSource{name: "GOG", extension: ".gog"},
		gameNativeSource{name: "AMAZON", extension: ".amazon"},
		gameNativeSource{name: "CUSTOM_GAME", extension: ".pcgame"},
	),
}

func buildGameNativeEntry(id, system string, sources ...gameNativeSource) gameNativeEntry {
	extensions := make([]string, len(sources))
	for i, source := range sources {
		extensions[i] = source.extension
	}
	return gameNativeEntry{
		sources: sources,
		catalogEntry: catalogEntry{
			group:   gameNativeGroup,
			folders: systemFolders(system),
			definition: LaunchDefinition{
				Version: 3, ID: id, Name: gameNativeGroup, System: system,
				Package: gameNativePackage, Activity: gameNativeActivity, Action: gameNativeAction,
				Strategy: StrategyApp, StorageAccess: "none", Repair: gameNativeRepair,
			},
		},
	}
}

// sourceForExtension finds the store an export extension belongs to, compared
// without case since a host-reported file name's case is not guaranteed.
func (e *gameNativeEntry) sourceForExtension(extension string) (gameNativeSource, bool) {
	for _, source := range e.sources {
		if strings.EqualFold(source.extension, extension) {
			return source, true
		}
	}
	return gameNativeSource{}, false
}

// gameNativeLauncher offers GameNative for the games of one system.
func (p *Platform) gameNativeLauncher(entry *gameNativeEntry, snapshot *hostSnapshot) platforms.Launcher {
	target := snapshot.targetFailure(&entry.definition)
	var availability error
	if target != "" {
		availability = hostRepairError(target, &entry.catalogEntry)
	}
	extensions := make([]string, len(entry.sources))
	for i, source := range entry.sources {
		extensions[i] = source.extension
	}
	media := platforms.Launcher{
		ID:         entry.definition.ID,
		SystemID:   entry.definition.System,
		Folders:    entry.folders,
		Extensions: extensions,
	}
	launcher := media
	launcher.Groups = []string{entry.group}
	launcher.Lifecycle = platforms.LifecycleExternal
	launcher.Available = availability == nil
	launcher.Availability = func(*config.Instance) error { return availability }
	launcher.Detected = detected(&entry.catalogEntry, snapshot, target)
	launcher.Preflight = func(cfg *config.Instance, path string, options *platforms.LaunchOptions) error {
		return preflight(&entry.catalogEntry, helpers.PathIsLauncher(cfg, p, &media, path), options, false)
	}
	launcher.Launch = func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
		return nil, p.dispatchGameNative(entry, path)
	}
	if availability != nil {
		launcher.AvailabilityReason = availability.Error()
	}
	return launcher
}

// dispatchGameNative reads the app ID from the export file and starts
// GameNative on it. The store comes from the file's own extension; the app ID
// from its content. Nothing about either is part of the file's identity.
func (p *Platform) dispatchGameNative(entry *gameNativeEntry, path string) error {
	ctx := p.launcherContext()
	if p.host == nil || ctx == nil {
		return unsupported("launch media before the host is ready")
	}
	source, ok := entry.sourceForExtension(helpers.GetPathInfo(path).Extension)
	if !ok {
		return repairError(platforms.LaunchRepairLauncherUnsupportedMedia, entry.repairParams(), msgWrongMedia)
	}
	content, err := p.ReadSourceFile(ctx, path, maxGameNativeExportBytes)
	if err != nil {
		return sourceFailure(ctx, &entry.catalogEntry, err)
	}
	appID, ok := canonicalAppID(content)
	if !ok {
		return repairError(platforms.LaunchRepairLauncherUnsupportedMedia, entry.repairParams(), msgWrongMedia)
	}
	definition := entry.definition.copy()
	definition.Extras = []LaunchExtra{
		{Name: gameNativeAppID, Type: "int", Source: extraSourceLiteral, Value: appID},
		{Name: gameNativeSourceKey, Type: "string", Source: extraSourceLiteral, Value: source.name},
	}
	if validateErr := definition.Validate(); validateErr != nil {
		return repairError(platforms.LaunchRepairLauncherUnsupportedMedia, entry.repairParams(), msgWrongMedia)
	}
	return p.dispatchApp(&catalogEntry{definition: definition, group: entry.group}, path)
}

// canonicalAppID accepts the file's trimmed content as a positive decimal
// that fits a Java int, the range GameNative's launch intent reads, and
// returns it without leading zeros.
func canonicalAppID(content []byte) (string, bool) {
	if len(content) > maxGameNativeExportBytes {
		return "", false
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" || len(trimmed) > 10 {
		return "", false
	}
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	number, err := strconv.ParseInt(trimmed, 10, 32)
	if err != nil || number <= 0 {
		return "", false
	}
	return strconv.FormatInt(number, 10), true
}
