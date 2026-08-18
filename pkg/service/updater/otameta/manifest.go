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

// Package otameta reads the signed update manifest and decides which release
// archive a device is allowed to install from it.
//
// It is deliberately a leaf: standard library and a YAML decoder, nothing else.
// The publishing workflow runs the same selection code against the manifest it
// is about to sign, and that job checks out only this directory, so the client
// and CI cannot drift apart on what an installable asset looks like.
package otameta

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// CurrentManifestVersion is the highest manifest schema this build understands.
// A manifest declaring a higher version is rejected outright rather than parsed
// on a best-effort basis, because the fields a newer publisher relies on to
// keep a device safe may be ones this build silently ignores.
const CurrentManifestVersion = 1

const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

// ArchiveExtTarGz and ArchiveExtZip are the extensions release builds package
// update archives with. They live here because selection decides which one a
// platform gets and extraction has to agree with that decision.
const (
	ArchiveExtTarGz = ".tar.gz"
	ArchiveExtZip   = ".zip"
)

var archiveExts = []string{ArchiveExtTarGz, ArchiveExtZip}

var (
	// ErrNoAsset means no archive in the release is installable here. That is
	// routine — a platform added after the release was built has no archive in
	// it — so callers skip the release rather than failing the update.
	ErrNoAsset = errors.New("release has no installable archive for this platform")

	// ErrAmbiguousAsset means more than one archive matched. Nothing legitimate
	// produces two archives with the same name, so the release is not installed.
	ErrAmbiguousAsset = errors.New("release has more than one installable archive for this platform")

	ErrBadSignature      = errors.New("manifest signature verification failed")
	ErrManifestTooNew    = errors.New("manifest declares an unsupported schema version")
	ErrManifestMalformed = errors.New("manifest is malformed")
)

// Manifest is the update metadata document published to the CDN.
//
// The releases, last_release_id and last_asset_id fields are dictated by
// go-selfupdate's HttpManifest format. Everything else is a Zaparoo addition
// that clients predating it ignore, because go-selfupdate decodes with a plain
// decoder rather than KnownFields(true).
//
//nolint:tagliatelle // yaml field names are dictated by go-selfupdate HttpManifest format
type Manifest struct {
	IssuedAt        time.Time  `yaml:"issued_at"`
	KeyID           string     `yaml:"key_id"`
	Releases        []*Release `yaml:"releases"`
	ManifestVersion int        `yaml:"manifest_version"`
	Generation      int64      `yaml:"generation"`
	LastReleaseID   int64      `yaml:"last_release_id"`
	LastAssetID     int64      `yaml:"last_asset_id"`
}

//nolint:tagliatelle // yaml field names are dictated by go-selfupdate HttpManifest format
type Release struct {
	PublishedAt    time.Time `yaml:"published_at"`
	Name           string    `yaml:"name"`
	TagName        string    `yaml:"tag_name"`
	URL            string    `yaml:"url"`
	ReleaseNotes   string    `yaml:"release_notes"`
	Channel        string    `yaml:"channel"`
	MinUpgradeFrom string    `yaml:"min_upgrade_from,omitempty"`
	Assets         []*Asset  `yaml:"assets"`
	ID             int64     `yaml:"id"`
	Rollout        int       `yaml:"rollout"`
	Draft          bool      `yaml:"draft"`
	Prerelease     bool      `yaml:"prerelease"`
}

type Asset struct {
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	SHA256 string `yaml:"sha256,omitempty"`
	ID     int64  `yaml:"id"`
	Size   int64  `yaml:"size"`
}

// manifestHead is the sliver of the manifest read before the signature has been
// checked, so the right key can be chosen. It carries one attacker-influenced
// string, and the only thing that string can do is decide which of our keys
// must validate the document.
//
//nolint:tagliatelle // yaml field names are dictated by go-selfupdate HttpManifest format
type manifestHead struct {
	KeyID string `yaml:"key_id"`
}

