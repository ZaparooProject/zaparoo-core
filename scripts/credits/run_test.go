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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets/credits"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repoRoot is the Core checkout, relative to this package.
var repoRoot = filepath.Join("..", "..")

func readCommittedBundle(t *testing.T) *credits.Bundle {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, outputDir, credits.ComponentsFile)) //nolint:gosec // repo path
	require.NoError(t, err)
	bundle, err := credits.DecodeBundle(data)
	require.NoError(t, err)
	return bundle
}

func TestRunCheckMatchesCommittedData(t *testing.T) {
	t.Parallel()

	require.NoError(t, run(repoRoot, filepath.Join(repoRoot, outputDir), true))
}

func TestRunWritesData(t *testing.T) {
	t.Parallel()

	out := t.TempDir()
	require.NoError(t, run(repoRoot, out, false))

	data, err := os.ReadFile(filepath.Join(out, credits.ComponentsFile)) //nolint:gosec // test temp dir
	require.NoError(t, err)
	written, err := credits.DecodeBundle(data)
	require.NoError(t, err)
	assert.Equal(t, readCommittedBundle(t), written, "a fresh run reproduces the committed bundle")

	contributors, err := os.ReadFile(filepath.Join(out, credits.ContributorsFile)) //nolint:gosec // test temp dir
	require.NoError(t, err)
	assert.Contains(t, string(contributors), `"login": "wizzomafizzo"`)

	require.NoError(t, run(repoRoot, out, true), "the written data passes the check")
}

func TestRunCheckFailsOnStaleData(t *testing.T) {
	t.Parallel()

	out := t.TempDir()
	stale, err := credits.EncodeBundle(&credits.Bundle{Texts: map[string]string{}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(out, credits.ComponentsFile), stale, 0o600))

	err = run(repoRoot, out, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "added libnfc")
	assert.Contains(t, err.Error(), "task credits")
}

func TestRunFailsWithoutCommands(t *testing.T) {
	t.Parallel()

	require.Error(t, run(t.TempDir(), t.TempDir(), true))
}

func TestCheckBundleErrors(t *testing.T) {
	t.Parallel()

	fresh := &credits.Bundle{Texts: map[string]string{}}
	require.Error(t, checkBundle(filepath.Join(t.TempDir(), "missing.json.gz"), fresh))

	corrupt := filepath.Join(t.TempDir(), credits.ComponentsFile)
	require.NoError(t, os.WriteFile(corrupt, []byte("not gzip"), 0o600))
	require.Error(t, checkBundle(corrupt, fresh))
}

func TestDescribeDiff(t *testing.T) {
	t.Parallel()

	one := credits.Component{Name: "one", License: "MIT"}
	two := credits.Component{Name: "two", License: "MIT"}
	before := &credits.Bundle{Texts: map[string]string{}, Components: []credits.Component{one, two}}
	after := &credits.Bundle{Texts: map[string]string{}, Components: []credits.Component{one}}
	assert.Equal(t, "removed two", describeDiff(before, after))

	onlyTexts := &credits.Bundle{Texts: map[string]string{"x": "unused"}, Components: before.Components}
	assert.Equal(t, "license texts changed", describeDiff(before, onlyTexts))
}

func TestReadmeContributorsErrors(t *testing.T) {
	t.Parallel()

	_, err := readmeContributors(filepath.Join(t.TempDir(), "README.md"))
	require.Error(t, err)

	_, err = parseContributors("<!-- readme: contributors -start --><!-- readme: contributors -end -->")
	require.Error(t, err)
}

// fakeCheckout is a minimal Core checkout and module cache holding every
// file extras reads.
type fakeCheckout struct {
	modules map[string]*linkedModule
	root    string
}

const fakeSQLite = `/*
** The author disclaims copyright to this source code.  In place of
** a legal notice, here is a blessing:
**
**    May you share freely, never taking more than you give.
*/`

