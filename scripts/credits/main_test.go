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
	"sort"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets/credits"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mitText = `MIT License

Copyright (c) 2020 Someone

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software, to deal in the Software without restriction.

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.`

const apacheText = `                                 Apache License
                           Version 2.0, January 2004`

const bsd3Text = `Redistribution and use in source and binary forms, with or without
modification, are permitted. Neither the name of the copyright holder nor
the names of its contributors may be used to endorse products.`

const bsd2Text = `Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met.`

func TestClassifyLicense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "MIT", text: mitText, want: []string{"MIT"}},
		{name: "Apache", text: apacheText, want: []string{"Apache-2.0"}},
		{name: "BSD-3-Clause", text: bsd3Text, want: []string{"BSD-3-Clause"}},
		{name: "BSD-2-Clause", text: bsd2Text, want: []string{"BSD-2-Clause"}},
		{
			name: "ISC",
			text: "Permission to use, copy, modify, and/or distribute this software for any purpose",
			want: []string{"ISC"},
		},
		{
			name: "Unlicense",
			text: "This is free and unencumbered software released into the public domain.",
			want: []string{"Unlicense"},
		},
		{
			name: "LGPL-3.0 is not also GPL",
			text: "GNU LESSER GENERAL PUBLIC LICENSE Version 3, 29 June 2007. " +
				"This version of the GNU Lesser General Public License incorporates the terms of " +
				"version 3 of the GNU General Public License",
			want: []string{"LGPL-3.0"},
		},
		{
			name: "LGPL-2.1",
			text: "GNU LESSER GENERAL PUBLIC LICENSE Version 2.1, February 1999",
			want: []string{"LGPL-2.1"},
		},
		{
			name: "MPL names GNU licenses as secondary licenses",
			text: "Mozilla Public License Version 2.0. Secondary License means the GNU General Public " +
				"License, Version 2.0, the GNU Lesser General Public License, Version 2.1",
			want: []string{"MPL-2.0"},
		},
		{name: "two licenses in one file", text: apacheText + "\n\n" + mitText, want: []string{"Apache-2.0", "MIT"}},
		{name: "unknown", text: "All rights reserved.", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, classifyLicense(tt.text))
		})
	}
}

func TestClassifyFileSkipsNoticesAndPatents(t *testing.T) {
	t.Parallel()

	assert.Nil(t, classifyFile("NOTICE", apacheText))
	assert.Nil(t, classifyFile("PATENTS", apacheText))
	assert.Equal(t, []string{"MIT"}, classifyFile("sub/LICENSE.txt", mitText))
}

func TestLicenseFilePattern(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"LICENSE", "LICENSE.md", "license.txt", "LICENCE", "COPYING", "COPYING.LESSER",
		"NOTICE", "NOTICE.md", "UNLICENSE", "PATENTS", "COPYRIGHT", "LICENSE-2.0.txt", "LICENSE_list",
	} {
		assert.True(t, licenseFilePattern.MatchString(name), name)
	}
	for _, name := range []string{"README.md", "licenses.go", "go.mod", "LICENSEE"} {
		assert.False(t, licenseFilePattern.MatchString(name), name)
	}
}

func TestModuleLicenseFilesFollowsLinkedPackages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, text string) {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	}
	write("LICENSE", mitText+"\r\n")
	write("used/vendored/LICENSE.txt", bsd3Text)
	write("used/vendored/code.go", "package vendored")
	write("unused/LICENSE", apacheText)

	files, err := moduleLicenseFiles(&linkedModule{
		dir:         root,
		packageDirs: map[string]bool{filepath.Join(root, "used", "vendored"): true},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"LICENSE", "used/vendored/LICENSE.txt"}, sortedKeys(files))
	assert.NotContains(t, files["LICENSE"], "\r", "line endings are normalized")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestParseContributors(t *testing.T) {
	t.Parallel()

	readme := `Intro
<!-- readme: contributors -start -->
<table><tr>
<td align="center">
    <a href="https://github.com/wizzomafizzo">
        <img src="x" alt="wizzomafizzo"/>
        <br />
        <sub><b>Callan Barrett</b></sub>
    </a>
</td>
<td align="center">
    <a href="https://github.com/no-name">
        <sub><b></b></sub>
    </a>
