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
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"golang.org/x/text/unicode/norm"
)

// ErrNotAllowed is returned when a run request is not allowed.
var ErrNotAllowed = errors.New("not allowed")

type NoContent struct{}

// runParamsForLog returns a copy of run params with any bearer credential in
// the ZapScript removed, so a profile card run through the API cannot leave
// its switch ID in the logs.
func runParamsForLog(params *models.RunParams) models.RunParams {
	safe := *params
	if safe.Text != nil {
		redacted := zapscript.RedactScript(*safe.Text)
		safe.Text = &redacted
	}
	if safe.Data != nil && safe.Text != nil && zapscript.HasSensitiveScript(*params.Text) {
		empty := ""
		safe.Data = &empty
	}
	return safe
}

func HandleRun(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received run request")

	if len(env.Params) == 0 {
		return nil, models.ClientErr(validation.ErrMissingParams)
	}

	var t tokens.Token

	var params models.RunParams
	err := json.Unmarshal(env.Params, &params)
	if err == nil {
		// Validate the params
		if err := validation.DefaultValidator.Validate(&params); err != nil {
			log.Warn().Err(err).Msg("invalid params")
			return nil, models.ClientErrf("invalid params: %w", err)
		}

		log.Debug().Msgf("unmarshalled run params: %+v", runParamsForLog(&params))

		if params.Type != nil {
			t.Type = *params.Type
		}

		if params.UID != nil {
			t.UID = *params.UID
		}

		if params.Text != nil {
			t.Text = norm.NFC.String(*params.Text)
		}

		if params.Data != nil {
			t.Data = strings.ToLower(*params.Data)
			t.Data = strings.ReplaceAll(t.Data, " ", "")

			if _, err := hex.DecodeString(t.Data); err != nil {
				return nil, models.ClientErr(validation.ErrInvalidParams)
			}
		}

		if t.UID == "" && t.Text == "" && t.Data == "" {
			return nil, models.ClientErr(validation.ErrInvalidParams)
		}

		if params.Unsafe {
			t.Unsafe = true
		}
	} else {
		log.Debug().Msg("could not unmarshal run params, trying string")

		var text string
		err := json.Unmarshal(env.Params, &text)
		if err != nil {
			return nil, models.ClientErr(validation.ErrInvalidParams)
		}

		if text == "" {
			return nil, models.ClientErr(validation.ErrMissingParams)
		}

		t.Text = norm.NFC.String(text)
	}

	t.ScanTime = time.Now()
	t.Source = tokens.SourceAPI
	t.Completion = tokens.NewCompletion()

	env.State.SetActiveCard(t)
	select {
	case env.TokenQueue <- t:
	case <-env.Context.Done():
		return nil, runContextError(&env, env.Context.Err())
	}

	// The worker completes every queued token exactly once (preflight
	// rejection, execution result, or recovered panic), so waiting here is
	// safe. Cancellation, timeout, or shutdown only stop the wait: the
	// worker's buffered completion still succeeds and execution that has
	// already started is not rolled back.
	select {
	case err := <-t.Completion.Done():
		return runResult(&env, err)
	case <-env.Context.Done():
		// A result that landed at the same instant wins, so the caller never
		// sees "cancelled" for a script that actually finished.
		select {
		case err := <-t.Completion.Done():
			return runResult(&env, err)
		default:
		}
		return nil, runContextError(&env, env.Context.Err())
	}
}

// runResult maps a terminal result onto the API response. Shutdown is checked
// before the error itself: cancelling the launcher context is how shutdown
// stops a running script, so the failure that surfaces is whatever the
// command made of the cancellation, and reporting that verbatim would tell
// the caller its script was broken rather than that the service went away.
func runResult(env *requests.RequestEnv, err error) (any, error) {
	if err == nil {
		return NoContent{}, nil
	}
	if env.State.GetContext().Err() != nil {
		return nil, models.CategorizedErr(models.ErrorCategoryUnavailable,
			"service is shutting down", err)
	}
	return nil, runError(err)
}

