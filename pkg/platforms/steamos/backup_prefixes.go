//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/vdfbinary"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	sharedsteam "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/andygrunwald/vdf"
)

const (
	steamShortcutsMaxSize = int64(16 << 20)
	steamLibraryMaxSize   = int64(4 << 20)
	faugusLibraryMaxSize  = int64(16 << 20)
)

type steamLibrary struct {
	id        string
	steamApps string
}

type faugusBackupGame struct {
	GameID string `json:"gameid"`
	Prefix string `json:"prefix"`
}

func broadNonSteamPoliciesAt(home, configuredSteamRoot string) []platforms.BackupDefinition {
	steamRoot := selectedSteamInstallRoot(home, configuredSteamRoot)
	steamAppsRoot := sharedsteam.FindSteamAppsDir(steamRoot)
	userdataRoot := filepath.Join(steamRoot, "userdata")
	compatdataRoot := filepath.Join(steamAppsRoot, "compatdata")
	definitions := make([]platforms.BackupDefinition, 0, 9)
	definitions = append(definitions,
		definition(
			userdataRoot, filepath.Join("Steam", "userdata"), backupCategorySettings,
			[]platforms.BackupPattern{{Glob: filepath.Join("*", "config", "shortcuts.vdf")}}, nil,
		),
		definition(
			compatdataRoot, filepath.Join("Steam", "nonsteam"), backupCategorySettings,
			[]platforms.BackupPattern{{Glob: filepath.Join("[234]?????????", "pfx", "*.reg")}}, nil,
		),
		definition(
			compatdataRoot, filepath.Join("Steam", "nonsteam"), backupCategorySaves,
			steamPrefixSavePatterns(), steamPrefixSaveExclusions(),
		),
	)

	for _, root := range bottleRoots(home) {
		definitions = append(definitions, broadBottlePolicies(root)...)
	}
	fallbackBottleRoot := preferredBottleRoot(home)
	fallbackBottleRoot.restore = filepath.Join("Bottles", "custom")
	definitions = append(definitions, broadBottlePolicies(fallbackBottleRoot)...)
	definitions = append(definitions,
		definition(
			filepath.Join(home, "Faugus"), "Faugus", backupCategorySettings,
			[]platforms.BackupPattern{
				{Glob: filepath.Join("*", "*.reg")},
				{Glob: filepath.Join("*", "pfx", "*.reg")},
			}, nil,
		),
		definition(
			filepath.Join(home, "Faugus"), "Faugus", backupCategorySaves,
			append(
				prefixWindowsPatterns(filepath.Join("*", "drive_c", "users"), windowsUserSavePatterns),
				prefixWindowsPatterns(filepath.Join("*", "pfx", "drive_c", "users"), windowsUserSavePatterns)...,
			),
			append(
				prefixWindowsPatterns(filepath.Join("*", "drive_c", "users"), windowsSaveExclusions),
				prefixWindowsPatterns(filepath.Join("*", "pfx", "drive_c", "users"), windowsSaveExclusions)...,
			),
		),
	)
	return definitions
}

func discoverNonSteamDefinitions(home string) ([]platforms.BackupDefinition, []platforms.BackupWarning) {
	return discoverNonSteamDefinitionsAt(home, "")
}

func discoverNonSteamDefinitionsAt(
	home, configuredSteamRoot string,
) ([]platforms.BackupDefinition, []platforms.BackupWarning) {
	definitions, warnings := discoverSteamShortcutDefinitionsAt(home, configuredSteamRoot)
	bottles, bottleWarnings := discoverBottlesDefinitions(home)
	definitions = append(definitions, bottles...)
	warnings = append(warnings, bottleWarnings...)
	faugus, faugusWarnings := discoverFaugusDefinitions(home)
	definitions = append(definitions, faugus...)
	warnings = append(warnings, faugusWarnings...)
	return definitions, warnings
}

