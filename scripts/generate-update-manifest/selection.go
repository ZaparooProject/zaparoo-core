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
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/spf13/afero"
)

var errSelectionMatrix = errors.New("manifest selection matrix check failed")

// verifySelectionMatrix runs the client's own asset selection over the manifest
// bytes about to be published, for every platform/arch pair the build produces.
// CI and the client share otameta, so the two cannot disagree about what a
// device will pick, and a release whose archives are misnamed fails the promote
// rather than the install.
//
// Ambiguity fails anywhere in the manifest: two archives a device could both
// install means the client has to guess, and it refuses to. A missing archive
// only fails on the newest release in each channel, because older entries
// legitimately predate platforms that did not exist when they were published.
func verifySelectionMatrix(fs afero.Fs, path string) (*result, error) {
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest to verify: %w", err)
	}

	m, err := otameta.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing manifest to verify: %w", err)
	}

	newest := newestPerChannel(m)
	targets := expectedTargets()

	problems := make([]string, 0)
	selections := 0

	for _, rel := range m.Releases {
		if rel == nil {
			continue
		}
		version := otameta.VersionFromTag(rel.TagName)
		required := newest[rel.TagName]

		for _, t := range targets {
			got, selErr := otameta.SelectAsset(rel, t.Platform, t.Arch)
			switch {
			case errors.Is(selErr, otameta.ErrNoAsset):
				if required {
					problems = append(problems, fmt.Sprintf(
						"%s: no archive for %s_%s", rel.TagName, t.Platform, t.Arch))
				}
			case selErr != nil:
				problems = append(problems, fmt.Sprintf(
					"%s: %s_%s: %v", rel.TagName, t.Platform, t.Arch, selErr))
			default:
				selections++
				// Only checked on the releases a device will actually be
				// offered; an archive that installs fine is not worth failing a
				// promote over just because an old release used the other
				// extension.
				if want := archiveName(t, version); required && got.Name != want {
					problems = append(problems, fmt.Sprintf(
						"%s: %s_%s selected %s, expected %s",
						rel.TagName, t.Platform, t.Arch, got.Name, want))
				}
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("%w:\n  %s", errSelectionMatrix, strings.Join(problems, "\n  "))
	}

	return &result{
		generation: m.Generation,
		releases:   len(m.Releases),
		selections: selections,
	}, nil
}

// newestPerChannel returns the tags of the release a device on each channel
// would be offered.
//
// The ordering is by semantic version, not by publish date, because that is
// what go-selfupdate does: it walks every release and keeps the highest version,
// regardless of the order they were published in. Ordering by date here would
// let a hotfix on an older line published after a newer release become "newest"
// in CI while devices were still being offered the newer one, and the newer
// one's asset matrix would then go unchecked. Retention orders by date instead,
// which is a different question — what to keep, not what to offer.
//
// A release missing this device's archive is deliberately still a candidate.
// go-selfupdate would skip it and fall back to an older release, which is the
// silent failure this check exists to catch: promoting a version most platforms
// get while one quietly stays behind.
func newestPerChannel(m *otameta.Manifest) map[string]bool {
	byChannel := make(map[string]*otameta.Release, 2)
	for _, rel := range m.Releases {
		// Drafts and unparseable tags are skipped by go-selfupdate, so a device
		// is never offered them and their matrix does not matter. The channel
		// grouping covers prereleases: releasesFor derives go-selfupdate's
		// prerelease flag from the channel, not from the manifest's own field.
		if rel == nil || rel.Draft {
			continue
		}
		if _, err := semver.NewVersion(otameta.VersionFromTag(rel.TagName)); err != nil {
			continue
		}
		if cur, ok := byChannel[rel.Channel]; !ok || newerRelease(rel, cur) {
			byChannel[rel.Channel] = rel
		}
	}

	newest := make(map[string]bool, len(byChannel))
	for _, rel := range byChannel {
		newest[rel.TagName] = true
	}
	return newest
}

// newerRelease reports whether a is the higher version. Both tags have already
// parsed, so a parse failure here cannot happen and orders conservatively.
func newerRelease(a, b *otameta.Release) bool {
	av, err := semver.NewVersion(otameta.VersionFromTag(a.TagName))
	if err != nil {
		return false
	}
	bv, err := semver.NewVersion(otameta.VersionFromTag(b.TagName))
	if err != nil {
		return true
	}
	return av.GreaterThan(bv)
}
