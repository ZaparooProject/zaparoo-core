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
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
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

// setupBeforeExitTest builds a service context whose platform exposes the given
// launchers, with defaults installed and a keypress-observing hook script.
// pressed receives one value each time the before_exit script runs.
func setupBeforeExitTest(
	t *testing.T,
	launchers []platforms.Launcher,
	defaults []config.SystemsDefault,
) (svc *ServiceContext, pressed chan string) {
	t.Helper()

	cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`[zapscript.input]
mode = "unrestricted"`))
	cfg.SetSystemDefaults(defaults)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform")
	mockPlatform.On("Launchers", cfg).Return(launchers).Maybe()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false).Maybe()

	pressed = make(chan string, 4)
	mockPlatform.On("KeyboardPress", mock.Anything).Run(func(args mock.Arguments) {
		pressed <- args.String(0)
	}).Return(nil).Maybe()

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()
	mockUserDB.On("GetSupportedZapLinkHosts").Return([]string{}, nil).Maybe()

	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	return &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  &database.Database{UserDB: mockUserDB},
		LaunchSoftwareQueue: make(chan *tokens.Token, 1),
		PlaylistQueue:       make(chan *playlists.Playlist, 1),
	}, pressed
}

func assertHookRan(t *testing.T, pressed <-chan string) {
	t.Helper()
	select {
	case <-pressed:
	case <-time.After(2 * time.Second):
		t.Fatal("before_exit hook did not run")
	}
}

func assertHookDidNotRun(t *testing.T, pressed <-chan string) {
	t.Helper()
	select {
	case key := <-pressed:
		t.Fatalf("before_exit hook ran unexpectedly (pressed %q)", key)
	case <-time.After(50 * time.Millisecond):
	}
}

// The lookup previously matched a launcher ID against the media's system ID, so
// before_exit never fired on platforms where those differ. The media's own
// system must resolve directly.
func TestBeforeExitHook_ResolvesSystemFromActiveMediaSystemID(t *testing.T) {
	t.Parallel()

	svc, pressed := setupBeforeExitTest(t,
		[]platforms.Launcher{{ID: "KodiMovie", SystemID: "Movie"}},
		[]config.SystemsDefault{{System: "NES", BeforeExit: "**input.keyboard:{f2}"}},
	)
	svc.State.SetActiveMedia(models.NewActiveMedia("NES", "NES", "game.nes", "Game", "KodiMovie"))

	runBeforeExitHook(svc)

	assertHookRan(t, pressed)
}

func TestBeforeExitHook_ResolvesSystemViaLauncherID(t *testing.T) {
	t.Parallel()

	svc, pressed := setupBeforeExitTest(t,
		[]platforms.Launcher{{ID: "LLAPIAtari2600", SystemID: "Atari2600"}},
		[]config.SystemsDefault{{System: "Atari2600", BeforeExit: "**input.keyboard:{f2}"}},
	)
	// Media whose SystemID does not itself match any configured default.
	svc.State.SetActiveMedia(models.NewActiveMedia("unmapped", "Atari 2600", "g.a26", "G", "LLAPIAtari2600"))

	runBeforeExitHook(svc)

	assertHookRan(t, pressed)
}

// MiSTer launcher IDs equal their system IDs, which is the only shape the old
// lookup matched. Existing configs relying on it must keep working.
func TestBeforeExitHook_LegacyLauncherIDEqualsSystemIDStillMatches(t *testing.T) {
	t.Parallel()

	svc, pressed := setupBeforeExitTest(t,
		[]platforms.Launcher{{ID: "test-launcher", SystemID: "NES"}},
		[]config.SystemsDefault{{System: "NES", BeforeExit: "**input.keyboard:{f2}"}},
	)
	svc.State.SetActiveMedia(models.NewActiveMedia("test-launcher", "NES", "game.nes", "Game", "other"))

	runBeforeExitHook(svc)

	assertHookRan(t, pressed)
}

func TestBeforeExitHook_MatchesSystemAlias(t *testing.T) {
	t.Parallel()

	svc, pressed := setupBeforeExitTest(t,
		[]platforms.Launcher{{ID: "test-launcher", SystemID: "Gameboy"}},
		// "GB" is an alias of the Gameboy system.
		[]config.SystemsDefault{{System: "GB", BeforeExit: "**input.keyboard:{f2}"}},
	)
	svc.State.SetActiveMedia(models.NewActiveMedia("Gameboy", "Game Boy", "g.gb", "G", "test-launcher"))

	runBeforeExitHook(svc)

	assertHookRan(t, pressed)
}

func TestBeforeExitHook_NoActiveMediaIsNoOp(t *testing.T) {
	t.Parallel()

	svc, pressed := setupBeforeExitTest(t,
		[]platforms.Launcher{{ID: "test-launcher", SystemID: "NES"}},
		[]config.SystemsDefault{{System: "NES", BeforeExit: "**input.keyboard:{f2}"}},
	)

	runBeforeExitHook(svc)

	assertHookDidNotRun(t, pressed)
}

func TestBeforeExitHook_NoScriptConfiguredIsNoOp(t *testing.T) {
	t.Parallel()

	svc, pressed := setupBeforeExitTest(t,
		[]platforms.Launcher{{ID: "test-launcher", SystemID: "NES"}},
		[]config.SystemsDefault{{System: "SNES", BeforeExit: "**input.keyboard:{f2}"}},
	)
	svc.State.SetActiveMedia(models.NewActiveMedia("NES", "NES", "game.nes", "Game", "test-launcher"))

	runBeforeExitHook(svc)

	assertHookDidNotRun(t, pressed)
}

// A hook that delays must not hold the exit open forever.
func TestBeforeExitHook_TimesOutOnDelayScript(t *testing.T) {
	svc, pressed := setupBeforeExitTest(t,
		[]platforms.Launcher{{ID: "test-launcher", SystemID: "NES"}},
		[]config.SystemsDefault{{System: "NES", BeforeExit: "**delay:60000||**input.keyboard:{f2}"}},
	)
	svc.State.SetActiveMedia(models.NewActiveMedia("NES", "NES", "game.nes", "Game", "test-launcher"))

	original := beforeExitHookTimeout
	beforeExitHookTimeout = 20 * time.Millisecond
	t.Cleanup(func() { beforeExitHookTimeout = original })

	done := make(chan struct{})
	go func() {
		runBeforeExitHook(svc)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("before_exit hook did not honour its timeout")
	}
	assertHookDidNotRun(t, pressed)
}

func TestBeforeExitSystemIDs_DedupesCaseInsensitively(t *testing.T) {
	t.Parallel()

	svc, _ := setupBeforeExitTest(t,
		[]platforms.Launcher{{ID: "NES", SystemID: "nes"}},
		nil,
	)
	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "NES")

	ids := beforeExitSystemIDs(svc, media)

	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		lower := strings.ToLower(id)
		assert.False(t, seen[lower], "duplicate candidate system ID %q", id)
		seen[lower] = true
	}
}