func discoverSteamShortcutDefinitions(home string) ([]platforms.BackupDefinition, []platforms.BackupWarning) {
	return discoverSteamShortcutDefinitionsAt(home, "")
}

func discoverSteamShortcutDefinitionsAt(
	home, configuredSteamRoot string,
) ([]platforms.BackupDefinition, []platforms.BackupWarning) {
	steamRoot := selectedSteamInstallRoot(home, configuredSteamRoot)
	userdataRoot := filepath.Join(steamRoot, "userdata")
	entries, err := os.ReadDir(userdataRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []platforms.BackupWarning{{
			Category: backupCategorySettings,
			Path:     filepath.ToSlash(filepath.Join("Steam", "userdata")),
			Reason:   "Steam shortcuts are unreadable",
		}}
	}

	definitions := make([]platforms.BackupDefinition, 0)
	warnings := make([]platforms.BackupWarning, 0)
	appIDs := make(map[uint32]struct{})
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, parseErr := strconv.ParseUint(entry.Name(), 10, 64); parseErr != nil {
			continue
		}
		configRoot := filepath.Join(userdataRoot, entry.Name(), "config")
		shortcutsPath := filepath.Join(configRoot, "shortcuts.vdf")
		info, statErr := os.Stat(shortcutsPath)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		logicalRoot := filepath.Join("Steam", "userdata", entry.Name(), "config")
		if statErr != nil || !info.Mode().IsRegular() {
			warnings = append(warnings, platforms.BackupWarning{
				Category: backupCategorySettings,
				Path:     filepath.ToSlash(filepath.Join(logicalRoot, "shortcuts.vdf")),
				Reason:   "Steam shortcuts are unreadable",
			})
			continue
		}
		definitions = append(definitions, nonRecursiveDefinition(
			configRoot, logicalRoot, backupCategorySettings,
			[]platforms.BackupPattern{{Glob: "shortcuts.vdf"}},
		))

		shortcuts, readErr := readSteamShortcuts(shortcutsPath)
		if readErr != nil {
			warnings = append(warnings, platforms.BackupWarning{
				Category: backupCategorySettings,
				Path:     filepath.ToSlash(filepath.Join(logicalRoot, "shortcuts.vdf")),
				Reason:   "Steam shortcuts could not be parsed; raw metadata was preserved",
			})
			continue
		}
		for _, shortcut := range shortcuts {
			appIDs[shortcut.AppID] = struct{}{}
		}
	}

	libraries, libraryWarnings := discoverSteamLibraries(steamRoot)
	warnings = append(warnings, libraryWarnings...)
	compatdataIDs, compatdataWarnings := discoverNonSteamCompatdataIDs(libraries)
	warnings = append(warnings, compatdataWarnings...)
	for appID := range compatdataIDs {
		appIDs[appID] = struct{}{}
	}
	for appID := range appIDs {
		id := strconv.FormatUint(uint64(appID), 10)
		for _, library := range libraries {
			prefixSource := filepath.Join(library.steamApps, "compatdata", id, "pfx")
			usersPath := filepath.Join(prefixSource, "drive_c", "users")
			if info, statErr := os.Stat(usersPath); statErr != nil || !info.IsDir() {
				continue
			}
			logicalRoot := filepath.Join("Steam", "nonsteam", id, "pfx")
			definitions = append(definitions, windowsPrefixDefinitions(prefixSource, logicalRoot)...)
			break
		}
	}
	return definitions, warnings
}

func discoverNonSteamCompatdataIDs(
	libraries []steamLibrary,
) (map[uint32]struct{}, []platforms.BackupWarning) {
	result := make(map[uint32]struct{})
	warnings := make([]platforms.BackupWarning, 0)
	for _, library := range libraries {
		compatdataRoot := filepath.Join(library.steamApps, "compatdata")
		entries, err := os.ReadDir(compatdataRoot)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			warnings = append(warnings, platforms.BackupWarning{
				Category: backupCategorySaves,
				Path:     filepath.ToSlash(filepath.Join("Steam", "nonsteam")),
				Reason:   "Steam compatibility data is unreadable",
			})
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			id, parseErr := strconv.ParseUint(entry.Name(), 10, 32)
			if parseErr == nil && id >= 1<<31 {
				result[uint32(id)] = struct{}{}
			}
		}
	}
	return result, warnings
}

