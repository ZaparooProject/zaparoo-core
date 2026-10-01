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
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// Standalone ScummVM starts a game its own launcher already knows. Its
// exported SplashActivity forwards the intent's action and data to
// ScummVMActivity, which passes the data URI's scheme-specific part to
// ScummVM as its only argument: a configured target ID. This is the intent
// ScummVM's own home-screen shortcuts use.
//
// The .scummvm file is ordinary source-backed media, matched by Folders and
// Extensions exactly like the RetroArch core that plays the same files: its
// identity is the plain source:// path, never a scheme built from the file's
// content. Only at dispatch is a small amount of the file read, to learn the
// target ID ScummVM already has the game under.
const (
	scummVMPackage      = "org.scummvm.scummvm"
	scummVMActivity     = "org.scummvm.scummvm.SplashActivity"
	scummVMDataScheme   = "scummvm"
	scummVMStandaloneID = "ScummVM.Standalone"
	scummVMGroup        = "ScummVM"
	scummVMExtension    = ".scummvm"
	// maxScummVMFileBytes bounds the file read. A target ID is at most 129
	// bytes; the rest allows for a line ending and surrounding space.
	maxScummVMFileBytes = 256
	// maxScummVMDataBytes bounds the Data field validData checks: the
	// "scummvm:" scheme prefix plus a target ID at its maximum length
	// (scummVMTargetPattern), so a valid maximum-length target is never
	// rejected by a limit meant only to bound the field, not shorten it.
	maxScummVMDataBytes = len(scummVMDataScheme) + 1 + 129
)

// scummVMTargetPattern is a ScummVM target ID, optionally engine-qualified.
// It must start with a letter or digit: ScummVM reads its only argument as an
// option when it starts with a dash.
var scummVMTargetPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}(:[A-Za-z0-9._-]{1,64})?$`)

// scummVMEntry is the launch template. The target is added per launch, from
// the file, so the template itself carries no data.
var scummVMEntry = catalogEntry{
	group:   scummVMGroup,
	folders: systemFolders(systemdefs.SystemScummVM),
	definition: LaunchDefinition{
		Version: 3, ID: scummVMStandaloneID, Name: "ScummVM", System: systemdefs.SystemScummVM,
		Package: scummVMPackage, Activity: scummVMActivity, Action: actionMain,
		Strategy: StrategyApp, StorageAccess: "none",
		Repair: "Install ScummVM and add this game in its own launcher, with the game ID the file names.",
	},
}

// scummVMLauncher offers standalone ScummVM for the same .scummvm files the
// RetroArch core plays. It registers ahead of the core, so it is preferred
// whenever it is installed.
func (p *Platform) scummVMLauncher(snapshot *hostSnapshot) platforms.Launcher {
	entry := &scummVMEntry
	target := snapshot.targetFailure(&entry.definition)
	var availability error
	if target != "" {
		availability = hostRepairError(target, entry)
	}
	media := platforms.Launcher{
		ID:         scummVMStandaloneID,
		SystemID:   systemdefs.SystemScummVM,
		Folders:    entry.folders,
		Extensions: []string{scummVMExtension},
	}
	launcher := media
	launcher.Groups = []string{entry.group}
	launcher.Lifecycle = platforms.LifecycleExternal
	launcher.Available = availability == nil
	launcher.Availability = func(*config.Instance) error { return availability }
	launcher.Detected = detected(entry, snapshot, target)
	launcher.Preflight = func(cfg *config.Instance, path string, options *platforms.LaunchOptions) error {
		return preflight(entry, helpers.PathIsLauncher(cfg, p, &media, path), options, false)
	}
	launcher.Launch = func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
		return nil, p.dispatchScummVM(path)
	}
	if availability != nil {
		launcher.AvailabilityReason = availability.Error()
	}
	return launcher
}

// dispatchScummVM reads the target ID from the .scummvm file and starts
// ScummVM on it. The file's source path stays the media identity; only the
// ID it names reaches ScummVM.
func (p *Platform) dispatchScummVM(path string) error {
	entry := &scummVMEntry
	ctx := p.launcherContext()
	if p.host == nil || ctx == nil {
		return unsupported("launch media before the host is ready")
	}
	content, err := p.ReadSourceFile(ctx, path, maxScummVMFileBytes)
	if err != nil {
		return sourceFailure(ctx, entry, err)
	}
	target, ok := scummVMTarget(content, path)
	if !ok {
		return repairError(platforms.LaunchRepairLauncherUnsupportedMedia, entry.repairParams(), msgWrongMedia)
	}
	definition := entry.definition.copy()
	definition.Data = scummVMDataScheme + ":" + target
	if validateErr := definition.Validate(); validateErr != nil {
		return repairError(platforms.LaunchRepairLauncherUnsupportedMedia, entry.repairParams(), msgWrongMedia)
	}
	return p.dispatchApp(&catalogEntry{definition: definition, group: entry.group}, path)
}

// scummVMTarget is the file's trimmed content, or its own name without the
// extension when the content is empty, the two conventions ScummVM frontends
// use. Anything that is not a plain target ID is refused rather than repaired.
func scummVMTarget(content []byte, path string) (string, bool) {
	if len(content) > maxScummVMFileBytes {
		return "", false
	}
	target := strings.TrimSpace(string(content))
	if target == "" {
		target = helpers.GetPathName(path)
	}
	return target, scummVMTargetPattern.MatchString(target)
}
