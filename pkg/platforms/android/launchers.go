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
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	maxInstalledCores = 512
	// hostSnapshotTTL bounds how long one host report is reused. Package and
	// core changes the host sees invalidate it sooner.
	hostSnapshotTTL = 60 * time.Second
)

// hostSnapshot is what the host reported while building launchers. Targets are
// inspected once each, because every RetroArch profile shares one. One
// snapshot is shared by concurrent launches, so its lazy parts are locked.
type hostSnapshot struct {
	host      Host
	targets   map[string]FailureReason
	cores     map[string]struct{}
	apps      []AppInfo
	mu        syncutil.Mutex
	coresSeen bool
	appsSeen  bool
}

// installedApps asks the host once per snapshot. A listing the host did not
// make, or one that is implausibly large, reports nothing rather than being
// read as absence.
func (s *hostSnapshot) installedApps() []AppInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appsSeen {
		return s.apps
	}
	s.appsSeen = true
	apps, scanned := s.host.InstalledApps()
	if !scanned || len(apps) > maxInstalledApps {
		return nil
	}
	kept := make([]AppInfo, 0, len(apps))
	seen := make(map[string]struct{}, len(apps))
	for _, app := range apps {
		if !dottedNamePattern.MatchString(app.Package) || !dottedNamePattern.MatchString(app.Activity) ||
			!validCatalogText(app.Label) {
			continue
		}
		if _, duplicate := seen[app.Package]; duplicate {
			continue
		}
		seen[app.Package] = struct{}{}
		kept = append(kept, app)
	}
	s.apps = kept
	return s.apps
}

func newHostSnapshot(host Host) *hostSnapshot {
	snapshot := &hostSnapshot{host: host, targets: make(map[string]FailureReason)}
	files, scanned := host.InstalledCores()
	if !scanned || len(files) > maxInstalledCores {
		return snapshot
	}
	cores := make(map[string]struct{}, len(files))
	for _, name := range files {
		// A malformed listing is no evidence that any core is absent.
		if name != strings.ToLower(name) || !validCoreFile(name) {
			return snapshot
		}
		cores[name] = struct{}{}
	}
	snapshot.cores, snapshot.coresSeen = cores, true
	return snapshot
}

// targetFailure returns the empty reason when the definition's target can be started.
func (s *hostSnapshot) targetFailure(definition *LaunchDefinition) FailureReason {
	key := definition.Package + "\x00" + definition.Activity + "\x00" + definition.Strategy
	s.mu.Lock()
	defer s.mu.Unlock()
	reason, inspected := s.targets[key]
	if !inspected {
		if err := s.host.InspectTarget(definition); err != nil {
			reason = failureReason(err)
		}
		s.targets[key] = reason
	}
	return reason
}

// detected is the launcher's tri-state install evidence: nil means the host has
// not looked, which must never be read as absence.
func detected(entry *catalogEntry, snapshot *hostSnapshot, target FailureReason) *bool {
	found := false
	switch {
	case target == FailureNotInstalled || target == FailureActivityUnavailable:
	case entry.coreFile != "":
		if !snapshot.coresSeen {
			return nil
		}
		_, found = snapshot.cores[strings.ToLower(entry.coreFile)]
	case target == "":
		found = true
	default:
		return nil
	}
	return &found
}

// Launchers lists the catalog in precedence order: within a system, the first
// launcher is the preferred one.
func (p *Platform) Launchers(*config.Instance) []platforms.Launcher {
	if p.host == nil {
		return nil
	}
	snapshot := p.hostSnapshot()
	launchers := make([]platforms.Launcher, 0, len(p.entries)+len(gameNativeEntries)+2)
	// The launchers Core builds rather than reads from the catalog come
	// first: standalone ScummVM leads the RetroArch core for the same files.
	launchers = append(launchers, p.scummVMLauncher(snapshot))
	for i := range gameNativeEntries {
		launchers = append(launchers, p.gameNativeLauncher(&gameNativeEntries[i], snapshot))
	}
	for i := range p.entries {
		launchers = append(launchers, p.launcher(&p.entries[i], snapshot))
	}
	// Last, so a profile that describes an app is always preferred over the
	// generic offer of the same app.
	return append(launchers, p.installedAppsLauncher(snapshot))
}

