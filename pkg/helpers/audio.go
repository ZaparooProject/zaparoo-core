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

// PlayConfiguredSound plays a sound based on configuration settings.
// If enabled is false, no sound is played.
// If path is empty, plays the default embedded sound.
// If path is set, plays the custom sound file from that path.
// Errors are logged but not returned.
func PlayConfiguredSound(path string, enabled bool, defaultSound []byte, soundName string) {
	if !enabled {
		return
	}

	if path == "" {
		// Use embedded default sound
		if err := audio.PlayWAVBytes(defaultSound); err != nil {
			log.Warn().Err(err).Msgf("error playing %s sound", soundName)
		}
	} else {
		// Use custom sound file
		if err := audio.PlayFile(path); err != nil {
			log.Warn().Str("path", path).Err(err).Msgf("error playing custom %s sound", soundName)
		}
	}
}
