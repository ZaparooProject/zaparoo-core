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

package remote

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

// remoteDefaultMaxResults must track methods.defaultMaxResults (unexported):
// it is what a paginated method uses when the caller omits maxResults, and
// shrinkPage needs that starting point to shrink from.
const remoteDefaultMaxResults = 100

// runMethod dispatches an allowlisted, method-backed operation through the
// shared API registry, shrinking a paginated result and retrying once if it
// exceeds spec.limit, per spec.shrink.
func (m *manager) runMethod(ctx context.Context, spec opSpec, raw json.RawMessage) operationResult {
	handler, ok := m.deps.Methods.GetMethod(spec.method)
	if !ok {
		log.Error().Str("method", spec.method).
			Msg("remote operation allowlist references unregistered API method")
		return failResult("internal_error")
	}

	params := raw
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	for {
		response, err := handler(m.requestEnv(ctx, spec.role, params))
		if err != nil {
			return classifyHandlerError(err)
		}
		encoded, marshalErr := json.Marshal(response)
		if marshalErr != nil {
			log.Error().Err(marshalErr).Str("method", spec.method).
				Msg("failed to encode remote operation result")
			return failResult("internal_error")
		}
		if len(encoded) <= spec.limit {
			return operationResult{Status: "succeeded", Result: encoded}
		}
		if spec.shrink == nil {
			return failResult("result_too_large")
		}
		next, shrinkOK := spec.shrink(params)
		if !shrinkOK {
			return failResult("result_too_large")
		}
		params = next
	}
}

// shrinkPage halves a paginated method's maxResults, leaving every other
// param untouched, for retry after an oversized result. Reports false once
// it cannot shrink further (maxResults would drop below 1) or params can't
// be parsed as a JSON object.
func shrinkPage(raw json.RawMessage) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, false
		}
	}
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}

	current := remoteDefaultMaxResults
	if encoded, ok := fields["maxResults"]; ok {
		var value int
		if err := json.Unmarshal(encoded, &value); err == nil && value > 0 {
			current = value
		}
	}
	next := current / 2
	if next < 1 {
		return nil, false
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return nil, false
	}
	fields["maxResults"] = encoded

	out, err := json.Marshal(fields)
	if err != nil {
		return nil, false
	}
	return out, true
}

// classifyHandlerError maps an arbitrary API method error to an opaque
// remote result. Handler error text is never returned to the caller — it is
// logged locally and only a stable code crosses the wire.
func classifyHandlerError(err error) operationResult {
	switch {
	case errors.Is(err, state.ErrLaunchInProgress):
		return operationResult{Status: "busy"}
	case errors.Is(err, methods.ErrForbidden):
		return failResult("forbidden")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return failResult("timeout")
	}
	var clientErr *models.ClientError
	if errors.As(err, &clientErr) {
		return failResult("bad_params")
	}
	log.Warn().Err(err).Msg("remote operation API method failed")
	return failResult("query_failed")
}
