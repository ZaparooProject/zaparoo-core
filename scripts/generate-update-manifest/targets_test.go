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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// buildWorkflow is the subset of .github/workflows/build.yml needed to recover
// the release build matrix.
type buildWorkflow struct {
	Jobs buildJobs `yaml:"jobs"`
}

type buildJobs struct {
	Build buildJob `yaml:"build"`
}

type buildJob struct {
	Strategy buildStrategy `yaml:"strategy"`
}

type buildStrategy struct {
	Matrix buildMatrix `yaml:"matrix"`
}

type buildMatrix struct {
	Platform []string           `yaml:"platform"`
	Arch     []string           `yaml:"arch"`
	Include  []buildMatrixEntry `yaml:"include"`
}

type buildMatrixEntry struct {
	Platform string `yaml:"platform"`
	Arch     string `yaml:"arch"`
}

// TestExpectedTargets_MatchesBuildWorkflow keeps the checked-in target list
// honest. If a platform is added to the build matrix without being added here,
// promotion would silently accept a release missing that platform's archive.
func TestExpectedTargets_MatchesBuildWorkflow(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", ".github", "workflows", "build.yml")
	data, err := os.ReadFile(path) //nolint:gosec // fixed path to a repository file
	require.NoError(t, err)

	var wf buildWorkflow
	require.NoError(t, yaml.Unmarshal(data, &wf))

	matrix := wf.Jobs.Build.Strategy.Matrix
	require.NotEmpty(t, matrix.Platform, "could not read the build matrix from build.yml")

	fromWorkflow := make(map[string]bool)
	for _, platform := range matrix.Platform {
		for _, arch := range matrix.Arch {
			fromWorkflow[platform+"_"+arch] = true
		}
	}
	for _, inc := range matrix.Include {
		fromWorkflow[inc.Platform+"_"+inc.Arch] = true
	}

	fromCode := make(map[string]bool)
	for _, target := range expectedTargets() {
		fromCode[target.Platform+"_"+target.Arch] = true
	}

	assert.Equal(t, keysOf(fromWorkflow), keysOf(fromCode),
		"expectedTargets() is out of step with the build matrix in build.yml")

	// The extension is half of the archive name validateAssetMatrix expects, so
	// it has to track build.yml's case statement too, not just the pair list.
	zipFromWorkflow := zipPlatformsFromWorkflow(t, string(data))
	for _, target := range expectedTargets() {
		wantExt := "tar.gz"
		if zipFromWorkflow[target.Platform] {
			wantExt = "zip"
		}
		assert.Equal(t, wantExt, target.Ext,
			"%s_%s archive extension is out of step with build.yml", target.Platform, target.Arch)
	}
}

// zipPlatformsFromWorkflow recovers the set of platforms build.yml archives as
// zip from the case statement that picks the extension. Windows is built by a
// separate job that names its own zip, so it is added here rather than parsed.
func zipPlatformsFromWorkflow(t *testing.T, workflow string) map[string]bool {
	t.Helper()

	match := regexp.MustCompile(`(?m)^\s+([a-z|]+)\)\s*\n\s+EXT="zip"`).FindStringSubmatch(workflow)
	require.NotNil(t, match, "could not find the zip extension case in build.yml")

	zip := map[string]bool{"windows": true}
	for _, platform := range strings.Split(match[1], "|") {
		zip[platform] = true
	}
	return zip
}

func keysOf(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestArchiveName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		target target
		want   string
	}{
		{target: target{Platform: "linux", Arch: "amd64", Ext: "tar.gz"}, want: "zaparoo-linux_amd64-2.16.1.tar.gz"},
		{target: target{Platform: "mister", Arch: "arm", Ext: "zip"}, want: "zaparoo-mister_arm-2.16.1.zip"},
		{target: target{Platform: "windows", Arch: "386", Ext: "zip"}, want: "zaparoo-windows_386-2.16.1.zip"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, archiveName(tt.target, versionFromTag("v2.16.1")))
		})
	}
}

func TestVersionFromTag(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2.16.1", versionFromTag("v2.16.1"))
	assert.Equal(t, "2.17.0-beta1", versionFromTag("v2.17.0-beta1"))
	assert.Equal(t, "2.16.1", versionFromTag("2.16.1"))
}

func TestValidateAssetMatrix_Complete(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})
	require.NoError(t, validateAssetMatrix(m.Releases[0], "v2.16.1"))
}

func TestValidateAssetMatrix_MissingArchive(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})
	rel := m.Releases[0]

	kept := make([]*asset, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		if a.Name != "zaparoo-mister_arm-2.16.1.zip" {
			kept = append(kept, a)
		}
	}
	rel.Assets = kept

	err := validateAssetMatrix(rel, "v2.16.1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing archives: zaparoo-mister_arm-2.16.1.zip")
}

func TestValidateAssetMatrix_UnexpectedArchive(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})
	rel := m.Releases[0]
	rel.Assets = append(rel.Assets, &asset{Name: "zaparoo-vita_arm-2.16.1.tar.gz", SHA256: "deadbeef"})

	err := validateAssetMatrix(rel, "v2.16.1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected archives: zaparoo-vita_arm-2.16.1.tar.gz")
}

// TestValidateAssetMatrix_WrongVersionInName catches an archive built from the
// wrong tag being uploaded to a release.
func TestValidateAssetMatrix_WrongVersionInName(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})
	err := validateAssetMatrix(m.Releases[0], "v2.16.2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing archives")
	assert.Contains(t, err.Error(), "unexpected archives")
}
