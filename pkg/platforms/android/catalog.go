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
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
)

const (
	retroArchPackage  = "com.retroarch.aarch64"
	retroArchActivity = "com.retroarch.browser.retroactivity.RetroActivityFuture"
	retroArchGroup    = "RetroArch"

	maxRetroArchCatalogBytes  = 512 * 1024
	maxRetroArchProfiles      = 512
	maxStandaloneCatalogBytes = 64 * 1024
	maxStandaloneProfiles     = 16
	maxCatalogTextBytes       = 255
	maxCoreNameBytes          = 64
	maxCoreFileBytes          = 96
)

//go:embed catalog/retroarch-android-catalog-v2.json
var retroArchCatalogJSON []byte

//go:embed catalog/standalone-content-uri-v2.json
var standaloneCatalogJSON []byte

//go:embed catalog/app-v3.json
var appCatalogJSON []byte

// retroArchCatalog is packaging metadata only: which system a core belongs
// to, its launcher ID and accepted extensions, and per-core display name and
// Android .so filename. The system→core assignment and precedence order this
// used to carry directly come from pkg/platforms/shared/retroarch instead
// (CoreLaunches(ProfileAndroid)), generalized there 2026-09-30 so Android's
// catalog stops duplicating data the shared package already owns for five
// other platforms.
type retroArchCatalog struct {
	Source   retroArchCatalogSource       `json:"source"`
	Package  string                       `json:"package"`
	Activity string                       `json:"activity"`
	Cores    map[string]retroArchCoreInfo `json:"cores"`
	Profiles []retroArchProfile           `json:"profiles"`
	Version  int                          `json:"version"`
}

type retroArchCatalogSource struct {
	Buildbot           string `json:"buildbot"`
	CoreInfoRepository string `json:"coreInfoRepository"`
	CoreInfoRevision   string `json:"coreInfoRevision"`
	Reviewed           string `json:"reviewed"`
}

// retroArchCoreInfo is the packaging data for one core, keyed by core name in
// retroArchCatalog.Cores: its display name and Android .so filename. Neither
// varies by system (verified: every core has exactly one file across every
// system it appears in), so each is stored once, not once per profile row.
type retroArchCoreInfo struct {
	Name string `json:"name"`
	File string `json:"file"`
}

// retroArchProfile is one (system, core) pairing's packaging data: its stable
// launcher ID (never regenerated - see catalog/README.md, "Stable ID ...
// remains for compatibility") and accepted extensions, which do vary by
// (system, core), not just by core.
type retroArchProfile struct {
	ID         string   `json:"id"`
	System     string   `json:"system"`
	Core       string   `json:"core"`
	Extensions []string `json:"extensions"`
}

type standaloneCatalog struct {
	Reviewed       string            `json:"reviewed"`
	Profiles       []json.RawMessage `json:"profiles"`
	CatalogVersion int               `json:"catalogVersion"`
}

// catalogEntry is one registered launcher: a validated definition plus what
// the platform needs to present and detect it.
type catalogEntry struct {
	// coreFile is the launcher core the definition loads, when it needs one.
	coreFile string
	// coreName is that core's display name, which a client may show.
	coreName string
	// group is the launcher application's display name.
	group string
	// folders are the folder names the system's media may sit in.
	folders    []string
	definition LaunchDefinition
}

// loadCatalog returns every registered launcher in precedence order. Within a
// system the first entry is the preferred one, so the standalone apps, which
// lead the systems they serve, register ahead of the RetroArch cores.
func loadCatalog() ([]catalogEntry, error) {
	standalone, err := loadStandaloneCatalog(standaloneCatalogJSON)
	if err != nil {
		return nil, err
	}
	retroArch, err := loadRetroArchCatalog(retroArchCatalogJSON)
	if err != nil {
		return nil, err
	}
	apps, err := loadAppCatalog(appCatalogJSON)
	if err != nil {
		return nil, err
	}
	entries := make([]catalogEntry, 0, len(standalone)+len(retroArch)+len(apps))
	entries = append(entries, standalone...)
	entries = append(entries, retroArch...)
	entries = append(entries, apps...)
	seen := make(map[string]struct{}, len(entries))
	for i := range entries {
		id := strings.ToLower(entries[i].definition.ID)
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate launcher ID %s: %w", entries[i].definition.ID, ErrLaunchDefinition)
		}
		seen[id] = struct{}{}
	}
	return entries, nil
}

func decodeCatalog(data []byte, limit int, out any) error {
	if len(data) == 0 || len(data) > limit {
		return ErrLaunchDefinition
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return ErrLaunchDefinition
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrLaunchDefinition
	}
	return nil
}

