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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/rs/zerolog/log"
)

// PlayConfiguredSound plays a sound based on configuration. Custom sound files
// fall back to the embedded default on error.
func PlayConfiguredSound(player audio.Player, path string, enabled bool, defaultSound []byte, soundName string) {
	if !enabled {
		return
	}

	if path == "" {
		if err := player.PlayWAVBytes(defaultSound); err != nil {
			log.Warn().Err(err).Msgf("error playing %s sound", soundName)
		}
		return
	}

	if err := player.PlayFile(path); err != nil {
		log.Warn().Str("path", path).Err(err).Msgf("error playing custom %s sound, falling back to default", soundName)
		if fbErr := player.PlayWAVBytes(defaultSound); fbErr != nil {
			log.Warn().Err(fbErr).Msgf("error playing fallback %s sound", soundName)
		}
	}
}
