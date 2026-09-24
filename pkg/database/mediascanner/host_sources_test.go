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

package mediascanner

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/sourcepath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type hostTestPlatform struct {
	*mocks.MockPlatform
	scan    *hostTestScan
	failure error
}

func (p *hostTestPlatform) OpenMediaScan(context.Context) (platforms.HostMediaScan, error) {
	return p.scan, p.failure
}

type hostTestScan struct {
	failure    error
	paths      []string
	closed     bool
	successful bool
}

func (*hostTestScan) Systems() []string { return []string{"NES"} }
func (s *hostTestScan) Walk(_ context.Context, _ string, _ func() error, yield func(platforms.ScanResult) error) error {
	for _, path := range s.paths {
		if err := yield(platforms.ScanResult{Path: path}); err != nil {
			return err
		}
	}
	return s.failure
}

func (s *hostTestScan) Close(successful bool) error {
	s.closed = true
	s.successful = successful
	return nil
}

func TestHostIndexStagesRealRowsWithoutLaunchersAndPreservesFailures(t *testing.T) {
	cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	base := mocks.NewMockPlatform()
	base.On("ID").Return("host-test")
	base.On("Settings").Return(platforms.Settings{})
	base.On("RootDirs", mock.Anything).Return([]string{})
	base.On("Launchers", mock.Anything).Return([]platforms.Launcher{})
	pl := &hostTestPlatform{MockPlatform: base}
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()
	testLauncherCacheMutex.Lock()
	previous := helpers.GlobalLauncherCache
	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice(nil)
	helpers.GlobalLauncherCache = cache
	defer func() { helpers.GlobalLauncherCache = previous; testLauncherCacheMutex.Unlock() }()
	identity, err := sourcepath.Format(sourcepath.ID("fixture-tree"), []string{"nes", "Fixture + 50% (USA).NES"})
	require.NoError(t, err)
	index := func(scan *hostTestScan) (int, error) {
		pl.scan = scan
		return NewNamesIndex(t.Context(), pl, cfg, []systemdefs.System{{ID: "NES"}}, db, func(IndexStatus) {}, nil)
	}
	first := &hostTestScan{paths: []string{identity}}
	count, err := index(first)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.True(t, first.closed && first.successful)
	rows, err := db.MediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, identity, rows[0].Path)
	require.Equal(t, "Fixture + 50%", rows[0].SortName)
	require.False(t, rows[0].IsMissing)
	originalID := rows[0].DBID
	_, err = index(&hostTestScan{paths: []string{identity}})
	require.NoError(t, err)
	rows, err = db.MediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, originalID, rows[0].DBID)

	partial, err := sourcepath.Format(sourcepath.ID("fixture-tree"), []string{"nes", "Partial.nes"})
	require.NoError(t, err)
	failure := errors.New("provider failed mid-scan")
	bad := &hostTestScan{paths: []string{partial}, failure: failure}
	_, err = index(bad)
	require.ErrorIs(t, err, failure)
	require.True(t, bad.closed)
	require.False(t, bad.successful)
	rows, err = db.MediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, rows, 1, "partial staging must not publish fake completion or delete previous media")
	require.Equal(t, identity, rows[0].Path)
	require.False(t, rows[0].IsMissing)

	_, err = index(&hostTestScan{})
	require.NoError(t, err)
	rows, err = db.MediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].IsMissing, "successful empty enumeration marks removed files, unlike failure")
}

func TestHostStreamStopsAtConsumerAndRejectsFilesystemIdentity(t *testing.T) {
	t.Parallel()
	identity, err := sourcepath.Format(sourcepath.ID("fixture"), []string{"nes", "game.nes"})
	require.NoError(t, err)
	scan := &hostTestScan{paths: []string{identity, "/storage/not-an-identity.nes"}}
	seen := 0
	for _, err := range indexResults(t.Context(), nil, scan, "NES", nil) {
		require.NoError(t, err)
		seen++
		break
	}
	require.Equal(t, 1, seen)
	var terminal error
	for _, err := range indexResults(t.Context(), nil, scan, "NES", nil) {
		if err != nil {
			terminal = err
		}
	}
	require.ErrorIs(t, terminal, sourcepath.ErrInvalid)
}
