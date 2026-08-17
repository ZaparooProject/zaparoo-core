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

package main

import (
	"fmt"
	"sort"
	"strings"
)

// target is one platform/architecture pair that the release build produces an
// update archive for. The list must stay in step with the build matrix in
// .github/workflows/build.yml; TestExpectedTargets_MatchesBuildWorkflow
// enforces that.
type target struct {
	Platform string
	Arch     string
	Ext      string
}

// zipPlatforms are the platforms whose archives are zip rather than tar.gz,
// mirroring the extension switch in the release build workflow.
var zipPlatforms = map[string]bool{
	"mac":    true,
	"mister": true,
	"mistex": true,
	// Windows is built by a separate signing job but publishes the same
	// naming scheme.
	"windows": true,
}

// expectedTargets returns every platform/arch pair a complete release publishes
// an update archive for.
func expectedTargets() []target {
	pairs := []struct {
		platform string
		arches   []string
	}{
		{platform: "batocera", arches: []string{"amd64", "arm", "arm64"}},
		{platform: "bazzite", arches: []string{"amd64", "arm64"}},
		{platform: "chimeraos", arches: []string{"amd64", "arm64"}},
		{platform: "libreelec", arches: []string{"amd64", "arm", "arm64"}},
		{platform: "linux", arches: []string{"amd64", "arm64"}},
		{platform: "mac", arches: []string{"amd64", "arm64"}},
		{platform: "mister", arches: []string{"arm"}},
		{platform: "mistex", arches: []string{"arm64"}},
		{platform: "replayos", arches: []string{"arm64"}},
		{platform: "steamos", arches: []string{"amd64"}},
		{platform: "windows", arches: []string{"386", "amd64", "arm64"}},
		{platform: "zapos", arches: []string{"arm64"}},
	}

	targets := make([]target, 0, 24)
	for _, pair := range pairs {
		for _, arch := range pair.arches {
			ext := "tar.gz"
			if zipPlatforms[pair.platform] {
				ext = "zip"
			}
			targets = append(targets, target{Platform: pair.platform, Arch: arch, Ext: ext})
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Platform != targets[j].Platform {
			return targets[i].Platform < targets[j].Platform
		}
		return targets[i].Arch < targets[j].Arch
	})
	return targets
}

// versionFromTag strips the leading "v" that release tags carry but asset
// filenames do not.
func versionFromTag(tag string) string {
	return strings.TrimPrefix(tag, "v")
}

// archiveName returns the release archive filename for a target at a version.
func archiveName(t target, version string) string {
	return fmt.Sprintf("zaparoo-%s_%s-%s.%s", t.Platform, t.Arch, version, t.Ext)
}

// validateAssetMatrix reports whether a release carries exactly the set of
// archives a complete build produces: nothing missing, nothing unexpected. A
// partial matrix means some platforms would see an update they cannot install.
func validateAssetMatrix(rel *release, tag string) error {
	version := versionFromTag(tag)

	present := make(map[string]int, len(rel.Assets))
	for _, a := range archiveAssets(rel) {
		present[a.Name]++
	}

	var missing, duplicated []string
	for _, t := range expectedTargets() {
		name := archiveName(t, version)
		switch present[name] {
		case 0:
			missing = append(missing, name)
		case 1:
		default:
			duplicated = append(duplicated, name)
		}
		delete(present, name)
	}

	unexpected := make([]string, 0, len(present))
	for name := range present {
		unexpected = append(unexpected, name)
	}
	sort.Strings(unexpected)

	var problems []string
	if len(missing) > 0 {
		problems = append(problems, "missing archives: "+strings.Join(missing, ", "))
	}
	if len(duplicated) > 0 {
		problems = append(problems, "duplicate archives: "+strings.Join(duplicated, ", "))
	}
	if len(unexpected) > 0 {
		problems = append(problems, "unexpected archives: "+strings.Join(unexpected, ", "))
	}
	if len(problems) > 0 {
		return fmt.Errorf("release %s asset matrix is incomplete: %s", tag, strings.Join(problems, "; "))
	}

	return nil
}
