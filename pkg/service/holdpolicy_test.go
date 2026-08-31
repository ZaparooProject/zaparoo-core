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
	"path/filepath"
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
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

// tapTraits and holdTraits build trait sets the way the service does, through
// the real resolver, so the tests cannot drift from how traits are settled.
func tapTraits() tokens.Traits {
	return tokens.ResolveTraits(map[string]any{tokens.TraitTap: true})
}

func holdTraits() tokens.Traits {
	return tokens.ResolveTraits(map[string]any{tokens.TraitHold: true})
}

func newHoldPolicyState(t *testing.T) *state.State {
	t.Helper()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	return st
}

func TestResolveTraitsScanMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		traits map[string]any
		name   string
		want   string
	}{
		{name: "no traits", traits: nil, want: ""},
		{name: "empty traits", traits: map[string]any{}, want: ""},
		{name: "unrelated trait", traits: map[string]any{"favorite": true}, want: ""},
		{name: "tap shorthand", traits: map[string]any{"tap": true}, want: config.ScanModeTap},
		{name: "hold shorthand", traits: map[string]any{"hold": true}, want: config.ScanModeHold},
		{name: "tap false means hold", traits: map[string]any{"tap": false}, want: config.ScanModeHold},
		{name: "hold false means tap", traits: map[string]any{"hold": false}, want: config.ScanModeTap},
		{
			name:   "both agreeing on tap",
			traits: map[string]any{"tap": true, "hold": false},
			want:   config.ScanModeTap,
		},
		{
			name:   "both agreeing on hold",
			traits: map[string]any{"tap": false, "hold": true},
			want:   config.ScanModeHold,
		},
		{
			name:   "both true conflicts",
			traits: map[string]any{"tap": true, "hold": true},
			want:   "",
		},
		{
			name:   "both false conflicts",
			traits: map[string]any{"tap": false, "hold": false},
			want:   "",
		},
		{name: "string true", traits: map[string]any{"tap": "true"}, want: config.ScanModeTap},
		{name: "string yes", traits: map[string]any{"hold": "yes"}, want: config.ScanModeHold},
		{name: "string no", traits: map[string]any{"hold": "no"}, want: config.ScanModeTap},
		{name: "unreadable string ignored", traits: map[string]any{"tap": "maybe"}, want: ""},
		{name: "non zero int", traits: map[string]any{"hold": int64(1)}, want: config.ScanModeHold},
		{name: "zero int", traits: map[string]any{"hold": int64(0)}, want: config.ScanModeTap},
		{name: "float", traits: map[string]any{"tap": 1.5}, want: config.ScanModeTap},
		{name: "array ignored", traits: map[string]any{"tap": []any{"a"}}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tokens.ResolveTraits(tt.traits).ScanMode())
		})
	}
}

// The traits reaching scanModeFromTraits come from the parser, so the two must
// agree on the shape of a real card's ZapScript.
func TestResolveTraitsScanModeFromParsedScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
		want   string
	}{
		{
			name:   "tap before launch",
			script: "#tap||**launch:/media/games/SNES/Mario.sfc",
			want:   config.ScanModeTap,
		},
		{
			name:   "hold before launch",
			script: "#hold||**launch:/media/games/SNES/Mario.sfc",
			want:   config.ScanModeHold,
		},
		{
			name:   "uppercase trait key",
			script: "#TAP||**launch:/media/games/SNES/Mario.sfc",
			want:   config.ScanModeTap,
		},
		{
			name:   "value form",
			script: "#hold=false||**launch:/media/games/SNES/Mario.sfc",
			want:   config.ScanModeTap,
		},
		{
			name:   "trait after the command",
			script: "**launch:/media/games/SNES/Mario.sfc||#tap",
			want:   config.ScanModeTap,
		},
		{
			name:   "json trait form",
			script: `**traits:{"Hold":true}||**launch:/media/games/SNES/Mario.sfc`,
			want:   config.ScanModeHold,
		},
		{
			name:   "conflicting traits inherit",
			script: "#tap #hold||**launch:/media/games/SNES/Mario.sfc",
			want:   "",
		},
		{
			name:   "no traits",
			script: "**launch:/media/games/SNES/Mario.sfc",
			want:   "",
		},
		{
			name:   "misspelled trait is not an override",
			script: "#taap||**launch:/media/games/SNES/Mario.sfc",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			script, err := gozapscript.NewParser(tt.script).ParseScript()
			require.NoError(t, err)
			assert.Equal(t, tt.want, tokens.ResolveTraits(script.Traits).ScanMode())
		})
	}
}

