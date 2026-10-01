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
	"context"
	"fmt"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

const (
	// maxInstalledApps bounds what the host may report in one sweep.
	maxInstalledApps = 2048
	// installedAppsID is the launcher that offers every launchable app the
	// host reports and that no profile already describes.
	installedAppsID = "Android.Apps"
	// installedAppsGroup is the display name for that launcher.
	installedAppsGroup = "Android"
	// installedAppsRepair is shown when the host cannot start a plain app.
	installedAppsRepair = "Install or enable this app."
)

// appLauncher offers one profile-described app, or one variant of it. The
// identity names the app; the definition holds how to start it, so a profile
// can change activity or extras without invalidating an identity already
// written to a card.
func (p *Platform) appLauncher(entry *catalogEntry, snapshot *hostSnapshot) platforms.Launcher {
	definition := &entry.definition
	target := snapshot.targetFailure(definition)
	var availability error
	if target != "" {
		availability = hostRepairError(target, entry)
	}
	identity := AppIdentity{
		Package: definition.Package,
		Variant: definition.Variant,
		Name:    definition.Name,
	}
	// An app the host cannot start is not offered as media. That is an
	// absence, not a launch failure, so the scan reports no error.
	offered := availability == nil
	launcher := platforms.Launcher{
		ID:                 definition.ID,
		SystemID:           definition.System,
		Groups:             []string{entry.group},
		Schemes:            []string{shared.SchemeAndroid},
		SkipFilesystemScan: true,
		Lifecycle:          platforms.LifecycleExternal,
		Available:          availability == nil,
		Availability:       func(*config.Instance) error { return availability },
		Detected:           detected(entry, snapshot, target),
		Test: func(_ *config.Instance, candidate string) bool {
			return matchesApp(candidate, identity)
		},
		Scanner: func(
			_ context.Context, _ *config.Instance, _ string, results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			if !offered {
				return results, nil
			}
			return append(results, platforms.ScanResult{
				Path: identity.AppPath(), Name: definition.Name, NoExt: true,
			}), nil
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			return nil, p.dispatchApp(entry, path)
		},
	}
	if availability != nil {
		launcher.AvailabilityReason = availability.Error()
	}
	return launcher
}

// installedAppsLauncher offers every launchable app the host reports that no
// profile already describes, so a plain app needs no catalog entry at all.
func (p *Platform) installedAppsLauncher(snapshot *hostSnapshot) platforms.Launcher {
	profiled := p.profiledPackages()
	return platforms.Launcher{
		ID:                 installedAppsID,
		SystemID:           systemdefs.SystemAndroid,
		Groups:             []string{installedAppsGroup},
		Schemes:            []string{shared.SchemeAndroid},
		SkipFilesystemScan: true,
		Lifecycle:          platforms.LifecycleExternal,
		Available:          true,
		Availability:       func(*config.Instance) error { return nil },
		Test: func(_ *config.Instance, candidate string) bool {
			parsed, err := ParseAppPath(candidate)
			if err != nil || parsed.Variant != "" {
				return false
			}
			_, covered := profiled[parsed.Package]
			return !covered
		},
		Scanner: func(
			_ context.Context, _ *config.Instance, _ string, results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			for _, app := range snapshot.installedApps() {
				if _, covered := profiled[app.Package]; covered {
					continue
				}
				identity := AppIdentity{Package: app.Package, Name: app.Label}
				results = append(results, platforms.ScanResult{
					Path: identity.AppPath(), Name: app.Label, NoExt: true,
				})
			}
			return results, nil
		},
		Launch: func(_ *config.Instance, identity string, _ *platforms.LaunchOptions) (*os.Process, error) {
			return nil, p.dispatchInstalledApp(snapshot, identity)
		},
	}
}

// profiledPackages is every package an app profile already describes. The
// generic launcher skips them so one app is never offered twice.
func (p *Platform) profiledPackages() map[string]struct{} {
	profiled := make(map[string]struct{})
	for i := range p.entries {
		if p.entries[i].definition.Strategy == StrategyApp {
			profiled[p.entries[i].definition.Package] = struct{}{}
		}
	}
	return profiled
}

// matchesApp compares identities, not strings, so an equivalent encoding of
// the same identity still matches.
func matchesApp(candidate string, identity AppIdentity) bool {
	parsed, err := ParseAppPath(candidate)
	if err != nil {
		return false
	}
	return parsed.Package == identity.Package && parsed.Variant == identity.Variant
}

// dispatchInstalledApp starts a plain app the host reported. The definition is
// built here from what the host said, and validated before it is sent.
func (p *Platform) dispatchInstalledApp(snapshot *hostSnapshot, identity string) error {
	parsed, err := ParseAppPath(identity)
	if err != nil {
		return repairError(platforms.LaunchRepairLauncherUnsupportedMedia, nil, msgWrongMedia)
	}
	var found *AppInfo
	for _, app := range snapshot.installedApps() {
		if app.Package == parsed.Package {
			found = &app
			break
		}
	}
	if found == nil {
		return repairError(platforms.LaunchRepairLauncherNotInstalled, nil, msgNotInstalled)
	}
	definition := LaunchDefinition{
		ID: installedAppsID, Name: found.Label, System: systemdefs.SystemAndroid,
		Package: found.Package, Activity: found.Activity, Action: actionMain,
		Strategy: StrategyApp, StorageAccess: "none", Repair: installedAppsRepair,
		Version: 3,
	}
	if validateErr := definition.Validate(); validateErr != nil {
		return repairError(platforms.LaunchRepairLauncherNotInstalled, nil, msgNotInstalled)
	}
	entry := &catalogEntry{definition: definition, group: installedAppsGroup}
	return p.dispatchApp(entry, identity)
}

// dispatchApp starts a definition that carries no media of its own. path is
// the media or app identity path a session is recorded under: the real
// source path for ScummVM/GameNative, or the app's own identity for a plain
// app launch.
func (p *Platform) dispatchApp(entry *catalogEntry, path string) error {
	definition := &entry.definition
	ctx := p.launcherContext()
	if p.host == nil || ctx == nil {
		return unsupported("launch an app before the host is ready")
	}
	return p.track(ctx, definition, definition.ID, path, func() error {
		// The host gets its own copy, so a Dispatch that wrote through the pointer
		// cannot corrupt the catalog entry for the rest of the process.
		sent := definition.copy()
		receipt, err := p.host.DispatchApp(&sent)
		if err != nil {
			return fmt.Errorf("%w: %w", hostRepairError(failureReason(err), entry), err)
		}
		if receipt.Package != definition.Package || receipt.Activity != definition.Activity ||
			receipt.Strategy != definition.Strategy {
			log.Error().
				Str("launcherID", definition.ID).
				Str("expectedPackage", definition.Package).
				Str("actualPackage", receipt.Package).
				Str("expectedActivity", definition.Activity).
				Str("actualActivity", receipt.Activity).
				Str("expectedStrategy", definition.Strategy).
				Str("actualStrategy", receipt.Strategy).
				Msg("host app dispatch receipt names a different component than the launch definition")
			return repairError(platforms.LaunchRepairOutcomeUnknown, entry.repairParams(), msgReceiptMismatch)
		}
		return nil
	})
}
