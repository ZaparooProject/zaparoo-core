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
	"strings"
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

//go:embed catalog/retroarch-aarch64-catalog-v1.json
var retroArchCatalogJSON []byte

//go:embed catalog/standalone-content-uri-v2.json
var standaloneCatalogJSON []byte

type retroArchCatalog struct {
	Source   retroArchCatalogSource `json:"source"`
	Package  string                 `json:"package"`
	Activity string                 `json:"activity"`
	Profiles []retroArchProfile     `json:"profiles"`
	Version  int                    `json:"version"`
}

type retroArchCatalogSource struct {
	Buildbot           string `json:"buildbot"`
	CoreInfoRepository string `json:"coreInfoRepository"`
	CoreInfoRevision   string `json:"coreInfoRevision"`
	Reviewed           string `json:"reviewed"`
}

type retroArchProfile struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	System     string   `json:"system"`
	Core       string   `json:"core"`
	File       string   `json:"file"`
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
	coreFile   string
	group      string
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
	entries := make([]catalogEntry, 0, len(standalone)+len(retroArch))
	entries = append(entries, standalone...)
	entries = append(entries, retroArch...)
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

func loadRetroArchCatalog(data []byte) ([]catalogEntry, error) {
	var catalog retroArchCatalog
	if err := decodeCatalog(data, maxRetroArchCatalogBytes, &catalog); err != nil {
		return nil, fmt.Errorf("decode RetroArch catalog: %w", err)
	}
	if catalog.Version != 1 || catalog.Package != retroArchPackage || catalog.Activity != retroArchActivity ||
		!validCatalogText(catalog.Source.Buildbot) || !validCatalogText(catalog.Source.CoreInfoRepository) ||
		!validCatalogText(catalog.Source.CoreInfoRevision) || !validCatalogText(catalog.Source.Reviewed) ||
		len(catalog.Profiles) == 0 || len(catalog.Profiles) > maxRetroArchProfiles {
		return nil, fmt.Errorf("RetroArch catalog header: %w", ErrLaunchDefinition)
	}
	seenSystemCores := make(map[string]struct{}, len(catalog.Profiles))
	entries := make([]catalogEntry, 0, len(catalog.Profiles))
	for i := range catalog.Profiles {
		profile := &catalog.Profiles[i]
		if !validCatalogText(profile.Name) || !validCoreName(profile.Core) || !validCoreFile(profile.File) {
			return nil, fmt.Errorf("RetroArch catalog row %d: %w", i, ErrLaunchDefinition)
		}
		systemCore := profile.System + "\x00" + profile.Core
		if _, found := seenSystemCores[systemCore]; found {
			return nil, fmt.Errorf("RetroArch catalog row %d repeats a core for a system: %w", i, ErrLaunchDefinition)
		}
		seenSystemCores[systemCore] = struct{}{}
		definition := retroArchDefinition(profile)
		if err := definition.Validate(); err != nil {
			return nil, fmt.Errorf("RetroArch catalog row %d: %w", i, err)
		}
		entries = append(entries, catalogEntry{definition: definition, coreFile: profile.File, group: retroArchGroup})
	}
	return entries, nil
}

// retroArchDefinition expands a catalog row into RetroArch's external-launch
// contract: the content path plus the core, config and data locations it
// expects as string extras.
func retroArchDefinition(profile *retroArchProfile) LaunchDefinition {
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
			{Name: "LIBRETRO", Type: "string", Source: "application_data", Suffix: "cores/" + profile.File},
			{Name: "CONFIGFILE", Type: "string", Source: "application_external_files", Suffix: "retroarch.cfg"},
			{Name: "DATADIR", Type: "string", Source: "application_data"},
			{Name: "APK", Type: "string", Source: "application_apk"},
			{Name: "SDCARD", Type: "string", Source: "external_storage"},
			{Name: "EXTERNAL", Type: "string", Source: "application_external_files"},
		},
		Repair: retroArchRepairMessage(profile.Name),
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

func standaloneGroup(packageName string) string {
	switch packageName {
	case "com.github.stenzek.duckstation":
		return "DuckStation"
	case "org.ppsspp.ppsspp":
		return "PPSSPP"
	case "org.dolphinemu.dolphinemu":
		return "Dolphin"
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
