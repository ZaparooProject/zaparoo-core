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

package widgets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeWidgetArgs(t *testing.T, fs afero.Fs, name string, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)
	path := filepath.Join("tmp", name)
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, afero.WriteFile(fs, path, data, 0o600))
	return path
}

func startWidgetApp(
	t *testing.T,
	app *tview.Application,
) (screen tcell.SimulationScreen, done <-chan error) {
	t.Helper()

	screen = tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, screen.Init())
	screen.SetSize(80, 24)
	app.SetScreen(screen)
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- app.Run()
	}()
	done = doneCh
	return screen, done
}

func waitForWidgetExit(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("widget did not stop")
	}
}

func TestSendUIResponseUsesBoundedContextAndPayload(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("request failed")
	tests := []struct {
		clientErr error
		action    models.UIResponseAction
		choiceID  string
		name      string
	}{
		{name: "dismiss", action: models.UIResponseActionDismiss},
		{name: "select", action: models.UIResponseActionSelect, choiceID: "choice-1"},
		{name: "client error", action: models.UIResponseActionConfirm, clientErr: expectedErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := sendUIResponse(
				&config.Instance{}, "event-1", tt.action, tt.choiceID,
				func(ctx context.Context, _ *config.Instance, method, params string) (string, error) {
					_, hasDeadline := ctx.Deadline()
					assert.True(t, hasDeadline)
					assert.Equal(t, models.MethodUIRespond, method)
					var response models.UIRespondParams
					require.NoError(t, json.Unmarshal([]byte(params), &response))
					assert.Equal(t, "event-1", response.ID)
					assert.Equal(t, tt.action, response.Action)
					assert.Equal(t, tt.choiceID, response.ChoiceID)
					return "", tt.clientErr
				},
			)
			if tt.clientErr != nil {
				require.ErrorIs(t, err, expectedErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestWatchCompletionRemovesFileAndStopsApp(t *testing.T) {
	fs := testhelpers.NewMemoryFS()
	completePath := filepath.Join("tmp", "notice.complete")
	require.NoError(t, fs.Fs.MkdirAll(filepath.Dir(completePath), 0o755))
	require.NoError(t, afero.WriteFile(fs.Fs, completePath, []byte{}, 0o600))

	app := tview.NewApplication().SetRoot(tview.NewTextView(), true)
	_, done := startWidgetApp(t, app)
	watchCompletion(app, fs.Fs, completePath)
	waitForWidgetExit(t, done)

	exists, err := afero.Exists(fs.Fs, completePath)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestNoticeUIDismissResponseControlsShutdown(t *testing.T) {
	tests := []struct {
		clientErr error
		name      string
	}{
		{name: "success"},
		{name: "failure", clientErr: errors.New("request failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := testhelpers.NewMemoryFS()
			argsPath := writeWidgetArgs(t, fs.Fs, "notice.json", widgetmodels.NoticeArgs{
				Text:        "Notice",
				EventID:     "event-1",
				Timeout:     -1,
				Dismissible: true,
			})
			called := make(chan models.UIRespondParams, 1)
			app, err := buildNoticeUI(
				&config.Instance{}, nil, fs.Fs, argsPath, false,
				func(_ context.Context, _ *config.Instance, method, params string) (string, error) {
					if method != models.MethodUIRespond {
						return "", fmt.Errorf("unexpected method: %s", method)
					}
					var response models.UIRespondParams
					if unmarshalErr := json.Unmarshal([]byte(params), &response); unmarshalErr != nil {
						return "", fmt.Errorf("unmarshal UI response: %w", unmarshalErr)
					}
					called <- response
					return "", tt.clientErr
				},
			)
			require.NoError(t, err)
			screen, done := startWidgetApp(t, app)
			require.NoError(t, screen.PostEvent(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)))

			select {
			case response := <-called:
				assert.Equal(t, models.UIResponseActionDismiss, response.Action)
			case <-time.After(time.Second):
				t.Fatal("notice did not send dismiss response")
			}
			if tt.clientErr == nil {
				waitForWidgetExit(t, done)
				return
			}
			select {
			case err = <-done:
				t.Fatalf("notice stopped after failed response: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			app.Stop()
			waitForWidgetExit(t, done)
		})
	}
}

func TestPickerUIResponses(t *testing.T) {
	tests := []struct {
		clientErr error
		key       *tcell.EventKey
		action    models.UIResponseAction
		choiceID  string
		name      string
	}{
		{
			name:     "select success",
			key:      tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
			action:   models.UIResponseActionSelect,
			choiceID: "choice-1",
		},
		{
			name:   "dismiss success",
			key:    tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone),
			action: models.UIResponseActionDismiss,
		},
		{
			name:      "select failure",
			key:       tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
			action:    models.UIResponseActionSelect,
			choiceID:  "choice-1",
			clientErr: errors.New("request failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := testhelpers.NewMemoryFS()
			argsPath := writeWidgetArgs(t, fs.Fs, "picker.json", widgetmodels.PickerArgs{
				Title:       "Pick",
				EventID:     "event-1",
				Items:       []widgetmodels.PickerItem{{ID: "choice-1", Name: "Game"}},
				Selected:    0,
				Timeout:     -1,
				Dismissible: true,
			})
			called := make(chan models.UIRespondParams, 1)
			app, err := buildPickerUI(
				&config.Instance{}, nil, fs.Fs, argsPath,
				func(_ context.Context, _ *config.Instance, method, params string) (string, error) {
					if method != models.MethodUIRespond {
						return "", fmt.Errorf("unexpected method: %s", method)
					}
					var response models.UIRespondParams
					if unmarshalErr := json.Unmarshal([]byte(params), &response); unmarshalErr != nil {
						return "", fmt.Errorf("unmarshal UI response: %w", unmarshalErr)
					}
					called <- response
					return "", tt.clientErr
				},
			)
			require.NoError(t, err)
			screen, done := startWidgetApp(t, app)
			require.NoError(t, screen.PostEvent(tt.key))

			select {
			case response := <-called:
				assert.Equal(t, tt.action, response.Action)
				assert.Equal(t, tt.choiceID, response.ChoiceID)
			case <-time.After(time.Second):
				t.Fatal("picker did not send UI response")
			}
			if tt.clientErr == nil {
				waitForWidgetExit(t, done)
				return
			}
			select {
			case err = <-done:
				t.Fatalf("picker stopped after failed response: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			app.Stop()
			waitForWidgetExit(t, done)
		})
	}
}