// loadRetroArchCatalog builds one launcher per (system, core) the shared
// package's CoreLaunches(ProfileAndroid) lists, in that order - the shared
// package owns the system→core assignment and Android's own documented
// precedence (catalog/README.md, "Order is precedence"); this file supplies
// only the packaging data (stable launcher ID, extensions, display name,
// .so filename) CoreLaunches has no reason to know about.
func loadRetroArchCatalog(data []byte) ([]catalogEntry, error) {
	var catalog retroArchCatalog
	if err := decodeCatalog(data, maxRetroArchCatalogBytes, &catalog); err != nil {
		return nil, fmt.Errorf("decode RetroArch catalog: %w", err)
	}
	if catalog.Version != 2 || catalog.Package != retroArchPackage || catalog.Activity != retroArchActivity ||
		!validCatalogText(catalog.Source.Buildbot) || !validCatalogText(catalog.Source.CoreInfoRepository) ||
		!validCatalogText(catalog.Source.CoreInfoRevision) || !validCatalogText(catalog.Source.Reviewed) ||
		len(catalog.Cores) == 0 || len(catalog.Cores) > maxRetroArchProfiles ||
		len(catalog.Profiles) == 0 || len(catalog.Profiles) > maxRetroArchProfiles {
		return nil, fmt.Errorf("RetroArch catalog header: %w", ErrLaunchDefinition)
	}
	for core, info := range catalog.Cores {
		if !validCoreName(core) || !validCatalogText(info.Name) || !validCoreFile(info.File) {
			return nil, fmt.Errorf("RetroArch catalog core %s: %w", core, ErrLaunchDefinition)
		}
	}
	byKey := make(map[string]*retroArchProfile, len(catalog.Profiles))
	for i := range catalog.Profiles {
		profile := &catalog.Profiles[i]
		if !validCatalogText(profile.ID) || !validCoreName(profile.Core) || len(profile.Extensions) == 0 {
			return nil, fmt.Errorf("RetroArch catalog row %d: %w", i, ErrLaunchDefinition)
		}
		if _, ok := catalog.Cores[profile.Core]; !ok {
			return nil, fmt.Errorf("RetroArch catalog row %d: core %s has no packaging entry: %w",
				i, profile.Core, ErrLaunchDefinition)
		}
		key := profile.System + "\x00" + profile.Core
		if _, found := byKey[key]; found {
			return nil, fmt.Errorf("RetroArch catalog row %d repeats a core for a system: %w", i, ErrLaunchDefinition)
		}
		byKey[key] = profile
	}

	launches := sharedretroarch.CoreLaunches(sharedretroarch.ProfileAndroid)
	entries := make([]catalogEntry, 0, len(launches))
	usedKeys := make(map[string]bool, len(byKey))
	usedCores := make(map[string]bool, len(catalog.Cores))
	for i := range launches {
		launch := &launches[i]
		core := strings.TrimSuffix(strings.TrimSuffix(launch.Core, ".so"), "_libretro")
		key := launch.SystemID + "\x00" + core
		profile, found := byKey[key]
		if !found {
			return nil, fmt.Errorf("RetroArch catalog has no packaging data for %s/%s: %w",
				launch.SystemID, core, ErrLaunchDefinition)
		}
		usedKeys[key] = true
		usedCores[core] = true
		info := catalog.Cores[core]
		definition := retroArchDefinition(profile, info.File, info.Name)
		if err := definition.Validate(); err != nil {
			return nil, fmt.Errorf("RetroArch catalog entry %s/%s: %w", launch.SystemID, core, err)
		}
		entries = append(entries, catalogEntry{
			definition: definition, coreFile: info.File, coreName: info.Name, group: retroArchGroup,
		})
	}
	// A row or packaged core the shared retroarch package's own ranked output
	// never matched is stale data silently carried forward from a previous
	// catalog edit: the embedded catalog is reviewed data, not a format that
	// tolerates drift, so this fails loudly instead of just shipping it unused.
	if unused, ok := firstUnused(byKey, usedKeys); ok {
		return nil, fmt.Errorf("RetroArch catalog row for %s was never matched: %w", unused, ErrLaunchDefinition)
	}
	if unused, ok := firstUnusedCore(catalog.Cores, usedCores); ok {
		return nil, fmt.Errorf("RetroArch catalog core %s was never matched: %w", unused, ErrLaunchDefinition)
	}
	return entries, nil
}

// firstUnused returns the lexicographically first key in byKey that usedKeys
// does not mark, for a deterministic error message.
func firstUnused(byKey map[string]*retroArchProfile, usedKeys map[string]bool) (string, bool) {
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		if !usedKeys[key] {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return "", false
	}
	sort.Strings(keys)
	return keys[0], true
}

