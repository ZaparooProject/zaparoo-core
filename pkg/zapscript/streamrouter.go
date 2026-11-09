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

package zapscript

import (
	"context"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/radio"
	"github.com/rs/zerolog/log"
)

// streamProbeTimeout bounds how long a launch waits to learn what an
// http(s) URL serves before launching it the way it always has.
const streamProbeTimeout = 5 * time.Second

// probeAudioStream asks link what it serves. It returns the probe when link
// is an audio stream, HLS or a stream playlist, and nil when it serves
// anything else or cannot be reached, so the launch goes on as before.
func probeAudioStream(parent context.Context, link string) *radio.Probe {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, streamProbeTimeout)
	defer cancel()
	probe, err := radio.ProbeURL(ctx, radio.NewHTTPClient(), link)
	if err != nil {
		log.Debug().Err(err).Str("url", link).Msg("url is not an audio stream")
		return nil
	}
	if probe.Kind() == radio.KindNotAudio {
		probe.Close()
		return nil
	}
	return probe
}
