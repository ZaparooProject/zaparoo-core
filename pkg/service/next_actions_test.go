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
	"os"
	"path/filepath"
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupNextActionTestEnv(t *testing.T) (*ServiceContext, *mocks.MockPlatform, *config.Instance) {
	t.Helper()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform").Maybe()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false).Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: t.TempDir()}).Maybe()

	cfg := &config.Instance{}
	st, ns := state.NewState(mockPlatform, "test-boot-uuid")
	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()

	svc := &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  &database.Database{UserDB: mockUserDB},
		LaunchSoftwareQueue: make(chan softwareTokenUpdate, 10),
		PlaylistQueue:       make(chan *playlists.Playlist, 10),
	}
	return svc, mockPlatform, cfg
}

func TestHandleNextActionPreflight_EvaluatesWritePayload(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	mediaPath := filepath.Join("games", "snes", "zelda.sfc")
	svc.State.SetActiveMedia(models.NewActiveMedia("snes", "SNES", mediaPath, "Zelda", "snes-launcher"))
	parser := gozapscript.NewParser("**write:[[active_media.path]]")
	script, err := parser.ParseScript()
	require.NoError(t, err)
	token := tokens.Token{UID: "source", Text: "**write:[[active_media.path]]", ScanTime: time.Now()}

	result := handleNextActionPreflight(svc, &token, &script)

	require.Equal(t, nextActionArmed, result)
	pending := svc.State.GetPendingWrite()
	require.NotNil(t, pending)
	assert.Equal(t, mediaPath, pending.Payload)
}

func TestHandleNextActionPreflight_RejectsEmptyWritePayload(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	parser := gozapscript.NewParser("**write:[[active_media.path]]")
	script, err := parser.ParseScript()
	require.NoError(t, err)
	token := tokens.Token{UID: "source", Text: "**write:[[active_media.path]]", ScanTime: time.Now()}

	result := handleNextActionPreflight(svc, &token, &script)

	require.Equal(t, nextActionInvalid, result)
	assert.Nil(t, svc.State.GetPendingWrite())
}

func TestHandleNextActionPreflight_RejectsBlockedWrite(t *testing.T) {
	t.Parallel()

	svc, _, cfg := setupNextActionTestEnv(t)
	require.NoError(t, cfg.LoadTOML(`
[zapscript]
block_commands = ["write"]
`))
	parser := gozapscript.NewParser("**write:payload")
	script, err := parser.ParseScript()
	require.NoError(t, err)
	token := tokens.Token{UID: "source", Text: "**write:payload", ScanTime: time.Now()}

	result := handleNextActionPreflight(svc, &token, &script)

	require.Equal(t, nextActionBlocked, result)
	assert.Nil(t, svc.State.GetPendingWrite())
}

const modifierReaderID = "mock-reader-0123456789abcdef"

func newModifierReader(removable bool) *mocks.MockReader {
	reader := mocks.NewMockReader()
	caps := []readers.Capability{readers.CapabilityWrite}
	if removable {
		caps = append(caps, readers.CapabilityRemovable)
	}
	reader.On("Metadata").Return(readers.DriverMetadata{ID: "mock-reader"}).Maybe()
	reader.On("IDs").Return([]string{"mock:"}).Maybe()
	reader.On("Connected").Return(true).Maybe()
	reader.On("Path").Return("/dev/mock-device").Maybe()
	reader.On("Info").Return("Mock Reader").Maybe()
	reader.On("Capabilities").Return(caps).Maybe()
	reader.On("ReaderID").Return(modifierReaderID).Maybe()
	return reader
}

func parseOne(t *testing.T, text string) gozapscript.Script {
	t.Helper()
	script, err := gozapscript.NewParser(text).ParseScript()
	require.NoError(t, err)
	return script
}

// placeModifier puts a modifier card on the mock reader the way the reader
// loop does, then runs the preflight on it.
func placeModifier(t *testing.T, svc *ServiceContext, text string) nextActionResult {
	t.Helper()
	token := tokens.Token{
		UID:      "modifier",
		Text:     text,
		ScanTime: time.Now(),
		Source:   tokens.SourceReader,
		ReaderID: modifierReaderID,
	}
	svc.State.SetReaderToken(modifierReaderID, &token, time.Now())
	token.Traits = tokens.ResolveTraits(parseOne(t, text).Traits)
	script := parseOne(t, text)
	return handleNextActionPreflight(svc, &token, &script)
}