// firstUnusedCore returns the lexicographically first core name in cores that
// usedCores does not mark, for a deterministic error message.
func firstUnusedCore(cores map[string]retroArchCoreInfo, usedCores map[string]bool) (string, bool) {
	names := make([]string, 0, len(cores))
	for name := range cores {
		if !usedCores[name] {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", false
	}
	sort.Strings(names)
	return names[0], true
}

// retroArchDefinition expands a catalog row into RetroArch's external-launch
// contract: the content path plus the core, config and data locations it
// expects as string extras. file and name are the core's packaging data
// (retroArchCatalog.Cores), looked up by the caller since they live outside
// the per-row profile now.
func retroArchDefinition(profile *retroArchProfile, file, name string) LaunchDefinition {
	return LaunchDefinition{
		Version:       1,
		ID:            profile.ID,
		System:        profile.System,
		Extensions:    append([]string(nil), profile.Extensions...),
		Package:       retroArchPackage,
		Activity:      retroArchActivity,
		Action:        actionMain,
		Strategy:      StrategyFilesystemPath,
		StorageAccess: "legacy_read",
		MaxTargetSDK:  28,
		Extras: []LaunchExtra{
			{Name: "ROM", Type: "string", Source: extraSourceMedia},
			{Name: "LIBRETRO", Type: "string", Source: "application_data", Suffix: "cores/" + file},
			{Name: "CONFIGFILE", Type: "string", Source: "application_external_files", Suffix: "retroarch.cfg"},
			{Name: "DATADIR", Type: "string", Source: "application_data"},
			{Name: "APK", Type: "string", Source: "application_apk"},
			{Name: "SDCARD", Type: "string", Source: "external_storage"},
			{Name: "EXTERNAL", Type: "string", Source: "application_external_files"},
		},
		Repair: retroArchRepairMessage(name),
	}
}

func loadStandaloneCatalog(data []byte) ([]catalogEntry, error) {
	var catalog standaloneCatalog
	if err := decodeCatalog(data, maxStandaloneCatalogBytes, &catalog); err != nil {
		return nil, fmt.Errorf("decode standalone catalog: %w", err)
	}
	if catalog.CatalogVersion != 1 || !validCatalogText(catalog.Reviewed) ||
		len(catalog.Profiles) == 0 || len(catalog.Profiles) > maxStandaloneProfiles {
		return nil, fmt.Errorf("standalone catalog header: %w", ErrLaunchDefinition)
	}
	entries := make([]catalogEntry, 0, len(catalog.Profiles))
	for i := range catalog.Profiles {
		definition, err := parseLaunchDefinition(catalog.Profiles[i])
		if err != nil || definition.Version != 2 {
			return nil, fmt.Errorf("standalone catalog row %d: %w", i, ErrLaunchDefinition)
		}
		entries = append(entries, catalogEntry{definition: definition, group: standaloneGroup(definition.Package)})
	}
	return entries, nil
}

// loadAppCatalog reads the app profiles: launches that carry no media, where
// an optional literal extra selects one profile-defined variant.
func loadAppCatalog(data []byte) ([]catalogEntry, error) {
	var catalog standaloneCatalog
	if err := decodeCatalog(data, maxStandaloneCatalogBytes, &catalog); err != nil {
		return nil, fmt.Errorf("decode app catalog: %w", err)
	}
	if catalog.CatalogVersion != 1 || !validCatalogText(catalog.Reviewed) ||
		len(catalog.Profiles) == 0 || len(catalog.Profiles) > maxStandaloneProfiles {
		return nil, fmt.Errorf("app catalog header: %w", ErrLaunchDefinition)
	}
	entries := make([]catalogEntry, 0, len(catalog.Profiles))
	seen := make(map[string]struct{}, len(catalog.Profiles))
	for i := range catalog.Profiles {
		definition, err := parseLaunchDefinition(catalog.Profiles[i])
		if err != nil || definition.Version != 3 {
			return nil, fmt.Errorf("app catalog row %d: %w", i, ErrLaunchDefinition)
		}
		identity := AppIdentity{Package: definition.Package, Variant: definition.Variant}
		if _, duplicate := seen[identity.id()]; duplicate {
			return nil, fmt.Errorf("app catalog row %d: %w", i, ErrLaunchDefinition)
		}
		seen[identity.id()] = struct{}{}
		entries = append(entries, catalogEntry{
			definition: definition,
			group:      standaloneGroup(definition.Package),
		})
	}
	return entries, nil
}

func standaloneGroup(packageName string) string {
	switch packageName {
	case "com.github.stenzek.duckstation":
		return "DuckStation"
	case "org.ppsspp.ppsspp":
		return "PPSSPP"
	case "org.dolphinemu.dolphinemu":
		return "Dolphin"
	case "com.seleuco.mame4d2024":
		return "MAME4droid"
	case "com.armsx2":
		return "ARMSX2"
	case "com.theboisclub.pokemonred":
		return "Pokeport"
	default:
		return "Android"
	}
}

func validCatalogText(value string) bool {
	return value != "" && len(value) <= maxCatalogTextBytes && !strings.ContainsAny(value, "\x00\r\n")
}

func isASCIIAlphanumeric(char rune) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
}

func validCoreName(value string) bool {
	if value == "" || len(value) > maxCoreNameBytes {
		return false
	}
	for _, char := range value {
		if !isASCIIAlphanumeric(char) && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

// validCoreFile accepts only a bare libretro core filename, never a path.
func validCoreFile(value string) bool {
	if value == "" || len(value) > maxCoreFileBytes || strings.Contains(value, "..") {
		return false
	}
	if !strings.HasSuffix(value, "_libretro_android.so") && !strings.HasSuffix(value, "_libretro.so") {
		return false
	}
	for _, char := range value {
		if !isASCIIAlphanumeric(char) && char != '_' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}