</td>
</tr></table>
<!-- readme: contributors -end -->
<a href="https://github.com/outside"><sub><b>Not A Contributor</b></sub></a>`

	contributors, err := parseContributors(readme)
	require.NoError(t, err)
	assert.Equal(t, []credits.Contributor{
		{Name: "Callan Barrett", Login: "wizzomafizzo"},
		{Name: "no-name", Login: "no-name"},
	}, contributors)

	_, err = parseContributors("no block here")
	require.Error(t, err)
}

func TestReadmeContributorsParsesRepositoryReadme(t *testing.T) {
	t.Parallel()

	contributors, err := readmeContributors(filepath.Join("..", "..", readmePath))
	require.NoError(t, err)
	assert.NotEmpty(t, contributors)
	assert.Equal(t, "wizzomafizzo", contributors[0].Login)
}

func TestDockerfileCommit(t *testing.T) {
	t.Parallel()

	dockerfile := "ENV LIBNFC_COMMIT=\"3fa0751ad58fb0053d3de2e61bbbd4066259104c\"\nENV OTHER=\"x\"\n"
	commit, err := dockerfileCommit(dockerfile, "LIBNFC_COMMIT")
	require.NoError(t, err)
	assert.Equal(t, "3fa0751ad58fb0053d3de2e61bbbd4066259104c", commit)

	_, err = dockerfileCommit(dockerfile, "LIBUSB_COMMIT")
	require.Error(t, err)
}

func TestSQLiteBlessing(t *testing.T) {
	t.Parallel()

	source := `/*
** 2001 September 15
**
** The author disclaims copyright to this source code.  In place of
** a legal notice, here is a blessing:
**
**    May you do good and not evil.
**    May you share freely, never taking more than you give.
**
*/`
	text, err := sqliteBlessing(source)
	require.NoError(t, err)
	assert.Equal(t, "The author disclaims copyright to this source code.  In place of\n"+
		"a legal notice, here is a blessing:\n\nMay you do good and not evil.\n"+
		"May you share freely, never taking more than you give.", text)

	_, err = sqliteBlessing("no dedication")
	require.Error(t, err)
}

func TestMiniaudioLicense(t *testing.T) {
	t.Parallel()

	text, err := miniaudioLicense("code\n/*\nThis software is available as a choice of the following licenses. " +
		"Choose\nwhichever you prefer.\nALTERNATIVE 1\n*/\n")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(text, "This software is available"))
	assert.True(t, strings.HasSuffix(text, "ALTERNATIVE 1\n"))

	_, err = miniaudioLicense("nothing")
	require.Error(t, err)
}

func TestCheckBundle(t *testing.T) {
	t.Parallel()

	bundle := &credits.Bundle{
		Texts: map[string]string{"a": mitText},
		Components: []credits.Component{{
			Name: "example.com/mod", Version: "v1.0.0", License: "MIT",
			Files: []credits.LicenseFile{{Name: "LICENSE", Text: "a"}},
		}},
	}
	encoded, err := credits.EncodeBundle(bundle)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), credits.ComponentsFile)
	require.NoError(t, os.WriteFile(path, encoded, 0o600))

	require.NoError(t, checkBundle(path, bundle))

	bumped := *bundle
	bumped.Components = []credits.Component{bundle.Components[0]}
	bumped.Components[0].Version = "v1.1.0"
	err = checkBundle(path, &bumped)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed example.com/mod")

	added := *bundle
	added.Components = append([]credits.Component{}, bundle.Components...)
	added.Components = append(added.Components, credits.Component{Name: "example.com/new", License: "MIT"})
	err = checkBundle(path, &added)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "added example.com/new")
}

func TestReleaseTargetsCoverEveryCommand(t *testing.T) {
	t.Parallel()

	targets, err := releaseTargets(filepath.Join("..", ".."))
	require.NoError(t, err)
	covered := make(map[string]bool)
	for _, target := range targets {
		for _, cmd := range target.cmds {
			covered[cmd] = true
		}
	}
	entries, err := os.ReadDir(filepath.Join("..", "..", "cmd"))
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() {
			assert.True(t, covered["./cmd/"+entry.Name()], "cmd/%s is not scanned", entry.Name())
		}
	}
}
