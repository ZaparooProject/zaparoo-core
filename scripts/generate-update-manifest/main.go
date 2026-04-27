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

// generate-update-manifest builds a manifest.yaml for go-selfupdate's
// HttpSource from a directory of release assets.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

//nolint:tagliatelle // yaml field names are dictated by go-selfupdate HttpManifest format
type manifest struct {
	Releases      []*release `yaml:"releases"`
	LastReleaseID int64      `yaml:"last_release_id"`
	LastAssetID   int64      `yaml:"last_asset_id"`
}

//nolint:tagliatelle // yaml field names are dictated by go-selfupdate HttpManifest format
type release struct {
	PublishedAt  time.Time `yaml:"published_at"`
	Name         string    `yaml:"name"`
	TagName      string    `yaml:"tag_name"`
	URL          string    `yaml:"url"`
	ReleaseNotes string    `yaml:"release_notes"`
	Assets       []*asset  `yaml:"assets"`
	ID           int64     `yaml:"id"`
	Draft        bool      `yaml:"draft"`
	Prerelease   bool      `yaml:"prerelease"`
}

type asset struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	ID   int64  `yaml:"id"`
	Size int64  `yaml:"size"`
}

type releaseAsset struct {
	Name string
	URL  string
	Size int64
}

type githubRelease struct {
	TagName     string        `json:"tagName"`
	URL         string        `json:"url"`
	PublishedAt time.Time     `json:"publishedAt"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

var errNoAssets = errors.New("no release assets found in directory")

const githubReleaseDownloadBase = "https://github.com/ZaparooProject/zaparoo-core/releases/download"

// loadManifest reads an existing manifest YAML file for merging.
func loadManifest(path string) (*manifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Path from CLI flag, not user input
	if err != nil {
		return nil, fmt.Errorf("reading existing manifest: %w", err)
	}

	var m manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing existing manifest: %w", err)
	}
	normalizeManifestForMerge(&m)

	return &m, nil
}

func normalizeManifestForMerge(m *manifest) {
	lastAssetID := m.LastAssetID
	for _, release := range m.Releases {
		for _, asset := range release.Assets {
			if asset.ID > lastAssetID {
				lastAssetID = asset.ID
			}
		}
	}

	for _, release := range m.Releases {
		hasChecksums := false
		hasSignature := false
		for _, asset := range release.Assets {
			if isUpdateArchive(asset.Name) && !strings.HasPrefix(asset.URL, githubReleaseDownloadBase+"/") {
				asset.URL = githubReleaseDownloadBase + "/" + release.TagName + "/" + asset.Name
			}
			if asset.Name == "checksums.txt" {
				hasChecksums = true
			}
			if asset.Name == "checksums.txt.sig" {
				hasSignature = true
			}
		}
		if hasChecksums && !hasSignature {
			lastAssetID++
			release.Assets = append(release.Assets, &asset{
				ID:   lastAssetID,
				Name: "checksums.txt.sig",
				URL:  "checksums.txt.sig",
			})
		}
	}
	m.LastAssetID = lastAssetID
}

// buildManifest reads assetsDir for release files and returns a manifest.
// When merging with an existing manifest, IDs continue from the existing values.
func buildManifest(version, assetsDir, releaseNotes string, prerelease bool, existing *manifest) (*manifest, error) {
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		return nil, fmt.Errorf("reading assets directory: %w", err)
	}

	var releaseAssets []releaseAsset

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == "manifest.yaml" {
			continue
		}

		if !strings.HasPrefix(name, "zaparoo-") && name != "checksums.txt" && name != "checksums.txt.sig" {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil, fmt.Errorf("getting file info for %s: %w", name, infoErr)
		}

		assetURL := version + "/" + name
		if name == "checksums.txt" || name == "checksums.txt.sig" {
			assetURL = name
		}

		releaseAssets = append(releaseAssets, releaseAsset{
			Name: name,
			Size: info.Size(),
			URL:  assetURL,
		})
	}

	return buildManifestFromAssets(version, "", time.Now().UTC(), releaseNotes, prerelease, releaseAssets, existing)
}

// buildManifestFromAssets returns a manifest from already-resolved asset metadata.
// When merging with an existing manifest, IDs continue from the existing values.
func buildManifestFromAssets(
	version string,
	releaseURL string,
	publishedAt time.Time,
	releaseNotes string,
	prerelease bool,
	releaseAssets []releaseAsset,
	existing *manifest,
) (*manifest, error) {
	var startReleaseID int64
	var startAssetID int64
	if existing != nil {
		startReleaseID = existing.LastReleaseID
		startAssetID = existing.LastAssetID
	}

	assets := make([]*asset, 0, len(releaseAssets))
	assetID := startAssetID
	for _, releaseAsset := range releaseAssets {
		assetID++
		assets = append(assets, &asset{
			ID:   assetID,
			Name: releaseAsset.Name,
			Size: releaseAsset.Size,
			URL:  releaseAsset.URL,
		})
	}

	if len(assets) == 0 {
		return nil, errNoAssets
	}
	if publishedAt.IsZero() {
		publishedAt = time.Now().UTC()
	}

	releaseID := startReleaseID + 1
	newRelease := &release{
		ID:           releaseID,
		Name:         version,
		TagName:      version,
		URL:          releaseURL,
		ReleaseNotes: releaseNotes,
		PublishedAt:  publishedAt.UTC(),
		Assets:       assets,
		Prerelease:   prerelease,
	}

	var releases []*release
	if existing != nil {
		releases = append(releases, existing.Releases...)
	}
	releases = append(releases, newRelease)

	return &manifest{
		LastReleaseID: releaseID,
		LastAssetID:   assetID,
		Releases:      releases,
	}, nil
}

func loadGithubRelease(path string) (*githubRelease, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Path from CLI flag, not user input
	if err != nil {
		return nil, fmt.Errorf("reading GitHub release metadata: %w", err)
	}

	var release githubRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return nil, fmt.Errorf("parsing GitHub release metadata: %w", err)
	}

	return &release, nil
}

func isUpdateArchive(name string) bool {
	return strings.HasPrefix(name, "zaparoo-") &&
		(strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip"))
}

func assetsFromGithubRelease(release *githubRelease, metadataDir string) ([]releaseAsset, error) {
	assets := make([]releaseAsset, 0, len(release.Assets)+2)
	for _, githubAsset := range release.Assets {
		if !isUpdateArchive(githubAsset.Name) {
			continue
		}
		if githubAsset.URL == "" {
			return nil, fmt.Errorf("GitHub asset %s has no download URL", githubAsset.Name)
		}
		assets = append(assets, releaseAsset(githubAsset))
	}

	metadataFiles := []string{"checksums.txt", "checksums.txt.sig"}
	for _, name := range metadataFiles {
		info, err := os.Stat(filepath.Join(metadataDir, name))
		if err != nil {
			return nil, fmt.Errorf("reading metadata asset %s: %w", name, err)
		}
		assets = append(assets, releaseAsset{
			Name: name,
			URL:  name,
			Size: info.Size(),
		})
	}

	return assets, nil
}

// writeManifest marshals the manifest to YAML and writes it to outputPath.
func writeManifest(m *manifest, outputPath string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshalling manifest: %w", err)
	}

	if dir := filepath.Dir(outputPath); dir != "." {
		if mkdirErr := os.MkdirAll(dir, 0o750); mkdirErr != nil {
			return fmt.Errorf("creating output directory: %w", mkdirErr)
		}
	}

	if writeErr := os.WriteFile(outputPath, data, 0o600); writeErr != nil {
		return fmt.Errorf("writing manifest: %w", writeErr)
	}

	return nil
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	version := flag.String("version", "", "release version tag (e.g. v2.10.0)")
	assetsDir := flag.String("assets-dir", "", "directory containing release asset files")
	githubReleasePath := flag.String("github-release", "", "GitHub release metadata JSON from gh release view")
	metadataDir := flag.String("metadata-dir", "", "directory containing checksums.txt and checksums.txt.sig")
	releaseNotes := flag.String("release-notes", "", "release notes text to include in manifest")
	output := flag.String("output", "manifest.yaml", "output manifest file path")
	prerelease := flag.Bool("prerelease", false, "mark release as pre-release in manifest")
	merge := flag.String("merge", "", "path to existing manifest to merge into")
	flag.Parse()

	if *version == "" || (*assetsDir == "" && *githubReleasePath == "") {
		log.Fatal().Msg("usage: generate-update-manifest --version <tag> --assets-dir <dir> " +
			"[--github-release <path> --metadata-dir <dir>] [--output <path>] [--prerelease] [--merge <path>]")
	}
	if *githubReleasePath != "" && *metadataDir == "" {
		log.Fatal().Msg("--metadata-dir is required with --github-release")
	}

	var existing *manifest
	if *merge != "" {
		var err error
		existing, err = loadManifest(*merge)
		if err != nil {
			log.Fatal().Err(err).Msg("error loading existing manifest for merge")
		}
		log.Info().Int("releases", len(existing.Releases)).Msg("loaded existing manifest for merge")
	}

	var m *manifest
	var err error
	if *githubReleasePath != "" {
		release, loadErr := loadGithubRelease(*githubReleasePath)
		if loadErr != nil {
			log.Fatal().Err(loadErr).Msg("error loading GitHub release metadata")
		}
		if release.TagName != "" && release.TagName != *version {
			log.Fatal().
				Str("metadata_tag", release.TagName).
				Str("version", *version).
				Msg("GitHub release metadata tag mismatch")
		}
		assets, assetsErr := assetsFromGithubRelease(release, *metadataDir)
		if assetsErr != nil {
			log.Fatal().Err(assetsErr).Msg("error loading GitHub release assets")
		}
		m, err = buildManifestFromAssets(
			*version,
			release.URL,
			release.PublishedAt,
			*releaseNotes,
			*prerelease,
			assets,
			existing,
		)
	} else {
		m, err = buildManifest(*version, *assetsDir, *releaseNotes, *prerelease, existing)
	}
	if err != nil {
		log.Fatal().Err(err).Msg("error building manifest")
	}

	if err := writeManifest(m, *output); err != nil {
		log.Fatal().Err(err).Msg("error writing manifest")
	}

	log.Info().
		Str("path", *output).
		Int("releases", len(m.Releases)).
		Int("new_assets", len(m.Releases[len(m.Releases)-1].Assets)).
		Msg("manifest written")
}