// runContextError explains why run stopped waiting. Shutdown is checked first
// because it also cancels the request context, and the caller should learn
// the service is going away rather than that its own request was cancelled.
func runContextError(env *requests.RequestEnv, ctxErr error) error {
	switch {
	case env.State.GetContext().Err() != nil:
		return models.CategorizedErr(models.ErrorCategoryUnavailable,
			"service is shutting down", ctxErr)
	case errors.Is(ctxErr, context.DeadlineExceeded):
		return models.CategorizedErr(models.ErrorCategoryTimeout,
			"timed out waiting for ZapScript to complete; anything already started continues", ctxErr)
	default:
		return models.CategorizedErr(models.ErrorCategoryCancelled,
			"request cancelled; anything already started continues", ctxErr)
	}
}

// runError maps a terminal execution error onto a stable category with a
// message that carries no filesystem paths or token contents. The cause is
// kept for logging and errors.Is.
func runError(err error) error {
	switch {
	case errors.Is(err, state.ErrLaunchInProgress),
		errors.Is(err, state.ErrMediaLaunchInProgress):
		return models.CategorizedErr(models.ErrorCategoryBusy,
			"another launch is in progress", err)
	case errors.Is(err, zapscript.ErrFileNotFound),
		errors.Is(err, titles.ErrNoMatch),
		errors.Is(err, titles.ErrLowConfidence):
		return models.CategorizedErr(models.ErrorCategoryMediaNotFound,
			"media not found", err)
	case errors.Is(err, state.ErrRunZapScriptDisabled):
		return models.CategorizedErr(models.ErrorCategoryDisabled,
			"ZapScript execution is disabled", err)
	case errors.Is(err, zapscript.ErrInvalidScript),
		errors.Is(err, zapscript.ErrUnknownCommand),
		errors.Is(err, systemdefs.ErrUnknownSystem),
		errors.Is(err, state.ErrInvalidNextAction):
		return models.CategorizedErr(models.ErrorCategoryInvalidScript,
			"ZapScript is invalid", err)
	case errors.Is(err, zapscript.ErrCommandBlocked),
		errors.Is(err, zapscript.ErrExecuteNotAllowed),
		errors.Is(err, zapscript.ErrHTTPNotAllowed),
		errors.Is(err, zapscript.ErrRemoteSource),
		errors.Is(err, state.ErrLaunchBlockedByHook),
		errors.Is(err, state.ErrLaunchRequiresProfile):
		return models.CategorizedErr(models.ErrorCategoryBlocked,
			"ZapScript execution was blocked", err)
	case errors.Is(err, playtime.ErrLimitReached):
		return models.CategorizedErr(models.ErrorCategoryPlaytimeLimit,
			"playtime limit reached", err)
	default:
		return models.CategorizedErr(models.ErrorCategoryExecutionFailed,
			"ZapScript execution failed", err)
	}
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
		if r.URL.RawPath != "" {
			var err error
			text, err = url.PathUnescape(text)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}

		if !isLocalRequest(r) && !cfg.IsRunAllowed(text) {
			log.Warn().Msg("REST run not allowed")
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		log.Info().Msg("running token via REST")

		t := tokens.Token{
			Text:     norm.NFC.String(text),
			ScanTime: time.Now(),
			Source:   tokens.SourceAPI,
		}

		st.SetActiveCard(t)
		select {
		case itq <- t:
		case <-r.Context().Done():
			return
		case <-st.GetContext().Done():
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
	}
}

func waitForCurrentMediaReady(ctx context.Context, st *state.State) error {
	for {
		gen, active := st.ActiveMediaReadyGeneration()
		if !active {
			return nil
		}

		err := st.WaitForActiveMediaReady(ctx, gen)
		switch {
		case err == nil, errors.Is(err, state.ErrNoActiveMedia):
			return nil
		case errors.Is(err, state.ErrActiveMediaChanged):
			continue
		default:
			return fmt.Errorf("wait for active media ready: %w", err)
		}
	}
}

func HandleStop(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received stop request")

	release, err := env.State.AcquireMediaStop(env.Context)
	if err != nil {
		return nil, fmt.Errorf("wait for in-flight media launch: %w", err)
	}
	defer release()

	if err := waitForCurrentMediaReady(env.Context, env.State); err != nil {
		return nil, err
	}

	// TODO: return an error when nothing is active, requires StopActiveLauncher
	// to report whether anything was actually stopped
	if err := env.Platform.StopActiveLauncher(platforms.StopForMenu); err != nil {
		return nil, fmt.Errorf("failed to stop active launcher: %w", err)
	}
	return NoContent{}, nil
}
