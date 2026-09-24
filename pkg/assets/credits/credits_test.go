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

package credits

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedComponents(t *testing.T) {
	t.Parallel()

	bundle, err := Components()
	require.NoError(t, err)
	require.NotEmpty(t, bundle.Components)

	byName := make(map[string]*Component, len(bundle.Components))
	for i := range bundle.Components {
		component := &bundle.Components[i]
		byName[component.Name] = component
		assert.NotEmpty(t, component.License, "%s has no license label", component.Name)
		require.NotEmpty(t, component.Files, "%s has no license text", component.Name)
		for _, file := range component.Files {
			assert.NotEmpty(t, bundle.Texts[file.Text], "%s %s text is missing", component.Name, file.Name)
		}
	}

	for _, name := range []string{
		"libnfc", "libusb", "libusb-compat-0.1", "SQLite", "miniaudio", "valve-vdf-binary",
		"EFF Short Wordlist #1", "ArcadeDatabase_MiSTer", "Sound effects",
		"github.com/rivo/tview", "github.com/mattn/go-sqlite3",
	} {
		assert.Contains(t, byName, name)
	}
	assert.NotContains(t, byName, "github.com/stretchr/testify", "test-only modules are not shipped")
	assert.NotContains(t, byName, "github.com/ZaparooProject/zaparoo-core/mister", "first-party code is not credited")

	libnfc := byName["libnfc"]
	require.NotNil(t, libnfc)
	assert.Contains(t, libnfc.Note, "https://github.com/nfc-tools/libnfc/tree/")
	assert.Equal(t, "LGPL-3.0", libnfc.License)
}

func TestEmbeddedComponentsAreSorted(t *testing.T) {
	t.Parallel()

	bundle, err := Components()
	require.NoError(t, err)
	for i := 1; i < len(bundle.Components); i++ {
		assert.LessOrEqual(t,
			strings.ToLower(bundle.Components[i-1].Name), strings.ToLower(bundle.Components[i].Name))
	}
}

func TestEmbeddedContributors(t *testing.T) {
	t.Parallel()

	contributors, err := Contributors()
	require.NoError(t, err)
	require.NotEmpty(t, contributors)
	for _, contributor := range contributors {
		assert.NotEmpty(t, contributor.Name)
		assert.NotEmpty(t, contributor.Login)
	}
}

func TestEncodeDecodeBundle(t *testing.T) {
	t.Parallel()

	bundle := &Bundle{
		Texts: map[string]string{"a": "text"},
		Components: []Component{{
			Name: "example", Version: "v1", License: "MIT", Files: []LicenseFile{{Name: "LICENSE", Text: "a"}},
		}},
	}
	first, err := EncodeBundle(bundle)
	require.NoError(t, err)
	second, err := EncodeBundle(bundle)
	require.NoError(t, err)
	assert.Equal(t, first, second, "encoding is deterministic")

	decoded, err := DecodeBundle(first)
	require.NoError(t, err)
	assert.Equal(t, bundle, decoded)

	_, err = DecodeBundle([]byte("not gzip"))
	require.Error(t, err)
}

func TestNoticesWriteSharedTextsOnce(t *testing.T) {
	t.Parallel()

	bundle := &Bundle{
		Texts: map[string]string{"gpl": "GPL TEXT\n", "mit": "MIT TEXT\n"},
		Components: []Component{
			{
				Name: "one", Version: "v1", URL: "https://one", License: "GPL-3.0", Note: "A note.",
				Files: []LicenseFile{{Name: "LICENSE", Text: "gpl"}},
			},
			{
				Name: "two", License: "GPL-3.0 AND MIT",
				Files: []LicenseFile{{Name: "COPYING", Text: "gpl"}, {Name: "LICENSE.mit", Text: "mit"}},
			},
		},
	}
	notices := bundle.Notices()
	assert.Equal(t, 1, strings.Count(notices, "GPL TEXT"))
	assert.Contains(t, notices, "Identical to the LICENSE of one above.")
	assert.Contains(t, notices, "MIT TEXT")
	assert.Contains(t, notices, "one v1\nhttps://one\nLicense: GPL-3.0\nA note.\n")

	text := bundle.ComponentText(&bundle.Components[1], strings.ToLower)
	assert.Contains(t, text, "two\nLicense: GPL-3.0 AND MIT\n")
	assert.Contains(t, text, "--- COPYING ---\n\ngpl text\n")
	assert.Contains(t, text, "--- LICENSE.mit ---\n\nmit text\n")
}
