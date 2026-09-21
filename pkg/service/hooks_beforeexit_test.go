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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// setupBeforeExitTest builds a service context whose launcher cache holds the
// given launchers, with defaults installed and a keypress-observing hook script.
// pressed receives one value each time the before_exit script runs.
//
// Each extraTOML fragment is merged onto the config after the defaults, for
// scopes SetSystemDefaults cannot reach. The cache is a fresh instance rather
// than helpers.GlobalLauncherCache so these tests stay parallel.
func setupBeforeExitTest(
	t *testing.T,
	launchers []platforms.Launcher,
	defaults []config.SystemsDefault,
	extraTOML ...string,
) (svc *ServiceContext, pressed chan string) {
	t.Helper()

	cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(`[zapscript.input]
mode = "unrestricted"`))
	cfg.SetSystemDefaults(defaults)
	for _, fragment := range extraTOML {
		require.NoError(t, cfg.LoadTOML(fragment))
	}

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

	launcherCache := &helpers.LauncherCache{}
	launcherCache.InitializeFromSlice(launchers)

	return &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		LauncherCache:       launcherCache,
		State:               st,
		DB:                  &database.Database{UserDB: mockUserDB},
		LaunchSoftwareQueue: make(chan softwareTokenUpdate, 1),
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

// assertHookPressed asserts which script ran, for the precedence tests. Scripts
// press a distinct single key so the received value names the tier that won.
func assertHookPressed(t *testing.T, pressed <-chan string, want string) {
	t.Helper()
	select {
	case key := <-pressed:
		assert.Equal(t, want, key, "wrong before_exit script ran")
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

func TestBeforeExitTargetFor_DedupesCaseInsensitively(t *testing.T) {
	t.Parallel()

	svc, _ := setupBeforeExitTest(t,
		[]platforms.Launcher{{ID: "NES", SystemID: "nes", Groups: []string{"LLAPI"}}},
		nil,
	)
	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "NES")

	target := beforeExitTargetFor(svc, media)

	seen := make(map[string]bool, len(target.systemIDs))
	for _, id := range target.systemIDs {
		lower := strings.ToLower(id)
		assert.False(t, seen[lower], "duplicate candidate system ID %q", id)
		seen[lower] = true
	}
	assert.Equal(t, "NES", target.launcherID)
	assert.Equal(t, []string{"LLAPI"}, target.groups, "the launcher's groups must reach the launcher tier")
}

// raLaunchers mirrors the MiSTer RetroAchievements cores: one launcher per
// system, all carrying the RetroAchievements group.
func raLaunchers() []platforms.Launcher {
	return []platforms.Launcher{
		{ID: "RANES", SystemID: "NES", Groups: []string{"RetroAchievements"}},
		{ID: "RASNES", SystemID: "SNES", Groups: []string{"RetroAchievements"}},
		{ID: "RAGenesis", SystemID: "Genesis", Groups: []string{"RetroAchievements"}},
	}
}

// The issue: one script had to be repeated under every system sent to the
// RetroAchievements cores. A single launcher-group entry must cover them all
// with no [[systems.default]] entries at all.
func TestBeforeExitHook_OneLauncherGroupCoversEverySystem(t *testing.T) {
	t.Parallel()

	for _, launcher := range raLaunchers() {
		t.Run(launcher.SystemID, func(t *testing.T) {
			t.Parallel()

			svc, pressed := setupBeforeExitTest(t, raLaunchers(), nil, `
[[launchers.default]]
launcher = "RetroAchievements"
before_exit = "**input.keyboard:a"`)
			svc.State.SetActiveMedia(models.NewActiveMedia(
				launcher.SystemID, launcher.SystemID, "game.bin", "Game", launcher.ID,
			))

			runBeforeExitHook(svc)

			assertHookPressed(t, pressed, "a")
		})
	}
}

// The precedence ladder, narrowest scope first. Each tier presses a distinct key
// so the assertion names the tier that won.
func TestBeforeExitHook_ScopePrecedence(t *testing.T) {
	t.Parallel()

	// One document per case: go-toml decodes an array of tables by index, so
	// separate LoadTOML fragments would overwrite entry 0 instead of appending.
	// The [launchers] scalar has to precede the [[launchers.default]] blocks.
	const (
		everyTierTOML = `
[launchers]
before_exit = "**input.keyboard:d"

[[launchers.default]]
launcher = "RetroAchievements"
before_exit = "**input.keyboard:c"

[[launchers.default]]
launcher = "RASNES"
before_exit = "**input.keyboard:a"`
		groupAndGlobalTOML = `
[launchers]
before_exit = "**input.keyboard:d"

[[launchers.default]]
launcher = "RetroAchievements"
before_exit = "**input.keyboard:c"`
		globalTOML = `
[launchers]
before_exit = "**input.keyboard:d"`
	)
	systemDefaults := []config.SystemsDefault{{System: "SNES", BeforeExit: "**input.keyboard:b"}}

	tests := []struct {
		name     string
		toml     string
		want     string
		defaults []config.SystemsDefault
	}{
		{
			name:     "exact launcher beats system, group and global",
			defaults: systemDefaults,
			toml:     everyTierTOML,
			want:     "a",
		},
		{
			name:     "system beats group and global",
			defaults: systemDefaults,
			toml:     groupAndGlobalTOML,
			want:     "b",
		},
		{
			name: "group beats global",
			toml: groupAndGlobalTOML,
			want: "c",
		},
		{
			name: "global applies when nothing else matches",
			toml: globalTOML,
			want: "d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, pressed := setupBeforeExitTest(t, raLaunchers(), tt.defaults, tt.toml)
			svc.State.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", "g.sfc", "G", "RASNES"))

			runBeforeExitHook(svc)

			assertHookPressed(t, pressed, tt.want)
		})
	}
}

