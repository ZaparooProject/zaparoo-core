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
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// notesTruncationMarker replaces the tail of release notes that are
	// trimmed for size. Only superseded releases are trimmed, so the notes a
	// client is about to show for an available update are always complete.
	notesTruncationMarker = "\n\n[Release notes truncated. See the GitHub release page for the full text.]"

	// checksumSeparator is the two-space separator sha256sum writes between
	// the digest and the filename.
	checksumSeparator = "  "

	// ed25519SignatureSize is the fixed size of a detached signature, used to
	// fill in the metadata asset size before the signature exists.
	ed25519SignatureSize = 64
)

// promoteOptions describes a release being added to the published manifest.
type promoteOptions struct {
	PublishedAt    time.Time
	Tag            string
	Channel        string
	MinUpgradeFrom string
	ReleaseNotes   string
	ReleaseURL     string
	Archives       []archiveFile
	Rollout        int
	Replace        bool
}

// checksumEntry is one line of a sha256sum-format checksums file.
type checksumEntry struct {
	Name   string
	SHA256 string
}

var (
	errGenerationNotAdvanced = errors.New("manifest generation did not advance")
	errInvalidRollout        = errors.New("rollout must be between 0 and 100")
	errManifestWouldBeEmpty  = errors.New("refusing to leave the manifest with no releases")
	errMissingDigest         = errors.New("retained release archive has no sha256 digest")
)

// stampManifest writes the fields identifying this publish: the monotonic
// generation clients use to reject a replayed manifest, the key it is signed
// with, and when it was issued. Callers must have validated the generation
// first. issued_at is informational - nothing expires, so no client needs a
// working clock to accept a manifest.
func stampManifest(m *manifest, generation int64, keyID string, now time.Time) {
	m.ManifestVersion = currentManifestVersion
	m.Generation = generation
	m.KeyID = keyID
	m.IssuedAt = now.UTC().Truncate(time.Second)
}

// validateGeneration enforces the monotonic counter that lets a client reject
// an older manifest replayed by a compromised or stale CDN.
func validateGeneration(previous, next int64) error {
	if next <= previous {
		return fmt.Errorf("%w: %d is not greater than the published generation %d",
			errGenerationNotAdvanced, next, previous)
	}
	return nil
}

// promote adds a release to the manifest, replacing an existing entry with the
// same tag only when explicitly asked to.
func promote(m *manifest, opts *promoteOptions) (*release, error) {
	if opts.Rollout < 0 || opts.Rollout > fullRollout {
		return nil, fmt.Errorf("%w, got %d", errInvalidRollout, opts.Rollout)
	}
	if len(opts.Archives) == 0 {
		return nil, fmt.Errorf("%w for %s", errNoAssets, opts.Tag)
	}

	existing := findRelease(m, opts.Tag)
	if existing != nil && !opts.Replace {
		return nil, fmt.Errorf("%w: %s", errReleaseExists, opts.Tag)
	}

	releaseID := m.LastReleaseID + 1
	if existing != nil {
		releaseID = existing.ID
	}

	assets, lastAssetID := buildReleaseAssets(opts.Tag, opts.Archives, m.LastAssetID)

	publishedAt := opts.PublishedAt.UTC()
	rel := &release{
		ID:             releaseID,
		Name:           opts.Tag,
		TagName:        opts.Tag,
		URL:            opts.ReleaseURL,
		ReleaseNotes:   opts.ReleaseNotes,
		PublishedAt:    publishedAt,
		Channel:        opts.Channel,
		Rollout:        opts.Rollout,
		MinUpgradeFrom: opts.MinUpgradeFrom,
		Prerelease:     opts.Channel == channelBeta,
		Assets:         assets,
	}

	if err := validateAssetMatrix(rel, opts.Tag); err != nil {
		return nil, err
	}

	if existing != nil {
		for i, r := range m.Releases {
			if r.TagName == opts.Tag {
				m.Releases[i] = rel
				break
			}
		}
	} else {
		m.Releases = append(m.Releases, rel)
		m.LastReleaseID = releaseID
	}
	m.LastAssetID = lastAssetID

	return rel, nil
}

// setRollout changes only the rollout percentage of an already published
// release. This is the fast path for halting an automatic rollout without
// re-downloading and re-hashing every archive.
func setRollout(m *manifest, tag string, rollout int) (*release, error) {
	if rollout < 0 || rollout > fullRollout {
		return nil, fmt.Errorf("%w, got %d", errInvalidRollout, rollout)
	}
	rel := findRelease(m, tag)
	if rel == nil {
		return nil, fmt.Errorf("%w: %s", errNoSuchRelease, tag)
	}
	rel.Rollout = rollout
	return rel, nil
}

// withdraw removes a release from the manifest entirely, which stops both
// automatic and manual installs of it.
func withdraw(m *manifest, tag string) (*release, error) {
	rel := findRelease(m, tag)
	if rel == nil {
		return nil, fmt.Errorf("%w: %s", errNoSuchRelease, tag)
	}
	if len(m.Releases) == 1 {
		return nil, errManifestWouldBeEmpty
	}

	kept := make([]*release, 0, len(m.Releases)-1)
	for _, r := range m.Releases {
		if r.TagName != tag {
			kept = append(kept, r)
		}
	}
	m.Releases = kept

	return rel, nil
}

