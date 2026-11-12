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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/rs/zerolog/log"
)

// HandlePlaylistsUpdate changes the shuffle and repeat of the playlist open
// in a slot, keeping its current item and playback. The change is applied by
// the playlist queue worker to whatever playlist is open when it gets there.
//
//nolint:gocritic // single-use parameter in API handler
func HandlePlaylistsUpdate(env requests.RequestEnv) (any, error) {
	var params models.PlaylistsUpdateParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if params.Shuffle == nil && params.Repeat == nil {
		return nil, models.ClientErrf("invalid params: shuffle or repeat is required")
	}
	slot, err := mediaslot.Normalize(params.Slot)
	if err != nil {
		return nil, models.ClientErrf("invalid slot: %w", err)
	}

	update := &playlists.Playlist{Slot: slot, ModeChange: true}
	if params.Shuffle != nil {
		update.ShuffleSet = true
		update.Shuffle = *params.Shuffle
	}
	if params.Repeat != nil {
		repeat, repeatErr := playlists.NormalizeRepeat(*params.Repeat)
		if repeatErr != nil {
			return nil, models.ClientErrf("invalid params: %w", repeatErr)
		}
		update.Repeat = &repeat
	}

	open := env.State.GetActivePlaylist()
	if slot == mediaslot.Background {
		open = env.State.GetBackgroundPlaylist()
	}
	if open == nil {
		return nil, models.ClientErrf("no playlist open in the %s slot", slot)
	}

	log.Info().Str("slot", slot).Any("shuffle", params.Shuffle).Any("repeat", update.Repeat).
		Msg("updating playlist shuffle and repeat")
	if err := env.State.RequestPlaylistUpdate(env.Context, update); err != nil {
		return nil, fmt.Errorf("queue playlist update: %w", err)
	}
	return NoContent{}, nil
}
