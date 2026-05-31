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

package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type readyHookPlatform struct {
	*mocks.MockPlatform
	cfg       *config.Instance
	ready     chan struct{}
	readySeen chan struct{}
	pressed   chan string
	dataDir   string
}

func newReadyHookPlatform(cfg *config.Instance, dataDir string) *readyHookPlatform {
	return &readyHookPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		cfg:          cfg,
		dataDir:      dataDir,
		ready:        make(chan struct{}),
		readySeen:    make(chan struct{}),
		pressed:      make(chan string, 4),
	}
}

func (*readyHookPlatform) ID() string { return "mock-platform" }

func (p *readyHookPlatform) Settings() platforms.Settings {
	return platforms.Settings{DataDir: p.dataDir}
}

func (*readyHookPlatform) LookupMapping(*tokens.Token) (string, bool) { return "", false }

func (p *readyHookPlatform) KeyboardPress(key string) error {
	p.pressed <- key
	return nil
}

func (p *readyHookPlatform) WaitForServiceReady(ctx context.Context, _ *config.Instance) error {
	close(p.readySeen)
	select {
	case <-p.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestIsFirstServiceStartForBootPersistsBootID(t *testing.T) {
	oldDetectSystemBootID := detectSystemBootID
	defer func() { detectSystemBootID = oldDetectSystemBootID }()

	bootID := "boot-1"
	detectSystemBootID = func() (string, error) {
		return bootID, nil
	}

	testRoot := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: testRoot})

	first, err := isFirstServiceStartForBoot(mockPlatform)
	require.NoError(t, err)
	assert.True(t, first)

	first, err = isFirstServiceStartForBoot(mockPlatform)
	require.NoError(t, err)
	assert.False(t, first)

	bootID = "boot-2"
	first, err = isFirstServiceStartForBoot(mockPlatform)
	require.NoError(t, err)
	assert.True(t, first)
}

func TestRunConfiguredServiceHooksRunsOnReadyAfterServiceReady(t *testing.T) {
	oldDetectSystemBootID := detectSystemBootID
	defer func() { detectSystemBootID = oldDetectSystemBootID }()
	detectSystemBootID = func() (string, error) { return "test-boot", nil }

	testRoot := t.TempDir()
	cfg, err := testhelpers.NewTestConfig(nil, filepath.Join(testRoot, "config"))
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`[service]
on_ready = "**input.keyboard:{f2}"
`))

	platform := newReadyHookPlatform(cfg, filepath.Join(testRoot, "data"))
	mockUserDB := &testhelpers.MockUserDBI{}
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil)
	st, _ := state.NewState(platform, "test-boot-uuid")
	svc := &ServiceContext{
		Platform:      platform,
		Config:        cfg,
		State:         st,
		DB:            &database.Database{UserDB: mockUserDB},
		PlaylistQueue: make(chan *playlists.Playlist, 1),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		runConfiguredServiceHooks(svc)
	}()

	select {
	case <-platform.readySeen:
	case <-time.After(time.Second):
		t.Fatal("service ready wait was not reached")
	}

	select {
	case key := <-platform.pressed:
		t.Fatalf("on_ready ran before service ready: %s", key)
	case <-time.After(25 * time.Millisecond):
	}

	close(platform.ready)

	select {
	case key := <-platform.pressed:
		assert.Equal(t, "{f2}", key)
	case <-time.After(time.Second):
		t.Fatal("on_ready did not run after service ready")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("hook runner did not finish")
	}
	mockUserDB.AssertExpectations(t)
}

func TestRunConfiguredServiceHooksUsesSingleBootCheckForBootAndReady(t *testing.T) {
	oldDetectSystemBootID := detectSystemBootID
	defer func() { detectSystemBootID = oldDetectSystemBootID }()
	bootChecks := 0
	detectSystemBootID = func() (string, error) {
		bootChecks++
		return "test-boot", nil
	}

	testRoot := t.TempDir()
	cfg, err := testhelpers.NewTestConfig(nil, filepath.Join(testRoot, "config"))
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`[service]
on_boot = "**input.keyboard:{f2}"
on_ready = "**input.keyboard:{f3}"
`))

	platform := newReadyHookPlatform(cfg, filepath.Join(testRoot, "data"))
	close(platform.ready)
	mockUserDB := &testhelpers.MockUserDBI{}
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Twice()
	st, _ := state.NewState(platform, "test-boot-uuid")
	svc := &ServiceContext{
		Platform:      platform,
		Config:        cfg,
		State:         st,
		DB:            &database.Database{UserDB: mockUserDB},
		PlaylistQueue: make(chan *playlists.Playlist, 1),
	}

	runConfiguredServiceHooks(svc)

	require.Equal(t, 1, bootChecks)
	select {
	case key := <-platform.pressed:
		assert.Equal(t, "{f2}", key)
	case <-time.After(time.Second):
		t.Fatal("on_boot keypress did not run")
	}
	select {
	case key := <-platform.pressed:
		assert.Equal(t, "{f3}", key)
	case <-time.After(time.Second):
		t.Fatal("on_ready keypress did not run")
	}
	mockUserDB.AssertExpectations(t)
}

func TestRunConfiguredServiceHooksRunsOnBootOnlyOncePerBoot(t *testing.T) {
	oldDetectSystemBootID := detectSystemBootID
	defer func() { detectSystemBootID = oldDetectSystemBootID }()
	detectSystemBootID = func() (string, error) { return "test-boot", nil }

	testRoot := t.TempDir()
	cfg, err := testhelpers.NewTestConfig(nil, filepath.Join(testRoot, "config"))
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`[service]
on_boot = "**input.keyboard:{f2}"
`))

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: filepath.Join(testRoot, "data")})
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false)
	mockPlatform.On("KeyboardPress", "{f2}").Return(nil).Once()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Once()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	svc := &ServiceContext{
		Platform:      mockPlatform,
		Config:        cfg,
		State:         st,
		DB:            &database.Database{UserDB: mockUserDB},
		PlaylistQueue: make(chan *playlists.Playlist, 1),
	}

	runConfiguredServiceHooks(svc)
	runConfiguredServiceHooks(svc)

	mockPlatform.AssertExpectations(t)
	mockUserDB.AssertExpectations(t)
}
