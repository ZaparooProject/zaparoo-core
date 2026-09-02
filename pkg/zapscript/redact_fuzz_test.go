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
	"strings"
	"testing"

	gozapscript "github.com/ZaparooProject/go-zapscript"
)

// FuzzRedactScript checks the invariant that matters for a security
// boundary: whatever untrusted token text arrives, no credential survives
// redaction. RedactToken text comes from an NFC tag, so it is entirely attacker
// controlled and may be malformed in ways the parser has to survive.
func FuzzRedactScript(f *testing.F) {
	seeds := []string{
		"**profile:sw-secret",
		"**playtime.extend:15m?profile=sw-secret",
		"**playtime.extend:today?profile=sw-secret",
		"**profile:sw-secret||**launch:/games/snes/mario.sfc",
		`**profile:"sw secret"`,
		`**profile:"unterminated`,
		"**launch:/games/snes/mario.sfc",
		"**profile:",
		"**PROFILE:sw-secret",
		"**playtime.extend:15m?PROFILE=sw-secret",
		"**playtime.extend:15m?Profile=sw-secret",
		"**playtime.extend:15m?profile=a&PROFILE=b",
		"plain text",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		redacted := RedactScript(text)

		// The fail-closed path replaces the script wholesale, so there is
		// nothing left to inspect.
		if redacted != redactedScript && strings.TrimSpace(redacted) != "" {
			// Check the credential position rather than searching for the
			// value as a substring: a short credential can legitimately
			// occur inside the placeholder itself.
			verified, err := gozapscript.NewParser(redacted).ParseScript()
			if err == nil {
				if survived := scriptCredentials(&verified); len(survived) > 0 {
					t.Fatalf("credentials %q survived redaction of %q -> %q",
						survived, text, redacted)
				}
			}
		}

		// History is redacted both when written and when served, so
		// redacting twice must not degrade the text further.
		if again := RedactScript(redacted); again != redacted {
			t.Fatalf("redaction is not idempotent: %q -> %q -> %q", text, redacted, again)
		}
	})
}
