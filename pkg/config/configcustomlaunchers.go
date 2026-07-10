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
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/rs/zerolog/log"
)

const defaultVirtualSystemCategory = "Other"

var validVirtualSystemCategories = map[string]struct{}{
	"Other":    {},
	"Console":  {},
	"Computer": {},
	"Handheld": {},
	"Arcade":   {},
}

func effectiveCustomLauncherKind(entry *LaunchersCustom) string {
	if entry.Kind == "" {
		return CustomLauncherKindLauncher
	}
	return entry.Kind
}

func effectiveCustomLauncherBackend(entry *LaunchersCustom) string {
	if entry.Backend == "" && entry.Execute != "" {
		return CustomLauncherBackendCommand
	}
	return entry.Backend
}

func validateCustomLaunchers(
	raw []LaunchersCustom,
	existing []LaunchersCustom,
	source string,
) []LaunchersCustom {
	valid := make([]LaunchersCustom, 0, len(raw))
	seenIDs := make(map[string]struct{}, len(existing)+len(raw))
	for i := range existing {
		seenIDs[strings.ToLower(existing[i].ID)] = struct{}{}
	}

	for i := range raw {
		entry := cloneCustomLauncher(&raw[i])
		if err := validateCustomLauncher(&entry); err != nil {
			log.Warn().Err(err).Str("source", source).Str("id", entry.ID).
				Msg("ignoring invalid custom launcher")
			continue
		}

		canonicalID := strings.ToLower(entry.ID)
		if _, exists := seenIDs[canonicalID]; exists {
			log.Warn().Str("source", source).Str("id", entry.ID).
				Msg("ignoring custom launcher with duplicate id")
			continue
		}
		seenIDs[canonicalID] = struct{}{}
		valid = append(valid, entry)
	}
	return valid
}

func validateCustomLauncher(entry *LaunchersCustom) error {
	if entry.ID == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(entry.ID) != entry.ID {
		return errors.New("id must not have surrounding whitespace")
	}
	if entry.Lifecycle != "" && entry.Lifecycle != "blocking" && entry.Lifecycle != "background" {
		return errors.New("lifecycle must be blocking or background")
	}

	kind := effectiveCustomLauncherKind(entry)
	switch kind {
	case CustomLauncherKindLauncher, CustomLauncherKindVirtualSystem:
	default:
		return fmt.Errorf("unsupported kind %q", entry.Kind)
	}

	backend := effectiveCustomLauncherBackend(entry)
	switch backend {
	case "", CustomLauncherBackendCommand, CustomLauncherBackendMisterCore:
	default:
		return fmt.Errorf("unsupported backend %q", entry.Backend)
	}

	if entry.Execute != "" && backend != CustomLauncherBackendCommand {
		return fmt.Errorf("execute cannot be combined with backend %q", backend)
	}
	if backend == CustomLauncherBackendCommand {
		if entry.Execute == "" {
			return errors.New("command backend requires execute")
		}
		if entry.LoadPath != "" {
			return errors.New("command backend cannot use load_path")
		}
	}
	if entry.LoadPath != "" && backend != CustomLauncherBackendMisterCore {
		return errors.New("load_path requires a backend that supports it")
	}

	if backend == CustomLauncherBackendMisterCore {
		if entry.LoadPath == "" {
			return errors.New("mister_core backend requires load_path")
		}
		if err := validateMisterLoadPath(entry.LoadPath); err != nil {
			return err
		}
	}

	switch kind {
	case CustomLauncherKindLauncher:
		if entry.Name != "" || entry.Category != "" {
			return fmt.Errorf("name and category require kind %q", CustomLauncherKindVirtualSystem)
		}
		if backend == CustomLauncherBackendMisterCore {
			return fmt.Errorf("backend %q currently requires kind %q",
				CustomLauncherBackendMisterCore, CustomLauncherKindVirtualSystem)
		}
	case CustomLauncherKindVirtualSystem:
		if backend != CustomLauncherBackendCommand && backend != CustomLauncherBackendMisterCore {
			return errors.New("virtual_system requires a supported backend")
		}
		if entry.Name == "" {
			return errors.New("virtual_system requires name")
		}
		if strings.ContainsAny(entry.LoadPath, "*?[") || strings.Contains(entry.LoadPath, "<date>") ||
			strings.Contains(entry.LoadPath, "<hash>") {
			return errors.New("virtual_system load_path cannot use RBF patterns")
		}
		if entry.Category == "" {
			entry.Category = defaultVirtualSystemCategory
		}
		if _, ok := validVirtualSystemCategories[entry.Category]; !ok {
			return fmt.Errorf("unsupported virtual_system category %q", entry.Category)
		}
		if entry.System != "" || len(entry.MediaDirs) > 0 || len(entry.FileExts) > 0 ||
			len(entry.Groups) > 0 || len(entry.Schemes) > 0 || len(entry.Controls) > 0 ||
			entry.Restricted {
			return errors.New("virtual_system cannot use media launcher fields")
		}
	}

	return nil
}

func validateMisterLoadPath(loadPath string) error {
	if strings.TrimSpace(loadPath) != loadPath {
		return errors.New("mister_core load_path must not have surrounding whitespace")
	}
	if strings.Contains(loadPath, `\`) || strings.HasPrefix(loadPath, "/") {
		return errors.New("mister_core load_path must use a relative forward-slash path")
	}
	if loadPath == "." || path.Clean(loadPath) != loadPath || strings.HasSuffix(strings.ToLower(loadPath), ".rbf") {
		return errors.New("mister_core load_path must be a clean extensionless MGL path")
	}
	for _, component := range strings.Split(loadPath, "/") {
		if component == "" || component == "." || component == ".." {
			return errors.New("mister_core load_path contains an invalid path component")
		}
	}
	return nil
}

func cloneCustomLaunchers(entries []LaunchersCustom) []LaunchersCustom {
	owned := make([]LaunchersCustom, len(entries))
	for i := range entries {
		owned[i] = cloneCustomLauncher(&entries[i])
	}
	return owned
}

func cloneCustomLauncher(source *LaunchersCustom) LaunchersCustom {
	entry := *source
	entry.Controls = cloneStringMap(entry.Controls)
	entry.MediaDirs = append([]string(nil), entry.MediaDirs...)
	entry.FileExts = append([]string(nil), entry.FileExts...)
	entry.Groups = append([]string(nil), entry.Groups...)
	entry.Schemes = append([]string(nil), entry.Schemes...)
	return entry
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	owned := make(map[string]string, len(values))
	for key, value := range values {
		owned[key] = value
	}
	return owned
}