// Media published by a platform tracker rather than by a launch carries no
// launcher. A [[launchers.default]] entry that leaves launcher unset must not
// become a wildcard for it, but the global must still reach it.
func TestBeforeExitHook_MediaWithNoLauncher(t *testing.T) {
	t.Parallel()

	const wildcardTOML = `
[[launchers.default]]
launcher = ""
before_exit = "**input.keyboard:a"`

	t.Run("degenerate empty launcher entry does not match", func(t *testing.T) {
		t.Parallel()

		svc, pressed := setupBeforeExitTest(t, raLaunchers(), nil, wildcardTOML)
		svc.State.SetActiveMedia(models.NewActiveMedia("NES", "NES", "g.nes", "G", ""))

		runBeforeExitHook(svc)

		assertHookDidNotRun(t, pressed)
	})

	t.Run("global still applies", func(t *testing.T) {
		t.Parallel()

		svc, pressed := setupBeforeExitTest(t, raLaunchers(), nil, `
[launchers]
before_exit = "**input.keyboard:d"

[[launchers.default]]
launcher = ""
before_exit = "**input.keyboard:a"`)
		svc.State.SetActiveMedia(models.NewActiveMedia("NES", "NES", "g.nes", "G", ""))

		runBeforeExitHook(svc)

		assertHookPressed(t, pressed, "d")
	})
}

// Launcher IDs fold case everywhere else in the codebase; the old resolver
// compared them with ==.
func TestBeforeExitHook_LauncherIDIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	svc, pressed := setupBeforeExitTest(t, raLaunchers(), nil, `
[[launchers.default]]
launcher = "RetroAchievements"
before_exit = "**input.keyboard:a"`)
	// Media recorded with different casing than the launcher definition.
	svc.State.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", "g.sfc", "G", "rasnes"))

	runBeforeExitHook(svc)

	assertHookPressed(t, pressed, "a")
}

// Resolution must not depend on Platform.Launchers: the cache is the only source
// holding launchers the platform cannot build, and MiSTer rebuilds its whole
// launcher list on every Launchers call.
func TestBeforeExitScript_DoesNotUsePlatformLaunchers(t *testing.T) {
	t.Parallel()

	svc, _ := setupBeforeExitTest(t, raLaunchers(), nil, `
[[launchers.default]]
launcher = "RetroAchievements"
before_exit = "**input.keyboard:a"`)
	media := models.NewActiveMedia("SNES", "SNES", "g.sfc", "G", "RASNES")

	assert.Equal(t, "**input.keyboard:a", beforeExitScript(svc, media))

	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	mockPlatform.AssertNotCalled(t, "Launchers", mock.Anything)
}

// A launcher removed from config since the media started keeps matching an entry
// that names it exactly.
func TestBeforeExitHook_UnknownLauncherStillMatchesExactEntry(t *testing.T) {
	t.Parallel()

	svc, pressed := setupBeforeExitTest(t, raLaunchers(), nil, `
[[launchers.default]]
launcher = "GoneLauncher"
before_exit = "**input.keyboard:a"`)
	svc.State.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", "g.sfc", "G", "GoneLauncher"))

	runBeforeExitHook(svc)

	assertHookPressed(t, pressed, "a")
}

// The config side of an alias already resolved; the media side did not, so a
// canonical entry never matched media whose own system is an alias. Media
// published by a platform tracker rather than by a launch carries no launcher, so
// there is no launcher whose system can stand in for the alias.
func TestBeforeExitHook_MatchesAliasOnTheMediaSide(t *testing.T) {
	t.Parallel()

	svc, pressed := setupBeforeExitTest(t,
		[]platforms.Launcher{{ID: "RAGenesis", SystemID: "Genesis"}},
		[]config.SystemsDefault{{System: "Genesis", BeforeExit: "**input.keyboard:a"}},
	)
	// "MegaDrive" is an alias of the Genesis system.
	svc.State.SetActiveMedia(models.NewActiveMedia("MegaDrive", "Mega Drive", "g.md", "G", ""))

	runBeforeExitHook(svc)

	assertHookPressed(t, pressed, "a")
}
