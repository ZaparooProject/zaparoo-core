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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
)

// FuzzProcessRequestObject tests the main JSON-RPC entry point with arbitrary
// byte inputs to discover edge cases in message parsing and routing.
func FuzzProcessRequestObject(f *testing.F) {
	// Valid JSON-RPC requests
	f.Add([]byte(`{"jsonrpc":"2.0","method":"version","id":1}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"version","id":"abc"}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"version","id":null}`))

	// Notifications (no ID)
	f.Add([]byte(`{"jsonrpc":"2.0","method":"notify","params":{}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"notify"}`))

	// Responses
	f.Add([]byte(`{"jsonrpc":"2.0","result":42,"id":1}`))
	f.Add([]byte(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"bad"},"id":1}`))

	// Wrong versions
	f.Add([]byte(`{"jsonrpc":"1.0","method":"x","id":1}`))
	f.Add([]byte(`{"jsonrpc":"3.0","method":"x","id":1}`))
	f.Add([]byte(`{"jsonrpc":"","method":"x","id":1}`))

	// Invalid JSON
	f.Add([]byte(`not json at all`))
	f.Add([]byte(`{`))
	f.Add([]byte(`}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Add([]byte(`"string"`))
	f.Add([]byte(`42`))

	// Nested/complex IDs
	f.Add([]byte(`{"jsonrpc":"2.0","method":"x","id":{"nested":true}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"x","id":[1,2]}`))

	// Large/unusual params
	f.Add([]byte(`{"jsonrpc":"2.0","method":"x","id":1,"params":null}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"x","id":1,"params":[1,2,3]}`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"x","id":1,"params":"string"}`))

	// Unicode
	f.Add([]byte(`{"jsonrpc":"2.0","method":"\u65e5\u672c\u8a9e","id":1}`))

	methodMap := &MethodMap{}
	methodMap.Store("test.echo", func(env requests.RequestEnv) (any, error) {
		return env.Params, nil
	})

	f.Fuzz(func(t *testing.T, msg []byte) {
		env := requests.RequestEnv{
			Context: context.Background(),
		}

		result := processRequestObject(methodMap, env, msg)

		// Invalid JSON must always return ParseError with ShouldReply=true
		if !json.Valid(msg) {
			if !result.ShouldReply {
				t.Error("invalid JSON should set ShouldReply=true")
			}
			if result.Error == nil {
				t.Error("invalid JSON should set Error")
			}
		}

		// Determinism check
		result2 := processRequestObject(methodMap, env, msg)
		if result.ShouldReply != result2.ShouldReply {
			t.Errorf("non-deterministic ShouldReply for input %q", msg)
		}
		if (result.Error == nil) != (result2.Error == nil) {
			t.Errorf("non-deterministic Error for input %q", msg)
		}
	})
}
