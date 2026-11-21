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
	"context"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/artwork"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

func importScreenshotArtwork(ctx context.Context, pl platforms.Platform, db database.MediaDBI) {
	dataDir := helpers.DataDir(pl)
	inbox := filepath.Join(pl.Settings().DataDir, artwork.InboxDir)
	maxBytes := methods.MediaImageMaxBytes(pl)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := artwork.ImportPending(ctx, db, inbox, dataDir, maxBytes, func(system string) {
			methods.InvalidateMediaThumbMemoSystems([]string{system})
		}); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("import screenshot artwork")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
