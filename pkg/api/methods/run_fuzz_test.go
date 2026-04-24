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
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
)

// FuzzHandleRun tests the JSON-RPC run handler with arbitrary params to discover
// edge cases in parameter parsing, Unicode normalization, hex validation, and
// input handling.
func FuzzHandleRun(f *testing.F) {
	// Valid structured params
	f.Add([]byte(`{"text":"SNES/Super Metroid.sfc"}`))
	f.Add([]byte(`{"text":"**launch.system:nes"}`))
	f.Add([]byte(`{"uid":"04ABCDEF"}`))
	f.Add([]byte(`{"data":"DEADBEEF"}`))
	f.Add([]byte(`{"text":"test","unsafe":true}`))
	f.Add([]byte(`{"text":"test","type":"nfc","uid":"04AB"}`))

	// Valid string params (legacy format)
	f.Add([]byte(`"SNES/Super Metroid.sfc"`))
	f.Add([]byte(`"**launch.random:snes"`))

	// Unicode normalization edge cases
	f.Add([]byte(`{"text":"caf\u0065\u0301"}`)) // decomposed e-acute
	f.Add([]byte(`{"text":"caf\u00e9"}`))       // precomposed e-acute
	f.Add([]byte(`{"text":"\u1100\u1161"}`))    // decomposed Hangul

	// Invalid/edge params
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"text":""}`))
	f.Add([]byte(`{"data":"not-hex"}`))
	f.Add([]byte(`{"data":"ZZZZ"}`))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Add([]byte(`42`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`not json`))

	// Missing required fields
	f.Add([]byte(`{"type":"nfc"}`))
	f.Add([]byte(`{"unsafe":true}`))

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	st, notifications := state.NewState(mockPlatform, "fuzz-boot-uuid")
	defer st.StopService()

	// Drain notifications to prevent goroutine leaks
	go func() {
		//nolint:revive // drain channel to prevent goroutine leak
		for range notifications {
		}
	}()

	f.Fuzz(func(t *testing.T, params []byte) {
		itq := make(chan tokens.Token, 1)

		env := requests.RequestEnv{
			Context:    context.Background(),
			State:      st,
			TokenQueue: itq,
			Params:     json.RawMessage(params),
		}

		result, err := HandleRun(env)

		// Empty params must always fail
		if len(params) == 0 && err == nil {
			t.Error("expected error for empty params")
		}

		// Success must return non-nil result
		if err == nil && result == nil {
			t.Error("nil result on success")
		}

		// On success, a token must have been queued
		if err == nil {
			select {
			case tok := <-itq:
				if tok.Source != tokens.SourceAPI {
					t.Errorf("unexpected source: %q", tok.Source)
				}
			default:
				t.Error("no token queued on success")
			}
		}
	})
}