func selectedSteamInstallRoot(home, configured string) string {
	if safeExternalRoot(configured, home) {
		if info, err := os.Stat(configured); err == nil && info.IsDir() {
			return filepath.Clean(configured)
		}
	}
	return steamInstallRoot(home)
}

func steamInstallRoot(home string) string {
	candidates := []string{
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".steam", "steam"),
		filepath.Join(home, "snap", "steam", "common", ".local", "share", "Steam"),
		filepath.Join(home, "snap", "steam", "common", ".steam", "steam"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return candidates[0]
}

func discoverSteamLibraries(steamRoot string) ([]steamLibrary, []platforms.BackupWarning) {
	mainSteamApps := sharedsteam.FindSteamAppsDir(steamRoot)
	libraries := []steamLibrary{{id: "0", steamApps: mainSteamApps}}
	path := filepath.Join(mainSteamApps, "libraryfolders.vdf")
	data, err := readLimitedBackupFile(path, steamLibraryMaxSize)
	if errors.Is(err, os.ErrNotExist) {
		return libraries, nil
	}
	if err != nil {
		return libraries, []platforms.BackupWarning{{
			Category: backupCategorySettings,
			Path:     filepath.ToSlash(filepath.Join("Steam", "libraryfolders.vdf")),
			Reason:   "Steam library configuration is unreadable",
		}}
	}
	parsed, err := vdf.NewParser(bytes.NewReader(data)).Parse()
	if err != nil {
		return libraries, []platforms.BackupWarning{{
			Category: backupCategorySettings,
			Path:     filepath.ToSlash(filepath.Join("Steam", "libraryfolders.vdf")),
			Reason:   "Steam library configuration could not be parsed",
		}}
	}
	folders, ok := parsed["libraryfolders"].(map[string]any)
	if !ok {
		return libraries, nil
	}
	for id, raw := range folders {
		folder, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		libraryPath, ok := folder["path"].(string)
		if !ok || !safeExternalRoot(libraryPath, steamRoot) {
			continue
		}
		libraries = append(libraries, steamLibrary{
			id: id, steamApps: filepath.Join(filepath.Clean(libraryPath), "steamapps"),
		})
	}
	sort.Slice(libraries[1:], func(i, j int) bool {
		return libraries[i+1].id < libraries[j+1].id
	})
	return libraries, nil
}

func readSteamShortcuts(path string) ([]vdfbinary.Shortcut, error) {
	data, err := readLimitedBackupFile(path, steamShortcutsMaxSize)
	if err != nil {
		return nil, fmt.Errorf("read Steam shortcuts: %w", err)
	}
	shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse Steam shortcuts: %w", err)
	}
	return shortcuts, nil
}

func windowsPrefixDefinitions(sourceRoot, restoreRoot string) []platforms.BackupDefinition {
	usersSource := filepath.Join(sourceRoot, "drive_c", "users")
	usersRestore := filepath.Join(restoreRoot, "drive_c", "users")
	return []platforms.BackupDefinition{
		nonRecursiveDefinition(
			sourceRoot, restoreRoot, backupCategorySettings,
			[]platforms.BackupPattern{{Glob: "*.reg"}},
		),
		definition(
			usersSource, usersRestore, backupCategorySaves,
			windowsUserSavePatterns, windowsSaveExclusions,
		),
	}
}

type bottleRoot struct {
	source  string
	restore string
}

