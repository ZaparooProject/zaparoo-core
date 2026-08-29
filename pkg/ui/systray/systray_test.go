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

package systray

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.design/x/clipboard"
)

func TestReloadCore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		failedMethod    string
		expectedError   string
		expectedMethods []string
	}{
		{
			name: "reloads settings then launchers",
			expectedMethods: []string{
				models.MethodSettingsReload,
				models.MethodLaunchersRefresh,
			},
		},
		{
			name:            "stops when settings reload fails",
			failedMethod:    models.MethodSettingsReload,
			expectedMethods: []string{models.MethodSettingsReload},
			expectedError:   "reload settings: request failed",
		},
		{
			name:         "reports launcher refresh failure",
			failedMethod: models.MethodLaunchersRefresh,
			expectedMethods: []string{
				models.MethodSettingsReload,
				models.MethodLaunchersRefresh,
			},
			expectedError: "refresh launchers: request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Instance{}
			var methods []string
			localClient := func(
				_ context.Context,
				actualCfg *config.Instance,
				method string,
				params string,
			) (string, error) {
				require.Same(t, cfg, actualCfg)
				assert.Empty(t, params)
				methods = append(methods, method)
				if method == tt.failedMethod {
					return "", errors.New("request failed")
				}
				return "", nil
			}

			err := reloadCore(cfg, localClient)

			assert.Equal(t, tt.expectedMethods, methods)
			if tt.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.expectedError)
			}
		})
	}
}

// The tray entry has to answer "is there anything to do" from its own label,
// because that is what someone opened the menu to find out.
func TestUpdateMenuTitle(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status    *models.UpdateCheckResponse
		want      string
		clickable bool
	}{
		"nothing read yet": {
			status: nil, want: "Check for Updates", clickable: true,
		},
		"up to date": {
			status:    &models.UpdateCheckResponse{Eligibility: updater.EligibilityEligible},
			want:      "Up to date",
			clickable: true,
		},
		"available": {
			status: &models.UpdateCheckResponse{
				Eligibility:     updater.EligibilityEligible,
				UpdateAvailable: true,
				LatestVersion:   "2.11.0",
			},
			want:      "Install 2.11.0",
			clickable: true,
		},
		"available but not rolled out here": {
			status: &models.UpdateCheckResponse{
				Eligibility:     updater.EligibilityEligible,
				UpdateAvailable: true,
				RolloutHeld:     true,
				LatestVersion:   "2.11.0",
			},
			want:      "not yet rolled out",
			clickable: true,
		},
		// Says so rather than going quiet: an install entry that does nothing
		// is more confusing than one that explains itself.
		"managed install cannot act": {
			status:    &models.UpdateCheckResponse{Eligibility: updater.EligibilityManaged},
			want:      "Updates managed externally",
			clickable: false,
		},
		"development build cannot act": {
			status:    &models.UpdateCheckResponse{Eligibility: updater.EligibilityDevelopment},
			want:      "Development build",
			clickable: false,
		},
		"an install that cannot replace itself cannot act": {
			status:    &models.UpdateCheckResponse{Eligibility: updater.EligibilityUnsupported},
			want:      "Updates unavailable here",
			clickable: false,
		},
		"a rollback is worth surfacing over anything else": {
			status: &models.UpdateCheckResponse{
				Eligibility:     updater.EligibilityEligible,
				UpdateAvailable: true,
				LatestVersion:   "2.11.0",
				LastResult:      &models.UpdateLastResult{Outcome: updater.OutcomeRolledBack},
			},
			want:      "rolled back",
			clickable: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, updateMenuTitle(tt.status), tt.want)
			assert.Equal(t, tt.clickable, updateMenuClickable(tt.status))
		})
	}
}

// The address entry is one of the few things in the tray that can fail on a
// machine that is otherwise fine — a session with no clipboard to write to —
// and the person clicking it has to be told rather than left guessing.
func TestCopyToClipboard(t *testing.T) {
	t.Parallel()

	t.Run("puts the text on the clipboard", func(t *testing.T) {
		t.Parallel()
		var got []byte
		var gotFormat clipboard.Format
		err := copyToClipboard("10.0.0.5",
			func() error { return nil },
			func(_ context.Context, format clipboard.Format, buf []byte,
				_ ...clipboard.Option,
			) (<-chan struct{}, error) {
				gotFormat, got = format, buf
				return make(chan struct{}), nil
			})
		require.NoError(t, err)
		assert.Equal(t, "10.0.0.5", string(got))
		assert.Equal(t, clipboard.FmtText, gotFormat)
	})

	t.Run("reports a clipboard that cannot be opened", func(t *testing.T) {
		t.Parallel()
		written := false
		err := copyToClipboard("10.0.0.5",
			func() error { return errors.New("no display") },
			func(context.Context, clipboard.Format, []byte,
				...clipboard.Option,
			) (<-chan struct{}, error) {
				written = true
				return make(chan struct{}), nil
			})
		require.Error(t, err)
		assert.False(t, written, "nothing should be written to a clipboard that would not open")
	})

	t.Run("reports a write that fails", func(t *testing.T) {
		t.Parallel()
		err := copyToClipboard("10.0.0.5",
			func() error { return nil },
			func(context.Context, clipboard.Format, []byte,
				...clipboard.Option,
			) (<-chan struct{}, error) {
				return nil, errors.New("clipboard unavailable")
			})
		require.Error(t, err)
	})
}