func TestHandleNextActionPreflight_ArmsLaunchOverride(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, cfg := setupNextActionTestEnv(t)
	mockPlatform.On("Launchers", cfg).
		Return([]platforms.Launcher{{ID: "3do-dualram", SystemID: "3do"}})
	script := parseOne(t, "**launch?launcher=3do-dualram")
	token := tokens.Token{UID: "source", Text: "**launch?launcher=3do-dualram", ScanTime: time.Now()}

	result := handleNextActionPreflight(svc, &token, &script)

	require.Equal(t, nextActionArmed, result)
	mods := svc.State.LaunchModifiers()
	require.Len(t, mods, 1)
	assert.Equal(t, map[string]string{"launcher": "3do-dualram"}, mods[0].Args)
	assert.Equal(t, "source", mods[0].Source.UID)
	assert.False(t, mods[0].Sticky)
}

func TestHandleNextActionPreflight_ArmsLaunchOverrideCaseInsensitively(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, cfg := setupNextActionTestEnv(t)
	mockPlatform.On("Launchers", cfg).
		Return([]platforms.Launcher{{ID: "Winamp_Main", SystemID: ""}})
	script := parseOne(t, "**launch?launcher=winamp_main")
	token := tokens.Token{UID: "source", Text: "**launch?launcher=winamp_main", ScanTime: time.Now()}

	result := handleNextActionPreflight(svc, &token, &script)

	require.Equal(t, nextActionArmed, result)
	mods := svc.State.LaunchModifiers()
	require.Len(t, mods, 1)
	assert.Equal(t, "winamp_main", mods[0].Args["launcher"])
}

func TestHandleNextActionPreflight_RejectsUnknownLaunchOverride(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, cfg := setupNextActionTestEnv(t)
	mockPlatform.On("Launchers", cfg).
		Return([]platforms.Launcher{{ID: "3do-dualram", SystemID: "3do"}})
	script := parseOne(t, "**launch?launcher=Winamp_Main")
	token := tokens.Token{UID: "source", Text: "**launch?launcher=Winamp_Main", ScanTime: time.Now()}

	result := handleNextActionPreflight(svc, &token, &script)

	require.Equal(t, nextActionInvalid, result)
	assert.Empty(t, svc.State.LaunchModifiers())
}

func TestHandleNextActionPreflight_ArmsEveryModifierKey(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	text := "**launch?set_name=GBC&set_name_same_dir=yes&action=DETAILS"
	script := parseOne(t, text)
	token := tokens.Token{UID: "source", Text: text, ScanTime: time.Now()}

	result := handleNextActionPreflight(svc, &token, &script)

	require.Equal(t, nextActionArmed, result)
	mods := svc.State.LaunchModifiers()
	require.Len(t, mods, 1)
	assert.Equal(t, map[string]string{
		"set_name":          "GBC",
		"set_name_same_dir": "yes",
		"action":            "DETAILS",
	}, mods[0].Args)
}

func TestHandleNextActionPreflight_ModifierRejectsBadValues(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"**launch?action=delete",
		"**launch?set_name=",
		"**launch?set_name=GBC&set_name_same_dir=maybe",
	} {
		svc, _, _ := setupNextActionTestEnv(t)
		script := parseOne(t, text)
		token := tokens.Token{UID: "source", Text: text, ScanTime: time.Now()}

		assert.Equal(t, nextActionInvalid, handleNextActionPreflight(svc, &token, &script), text)
		assert.Empty(t, svc.State.LaunchModifiers(), text)
	}
}

func TestHandleNextActionPreflight_ModifierIgnoresOtherKeys(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"**launch?system=SNES",
		"**launch?set_name=GBC&slot=background",
		"**launch:game.sfc?set_name=GBC",
	} {
		svc, _, _ := setupNextActionTestEnv(t)
		script := parseOne(t, text)
		token := tokens.Token{UID: "source", Text: text, ScanTime: time.Now()}

		assert.Equal(t, nextActionNone, handleNextActionPreflight(svc, &token, &script), text)
		assert.Empty(t, svc.State.LaunchModifiers(), text)
	}
}