const fakeMiniaudio = "/*\nThis software is available as a choice of the following licenses.\nALT 1\n*/\n"

func newFakeCheckout(t *testing.T) *fakeCheckout {
	t.Helper()
	root := t.TempDir()
	write := func(rel, text string) {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	}
	write("LICENSE", "GNU GENERAL PUBLIC LICENSE Version 3")
	write(dockerfilePath, `ENV LIBUSB_COMMIT="a45bb163a603ac5ae3499806b151509848b0065c"
ENV LIBUSB_COMPAT_COMMIT="4831748a1439aec81e9cbb9984b1b0289c1c7051"
ENV LIBNFC_COMMIT="3fa0751ad58fb0053d3de2e61bbbd4066259104c"
`)
	for _, lib := range nativeLibraries {
		write(filepath.Join(textsDir, lib.textFile), "GNU LESSER GENERAL PUBLIC LICENSE")
	}
	write(filepath.Join(textsDir, "eff-wordlist.txt"), "EFF attribution")
	write("internal/vdfbinary/LICENSE", mitText)
	write("mod/sqlite/sqlite3-binding.c", fakeSQLite)
	write("mod/sqlite/LICENSE", mitText)
	write("mod/malgo/miniaudio.h", fakeMiniaudio)
	write("mod/malgo/LICENSE", "This is free and unencumbered software released into the public domain.")

	module := func(path, dir string) *linkedModule {
		full := filepath.Join(root, dir)
		return &linkedModule{path: path, version: "v1.0.0", dir: full, packageDirs: map[string]bool{full: true}}
	}
	return &fakeCheckout{
		root: root,
		modules: map[string]*linkedModule{
			sqliteModule: module(sqliteModule, "mod/sqlite"),
			malgoModule:  module(malgoModule, "mod/malgo"),
		},
	}
}

func TestBuildBundleFromFakeCheckout(t *testing.T) {
	t.Parallel()

	checkout := newFakeCheckout(t)
	checkout.modules[sqliteModule].replacedBy = "github.com/example/fork"
	bundle, err := buildBundle(checkout.root, checkout.modules)
	require.NoError(t, err)

	byName := make(map[string]credits.Component)
	for _, component := range bundle.Components {
		byName[component.Name] = component
	}
	for _, name := range []string{
		sqliteModule, malgoModule, "libnfc", "libusb", "libusb-compat-0.1", "SQLite", "miniaudio",
		"valve-vdf-binary", "EFF Short Wordlist #1", "ArcadeDatabase_MiSTer", "Sound effects",
	} {
		assert.Contains(t, byName, name)
	}
	assert.Equal(t, "Built from the fork github.com/example/fork.", byName[sqliteModule].Note)
	assert.Equal(t, "MIT", byName[sqliteModule].License)
	assert.Equal(t, "3fa0751ad58f", byName["libnfc"].Version)
	assert.Len(t, byName["libnfc"].Files, 2, "libnfc carries the GPL text LGPL-3.0 builds on")
	assert.Len(t, byName["libusb"].Files, 1)
	assert.Contains(t, bundle.Texts[byName["SQLite"].Files[0].Text], "May you share freely")
	assert.True(t, strings.HasPrefix(bundle.Texts[byName["miniaudio"].Files[0].Text], "This software is available"))
}

func TestBuildBundleRejectsUnknownAndMissingLicenses(t *testing.T) {
	t.Parallel()

	checkout := newFakeCheckout(t)
	unknownDir := filepath.Join(checkout.root, "mod", "unknown")
	require.NoError(t, os.MkdirAll(unknownDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(unknownDir, "LICENSE"), []byte("All rights reserved."), 0o600))
	checkout.modules["example.com/unknown"] = &linkedModule{
		path: "example.com/unknown", dir: unknownDir, packageDirs: map[string]bool{unknownDir: true},
	}
	_, err := buildBundle(checkout.root, checkout.modules)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot identify the license of example.com/unknown")

	bare := newFakeCheckout(t)
	bareDir := filepath.Join(bare.root, "mod", "bare")
	require.NoError(t, os.MkdirAll(bareDir, 0o750))
	bare.modules["example.com/bare"] = &linkedModule{
		path: "example.com/bare", dir: bareDir, packageDirs: map[string]bool{bareDir: true},
	}
	_, err = buildBundle(bare.root, bare.modules)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "module example.com/bare has no license file")
}

