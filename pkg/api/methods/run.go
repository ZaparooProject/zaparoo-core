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

package methods

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrMissingParams = errors.New("missing params")
	ErrInvalidParams = errors.New("invalid params")
	ErrNotAllowed    = errors.New("not allowed")
)

type NoContent struct{}

func HandleRun(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received run request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var t tokens.Token

	var params models.RunParams
	err := json.Unmarshal(env.Params, &params)
	if err == nil {
		log.Debug().Msgf("unmarshalled run params: %+v", params)

		if params.Type != nil {
			t.Type = *params.Type
		}

		hasArg := false

		if params.UID != nil {
			t.UID = *params.UID
			hasArg = true
		}

		if params.Text != nil {
			t.Text = norm.NFC.String(*params.Text)
			hasArg = true
		}

		if params.Data != nil {
			t.Data = strings.ToLower(*params.Data)
			t.Data = strings.ReplaceAll(t.Data, " ", "")

			if _, err := hex.DecodeString(t.Data); err != nil {
				return nil, ErrInvalidParams
			}

			hasArg = true
		}

		if !hasArg {
			return nil, ErrInvalidParams
		}

		if params.Unsafe {
			t.Unsafe = true
		}
	} else {
		log.Debug().Msgf("could not unmarshal run params, trying string: %s", env.Params)

		var text string
		err := json.Unmarshal(env.Params, &text)
		if err != nil {
			return nil, ErrInvalidParams
		}

		if text == "" {
			return nil, ErrMissingParams
		}

		t.Text = norm.NFC.String(text)
	}

	t.ScanTime = time.Now()
	t.FromAPI = true

	// TODO: how do we report back errors? put channel in queue
	env.State.SetActiveCard(t)
	env.TokenQueue <- t

	return NoContent{}, nil
}

func isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback()
}

func HandleRunRest(
	cfg *config.Instance,
	st *state.State,
	itq chan<- tokens.Token,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Info().Msg("received REST run request")

		text := chi.URLParam(r, "*")

		if !isLocalRequest(r) && !cfg.IsRunAllowed(text) {
			log.Error().Msgf("run not allowed: %s", text)
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		log.Info().Msgf("running token: %s", text)

		t := tokens.Token{
			Text:     norm.NFC.String(text),
			ScanTime: time.Now(),
			FromAPI:  true,
		}

		st.SetActiveCard(t)
		itq <- t
	}
}

func HandleStop(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received stop request")
	err := env.Platform.StopActiveLauncher()
	if err != nil {
		return nil, fmt.Errorf("failed to stop active launcher: %w", err)
	}
	return NoContent{}, nil
}