// applyRetention keeps the newest releases per channel and trims the release
// notes of everything a client will not be offered, bounding manifest growth.
// It returns the tags that were dropped.
func applyRetention(m *manifest, retainStable, retainBeta, notesLimit int) []string {
	limits := map[string]int{channelStable: retainStable, channelBeta: retainBeta}

	byChannel := make(map[string][]*release, len(limits))
	for _, rel := range m.Releases {
		byChannel[rel.Channel] = append(byChannel[rel.Channel], rel)
	}

	keep := make(map[string]bool, len(m.Releases))
	newest := make(map[string]bool, len(limits))
	for channel, rels := range byChannel {
		sorted := make([]*release, len(rels))
		copy(sorted, rels)
		sort.SliceStable(sorted, func(i, j int) bool {
			if !sorted[i].PublishedAt.Equal(sorted[j].PublishedAt) {
				return sorted[i].PublishedAt.After(sorted[j].PublishedAt)
			}
			return sorted[i].TagName > sorted[j].TagName
		})

		limit, ok := limits[channel]
		if !ok || limit <= 0 {
			limit = len(sorted)
		}
		for i, rel := range sorted {
			if i >= limit {
				break
			}
			keep[rel.TagName] = true
			if i == 0 {
				newest[rel.TagName] = true
			}
		}
	}

	dropped := make([]string, 0, len(m.Releases))
	kept := make([]*release, 0, len(m.Releases))
	for _, rel := range m.Releases {
		if !keep[rel.TagName] {
			dropped = append(dropped, rel.TagName)
			continue
		}
		if notesLimit > 0 && !newest[rel.TagName] {
			rel.ReleaseNotes = truncateNotes(rel.ReleaseNotes, notesLimit)
		}
		kept = append(kept, rel)
	}
	m.Releases = kept

	sort.Strings(dropped)
	return dropped
}

// truncateNotes trims notes to at most limit runes, appending a marker so the
// truncation is visible rather than looking like the notes simply end.
func truncateNotes(notes string, limit int) string {
	runes := []rune(notes)
	if len(runes) <= limit {
		return notes
	}
	return strings.TrimRight(string(runes[:limit]), " \t\r\n") + notesTruncationMarker
}

// backfillDigests fills in per-asset digests for releases carried over from
// before the manifest recorded them, using the previously published and
// signature-verified checksums file.
func backfillDigests(m *manifest, entries []checksumEntry) {
	known := make(map[string]string, len(entries))
	for _, e := range entries {
		known[e.Name] = e.SHA256
	}
	for _, rel := range m.Releases {
		for _, a := range archiveAssets(rel) {
			if a.SHA256 == "" {
				a.SHA256 = known[a.Name]
			}
		}
	}
}

// requireDigests refuses to publish a manifest containing an archive no client
// can verify.
func requireDigests(m *manifest) error {
	var missing []string
	for _, rel := range m.Releases {
		for _, a := range archiveAssets(rel) {
			if a.SHA256 == "" {
				missing = append(missing, rel.TagName+"/"+a.Name)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: %s", errMissingDigest, strings.Join(missing, ", "))
	}
	return nil
}

// checksumsFromManifest derives the checksums file from the manifest digests,
// so the two documents can never disagree and pruned releases drop out of both.
func checksumsFromManifest(m *manifest) []checksumEntry {
	seen := make(map[string]bool)
	entries := make([]checksumEntry, 0, len(m.Releases)*24)
	for _, rel := range m.Releases {
		for _, a := range archiveAssets(rel) {
			if a.SHA256 == "" || seen[a.Name] {
				continue
			}
			seen[a.Name] = true
			entries = append(entries, checksumEntry{Name: a.Name, SHA256: a.SHA256})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func parseChecksums(data []byte) ([]checksumEntry, error) {
	var entries []checksumEntry
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == "" {
			continue
		}
		digest, name, ok := strings.Cut(trimmed, checksumSeparator)
		if !ok {
			return nil, fmt.Errorf("checksums line %d is not in sha256sum format: %q", i+1, trimmed)
		}
		name = strings.TrimPrefix(name, "*")
		if digest == "" || name == "" {
			return nil, fmt.Errorf("checksums line %d is missing a digest or filename: %q", i+1, trimmed)
		}
		entries = append(entries, checksumEntry{Name: name, SHA256: digest})
	}
	return entries, nil
}

func renderChecksums(entries []checksumEntry) []byte {
	var buf bytes.Buffer
	for _, e := range entries {
		_, _ = buf.WriteString(e.SHA256)
		_, _ = buf.WriteString(checksumSeparator)
		_, _ = buf.WriteString(e.Name)
		_ = buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// setMetadataAssetSizes records the size of the shared metadata files on every
// release entry. The checksums file is derived from the manifest, so its size
// is only known once the manifest content is final.
func setMetadataAssetSizes(m *manifest, checksumsSize int64) {
	for _, rel := range m.Releases {
		for _, a := range rel.Assets {
			switch a.Name {
			case checksumsName:
				a.Size = checksumsSize
			case checksumsSigName:
				a.Size = ed25519SignatureSize
			}
		}
	}
}