func broadBottlePolicies(root bottleRoot) []platforms.BackupDefinition {
	return []platforms.BackupDefinition{
		definition(
			root.source, root.restore, backupCategorySettings,
			[]platforms.BackupPattern{
				{Glob: filepath.Join("*", "bottle.yml")},
				{Glob: filepath.Join("*", "*.reg")},
			}, nil,
		),
		definition(
			root.source, root.restore, backupCategorySaves,
			prefixWindowsPatterns(filepath.Join("*", "drive_c", "users"), windowsUserSavePatterns),
			prefixWindowsPatterns(filepath.Join("*", "drive_c", "users"), windowsSaveExclusions),
		),
	}
}

func preferredBottleRoot(home string) bottleRoot {
	known := bottleRoots(home)
	for _, root := range known {
		if info, err := os.Stat(root.source); err == nil && info.IsDir() {
			return root
		}
	}
	return known[0]
}

func bottleRoots(home string) []bottleRoot {
	roots := []bottleRoot{
		{
			source:  filepath.Join(home, ".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles"),
			restore: filepath.Join(".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles"),
		},
		{
			source:  filepath.Join(home, ".local", "share", "bottles", "bottles"),
			restore: filepath.Join(".local", "share", "bottles", "bottles"),
		},
	}
	if configured := strings.TrimSpace(os.Getenv("BOTTLES_PATH")); safeExternalRoot(configured, home) {
		if filepath.Base(configured) != "bottles" {
			configured = filepath.Join(configured, "bottles")
		}
		roots = append([]bottleRoot{{source: configured, restore: filepath.Join("Bottles", "custom")}}, roots...)
	}
	return roots
}

func discoverBottlesDefinitions(home string) ([]platforms.BackupDefinition, []platforms.BackupWarning) {
	definitions := make([]platforms.BackupDefinition, 0)
	warnings := make([]platforms.BackupWarning, 0)
	for _, root := range bottleRoots(home) {
		entries, err := os.ReadDir(root.source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			warnings = append(warnings, platforms.BackupWarning{
				Category: backupCategorySettings,
				Path:     filepath.ToSlash(root.restore), Reason: "Bottles data is unreadable",
			})
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !safePortableSegment(entry.Name()) {
				continue
			}
			source := filepath.Join(root.source, entry.Name())
			restore := filepath.Join(root.restore, entry.Name())
			settings := nonRecursiveDefinition(
				source, restore, backupCategorySettings,
				[]platforms.BackupPattern{{Glob: "bottle.yml"}, {Glob: "*.reg"}},
			)
			definitions = append(definitions, settings)
			users := filepath.Join(source, "drive_c", "users")
			if info, statErr := os.Stat(users); statErr == nil && info.IsDir() {
				definitions = append(definitions, definition(
					users, filepath.Join(restore, "drive_c", "users"), backupCategorySaves,
					windowsUserSavePatterns, windowsSaveExclusions,
				))
			}
		}
	}
	return definitions, warnings
}

