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
	"context"
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// sourceBrowseTestPlatform is a minimal platforms.SourceRootReader for the
// media.browse dispatch tests: only SourceRoots is ever consulted by the
// browse path (ReadSourceDir is a launch/index-time concern, unused here),
// but Go requires both to satisfy the interface for the type assertion.
type sourceBrowseTestPlatform struct {
	*mocks.MockPlatform
	roots []string
}

func (p *sourceBrowseTestPlatform) SourceRoots(context.Context) ([]string, error) {
	return p.roots, nil
}

func (*sourceBrowseTestPlatform) ReadSourceDir(context.Context, string) ([]platforms.SourceEntry, error) {
	return nil, assert.AnError
}

func newSourceBrowseEnv(
	t *testing.T, mockMediaDB *helpers.MockMediaDBI, roots []string, path string,
) requests.RequestEnv {
	t.Helper()
	platform := &sourceBrowseTestPlatform{MockPlatform: mocks.NewMockPlatform(), roots: roots}
	platform.On("RootDirs", mock.Anything).Return([]string{})
	platform.On("Launchers", mock.Anything).Return([]platforms.Launcher{})

	paramsJSON, err := json.Marshal(models.BrowseParams{Path: &path})
	require.NoError(t, err)
	return requests.RequestEnv{
		Context:  context.Background(),
		Params:   paramsJSON,
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: platform,
		Config:   &config.Instance{},
	}
}

// The exact bug this PR fixes: opening a system backed by a source root used
// to fail every time with "unknown virtual scheme: source://", regardless of
// whether the root was actually granted. A granted root must browse cleanly.
func TestHandleMediaBrowseOpensAGrantedSourceRootWithoutUnknownSchemeError(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, mock.Anything).
		Return([]database.BrowseDirectoryResult{}, nil)
	mockMediaDB.On("BrowseDirCount", mock.Anything, mock.Anything).Return(0, nil)
	mockMediaDB.On("BrowseFileCount", mock.Anything, mock.Anything).Return(0, nil)
	mockMediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{}, nil)

	env := newSourceBrowseEnv(t, mockMediaDB, []string{"source://granted"}, "source://granted/NES/")
	_, err := HandleMediaBrowse(env)
	require.NoError(t, err)
}

// A root the host no longer grants (revoked, or never existed) must be
// rejected outright, not silently browsed as if it still were: a stale
// Frontend path_stack entry from before a revocation must not leak whatever
// happens to still be indexed under that id.
func TestHandleMediaBrowseRejectsASourceRootThatIsNoLongerGranted(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	env := newSourceBrowseEnv(t, mockMediaDB, []string{"source://granted"}, "source://revoked/NES/")

	_, err := HandleMediaBrowse(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer granted")
	mockMediaDB.AssertNotCalled(t, "BrowseDirectories", mock.Anything, mock.Anything)
}

// A platform that does not implement platforms.SourceRootReader at all (no
// Android-style host) must refuse a source path cleanly, not panic on the
// type assertion or silently browse nothing.
func TestHandleMediaBrowseRefusesSourcePathsWithoutASourceRootReader(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	platform := mocks.NewMockPlatform()
	platform.On("RootDirs", mock.Anything).Return([]string{})
	platform.On("Launchers", mock.Anything).Return([]platforms.Launcher{})

	path := "source://abc/NES/"
	paramsJSON, err := json.Marshal(models.BrowseParams{Path: &path})
	require.NoError(t, err)
	env := requests.RequestEnv{
		Context:  context.Background(),
		Params:   paramsJSON,
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: platform,
		Config:   &config.Instance{},
	}

	_, err = HandleMediaBrowse(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support source root paths")
}

// media.browse.index shares the same root-validation gap browse itself had;
// confirm its prefix resolver is fixed the same way, trailing slash and all.
func TestResolveSourceIndexPrefix(t *testing.T) {
	t.Parallel()

	granted := &sourceBrowseTestPlatform{MockPlatform: mocks.NewMockPlatform(), roots: []string{"source://granted"}}
	env := &requests.RequestEnv{Context: context.Background(), Platform: granted}

	prefix, err := resolveSourceIndexPrefix(env, "source://granted/NES")
	require.NoError(t, err)
	assert.Equal(t, "source://granted/NES/", prefix, "a trailing slash is added for prefix matching")

	prefix, err = resolveSourceIndexPrefix(env, "source://granted/NES/")
	require.NoError(t, err)
	assert.Equal(t, "source://granted/NES/", prefix, "an already-trailing-slash path is tolerated, not doubled")

	_, err = resolveSourceIndexPrefix(env, "source://revoked/NES")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer granted")
}
