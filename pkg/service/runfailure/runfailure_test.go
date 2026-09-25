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
package runfailure

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err      error
		name     string
		category string
		message  string
	}{
		{name: "script busy", err: platforms.ErrScriptAlreadyRunning, category: models.ErrorCategoryBusy},
		{name: "launch in progress", err: state.ErrLaunchInProgress, category: models.ErrorCategoryBusy},
		{name: "file not found", err: zapscript.ErrFileNotFound, category: models.ErrorCategoryMediaNotFound},
		{
			name:     "wrapped no title match",
			err:      fmt.Errorf("error resolving title SNES/Zelda: %w", titles.ErrNoMatch),
			category: models.ErrorCategoryMediaNotFound, message: "media not found",
		},
		{name: "low confidence", err: titles.ErrLowConfidence, category: models.ErrorCategoryMediaNotFound},
		{name: "disabled", err: state.ErrRunZapScriptDisabled, category: models.ErrorCategoryDisabled},
		{
			name:     "too long keeps the limit text",
			err:      zapscript.ValidateScriptLength(string(make([]byte, zapscript.MaxScriptLength+1))),
			category: models.ErrorCategoryInvalidScript,
			message: fmt.Sprintf("zapscript exceeds maximum length: %d bytes (max %d)",
				zapscript.MaxScriptLength+1, zapscript.MaxScriptLength),
		},
		{name: "unknown command", err: zapscript.ErrUnknownCommand, category: models.ErrorCategoryInvalidScript},
		{name: "unknown system", err: systemdefs.ErrUnknownSystem, category: models.ErrorCategoryInvalidScript},
		{name: "blocked", err: zapscript.ErrCommandBlocked, category: models.ErrorCategoryBlocked},
		{name: "requires profile", err: state.ErrLaunchRequiresProfile, category: models.ErrorCategoryBlocked},
		{name: "playtime", err: playtime.ErrLimitReached, category: models.ErrorCategoryPlaytimeLimit},
		{
			name:     "unknown error carries no detail",
			err:      errors.New("open /roms/secret.sfc: permission denied"),
			category: models.ErrorCategoryExecutionFailed, message: "ZapScript execution failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			category, message := Classify(tt.err)
			assert.Equal(t, tt.category, category)
			if tt.message != "" {
				assert.Equal(t, tt.message, message)
			}
			assert.NotContains(t, message, "/roms/", "message must never carry a path")
		})
	}
}

func notified(t *testing.T, ns chan models.Notification) models.RunFailedParams {
	t.Helper()
	select {
	case n := <-ns:
		require.Equal(t, models.NotificationRunFailed, n.Method)
		var got models.RunFailedParams
		require.NoError(t, json.Unmarshal(n.Params, &got))
		return got
	default:
		t.Fatal("no notification sent")
		return models.RunFailedParams{}
	}
}

func TestNotify_NamesTheFailedCommandAndPlaylistItem(t *testing.T) {
	t.Parallel()
	ns := make(chan models.Notification, 1)
	pls := playlists.NewPlaylist("deck://abc", "abc", []playlists.PlaylistItem{{ZapScript: "a"}, {ZapScript: "b"}})
	pls.Index = 1

	Notify(ns, &tokens.Token{Text: "**launch.title:SNES/Zelda", Source: tokens.SourcePlaylist},
		&CommandError{Name: "launch.title", Err: fmt.Errorf("resolve: %w", titles.ErrNoMatch)}, pls)

	got := notified(t, ns)
	assert.Equal(t, "launch.title", got.Command)
	assert.Equal(t, models.ErrorCategoryMediaNotFound, got.Category)
	assert.Equal(t, "deck://abc", got.PlaylistID)
	require.NotNil(t, got.PlaylistIndex)
	assert.Equal(t, 1, *got.PlaylistIndex)
}

// A zero index is a real position: it must be sent, not dropped as empty.
func TestNotify_SendsPlaylistIndexZero(t *testing.T) {
	t.Parallel()
	ns := make(chan models.Notification, 1)

	Notify(ns, &tokens.Token{Text: "**x", Source: tokens.SourcePlaylist}, errors.New("boom"),
		playlists.NewPlaylist("p", "p", []playlists.PlaylistItem{{ZapScript: "**x"}}))

	var raw map[string]any
	n := <-ns
	require.NoError(t, json.Unmarshal(n.Params, &raw))
	assert.Contains(t, raw, "playlistIndex")
	assert.InDelta(t, 0, raw["playlistIndex"], 0)
}

// A token that carries parsed commands and no text, as a remote operation
// does, reports those commands as its script.
func TestNotify_RendersCommandsWhenThereIsNoText(t *testing.T) {
	t.Parallel()
	ns := make(chan models.Notification, 1)

	Notify(ns, &tokens.Token{Source: tokens.SourceRemote, Commands: []gozapscript.Command{
		{Name: "launch.system", Args: []string{"SNES"}},
	}}, errors.New("boom"), nil)

	assert.Equal(t, "**launch.system:SNES", notified(t, ns).Script)
}

// An over-long script was never run and is never parsed, so it is left out.
func TestNotify_LeavesOutAnOversizedScript(t *testing.T) {
	t.Parallel()
	ns := make(chan models.Notification, 1)
	text := "**echo:" + strings.Repeat("a", zapscript.MaxScriptLength)

	Notify(ns, &tokens.Token{Text: text, Source: tokens.SourceReader},
		zapscript.ValidateScriptLength(text), nil)

	got := notified(t, ns)
	assert.Empty(t, got.Script)
	assert.Equal(t, models.ErrorCategoryInvalidScript, got.Category)
}

// A credential in the script is redacted exactly as history redacts it.
func TestNotify_RedactsCredentials(t *testing.T) {
	t.Parallel()
	ns := make(chan models.Notification, 1)

	Notify(ns, &tokens.Token{Text: "**profile:sw-7f3a9c21", Source: tokens.SourceReader}, errors.New("boom"), nil)

	got := notified(t, ns)
	assert.NotContains(t, got.Script, "sw-7f3a9c21")
	assert.NotEmpty(t, got.Script)
}
