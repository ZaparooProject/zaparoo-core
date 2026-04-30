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

package crypto_test

import (
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKAT_PakeWireFormat_SpecExample uses the verbatim message-A example
// from docs/api/encryption.md and confirms it round-trips through the
// implementation's encode/decode without any field rename, type change,
// or coordinate-format mutation. Acts as a frozen contract for client
// implementors quoting the spec.
func TestKAT_PakeWireFormat_SpecExample(t *testing.T) {
	t.Parallel()

	specExample := []byte(`{
		"role": 0,
		"ux": "793136080485469241208656611513609866400481671852",
		"uy": "59748757929350367369315811184980635230185250460108398961713395032485227207304",
		"vx": "1086685267857089638167386722555472967068468061489",
		"vy": "9157340230202296554417312816309453883742349874205386245733062928888341584123",
		"xx": "48439561293906451759052585252797914202762949526041747995844080717082404635286",
		"xy": "36134250956749795798585127919587881956611106672985015071877198253568414405109",
		"yx": "0",
		"yy": "0"
	}`)

	internal, err := crypto.DecodePakeMessage(specExample)
	require.NoError(t, err, "spec example must decode")

	wire, err := crypto.EncodePakeMessage(internal)
	require.NoError(t, err, "decoded message must re-encode")

	var got, want map[string]any
	require.NoError(t, json.Unmarshal(wire, &got))
	require.NoError(t, json.Unmarshal(specExample, &want))

	assert.Equal(t, want, got, "wire format must round-trip the spec example exactly")

	for _, k := range []string{"ux", "uy", "vx", "vy", "xx", "xy", "yx", "yy"} {
		_, isString := got[k].(string)
		assert.True(t, isString,
			"field %q must be a JSON string per spec (avoids IEEE-754 truncation)", k)
	}
}
