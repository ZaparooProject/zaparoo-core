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
	"encoding/base64"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

func HandleLogsDownload(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received logs download request")

	// Carries the captured stderr with the log: a crash that kills the service
	// exists only there, and this is the path a user's uploaded log comes from.
	data, err := helpers.ReadLogBundle(env.Platform, config.LogBundleMaxBytes)
	if err != nil {
		log.Error().Err(err).Msg("failed to read log file")
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	return models.LogDownloadResponse{
		Filename: config.LogFile,
		Size:     len(data),
		Content:  encoded,
	}, nil
}