func TestWithHoldOwnerTraits(t *testing.T) {
	t.Parallel()

	t.Run("nil removed token", func(t *testing.T) {
		t.Parallel()
		st := newHoldPolicyState(t)
		assert.Equal(t, tokens.Token{}, withHoldOwnerTraits(st, nil))
	})

	t.Run("keeps its own override", func(t *testing.T) {
		t.Parallel()
		st := newHoldPolicyState(t)
		st.SetSoftwareToken(&tokens.Token{UID: "card", Text: "script", Traits: holdTraits()})

		removed := &tokens.Token{UID: "card", Text: "script", Traits: tapTraits()}
		assert.Equal(t, config.ScanModeTap, withHoldOwnerTraits(st, removed).Traits.ScanMode())
	})

	t.Run("adopts the owner override", func(t *testing.T) {
		t.Parallel()
		st := newHoldPolicyState(t)
		st.SetSoftwareToken(&tokens.Token{
			UID: "card", Text: "script", ReaderID: "reader-a", Traits: tapTraits(),
		})

		removed := &tokens.Token{UID: "card", Text: "script", ReaderID: "reader-b"}
		got := withHoldOwnerTraits(st, removed)
		assert.Equal(t, config.ScanModeTap, got.Traits.ScanMode())
		assert.Equal(t, "reader-b", got.ReaderID, "removal reader must be preserved")
	})

	t.Run("ignores a different owner", func(t *testing.T) {
		t.Parallel()
		st := newHoldPolicyState(t)
		st.SetSoftwareToken(&tokens.Token{UID: "other", Text: "other", Traits: tapTraits()})

		removed := &tokens.Token{UID: "card", Text: "script"}
		assert.Empty(t, withHoldOwnerTraits(st, removed).Traits.ScanMode())
	})

	t.Run("no owner", func(t *testing.T) {
		t.Parallel()
		st := newHoldPolicyState(t)

		removed := &tokens.Token{UID: "card", Text: "script"}
		assert.Empty(t, withHoldOwnerTraits(st, removed).Traits.ScanMode())
	})
}

func TestHoldModeForToken(t *testing.T) {
	t.Parallel()

	const (
		holdReaderID = "mock-hold-1234567890ab"
		tapReaderID  = "mock-tap-1234567890ab"
	)

	newSvc := func(t *testing.T, globalMode, extraTOML string) *ServiceContext {
		t.Helper()

		cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
		require.NoError(t, err)
		cfg.SetScanMode(globalMode)
		if extraTOML != "" {
			require.NoError(t, cfg.LoadTOML(extraTOML))
		}

		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.SetupBasicMock()
		st, _ := state.NewState(mockPlatform, "test-boot-uuid")

		for readerID, path := range map[string]string{
			holdReaderID: "/dev/hold-reader",
			tapReaderID:  "/dev/tap-reader",
		} {
			r := mocks.NewMockReader()
			r.On("ReaderID").Return(readerID).Maybe()
			r.On("Path").Return(path).Maybe()
			r.On("Connected").Return(true).Maybe()
			r.On("Info").Return(readerID).Maybe()
			r.On("Metadata").Return(readers.DriverMetadata{ID: "mock-reader"}).Maybe()
			r.On("Capabilities").Return([]readers.Capability{readers.CapabilityRemovable}).Maybe()
			r.On("OnMediaChange", mock.Anything).Return(nil).Maybe()
			st.SetReader(r)
		}

		return &ServiceContext{Config: cfg, State: st}
	}

	// One reader per physical slot: the cartridge slot holds, the tap antenna
	// does not, on a device whose global mode is tap.
	const twoReaderTOML = `
[[readers.connect]]
driver = "mock-reader"
path = "/dev/hold-reader"
scan_mode = "hold"

[[readers.connect]]
driver = "mock-reader"
path = "/dev/tap-reader"
scan_mode = "tap"
`

	tests := []struct {
		token      *tokens.Token
		name       string
		globalMode string
		extraTOML  string
		want       bool
	}{
		{
			name:       "nil token uses global hold",
			globalMode: config.ScanModeHold,
			token:      nil,
			want:       true,
		},
		{
			name:       "nil token uses global tap",
			globalMode: config.ScanModeTap,
			token:      nil,
			want:       false,
		},
		{
			name:       "no override and no reader uses global",
			globalMode: config.ScanModeHold,
			token:      &tokens.Token{UID: "card"},
			want:       true,
		},
		{
			name:       "token hold override beats global tap",
			globalMode: config.ScanModeTap,
			token:      &tokens.Token{UID: "card", Traits: holdTraits()},
			want:       true,
		},
		{
			name:       "token tap override beats global hold",
			globalMode: config.ScanModeHold,
			token:      &tokens.Token{UID: "card", Traits: tapTraits()},
			want:       false,
		},
		{
			name:       "hold reader holds while global is tap",
			globalMode: config.ScanModeTap,
			extraTOML:  twoReaderTOML,
			token:      &tokens.Token{UID: "card", ReaderID: holdReaderID},
			want:       true,
		},
		{
			name:       "tap reader taps while global is tap",
			globalMode: config.ScanModeTap,
			extraTOML:  twoReaderTOML,
			token:      &tokens.Token{UID: "card", ReaderID: tapReaderID},
			want:       false,
		},
		{
			name:       "tap reader taps while global is hold",
			globalMode: config.ScanModeHold,
			extraTOML:  twoReaderTOML,
			token:      &tokens.Token{UID: "card", ReaderID: tapReaderID},
			want:       false,
		},
		{
			name:       "token override beats its reader config",
			globalMode: config.ScanModeTap,
			extraTOML:  twoReaderTOML,
			token: &tokens.Token{
				UID: "card", ReaderID: holdReaderID, Traits: tapTraits(),
			},
			want: false,
		},
		{
			name:       "disconnected reader falls back to global",
			globalMode: config.ScanModeHold,
			extraTOML:  twoReaderTOML,
			token:      &tokens.Token{UID: "card", ReaderID: "gone-1234567890abcd"},
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newSvc(t, tt.globalMode, tt.extraTOML)
			assert.Equal(t, tt.want, holdModeForToken(svc, tt.token))
		})
	}
}

