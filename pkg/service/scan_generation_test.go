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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// settled reports whether c completed within d, and with what.
func settled(c *tokens.Completion, d time.Duration) (done bool, err error) {
	select {
	case err := <-c.Done():
		return true, err
	case <-time.After(d):
		return false, nil
	}
}

func TestHandleQueuedToken_NewerScanCutsEarlierDelayShort(t *testing.T) {
	t.Parallel()

	for _, superseding := range []string{"**playlist.stop", "**launch.random:NES"} {
		t.Run(superseding, func(t *testing.T) {
			t.Parallel()
			env := setupRunFailedEnv(t)
			env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

			first := env.queue(t, tokens.SourceReader, "**delay:20s||**playlist.stop")
			done, _ := settled(first, 100*time.Millisecond)
			require.False(t, done, "the delay is still pending")

			// The newer scan's own result does not matter here: a launch
			// with no media to pick fails, but it still supersedes.
			env.queue(t, tokens.SourceReader, superseding)
			done, err := settled(first, runFailedTimeout)
			require.True(t, done, "the newer scan ended the delay")
			require.NoError(t, err, "a delay cut short is not a failure")
		})
	}
}

func TestHandleQueuedToken_CutShortDelayIsNotAFailure(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	first := env.queue(t, tokens.SourceReader, "**delay:20s||**playlist.stop")
	env.queue(t, tokens.SourceReader, "**playlist.stop")
	require.NoError(t, waitRunCompletion(t, first))
	env.expectNoRunFailed(t)
}

func TestHandleQueuedToken_CommandOnlyScanLeavesDelayAlone(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	first := env.queue(t, tokens.SourceReader, "**delay:20s||**echo:after")
	second := env.queue(t, tokens.SourceReader, "**echo:volume")
	require.NoError(t, waitRunCompletion(t, second))
	done, _ := settled(first, 200*time.Millisecond)
	assert.False(t, done, "a command-only scan does not end the delay")

	// A scan that does supersede it still can.
	env.queue(t, tokens.SourceReader, "**playlist.stop")
	done, _ = settled(first, runFailedTimeout)
	assert.True(t, done)
}

func TestHandleQueuedToken_PlaylistItemLeavesDelayAlone(t *testing.T) {
	t.Parallel()
	env := setupRunFailedEnv(t)
	env.player.On("PlayBytes", mock.Anything).Return(nil).Maybe()

	first := env.queue(t, tokens.SourceReader, "**delay:20s||**echo:after")
	item := env.queue(t, tokens.SourcePlaylist, "**playlist.stop")
	require.NoError(t, waitRunCompletion(t, item))
	done, _ := settled(first, 200*time.Millisecond)
	assert.False(t, done, "a playlist's own item does not end the delay")
}