func TestHandleNextActionPreflight_ModifierStickiness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		config    string
		text      string
		removable bool
		sticky    bool
	}{
		{
			name: "global hold on removable reader", config: holdModeTOML, text: "**launch?set_name=GBC",
			removable: true, sticky: true,
		},
		{name: "tap mode stays one-shot", text: "**launch?set_name=GBC", removable: true},
		{name: "hold trait on the card", text: "#hold||**launch?set_name=GBC", removable: true, sticky: true},
		{
			name: "tap trait overrides hold mode", config: holdModeTOML, text: "#tap||**launch?set_name=GBC",
			removable: true,
		},
		{name: "reader without removal", config: holdModeTOML, text: "**launch?set_name=GBC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, _, cfg := setupNextActionTestEnv(t)
			if tt.config != "" {
				require.NoError(t, cfg.LoadTOML(tt.config))
			}
			svc.State.SetReader(newModifierReader(tt.removable))

			require.Equal(t, nextActionArmed, placeModifier(t, svc, tt.text))
			mods := svc.State.LaunchModifiers()
			require.Len(t, mods, 1)
			assert.Equal(t, tt.sticky, mods[0].Sticky)
		})
	}
}

func TestHandleNextActionPreflight_ModifierLiftedBeforeArmIsOneShot(t *testing.T) {
	t.Parallel()

	svc, _, cfg := setupNextActionTestEnv(t)
	require.NoError(t, cfg.LoadTOML(holdModeTOML))
	svc.State.SetReader(newModifierReader(true))
	text := "**launch?set_name=GBC"
	token := tokens.Token{
		UID: "modifier", Text: text, ScanTime: time.Now(),
		Source: tokens.SourceReader, ReaderID: modifierReaderID,
	}
	svc.State.SetReaderToken(modifierReaderID, &token, time.Now())
	svc.State.ReleaseReaderToken(modifierReaderID, time.Now(), false)
	script := parseOne(t, text)

	require.Equal(t, nextActionArmed, handleNextActionPreflight(svc, &token, &script))
	mods := svc.State.LaunchModifiers()
	require.Len(t, mods, 1)
	assert.False(t, mods[0].Sticky)
}

const holdModeTOML = `
[readers.scan]
mode = "hold"
`

func expectModifiedLaunch(
	t *testing.T,
	mockPlatform *mocks.MockPlatform,
	cfg *config.Instance,
	svc *ServiceContext,
	path, setName string,
	times int,
) {
	t.Helper()
	mockPlatform.On("LaunchMedia", cfg, path,
		(*platforms.Launcher)(nil),
		svc.DB,
		mock.MatchedBy(func(opts *platforms.LaunchOptions) bool {
			return opts != nil && opts.ActiveMediaPublisher != nil && opts.SetName == setName
		})).Return(nil).Times(times)
}

func runReaderLaunch(t *testing.T, svc *ServiceContext, path string) {
	t.Helper()
	plsc := playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)}
	token := tokens.Token{Text: "**launch:" + path, ScanTime: time.Now(), Source: tokens.SourceReader}
	require.NoError(t, runTokenZapScript(svc, token, plsc, nil, false))
}

func TestRunTokenZapScript_StickyModifierAppliesUntilLifted(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, cfg := setupNextActionTestEnv(t)
	require.NoError(t, cfg.LoadTOML(holdModeTOML))
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	svc.State.SetReader(newModifierReader(true))
	path := filepath.Join(t.TempDir(), "game.gb")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))

	require.Equal(t, nextActionArmed, placeModifier(t, svc, "**launch?set_name=GBC"))

	expectModifiedLaunch(t, mockPlatform, cfg, svc, path, "GBC", 2)
	runReaderLaunch(t, svc, path)
	runReaderLaunch(t, svc, path)
	require.Len(t, svc.State.LaunchModifiers(), 1)

	svc.State.ReleaseReaderToken(modifierReaderID, time.Now(), false)
	assert.Empty(t, svc.State.LaunchModifiers())

	expectModifiedLaunch(t, mockPlatform, cfg, svc, path, "", 1)
	runReaderLaunch(t, svc, path)
	mockPlatform.AssertExpectations(t)
}

func TestRunTokenZapScript_AppliesPendingLaunchOverride(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, cfg := setupNextActionTestEnv(t)
	launcher := platforms.Launcher{ID: "3do-dualram", SystemID: "3do"}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{launcher})

	path := filepath.Join(t.TempDir(), "game.chd")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	mockPlatform.On("LaunchMedia", cfg, path,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "3do-dualram"
		}),
		svc.DB,
		mock.MatchedBy(func(opts *platforms.LaunchOptions) bool {
			return opts != nil && opts.ActiveMediaPublisher != nil
		})).Return(nil).Once()

	svc.State.ArmLaunchModifier(&state.LaunchModifier{
		Args:      map[string]string{"launcher": "3do-dualram"},
		Source:    tokens.Token{UID: "source"},
		CreatedAt: time.Now(),
	})

	runReaderLaunch(t, svc, path)

	assert.Empty(t, svc.State.LaunchModifiers())
	mockPlatform.AssertExpectations(t)
}

