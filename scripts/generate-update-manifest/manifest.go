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
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

const (
	// currentManifestVersion is stamped into every generated manifest.
	// Clients reject a manifest declaring a version they do not understand,
	// so this only increases when a change is not backwards compatible.
	currentManifestVersion = 1

	githubReleaseDownloadBase = "https://github.com/ZaparooProject/zaparoo-core/releases/download"

	channelStable = "stable"
	channelBeta   = "beta"

	// fullRollout means every device is eligible for automatic installation.
	fullRollout = 100

	checksumsName    = "checksums.txt"
	checksumsSigName = "checksums.txt.sig"
	manifestName     = "manifest.yaml"
)

// manifest is the document published to the CDN. The releases, last_release_id
// and last_asset_id fields are dictated by go-selfupdate's HttpManifest format;
// everything else is a Zaparoo addition that older clients ignore because
// go-selfupdate decodes with a plain decoder rather than KnownFields(true).
//
//nolint:tagliatelle // yaml field names are dictated by go-selfupdate HttpManifest format
type manifest struct {
	IssuedAt        time.Time  `yaml:"issued_at"`
	KeyID           string     `yaml:"key_id"`
	Releases        []*release `yaml:"releases"`
	ManifestVersion int        `yaml:"manifest_version"`
	Generation      int64      `yaml:"generation"`
	LastReleaseID   int64      `yaml:"last_release_id"`
	LastAssetID     int64      `yaml:"last_asset_id"`
}

//nolint:tagliatelle // yaml field names are dictated by go-selfupdate HttpManifest format
type release struct {
	PublishedAt    time.Time `yaml:"published_at"`
	Name           string    `yaml:"name"`
	TagName        string    `yaml:"tag_name"`
	URL            string    `yaml:"url"`
	ReleaseNotes   string    `yaml:"release_notes"`
	Channel        string    `yaml:"channel"`
	MinUpgradeFrom string    `yaml:"min_upgrade_from,omitempty"`
	Assets         []*asset  `yaml:"assets"`
	ID             int64     `yaml:"id"`
	Rollout        int       `yaml:"rollout"`
	Draft          bool      `yaml:"draft"`
	Prerelease     bool      `yaml:"prerelease"`
}

type asset struct {
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	SHA256 string `yaml:"sha256,omitempty"`
	ID     int64  `yaml:"id"`
	Size   int64  `yaml:"size"`
}

var (
	errNoAssets      = errors.New("no release assets found")
	errReleaseExists = errors.New("release already present in manifest")
	errNoSuchRelease = errors.New("release not present in manifest")
)

// isUpdateArchive reports whether name is an installable release archive
// rather than a metadata file or an installer.
func isUpdateArchive(name string) bool {
	return strings.HasPrefix(name, "zaparoo-") &&
		(strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip"))
}

// isMetadataAsset reports whether name is one of the shared metadata files
// published alongside the manifest rather than a per-release archive.
func isMetadataAsset(name string) bool {
	return name == checksumsName || name == checksumsSigName
}

func channelForPrerelease(prerelease bool) string {
	if prerelease {
		return channelBeta
	}
	return channelStable
}

// loadManifest reads and normalizes an existing manifest.
func loadManifest(fs afero.Fs, path string) (*manifest, error) {
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return nil, fmt.Errorf("reading existing manifest: %w", err)
	}

	var m manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing existing manifest: %w", err)
	}
	normalizeManifest(&m)

	return &m, nil
}

// normalizeManifest repairs manifests written by earlier versions of this tool
// so later stages can assume every field is populated. Releases published
// before channels and rollouts existed are treated as fully rolled out, and
// releases published before checksums.txt.sig existed gain the asset entry.
func normalizeManifest(m *manifest) {
	lastAssetID := m.LastAssetID
	for _, rel := range m.Releases {
		for _, a := range rel.Assets {
			if a.ID > lastAssetID {
				lastAssetID = a.ID
			}
		}
	}

	for _, rel := range m.Releases {
		if rel.Channel == "" {
			rel.Channel = channelForPrerelease(rel.Prerelease)
			rel.Rollout = fullRollout
		}

		hasChecksums := false
		hasSignature := false
		for _, a := range rel.Assets {
			if isUpdateArchive(a.Name) && !strings.HasPrefix(a.URL, githubReleaseDownloadBase+"/") {
				a.URL = githubReleaseDownloadBase + "/" + rel.TagName + "/" + a.Name
			}
			if a.Name == checksumsName {
				hasChecksums = true
			}
			if a.Name == checksumsSigName {
				hasSignature = true
			}
		}
		if hasChecksums && !hasSignature {
			lastAssetID++
			rel.Assets = append(rel.Assets, &asset{
				ID:   lastAssetID,
				Name: checksumsSigName,
				URL:  checksumsSigName,
			})
		}
	}
	m.LastAssetID = lastAssetID
}

// writeManifest marshals the manifest to YAML and writes it to outputPath.
func writeManifest(fs afero.Fs, m *manifest, outputPath string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshalling manifest: %w", err)
	}

	if dir := filepath.Dir(outputPath); dir != "." {
		if mkdirErr := fs.MkdirAll(dir, 0o750); mkdirErr != nil {
			return fmt.Errorf("creating output directory: %w", mkdirErr)
		}
	}

	if writeErr := afero.WriteFile(fs, outputPath, data, 0o600); writeErr != nil {
		return fmt.Errorf("writing manifest: %w", writeErr)
	}

	return nil
}

// findRelease returns the release with the given tag, or nil.
func findRelease(m *manifest, tag string) *release {
	for _, rel := range m.Releases {
		if rel.TagName == tag {
			return rel
		}
	}
	return nil
}

// archiveAssets returns only the installable archives of a release.
func archiveAssets(rel *release) []*asset {
	out := make([]*asset, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		if isUpdateArchive(a.Name) {
			out = append(out, a)
		}
	}
	return out
}
