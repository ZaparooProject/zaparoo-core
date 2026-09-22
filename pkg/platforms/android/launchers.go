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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/hostmedia"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/sourcepath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	// sourceScheme is the URI scheme of canonical host media identities.
	sourceScheme      = sourcepath.Scheme
	maxInstalledCores = 512
)

// hostSnapshot is what the host reported while building launchers. Targets are
// inspected once each, because every RetroArch profile shares one.
type hostSnapshot struct {
	host      Host
	targets   map[string]FailureReason
	cores     map[string]struct{}
	coresSeen bool
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
	reason, inspected := s.targets[key]
	if !inspected {
		if err := s.host.InspectTarget(definition); err != nil {
			reason = failureReason(err)
		}
		s.targets[key] = reason
	}
	return reason
}

// detected is the launcher's tri-state player evidence: nil means the host has
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
	snapshot := newHostSnapshot(p.host)
	launchers := make([]platforms.Launcher, 0, len(p.entries))
	for i := range p.entries {
		launchers = append(launchers, p.launcher(&p.entries[i], snapshot))
	}
	return launchers
}

func (p *Platform) launcher(entry *catalogEntry, snapshot *hostSnapshot) platforms.Launcher {
	definition := &entry.definition
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
	launcher := platforms.Launcher{
		ID:                 definition.ID,
		SystemID:           definition.System,
		Groups:             []string{entry.group},
		Schemes:            []string{sourceScheme},
		Extensions:         definition.Extensions,
		SkipFilesystemScan: true,
		Lifecycle:          platforms.LifecycleExternal,
		Available:          availability == nil,
		Availability:       func(*config.Instance) error { return availability },
		Detected:           detected(entry, snapshot, target),
		Test:               func(_ *config.Instance, identity string) bool { return definition.Matches(identity) },
		Preflight: func(_ *config.Instance, identity string, options *platforms.LaunchOptions) error {
			return preflight(entry, identity, options, coreMissing)
		},
		Launch: func(_ *config.Instance, identity string, _ *platforms.LaunchOptions) (*os.Process, error) {
			return nil, p.dispatch(entry, identity)
		},
	}
	if availability != nil {
		launcher.AvailabilityReason = availability.Error()
	}
	return launcher
}

func preflight(
	entry *catalogEntry,
	identity string,
	options *platforms.LaunchOptions,
	coreMissing bool,
) error {
	if coreMissing {
		return repairError(platforms.LaunchRepairLauncherPluginMissing, entry.repairParams(), msgCoreMissing)
	}
	if !entry.definition.Matches(identity) {
		return repairError(platforms.LaunchRepairLauncherUnsupportedMedia, entry.repairParams(), msgWrongMedia)
	}
	if options != nil && (options.SetName != "" || options.SetNameSameDir != "" ||
		options.RenderScale != nil || options.RenderResolution != "") {
		return repairError(platforms.LaunchRepairLauncherOptionsUnsupported,
			entry.repairParams(), msgOverridesUnsupported)
	}
	return nil
}

// LaunchMedia starts identity with launcher, or with the launcher Core's usual
// inference picks when none was chosen upstream.
func (p *Platform) LaunchMedia(
	cfg *config.Instance,
	identity string,
	launcher *platforms.Launcher,
	db *database.Database,
	options *platforms.LaunchOptions,
) error {
	ctx := p.launcherContext()
	if p.host == nil || ctx == nil {
		return unsupported("launch media before the host is ready")
	}
	if launcher == nil {
		found, err := helpers.FindLauncher(cfg, p, identity)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &found
	}
	// Only a catalog definition may reach the host. The launcher is rebuilt
	// from the catalog by ID, so a custom launcher with a colliding ID can
	// substitute neither a command nor an intent.
	entry, registered := p.entryByID[launcher.ID]
	if !registered {
		return fmt.Errorf("launcher %s is not in the Android catalog: %w", launcher.ID, platforms.ErrNotSupported)
	}
	owned := p.launcher(entry, newHostSnapshot(p.host))
	err := platforms.DoLaunch(&platforms.LaunchParams{
		Context:  ctx,
		Platform: p,
		Config:   cfg,
		Launcher: &owned,
		DB:       db,
		Options:  options,
		Path:     identity,
	}, displayName)
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}
	return nil
}

func displayName(identity string) string {
	_, parts, err := sourcepath.Parse(identity)
	if err != nil || len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// dispatch resolves identity to a live document and asks the host to start it.
// A dispatch is not evidence of gameplay: the launcher is LifecycleExternal, so
// no active media or playtime is published from here.
func (p *Platform) dispatch(entry *catalogEntry, identity string) (err error) {
	definition := &entry.definition
	ctx := p.launcherContext()
	if p.host == nil || ctx == nil {
		return unsupported("launch media before the host is ready")
	}
	session, err := p.host.OpenDocuments(ctx)
	if err != nil {
		return sourceFailure(ctx, entry, err)
	}
	defer func() {
		if closeErr := session.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("closing host document session after dispatch")
		}
	}()
	stopCancel := context.AfterFunc(ctx, func() { _ = session.CancelDispatch() })
	defer stopCancel()

	sources, err := session.Sources(ctx)
	if err != nil {
		return sourceFailure(ctx, entry, err)
	}
	document, err := hostmedia.Resolve(ctx, session, sources, identity)
	if err != nil {
		return sourceFailure(ctx, entry, err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("launch cancelled: %w", ctxErr)
	}
	// The host gets its own copy. A Dispatch that wrote through the pointer
	// would corrupt the catalog entry for the rest of the process, and the
	// receipt check below would then compare a substituted component against
	// itself and pass.
	sent := definition.copy()
	receipt, err := session.Dispatch(&sent, document)
	if err != nil {
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
}

func sourceFailure(ctx context.Context, entry *catalogEntry, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("launch cancelled: %w", ctxErr)
	}
	// A host that said what went wrong keeps its reason: a revoked grant and
	// an unmounted card are the two the user can actually act on, and
	// flattening them to "unavailable" loses the only useful advice. An
	// untyped error is a source Core could not reach.
	reason := FailureSourceUnavailable
	var hostErr *HostError
	if errors.As(err, &hostErr) && hostErr.Reason != "" {
		reason = hostErr.Reason
	}
	return fmt.Errorf("%w: %w", hostRepairError(reason, entry), err)
}
