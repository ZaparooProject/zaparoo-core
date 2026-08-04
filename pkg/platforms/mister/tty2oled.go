//go:build linux

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

package mister

import (
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/rs/zerolog/log"
)

// TTY2OLEDPictureName returns MiSTer's arcade setname, matching upstream picture filenames.
func (*Platform) TTY2OLEDPictureName(media *models.ActiveMedia) string {
	if media == nil || media.SystemID != systemdefs.SystemArcade {
		return ""
	}

	mediaPath := strings.TrimSpace(media.Path)
	if mediaPath == "" {
		return ""
	}

	if strings.EqualFold(filepath.Ext(mediaPath), ".mra") {
		mra, err := mgls.ReadMRA(mediaPath)
		if err != nil {
			log.Debug().Err(err).Str("path", mediaPath).Msg("failed to read arcade setname for tty2oled")
			return ""
		}
		return strings.TrimSpace(mra.SetName)
	}

	if filepath.Base(mediaPath) == mediaPath && filepath.Ext(mediaPath) == "" {
		return mediaPath
	}

	return ""
}
