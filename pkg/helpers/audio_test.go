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

package helpers

import (
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/mock"
)

func TestPlayConfiguredSound_Disabled(t *testing.T) {
	t.Parallel()

	player := mocks.NewMockPlayer()
	// No expectations set — any call would fail

	PlayConfiguredSound(player, "", false, []byte("default"), "test")

	player.AssertNotCalled(t, "PlayWAVBytes", mock.Anything)
	player.AssertNotCalled(t, "PlayFile", mock.Anything)
}

func TestPlayConfiguredSound_DefaultSound(t *testing.T) {
	t.Parallel()

	player := mocks.NewMockPlayer()
	defaultData := []byte("default-wav-data")
	player.On("PlayWAVBytes", defaultData).Return(nil).Once()

	PlayConfiguredSound(player, "", true, defaultData, "success")

	player.AssertExpectations(t)
}

func TestPlayConfiguredSound_CustomSound(t *testing.T) {
	t.Parallel()

	player := mocks.NewMockPlayer()
	player.On("PlayFile", "/path/to/custom.wav").Return(nil).Once()

	PlayConfiguredSound(player, "/path/to/custom.wav", true, []byte("default"), "success")

	player.AssertExpectations(t)
	player.AssertNotCalled(t, "PlayWAVBytes", mock.Anything)
}

func TestPlayConfiguredSound_CustomSoundFailsFallsBackToDefault(t *testing.T) {
	t.Parallel()

	player := mocks.NewMockPlayer()
	defaultData := []byte("default-wav-data")
	player.On("PlayFile", "/bad/path.wav").Return(errors.New("file not found")).Once()
	player.On("PlayWAVBytes", defaultData).Return(nil).Once()

	PlayConfiguredSound(player, "/bad/path.wav", true, defaultData, "success")

	player.AssertExpectations(t)
}

func TestPlayConfiguredSound_BothCustomAndFallbackFail(t *testing.T) {
	t.Parallel()

	player := mocks.NewMockPlayer()
	defaultData := []byte("default-wav-data")
	player.On("PlayFile", "/bad/path.wav").Return(errors.New("file not found")).Once()
	player.On("PlayWAVBytes", defaultData).Return(errors.New("decode error")).Once()

	// Should not panic — errors are logged
	PlayConfiguredSound(player, "/bad/path.wav", true, defaultData, "success")

	player.AssertExpectations(t)
}

func TestPlayConfiguredSound_DefaultSoundError(t *testing.T) {
	t.Parallel()

	player := mocks.NewMockPlayer()
	defaultData := []byte("bad-data")
	player.On("PlayWAVBytes", defaultData).Return(errors.New("decode error")).Once()

	// Should not panic — error is logged
	PlayConfiguredSound(player, "", true, defaultData, "fail")

	player.AssertExpectations(t)
}