func TestRunTokenZapScript_DiscardsExpiredPendingLaunchOverride(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, cfg := setupNextActionTestEnv(t)
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})

	path := filepath.Join(t.TempDir(), "game.chd")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	mockPlatform.On("LaunchMedia", cfg, path,
		(*platforms.Launcher)(nil),
		svc.DB,
		mock.MatchedBy(func(opts *platforms.LaunchOptions) bool {
			return opts != nil && opts.ActiveMediaPublisher != nil
		})).Return(nil).Once()

	svc.State.ArmLaunchModifier(&state.LaunchModifier{
		Args:      map[string]string{"launcher": "3do-dualram"},
		Source:    tokens.Token{UID: "source"},
		CreatedAt: time.Now().Add(-state.LaunchModifierTTL - time.Minute),
	})

	runReaderLaunch(t, svc, path)

	assert.Empty(t, svc.State.LaunchModifiers())
	mockPlatform.AssertExpectations(t)
}

func TestRunTokenZapScript_PlaylistDoesNotConsumePendingOverride(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, cfg := setupNextActionTestEnv(t)
	path := filepath.Join(t.TempDir(), "playlist-game.chd")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockPlatform.On("LaunchMedia", cfg, path,
		(*platforms.Launcher)(nil),
		svc.DB,
		mock.MatchedBy(func(opts *platforms.LaunchOptions) bool {
			return opts != nil && opts.ActiveMediaPublisher != nil
		})).Return(nil).Once()
	svc.State.ArmLaunchModifier(&state.LaunchModifier{
		Args:      map[string]string{"launcher": "3do-dualram"},
		Source:    tokens.Token{UID: "source"},
		CreatedAt: time.Now(),
	})

	plsc := playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)}
	token := tokens.Token{Text: "**launch:" + path, ScanTime: time.Now(), Source: tokens.SourcePlaylist}
	err := runTokenZapScript(svc, token, plsc, nil, false)

	require.NoError(t, err)
	assert.Len(t, svc.State.LaunchModifiers(), 1)
	mockPlatform.AssertExpectations(t)
}

func TestRunTokenZapScript_LaunchSystemDoesNotConsumePendingOverride(t *testing.T) {
	t.Parallel()

	svc, mockPlatform, _ := setupNextActionTestEnv(t)
	mockPlatform.On("LaunchSystem", mock.AnythingOfType("*config.Instance"), "menu").Return(nil).Maybe()
	mockPlatform.On("ReturnToMenu").Return(nil).Once()
	svc.State.ArmLaunchModifier(&state.LaunchModifier{
		Args:      map[string]string{"launcher": "3do-dualram"},
		Source:    tokens.Token{UID: "source"},
		CreatedAt: time.Now(),
	})

	plsc := playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)}
	err := runTokenZapScript(svc, tokens.Token{Text: "**launch.system:menu", ScanTime: time.Now()}, plsc, nil, false)

	require.NoError(t, err)
	assert.Len(t, svc.State.LaunchModifiers(), 1)
}

func TestApplyLaunchModifiers_GroupsNewestWins(t *testing.T) {
	t.Parallel()

	base := gozapscript.NewAdvArgs(map[string]string{
		"launcher":          "card-launcher",
		"set_name_same_dir": "yes",
		"system":            "GB",
	})
	modifiers := []state.LaunchModifier{
		{Args: map[string]string{"launcher": "old", "set_name": "SGB", "set_name_same_dir": "no"}},
		{Args: map[string]string{"set_name": "GBC"}},
		{Args: map[string]string{"launcher": "new"}},
	}

	got := applyLaunchModifiers(base, modifiers).Raw()

	assert.Equal(t, map[string]string{
		"launcher": "new",
		"set_name": "GBC",
		"system":   "GB",
	}, got)
	assert.Equal(t, "card-launcher", base.Get(gozapscript.KeyLauncher), "base args must not be mutated")
}

