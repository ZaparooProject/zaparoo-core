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

package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/rs/zerolog/log"
)

func beginAPIDiagnostics(ctx context.Context, methodMap *MethodMap, env *requests.RequestEnv,
	method string, transport apidiag.Transport, queueWait time.Duration,
) (context.Context, *apidiag.Recorder) {
	var reporter func(apidiag.Report)
	if methodMap != nil {
		reporter = methodMap.timeoutReporter
	}
	if reporter == nil {
		if !telemetry.Enabled() {
			return ctx, nil
		}
		reporter = telemetry.CaptureAPITimeout
	}
	// Capture providers only, not the request environment or its params/identity.
	db, st := env.Database, env.State
	snapshot := func() apidiag.Snapshot {
		result := apidiag.Snapshot{Scraping: methods.ScrapingDiagnosticState()}
		if db != nil {
			if provider, ok := db.MediaDB.(apidiag.DatabaseProvider); ok {
				result.MediaDB = provider.APIDiagnostics()
			}
			if provider, ok := db.UserDB.(apidiag.DatabaseProvider); ok {
				result.UserDB = provider.APIDiagnostics()
			}
		}
		result.MediaPlaying, result.Recovery = st.APIDiagnosticActivity()
		return result
	}
	return apidiag.New(ctx, method, transport, queueWait, snapshot, reporter, nil)
}

func marshalDiagnosticResponse(ctx context.Context, response any) ([]byte, error) {
	end := apidiag.Begin(ctx, apidiag.ResponseBuild)
	defer end()
	data, err := json.Marshal(response)
	apidiag.RecordError(ctx, err)
	return data, err //nolint:wrapcheck // Callers preserve existing transport-specific error wrapping.
}

func logAPIContextFailure(ctx context.Context, err error, method string) {
	if apidiag.IsTimeout(err) || (ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		log.Warn().Str("method", apidiag.MethodName(method)).Msg("API operation timed out")
		return
	}
	log.Debug().Str("method", apidiag.MethodName(method)).Msg("API operation canceled")
}

func isAPIContextFailure(ctx context.Context, err error) bool {
	return apidiag.IsContextFailure(ctx, err)
}
