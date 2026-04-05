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

var errNoAssets = errors.New("no release assets found in directory")

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

	return &m, nil
}

// buildManifest reads assetsDir for release files and returns a manifest.
// When merging with an existing manifest, IDs continue from the existing values.
func buildManifest(version, assetsDir, releaseNotes string, prerelease bool, existing *manifest) (*manifest, error) {
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		return nil, fmt.Errorf("reading assets directory: %w", err)
	}

	var startReleaseID int64
	var startAssetID int64
	if existing != nil {
		startReleaseID = existing.LastReleaseID
		startAssetID = existing.LastAssetID
	}

	var assets []*asset
	assetID := startAssetID

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == "manifest.yaml" {
			continue
		}

		if !strings.HasPrefix(name, "zaparoo-") && name != "checksums.txt" {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil, fmt.Errorf("getting file info for %s: %w", name, infoErr)
		}

		assetURL := version + "/" + name
		if name == "checksums.txt" {
			assetURL = name
		}

		assetID++
		assets = append(assets, &asset{
			ID:   assetID,
			Name: name,
			Size: info.Size(),
			URL:  assetURL,
		})
	}

	if len(assets) == 0 {
		return nil, errNoAssets
	}

	releaseID := startReleaseID + 1
	newRelease := &release{
		ID:           releaseID,
		Name:         version,
		TagName:      version,
		URL:          "",
		ReleaseNotes: releaseNotes,
		PublishedAt:  time.Now().UTC(),
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
	releaseNotes := flag.String("release-notes", "", "release notes text to include in manifest")
	output := flag.String("output", "manifest.yaml", "output manifest file path")
	prerelease := flag.Bool("prerelease", false, "mark release as pre-release in manifest")
	merge := flag.String("merge", "", "path to existing manifest to merge into")
	flag.Parse()

	if *version == "" || *assetsDir == "" {
		log.Fatal().Msg("usage: generate-update-manifest --version <tag> --assets-dir <dir> " +
			"[--output <path>] [--prerelease] [--merge <path>]")
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

	m, err := buildManifest(*version, *assetsDir, *releaseNotes, *prerelease, existing)
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
