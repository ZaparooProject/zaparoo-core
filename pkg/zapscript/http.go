// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"net/http"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

//nolint:gocritic // single-use parameter in command handler
func cmdHTTPGet(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	} else if env.Cmd.Args[0] == "" {
		return platforms.CmdResult{}, ErrRequiredArgs
	}

	url := env.Cmd.Args[0]

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			log.Error().Err(err).Msgf("creating request for url: %s", url)
			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Error().Err(err).Msgf("getting url: %s", url)
			return
		}
		err = resp.Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
			return
		}
	}()

	return platforms.CmdResult{}, nil
}

//nolint:gocritic // single-use parameter in command handler
func cmdHTTPPost(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 3 {
		return platforms.CmdResult{}, ErrArgCount
	}

	url := env.Cmd.Args[0]
	mime := env.Cmd.Args[1]
	payload := env.Cmd.Args[2]

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(payload))
		if err != nil {
			log.Error().Err(err).Msgf("creating request for url: %s", url)
			return
		}
		req.Header.Set("Content-Type", mime)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Error().Err(err).Msgf("error posting to url: %s", url)
			return
		}
		err = resp.Body.Close()
		if err != nil {
			log.Error().Err(err).Msgf("closing body")
			return
		}
	}()

	return platforms.CmdResult{}, nil
}