func TestHandlePendingWrite_WritesTargetAndConsumesScan(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	require.NoError(t, svc.Config.LoadTOML(`
[audio]
scan_feedback = true
`))
	reader := mocks.NewMockReader()
	reader.SetupBasicMock()
	svc.State.SetReader(reader)
	readerID := "mock-reader-0123456789abcdef"
	source := tokens.Token{UID: "source", Text: "**write:payload", ScanTime: time.Now(), ReaderID: readerID}
	target := &tokens.Token{UID: "target", Text: "old", ScanTime: time.Now(), ReaderID: readerID}
	written := &tokens.Token{UID: "target", Text: "payload", ScanTime: time.Now()}
	svc.State.SetPendingWrite(&state.PendingWrite{Payload: "payload", Source: source, CreatedAt: time.Now()})
	reader.On("WriteTarget", mock.Anything, "payload", readers.WriteOptions{
		TargetUID:  "target",
		ExcludeUID: "source",
	}).Return(written, nil).Once()
	player := mocks.NewMockPlayer()
	player.On("PlayBytes", assets.SuccessSound).Return(nil).Once()

	consumed := handlePendingWrite(svc, target, player)

	require.True(t, consumed)
	assert.Nil(t, svc.State.GetPendingWrite())
	assert.Equal(t, readerID, written.ReaderID)
	assert.Equal(t, written, svc.State.GetWroteToken(readerID))
	reader.AssertCalled(t, "WriteTarget", mock.Anything, "payload", readers.WriteOptions{
		TargetUID:  "target",
		ExcludeUID: "source",
	})
	player.AssertExpectations(t)
}

func TestHandlePendingWrite_PlaysFailSoundOnError(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	require.NoError(t, svc.Config.LoadTOML(`
[audio]
scan_feedback = true
`))
	reader := mocks.NewMockReader()
	reader.SetupBasicMock()
	svc.State.SetReader(reader)
	readerID := "mock-reader-0123456789abcdef"
	source := tokens.Token{UID: "source", Text: "**write:payload", ScanTime: time.Now(), ReaderID: readerID}
	target := &tokens.Token{UID: "target", Text: "old", ScanTime: time.Now(), ReaderID: readerID}
	svc.State.SetPendingWrite(&state.PendingWrite{Payload: "payload", Source: source, CreatedAt: time.Now()})
	reader.On("WriteTarget", mock.Anything, "payload", readers.WriteOptions{
		TargetUID:  "target",
		ExcludeUID: "source",
	}).Return((*tokens.Token)(nil), assert.AnError).Once()
	player := mocks.NewMockPlayer()
	player.On("PlayBytes", assets.FailSound).Return(nil).Once()

	consumed := handlePendingWrite(svc, target, player)

	require.True(t, consumed)
	assert.Nil(t, svc.State.GetPendingWrite())
	player.AssertExpectations(t)
}

func TestHandlePendingWrite_NoWriterPlaysFailSoundAndClearsPending(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	require.NoError(t, svc.Config.LoadTOML(`
[audio]
scan_feedback = true
`))
	source := tokens.Token{UID: "source", Text: "**write:payload", ScanTime: time.Now(), ReaderID: "reader"}
	target := &tokens.Token{UID: "target", Text: "old", ScanTime: time.Now(), ReaderID: "missing-reader"}
	svc.State.SetPendingWrite(&state.PendingWrite{Payload: "payload", Source: source, CreatedAt: time.Now()})
	player := mocks.NewMockPlayer()
	player.On("PlayBytes", assets.FailSound).Return(nil).Once()

	consumed := handlePendingWrite(svc, target, player)

	require.True(t, consumed)
	assert.Nil(t, svc.State.GetPendingWrite())
	player.AssertExpectations(t)
}

func TestHandlePendingWrite_ExpiresStaleWrite(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	source := tokens.Token{UID: "source", Text: "**write:payload", ScanTime: time.Now(), ReaderID: "reader"}
	target := &tokens.Token{UID: "target", Text: "old", ScanTime: time.Now(), ReaderID: "reader"}
	svc.State.SetPendingWrite(&state.PendingWrite{
		Payload:   "payload",
		Source:    source,
		CreatedAt: time.Now().Add(-pendingWriteTTL - time.Second),
	})

	player := mocks.NewMockPlayer()
	consumed := handlePendingWrite(svc, target, player)

	require.False(t, consumed)
	assert.Nil(t, svc.State.GetPendingWrite())
}

func TestHandlePendingWrite_IgnoresSourceToken(t *testing.T) {
	t.Parallel()

	svc, _, _ := setupNextActionTestEnv(t)
	source := tokens.Token{UID: "source", Text: "**write:payload", ScanTime: time.Now(), ReaderID: "reader"}
	svc.State.SetPendingWrite(&state.PendingWrite{Payload: "payload", Source: source, CreatedAt: time.Now()})

	player := mocks.NewMockPlayer()
	consumed := handlePendingWrite(svc, &source, player)

	require.True(t, consumed)
	assert.NotNil(t, svc.State.GetPendingWrite())
}
