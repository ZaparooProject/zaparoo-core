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
	"flag"
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

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	version := flag.String("version", "", "release version tag (e.g. v2.10.0)")
	assetsDir := flag.String("assets-dir", "", "directory containing release asset files")
	output := flag.String("output", "manifest.yaml", "output manifest file path")
	flag.Parse()

	if *version == "" || *assetsDir == "" {
		log.Fatal().Msg("usage: generate-update-manifest --version <tag> --assets-dir <dir> [--output <path>]")
	}

	entries, err := os.ReadDir(*assetsDir)
	if err != nil {
		log.Fatal().Err(err).Msg("error reading assets directory")
	}

	var assets []*asset
	var assetID int64

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
			log.Fatal().Err(infoErr).Str("file", name).Msg("error getting file info")
		}

		assetID++
		assets = append(assets, &asset{
			ID:   assetID,
			Name: name,
			Size: info.Size(),
			URL:  name,
		})
	}

	if len(assets) == 0 {
		log.Fatal().Msg("no release assets found in directory")
	}

	m := &manifest{
		LastReleaseID: 1,
		LastAssetID:   assetID,
		Releases: []*release{
			{
				ID:          1,
				Name:        *version,
				TagName:     *version,
				URL:         "",
				PublishedAt: time.Now().UTC(),
				Assets:      assets,
			},
		},
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		log.Fatal().Err(err).Msg("error marshalling manifest")
	}

	outputPath := *output
	if dir := filepath.Dir(outputPath); dir != "." {
		if mkdirErr := os.MkdirAll(dir, 0o750); mkdirErr != nil {
			log.Fatal().Err(mkdirErr).Msg("error creating output directory")
		}
	}

	if writeErr := os.WriteFile(outputPath, data, 0o600); writeErr != nil {
		log.Fatal().Err(writeErr).Msg("error writing manifest")
	}

	log.Info().Str("path", outputPath).Int("assets", len(assets)).Msg("manifest written")
}