func discoverFaugusDefinitions(home string) ([]platforms.BackupDefinition, []platforms.BackupWarning) {
	prefixes := make(map[string]string)
	warnings := make([]platforms.BackupWarning, 0)
	for _, rel := range []string{
		filepath.Join(".local", "share", "faugus-launcher", "games.json"),
		filepath.Join(".var", "app", "io.github.Faugus.faugus-launcher", "data", "faugus-launcher", "games.json"),
	} {
		path := filepath.Join(home, rel)
		data, err := readLimitedBackupFile(path, faugusLibraryMaxSize)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			warnings = append(warnings, platforms.BackupWarning{
				Category: backupCategorySettings,
				Path:     filepath.ToSlash(rel), Reason: "Faugus game metadata is unreadable",
			})
			continue
		}
		var games []faugusBackupGame
		if err = json.Unmarshal(data, &games); err != nil {
			warnings = append(warnings, platforms.BackupWarning{
				Category: backupCategorySettings,
				Path:     filepath.ToSlash(rel), Reason: "Faugus game metadata could not be parsed",
			})
			continue
		}
		for _, game := range games {
			prefix := normalizeWinePrefix(game.Prefix)
			if !safePortableSegment(game.GameID) || !safeExternalRoot(prefix, home) {
				continue
			}
			prefixes[game.GameID] = prefix
		}
	}

	root := filepath.Join(home, "Faugus")
	entries, err := os.ReadDir(root)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || !safePortableSegment(entry.Name()) {
				continue
			}
			if _, exists := prefixes[entry.Name()]; exists {
				continue
			}
			prefix := normalizeWinePrefix(filepath.Join(root, entry.Name()))
			if prefix != "" {
				prefixes[entry.Name()] = prefix
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		warnings = append(warnings, platforms.BackupWarning{
			Category: backupCategorySaves, Path: "Faugus", Reason: "Faugus prefixes are unreadable",
		})
	}

	ids := make([]string, 0, len(prefixes))
	for id := range prefixes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	definitions := make([]platforms.BackupDefinition, 0, len(ids)*2)
	for _, id := range ids {
		definitions = append(definitions, windowsPrefixDefinitions(
			prefixes[id], filepath.Join("Faugus", id, "pfx"),
		)...)
	}
	return definitions, warnings
}

func normalizeWinePrefix(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || !filepath.IsAbs(candidate) {
		return ""
	}
	candidate = filepath.Clean(candidate)
	for _, root := range []string{candidate, filepath.Join(candidate, "pfx")} {
		if info, err := os.Stat(filepath.Join(root, "drive_c", "users")); err == nil && info.IsDir() {
			return root
		}
	}
	return ""
}

func steamPrefixSavePatterns() []platforms.BackupPattern {
	return prefixWindowsPatterns(
		filepath.Join("[234]?????????", "pfx", "drive_c", "users"), windowsUserSavePatterns,
	)
}

func steamPrefixSaveExclusions() []platforms.BackupPattern {
	return prefixWindowsPatterns(
		filepath.Join("[234]?????????", "pfx", "drive_c", "users"), windowsSaveExclusions,
	)
}

func prefixWindowsPatterns(prefix string, patterns []platforms.BackupPattern) []platforms.BackupPattern {
	result := make([]platforms.BackupPattern, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern.Glob != "" {
			if !strings.ContainsAny(pattern.Glob, `/\\`) {
				result = append(result, pattern)
				continue
			}
			result = append(result, platforms.BackupPattern{
				Glob: filepath.Join(prefix, pattern.Glob),
			})
			continue
		}
		result = append(result, pattern)
	}
	return result
}

func readLimitedBackupFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path) //nolint:gosec // Fixed per-user integration metadata path.
	if err != nil {
		return nil, fmt.Errorf("open backup metadata: %w", err)
	}
	defer file.Close() //nolint:errcheck // Read-only file.
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read backup metadata: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, errors.New("backup metadata exceeds size limit")
	}
	return data, nil
}

func safeExternalRoot(candidate, forbiddenRoot string) bool {
	if candidate == "" || !filepath.IsAbs(candidate) || strings.ContainsAny(candidate, "\x00\n\r") {
		return false
	}
	clean := filepath.Clean(candidate)
	if clean == string(filepath.Separator) || clean == filepath.Clean(forbiddenRoot) {
		return false
	}
	resolved, existingRoot, ok := resolvedBackupPathFromExistingAncestor(clean)
	if !ok || existingRoot == string(filepath.Separator) || resolved == string(filepath.Separator) {
		return false
	}
	forbiddenResolved, _, forbiddenOK := resolvedBackupPathFromExistingAncestor(forbiddenRoot)
	if !forbiddenOK {
		forbiddenResolved = filepath.Clean(forbiddenRoot)
	}
	return resolved != filepath.Clean(forbiddenResolved)
}

func safePortableSegment(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value &&
		!strings.ContainsAny(value, "/\\\x00\n\r")
}