func (p *Platform) launcher(entry *catalogEntry, snapshot *hostSnapshot) platforms.Launcher {
	definition := &entry.definition
	if definition.Strategy == StrategyApp {
		return p.appLauncher(entry, snapshot)
	}
	target := snapshot.targetFailure(definition)
	var availability error
	if target != "" {
		availability = hostRepairError(target, entry)
	}
	coreMissing := false
	if entry.coreFile != "" && snapshot.coresSeen {
		_, installed := snapshot.cores[strings.ToLower(entry.coreFile)]
		coreMissing = !installed
	}
	// Media is matched the way Core matches a root directory: a file must sit
	// in one of the system's folders in a source root and have one of the
	// definition's extensions.
	media := platforms.Launcher{
		ID:         definition.ID,
		SystemID:   definition.System,
		Folders:    slices.Clone(entry.folders),
		Extensions: definition.Extensions,
	}
	launcher := media
	launcher.Groups = []string{entry.group}
	launcher.Lifecycle = platforms.LifecycleExternal
	launcher.Available = availability == nil
	launcher.Availability = func(*config.Instance) error { return availability }
	launcher.Detected = detected(entry, snapshot, target)
	launcher.Preflight = func(cfg *config.Instance, path string, options *platforms.LaunchOptions) error {
		return preflight(entry, helpers.PathIsLauncher(cfg, p, &media, path), options, coreMissing)
	}
	launcher.Launch = func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
		return nil, p.dispatch(entry, path)
	}
	if availability != nil {
		launcher.AvailabilityReason = availability.Error()
	}
	return launcher
}

func preflight(
	entry *catalogEntry,
	matches bool,
	options *platforms.LaunchOptions,
	coreMissing bool,
) error {
	if coreMissing {
		return repairError(platforms.LaunchRepairLauncherPluginMissing, entry.repairParams(), msgCoreMissing)
	}
	if !matches {
		return repairError(platforms.LaunchRepairLauncherUnsupportedMedia, entry.repairParams(), msgWrongMedia)
	}
	if options != nil && (options.SetName != "" || options.SetNameSameDir != "" ||
		options.RenderScale != nil || options.RenderResolution != "") {
		return repairError(platforms.LaunchRepairLauncherOptionsUnsupported,
			entry.repairParams(), msgOverridesUnsupported)
	}
	return nil
}

// ownedLauncher rebuilds a launcher this platform registered, by ID: a
// catalog entry, the generic installed-apps launcher, or one of the
// launchers Core builds rather than reads from the catalog.
func (p *Platform) ownedLauncher(id string, snapshot *hostSnapshot) (platforms.Launcher, bool) {
	switch id {
	case installedAppsID:
		return p.installedAppsLauncher(snapshot), true
	case scummVMStandaloneID:
		return p.scummVMLauncher(snapshot), true
	}
	if entry := gameNativeEntryByID(id); entry != nil {
		return p.gameNativeLauncher(entry, snapshot), true
	}
	entry, registered := p.entryByID[id]
	if !registered {
		return platforms.Launcher{}, false
	}
	return p.launcher(entry, snapshot), true
}

// gameNativeEntryByID finds the launch template of one GameNative launcher.
func gameNativeEntryByID(id string) *gameNativeEntry {
	for i := range gameNativeEntries {
		if gameNativeEntries[i].definition.ID == id {
			return &gameNativeEntries[i]
		}
	}
	return nil
}

