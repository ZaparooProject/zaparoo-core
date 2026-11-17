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

package methods

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/hostfiles"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newFilesTestEnv is the user fields test env with one index root that holds
// a folder of covers, and a private folder outside every root.
func newFilesTestEnv(t *testing.T) (e *userFieldsTestEnv, rootID, private string) {
	t.Helper()
	e = newUserFieldsTestEnv(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	games := filepath.Join(base, "games")
	private = filepath.Join(base, "private", "secret.png")
	for path, data := range map[string][]byte{
		filepath.Join(games, "covers", "metroid.png"): testPNG(t, 12),
		filepath.Join(games, "covers", "zelda.png"):   testPNG(t, 14),
		filepath.Join(games, "covers", "readme.txt"):  []byte("not an image"),
		filepath.Join(games, "covers", "fake.png"):    []byte("not an image either"),
		private: testPNG(t, 16),
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, data, 0o600))
	}

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: t.TempDir(), HostManagedPaths: true}).Maybe()
	pl.On("Scrapers", mock.Anything).Return(map[string]platforms.Scraper{}).Maybe()
	pl.On("RootDirs", mock.Anything).Return([]string{games}).Maybe()
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	e.env.Platform, e.env.Config = pl, cfg
	e.env.Database.UserImages = NewUserImages(pl)

	result, err := HandleFilesBrowse(withParams(&e.env, `{"purpose":"image"}`))
	require.NoError(t, err)
	roots, ok := result.(models.FilesBrowseResponse)
	require.True(t, ok)
	require.Len(t, roots.Roots, 1, "the image folder does not exist yet, so only the index root is offered")
	assert.Equal(t, games, roots.Roots[0].Name)
	return e, roots.Roots[0].ID, private
}

func TestHandleFilesBrowse_ListsAndPagesAFolder(t *testing.T) {
	t.Parallel()
	e, rootID, _ := newFilesTestEnv(t)

	result, err := HandleFilesBrowse(withParams(&e.env, fmt.Sprintf(`{"purpose":"image","root":%q}`, rootID)))
	require.NoError(t, err)
	top, ok := result.(models.FilesBrowseResponse)
	require.True(t, ok)
	require.Len(t, top.Entries, 1)
	assert.True(t, top.Entries[0].IsDir)
	assert.Equal(t, "covers", top.Entries[0].Path)

	result, err = HandleFilesBrowse(withParams(&e.env,
		fmt.Sprintf(`{"purpose":"image","root":%q,"path":"covers","offset":1,"limit":1}`, rootID)))
	require.NoError(t, err)
	page, ok := result.(models.FilesBrowseResponse)
	require.True(t, ok)
	assert.Equal(t, 3, page.Total, "text files are not listed")
	require.Len(t, page.Entries, 1)
	assert.Equal(t, "covers/metroid.png", page.Entries[0].Path)
	assert.NotEmpty(t, page.Entries[0].Modified)
	assert.Positive(t, page.Entries[0].Size)
}

func TestHandleFilesBrowse_RefusesStrangersAndEscapes(t *testing.T) {
	t.Parallel()
	e, rootID, _ := newFilesTestEnv(t)

	for _, path := range []string{"..", "../private", "covers/../../private"} {
		_, err := HandleFilesBrowse(withParams(&e.env,
			fmt.Sprintf(`{"purpose":"image","root":%q,"path":%q}`, rootID, path)))
		require.ErrorIs(t, err, hostfiles.ErrInvalidPath, "path %q", path)
	}
	_, err := HandleFilesBrowse(withParams(&e.env, `{"purpose":"image","root":"nope"}`))
	require.ErrorIs(t, err, hostfiles.ErrUnknownRoot)
	_, err = HandleFilesBrowse(withParams(&e.env, `{"purpose":"everything"}`))
	require.Error(t, err, "only the image purpose exists")

	e.env.IsLocal = false
	_, err = HandleFilesBrowse(withParams(&e.env, `{"purpose":"image"}`))
	require.ErrorIs(t, err, ErrForbidden)
	_, err = HandleFilesImage(withParams(&e.env, fmt.Sprintf(`{"root":%q,"path":"covers/metroid.png"}`, rootID)))
	require.ErrorIs(t, err, ErrForbidden)
}

func TestHandleFilesImage_ReturnsTheImage(t *testing.T) {
	t.Parallel()
	e, rootID, _ := newFilesTestEnv(t)

	result, err := HandleFilesImage(withParams(&e.env, fmt.Sprintf(`{"root":%q,"path":"covers/zelda.png"}`, rootID)))
	require.NoError(t, err)
	image, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "image/png", image.ContentType)
	data, err := base64.StdEncoding.DecodeString(image.Data)
	require.NoError(t, err)
	assert.Equal(t, testPNG(t, 14), data)

	for name, path := range map[string]string{
		"a text file":           "covers/readme.txt",
		"a folder":              "covers",
		"text named like a png": "covers/fake.png",
		"outside the root":      "../private/secret.png",
	} {
		_, err = HandleFilesImage(withParams(&e.env, fmt.Sprintf(`{"root":%q,"path":%q}`, rootID, path)))
		require.Errorf(t, err, "%s", name)
		assert.Truef(t, isClientErr(err), "%s: want a client error, got %v", name, err)
	}
}

func TestHandleMediaMetaUpdate_ImageFromBrowse(t *testing.T) {
	t.Parallel()
	e, rootID, private := newFilesTestEnv(t)
	// A paired member: it may not name a host path, but it may pick an image
	// it browsed to.
	e.env.IsLocal = false
	e.env.ClientRole = "member"

	media := e.update(t, 0, fmt.Sprintf(
		`{"images": {"boxart": {"kind": "browse", "root": %q, "path": "covers/metroid.png"}}}`, rootID))
	stored := media.Properties[tags.PropertyTypeTag(tags.TagPropertyImageBoxart)].Text
	data, err := os.ReadFile(stored) //nolint:gosec // a path under the test's own temp dir
	require.NoError(t, err)
	assert.Equal(t, testPNG(t, 12), data)

	err = e.updateErr(t, fmt.Sprintf(
		`{"images": {"boxart": {"kind": "browse", "root": %q, "path": "../private/secret.png"}}}`, rootID))
	require.ErrorIs(t, err, hostfiles.ErrInvalidPath)
	err = e.updateErr(t, fmt.Sprintf(`{"images": {"boxart": {"kind": "file", "path": %q}}}`, private))
	require.ErrorIs(t, err, ErrForbidden)
	err = e.updateErr(t, `{"images": {"boxart": {"kind": "browse", "root": "x"}}}`)
	assert.True(t, isClientErr(err), "a browse image needs a path")
}
