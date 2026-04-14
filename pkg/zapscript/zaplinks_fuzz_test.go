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

package zapscript

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

// mockHTTPDoer implements httpDoer for fuzz testing by returning a fixed
// response body without making real network calls.
type mockHTTPDoer struct {
	body       []byte
	statusCode int
}

func (m *mockHTTPDoer) Do(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(bytes.NewReader(m.body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

// FuzzFetchWellKnown tests WellKnown JSON parsing with arbitrary response bodies
// to discover edge cases in JSON decoding and size limit handling.
func FuzzFetchWellKnown(f *testing.F) {
	// Valid WellKnown JSON
	f.Add([]byte(`{"zapscript": 1}`))
	f.Add([]byte(`{"zapscript": 1, "auth": 1}`))
	f.Add([]byte(`{"zapscript": 1, "auth": 1, "trusted": ["zpr.au", "other.com"]}`))
	f.Add([]byte(`{"zapscript": 0}`))
	f.Add([]byte(`{"zapscript": 1, "trusted": []}`))

	// Invalid JSON
	f.Add([]byte(`{invalid json}`))
	f.Add([]byte(`{`))
	f.Add([]byte(`}`))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Add([]byte(`"string"`))
	f.Add([]byte(`42`))

	// Type mismatches
	f.Add([]byte(`{"zapscript": "not_an_int"}`))
	f.Add([]byte(`{"zapscript": null}`))
	f.Add([]byte(`{"zapscript": true}`))
	f.Add([]byte(`{"zapscript": 1, "trusted": "not_array"}`))
	f.Add([]byte(`{"zapscript": 1, "auth": "not_int"}`))

	// Extra/unknown fields
	f.Add([]byte(`{"zapscript": 1, "unknown": "field"}`))
	f.Add([]byte(`{"zapscript": 1, "extra": [1,2,3], "nested": {"a": "b"}}`))

	// Deeply nested
	f.Add([]byte(`{"zapscript": 1, "trusted": ["a","b","c","d","e","f","g","h","i","j"]}`))

	// Unicode in trusted domains
	f.Add([]byte("{\"zapscript\": 1, \"trusted\": [\"\u65e5\u672c\u8a9e.example.com\"]}"))

	f.Fuzz(func(t *testing.T, body []byte) {
		client := &mockHTTPDoer{
			body:       body,
			statusCode: http.StatusOK,
		}

		wk, err := doFetchWellKnown("http://example.com", client)

		// Valid result must have non-nil WellKnown
		if err == nil && wk == nil {
			t.Error("nil WellKnown on success")
		}

		// Error must have nil WellKnown
		if err != nil && wk != nil {
			t.Error("non-nil WellKnown on error")
		}

		// Determinism
		wk2, err2 := doFetchWellKnown("http://example.com", &mockHTTPDoer{
			body:       body,
			statusCode: http.StatusOK,
		})
		if (err == nil) != (err2 == nil) {
			t.Errorf("non-deterministic error for body %q", body)
		}
		if err == nil && err2 == nil {
			if wk.ZapScript != wk2.ZapScript || wk.Auth != wk2.Auth {
				t.Errorf("non-deterministic result for body %q", body)
			}
			if len(wk.Trusted) != len(wk2.Trusted) {
				t.Errorf("non-deterministic Trusted length for body %q: "+
					"%d vs %d", body, len(wk.Trusted), len(wk2.Trusted))
			} else {
				for i := range wk.Trusted {
					if wk.Trusted[i] != wk2.Trusted[i] {
						t.Errorf("non-deterministic Trusted[%d] "+
							"for body %q: %q vs %q",
							i, body, wk.Trusted[i], wk2.Trusted[i])
						break
					}
				}
			}
		}
	})
}