// Traits are resolved where a token enters the system, so the execution path
// must not re-derive them from the script it is about to run. Without that,
// a hook or an injected command could change the traits of the token running
// it half way through.
// A token with no reader falls through to the global mode, which has to be
// read the same way ScanModeForReader reads it. Before this, a global mode of
// "HOLD" held for a token that came from a reader and tapped for one that did
// not, from the same config.
func TestHoldModeForToken_NoReaderNormalizesGlobalMode(t *testing.T) {
	t.Parallel()

	for _, globalMode := range []string{"HOLD", " hold ", "Hold"} {
		t.Run(globalMode, func(t *testing.T) {
			t.Parallel()

			cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
			require.NoError(t, err)
			cfg.SetScanMode(globalMode)

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.SetupBasicMock()
			st, _ := state.NewState(mockPlatform, "test-boot-uuid")
			svc := &ServiceContext{Platform: mockPlatform, Config: cfg, State: st}

			assert.True(t, holdModeForToken(svc, &tokens.Token{UID: "no-reader"}),
				"a token with no reader must resolve the global mode the same way a reader does")
			assert.True(t, holdModeForToken(svc, nil),
				"and so must a nil token")
		})
	}
}

func TestRunTokenZapScriptDoesNotResolveTraits(t *testing.T) {
	t.Parallel()

	svc := setupPlaylistTestEnv(t)
	mockPlatform, ok := svc.Platform.(*mocks.MockPlatform)
	require.True(t, ok)

	const readerID = "mock-removable-reader"
	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{ID: "mock-reader"}).Maybe()
	mockReader.On("Path").Return(filepath.Join(string(filepath.Separator), "dev", "mock-device")).Maybe()
	mockReader.On("Capabilities").Return([]readers.Capability{readers.CapabilityRemovable}).Maybe()
	mockReader.On("ReaderID").Return(readerID).Maybe()
	svc.State.SetReader(mockReader)

	path := filepath.Join(t.TempDir(), "game.rom")
	mockPlatform.On("LaunchMedia", svc.Config, path, (*platforms.Launcher)(nil), svc.DB,
		mock.Anything).Return(nil).Once()

	// The script says #hold, but this token was handed straight to the
	// execution funnel without passing the resolve step.
	err := runTokenZapScript(svc, tokens.Token{
		UID:      "card",
		Text:     "#hold||**launch:" + path,
		Source:   tokens.SourceReader,
		ReaderID: readerID,
		ScanTime: time.Now(),
	}, playlists.PlaylistController{Queue: make(chan *playlists.Playlist, 1)}, nil, false)
	require.NoError(t, err)

	softwareToken := <-svc.LaunchSoftwareQueue
	require.NotNil(t, softwareToken)
	assert.True(t, softwareToken.Traits.IsEmpty(),
		"running a script must not resolve traits onto the token running it")
}
