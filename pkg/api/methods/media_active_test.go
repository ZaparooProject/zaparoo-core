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
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleActiveMedia_WithZapScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		activeMedia       *models.ActiveMedia
		setupMock         func(*helpers.MockMediaDBI)
		expectedZapScript string
		expectNil         bool
	}{
		{
			name:              "returns nil when no active media",
			activeMedia:       nil,
			setupMock:         nil,
			expectedZapScript: "",
			expectNil:         true,
		},
		{
			name: "returns zapScript with year when available",
			activeMedia: models.NewActiveMedia(
				"snes",
				"Super Nintendo",
				"/roms/snes/Super Mario World.sfc",
				"Super Mario World",
				"launcher1",
			),
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("GetYearBySystemAndPath", mock.Anything, "snes", "/roms/snes/Super Mario World.sfc").
					Return("1990", nil)
			},
			expectedZapScript: "@snes/Super Mario World (year:1990)",
			expectNil:         false,
		},
		{
			name: "returns zapScript without year when not found",
			activeMedia: models.NewActiveMedia(
				"snes",
				"Super Nintendo",
				"/roms/snes/Unknown Game.sfc",
				"Unknown Game",
				"launcher1",
			),
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("GetYearBySystemAndPath", mock.Anything, "snes", "/roms/snes/Unknown Game.sfc").
					Return("", nil)
			},
			expectedZapScript: "@snes/Unknown Game",
			expectNil:         false,
		},
		{
			name: "returns zapScript without year on error",
			activeMedia: models.NewActiveMedia(
				"snes",
				"Super Nintendo",
				"/roms/snes/Error Game.sfc",
				"Error Game",
				"launcher1",
			),
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("GetYearBySystemAndPath", mock.Anything, "snes", "/roms/snes/Error Game.sfc").
					Return("", errors.New("db error"))
			},
			expectedZapScript: "@snes/Error Game",
			expectNil:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			mockMediaDB := helpers.NewMockMediaDBI()

			if tt.setupMock != nil {
				tt.setupMock(mockMediaDB)
			}

			// Create state and set active media
			appState, _ := state.NewState(mockPlatform, "test-boot-uuid")
			if tt.activeMedia != nil {
				appState.SetActiveMedia(tt.activeMedia)
			}

			env := requests.RequestEnv{
				Database: &database.Database{
					MediaDB: mockMediaDB,
				},
				Platform: mockPlatform,
				State:    appState,
			}

			result, err := HandleActiveMedia(env)
			require.NoError(t, err)

			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				response, ok := result.(models.ActiveMediaResponse)
				require.True(t, ok, "Should return ActiveMediaResponse")
				assert.Equal(t, tt.expectedZapScript, response.ZapScript)
				assert.Equal(t, tt.activeMedia.SystemID, response.SystemID)
				assert.Equal(t, tt.activeMedia.Name, response.Name)
				assert.Equal(t, tt.activeMedia.Path, response.Path)
			}

			mockMediaDB.AssertExpectations(t)
		})
	}
}