// Verify checks the detached ed25519 signature over the exact manifest bytes
// and returns the parsed document. No wall clock is consulted anywhere in this
// path: devices without an RTC must be able to verify update metadata.
func Verify(data, sig []byte) (*Manifest, error) {
	return verifyWith(data, sig, PublicKey)
}

// verifyWith is Verify with the key source injected, so tests can sign with a
// key pair they generate rather than needing the production private key.
func verifyWith(data, sig []byte, lookup func(string) (ed25519.PublicKey, error)) (*Manifest, error) {
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: signature is %d bytes, want %d",
			ErrBadSignature, len(sig), ed25519.SignatureSize)
	}

	var head manifestHead
	if err := yaml.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("%w: reading key id: %w", ErrManifestMalformed, err)
	}
	if head.KeyID == "" {
		return nil, fmt.Errorf("%w: manifest names no signing key", ErrManifestMalformed)
	}

	key, err := lookup(head.KeyID)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(key, data, sig) {
		return nil, fmt.Errorf("%w with key %q", ErrBadSignature, head.KeyID)
	}

	m, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Parse decodes manifest bytes and rejects a schema this build cannot reason
// about. It performs no signature check; use Verify for anything fetched.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrManifestMalformed, err)
	}
	if m.ManifestVersion < 1 || m.ManifestVersion > CurrentManifestVersion {
		return nil, fmt.Errorf("%w: manifest_version %d, this build understands 1 to %d",
			ErrManifestTooNew, m.ManifestVersion, CurrentManifestVersion)
	}
	return &m, nil
}

// VersionFromTag strips the leading "v" that release tags carry but archive
// filenames do not.
func VersionFromTag(tag string) string {
	return strings.TrimPrefix(tag, "v")
}

// ArchiveBaseName returns the archive filename, minus its extension, that a
// release must publish to be installable on a platform and architecture.
func ArchiveBaseName(platformID, goarch, version string) string {
	return fmt.Sprintf("zaparoo-%s_%s-%s", platformID, goarch, version)
}

// SelectAsset returns the one archive in rel that this platform and
// architecture may install, and fails if there is not exactly one.
//
// The candidate names are built from the release's own tag and compared for
// equality, which is what binds an archive to the version it is published as.
// Metadata alone can no longer relabel a genuine older archive as a newer
// release: change the version and the names stop matching. Equality also means
// a separator is mandatory on both sides, so an arm device cannot pick up an
// arm64 archive, and a version containing punctuation is just a string rather
// than a pattern.
func SelectAsset(rel *Release, platformID, goarch string) (*Asset, error) {
	if rel == nil {
		return nil, fmt.Errorf("%w: no release", ErrNoAsset)
	}

	base := ArchiveBaseName(platformID, goarch, VersionFromTag(rel.TagName))

	var found *Asset
	for _, a := range rel.Assets {
		if a == nil || !isArchiveName(a.Name, base) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("%w: %s matches both %q and %q",
				ErrAmbiguousAsset, rel.TagName, found.Name, a.Name)
		}
		found = a
	}

	if found == nil {
		return nil, fmt.Errorf("%w: %s has no %s archive", ErrNoAsset, rel.TagName, base)
	}
	return found, nil
}

// isArchiveName reports whether name is base plus one of the archive
// extensions and nothing else.
func isArchiveName(name, base string) bool {
	for _, ext := range archiveExts {
		if name == base+ext {
			return true
		}
	}
	return false
}

// ArchiveExtension returns the extension of an archive name, which is what
// decides how it is unpacked. The list of extensions lives here so the client
// and the publisher cannot disagree about what an update archive is.
func ArchiveExtension(name string) (string, error) {
	for _, ext := range archiveExts {
		if strings.HasSuffix(name, ext) {
			return ext, nil
		}
	}
	return "", fmt.Errorf("%w: %q is not an update archive", ErrNoAsset, name)
}

// FindRelease returns the release carrying a tag, or nil.
func FindRelease(m *Manifest, tag string) *Release {
	if m == nil {
		return nil
	}
	for _, rel := range m.Releases {
		if rel != nil && rel.TagName == tag {
			return rel
		}
	}
	return nil
}