// LaunchMedia starts path with launcher, or with the launcher Core's usual
// inference picks when none was chosen upstream.
func (p *Platform) LaunchMedia(
	cfg *config.Instance,
	path string,
	launcher *platforms.Launcher,
	db *database.Database,
	options *platforms.LaunchOptions,
) error {
	ctx := p.launcherContext()
	if p.host == nil || ctx == nil {
		return unsupported("launch media before the host is ready")
	}
	if launcher == nil {
		found, err := helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &found
	}
	// Only a definition this platform built may reach the host. The launcher
	// is rebuilt here from the catalog, or from what the host reports, so a
	// custom launcher with a colliding ID can substitute neither a command nor
	// an intent.
	owned, registered := p.ownedLauncher(launcher.ID, p.hostSnapshot())
	if !registered {
		return fmt.Errorf("launcher %s is not in the Android catalog: %w", launcher.ID, platforms.ErrNotSupported)
	}
	err := platforms.DoLaunch(&platforms.LaunchParams{
		Context:  ctx,
		Platform: p,
		Config:   cfg,
		Launcher: &owned,
		DB:       db,
		Options:  options,
		Path:     path,
	}, helpers.GetPathName)
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}
	return nil
}

// dispatch asks the host to start the file at path. A dispatch is not
// evidence of gameplay: the launcher is LifecycleExternal, so no active media
// or playtime is published from here.
func (p *Platform) dispatch(entry *catalogEntry, path string) error {
	definition := &entry.definition
	ctx := p.launcherContext()
	if p.host == nil || ctx == nil {
		return unsupported("launch media before the host is ready")
	}
	id, segments, err := platforms.SourceLocation(path)
	if err != nil || len(segments) == 0 {
		return repairError(platforms.LaunchRepairLauncherUnsupportedMedia, entry.repairParams(), msgWrongMedia)
	}
	reference, err := p.folderReference(ctx, id)
	if err != nil {
		return sourceFailure(ctx, entry, err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("launch cancelled: %w", ctxErr)
	}
	return p.track(ctx, definition, definition.ID, path, func() error {
		// The host gets its own copy. A Dispatch that wrote through the pointer
		// would corrupt the catalog entry for the rest of the process, and the
		// receipt check below would then compare a substituted component against
		// itself and pass.
		sent := definition.copy()
		receipt, err := p.host.Dispatch(ctx, &sent, reference, segments)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return fmt.Errorf("launch cancelled: %w", ctxErr)
			}
			return fmt.Errorf("%w: %w", hostRepairError(failureReason(err), entry), err)
		}
		if receipt.Package != definition.Package || receipt.Activity != definition.Activity ||
			receipt.Strategy != definition.Strategy {
			// The host started something the definition did not name. A user can do
			// nothing about it, so the client only learns the outcome is
			// unconfirmed. An operator has to be able to find this, so it is logged
			// at error level with both components named.
			log.Error().
				Str("launcherID", definition.ID).
				Str("expectedPackage", definition.Package).
				Str("actualPackage", receipt.Package).
				Str("expectedActivity", definition.Activity).
				Str("actualActivity", receipt.Activity).
				Str("expectedStrategy", definition.Strategy).
				Str("actualStrategy", receipt.Strategy).
				Msg("host dispatch receipt names a different component than the launch definition")
			return repairError(platforms.LaunchRepairOutcomeUnknown, entry.repairParams(), msgReceiptMismatch)
		}
		return nil
	})
}

func sourceFailure(ctx context.Context, entry *catalogEntry, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("launch cancelled: %w", ctxErr)
	}
	// A host that said what went wrong keeps its reason: a revoked grant
	// (FailureSourceRevoked) and an unmounted card are the two the user can
	// actually act on, and flattening them to "unavailable" loses the only
	// useful advice. An untyped error is a source Core could not reach.
	reason := FailureSourceUnavailable
	var hostErr *HostError
	if errors.As(err, &hostErr) && hostErr.Reason != "" {
		reason = hostErr.Reason
	}
	return fmt.Errorf("%w: %w", hostRepairError(reason, entry), err)
}