func TestExtrasFailWhenInputsAreMissing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate func(t *testing.T, checkout *fakeCheckout)
		name   string
		want   string
	}{
		{name: "Core license", want: "LICENSE", mutate: func(t *testing.T, c *fakeCheckout) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(c.root, "LICENSE")))
		}},
		{name: "Dockerfile", want: dockerfilePath, mutate: func(t *testing.T, c *fakeCheckout) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(c.root, dockerfilePath)))
		}},
		{name: "pinned commit", want: "LIBNFC_COMMIT not pinned", mutate: func(t *testing.T, c *fakeCheckout) {
			t.Helper()
			require.NoError(t, os.WriteFile(filepath.Join(c.root, dockerfilePath), []byte("FROM x\n"), 0o600))
		}},
		{name: "native license text", want: "libnfc-COPYING", mutate: func(t *testing.T, c *fakeCheckout) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(c.root, textsDir, "libnfc-COPYING")))
		}},
		{name: "SQLite module", want: "is not linked", mutate: func(_ *testing.T, c *fakeCheckout) {
			delete(c.modules, sqliteModule)
		}},
		{name: "SQLite source", want: "sqlite3-binding.c", mutate: func(t *testing.T, c *fakeCheckout) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(c.modules[sqliteModule].dir, "sqlite3-binding.c")))
		}},
		{name: "SQLite dedication", want: "SQLite dedication", mutate: func(t *testing.T, c *fakeCheckout) {
			t.Helper()
			path := filepath.Join(c.modules[sqliteModule].dir, "sqlite3-binding.c")
			require.NoError(t, os.WriteFile(path, []byte("int x;"), 0o600))
		}},
		{name: "miniaudio module", want: "is not linked", mutate: func(_ *testing.T, c *fakeCheckout) {
			delete(c.modules, malgoModule)
		}},
		{name: "vendored license", want: "internal/vdfbinary/LICENSE", mutate: func(t *testing.T, c *fakeCheckout) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(c.root, "internal", "vdfbinary", "LICENSE")))
		}},
		{name: "EFF attribution", want: "eff-wordlist.txt", mutate: func(t *testing.T, c *fakeCheckout) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(c.root, textsDir, "eff-wordlist.txt")))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkout := newFakeCheckout(t)
			tt.mutate(t, checkout)
			_, err := extras(checkout.root, checkout.modules)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestMiniaudioLicenseNeedsCommentEnd(t *testing.T) {
	t.Parallel()

	_, err := miniaudioLicense("This software is available as a choice of the following licenses.")
	require.Error(t, err)
	_, err = sqliteBlessing("The author disclaims copyright to this source code.")
	require.Error(t, err)
}

// TestListEnvDropsCToolchain pins that a cross build's compiler settings do
// not reach go list: the MiSTer build points CC at an ARM compiler.
func TestListEnvDropsCToolchain(t *testing.T) {
	t.Setenv("CC", "zig cc -target arm-linux-gnueabihf")
	t.Setenv("CGO_LDFLAGS", "-lnfc")
	t.Setenv("CREDITS_TEST_KEEP", "1")

	env := listEnv()
	assert.Contains(t, env, "CREDITS_TEST_KEEP=1")
	for _, entry := range env {
		assert.False(t, strings.HasPrefix(entry, "CC="), entry)
		assert.False(t, strings.HasPrefix(entry, "CGO_LDFLAGS="), entry)
	}
}
