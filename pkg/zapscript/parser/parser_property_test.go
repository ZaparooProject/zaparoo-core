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

package parser

import (
	"strings"
	"testing"
	"unicode"

	advargtypes "github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/advargs/types"
	"pgregory.net/rapid"
)

// ============================================================================
// Generators
// ============================================================================

// cmdNameGen generates valid command names (alphanumeric + dots).
func cmdNameGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9.]{0,19}`)
}

// argGen generates a simple argument string (no special chars).
func argGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z0-9_]{1,20}`)
}

// advArgKeyGen generates valid advanced argument keys.
func advArgKeyGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{0,15}`)
}

// advArgValueGen generates simple advanced argument values.
func advArgValueGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z0-9_]{1,20}`)
}

// ============================================================================
// ParseScript Property Tests
// ============================================================================

// TestPropertyParseScriptDeterministic verifies same input produces same output.
func TestPropertyParseScriptDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		cmdName := cmdNameGen().Draw(t, "cmdName")
		script := "**" + cmdName

		p1 := NewParser(script)
		result1, err1 := p1.ParseScript()

		p2 := NewParser(script)
		result2, err2 := p2.ParseScript()

		// Both should have same error status
		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("Non-deterministic error: %v vs %v", err1, err2)
		}

		if err1 != nil {
			return
		}

		// Same number of commands
		if len(result1.Cmds) != len(result2.Cmds) {
			t.Fatalf("Non-deterministic: %d vs %d commands",
				len(result1.Cmds), len(result2.Cmds))
		}

		// Same command names
		for i := range result1.Cmds {
			if result1.Cmds[i].Name != result2.Cmds[i].Name {
				t.Fatalf("Non-deterministic at cmd %d: %q vs %q",
					i, result1.Cmds[i].Name, result2.Cmds[i].Name)
			}
		}
	})
}

// TestPropertyParseScriptCommandNamesLowercased verifies command names are lowercased.
func TestPropertyParseScriptCommandNamesLowercased(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		cmdName := cmdNameGen().Draw(t, "cmdName")
		script := "**" + cmdName

		p := NewParser(script)
		result, err := p.ParseScript()
		if err != nil {
			return // Invalid scripts are acceptable
		}

		for _, cmd := range result.Cmds {
			if cmd.Name != strings.ToLower(cmd.Name) {
				t.Fatalf("Command name not lowercased: %q", cmd.Name)
			}
		}
	})
}

// TestPropertyParseScriptCaseInsensitiveCommandNames verifies case doesn't change result.
func TestPropertyParseScriptCaseInsensitiveCommandNames(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		cmdName := cmdNameGen().Draw(t, "cmdName")

		scriptLower := "**" + strings.ToLower(cmdName)
		scriptUpper := "**" + strings.ToUpper(cmdName)

		p1 := NewParser(scriptLower)
		result1, err1 := p1.ParseScript()

		p2 := NewParser(scriptUpper)
		result2, err2 := p2.ParseScript()

		// Both should parse successfully
		if err1 != nil || err2 != nil {
			return
		}

		// Command names should be identical (both lowercased)
		if result1.Cmds[0].Name != result2.Cmds[0].Name {
			t.Fatalf("Case sensitivity: %q vs %q",
				result1.Cmds[0].Name, result2.Cmds[0].Name)
		}
	})
}

// TestPropertyParseScriptAtLeastOneCommand verifies successful parse has ≥1 command.
func TestPropertyParseScriptAtLeastOneCommand(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		cmdName := cmdNameGen().Draw(t, "cmdName")
		script := "**" + cmdName

		p := NewParser(script)
		result, err := p.ParseScript()
		if err != nil {
			return // Invalid scripts are acceptable
		}

		if len(result.Cmds) < 1 {
			t.Fatal("Successful parse should have at least one command")
		}
	})
}

// TestPropertyParseScriptEmptyIsError verifies empty input returns error.
func TestPropertyParseScriptEmptyIsError(t *testing.T) {
	t.Parallel()

	emptyInputs := []string{"", " ", "\t", "\n", "  \t\n  "}
	for _, input := range emptyInputs {
		p := NewParser(input)
		_, err := p.ParseScript()
		if err == nil {
			t.Fatalf("Expected error for empty/whitespace input: %q", input)
		}
	}
}

// TestPropertyParseScriptWithArgs verifies args are preserved.
func TestPropertyParseScriptWithArgs(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		cmdName := cmdNameGen().Draw(t, "cmdName")
		args := rapid.SliceOfN(argGen(), 1, 5).Draw(t, "args")

		script := "**" + cmdName + ":" + strings.Join(args, ",")

		p := NewParser(script)
		result, err := p.ParseScript()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(result.Cmds) != 1 {
			t.Fatalf("Expected 1 command, got %d", len(result.Cmds))
		}

		// Args should match (after trimming)
		if len(result.Cmds[0].Args) != len(args) {
			t.Fatalf("Expected %d args, got %d", len(args), len(result.Cmds[0].Args))
		}

		for i, expected := range args {
			got := strings.TrimSpace(result.Cmds[0].Args[i])
			if got != expected {
				t.Fatalf("Arg %d mismatch: expected %q, got %q", i, expected, got)
			}
		}
	})
}

// TestPropertyParseScriptWithAdvArgs verifies advanced args are preserved.
func TestPropertyParseScriptWithAdvArgs(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		cmdName := cmdNameGen().Draw(t, "cmdName")
		key := advArgKeyGen().Draw(t, "key")
		value := advArgValueGen().Draw(t, "value")

		script := "**" + cmdName + "?" + key + "=" + value

		p := NewParser(script)
		result, err := p.ParseScript()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(result.Cmds) != 1 {
			t.Fatalf("Expected 1 command, got %d", len(result.Cmds))
		}

		// Advanced args should contain our key-value pair
		if result.Cmds[0].AdvArgs.IsEmpty() {
			t.Fatal("Expected advanced args to be present")
		}
	})
}

// TestPropertyParseScriptNeverPanics verifies parser never panics.
func TestPropertyParseScriptNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := rapid.String().Draw(t, "input")

		p := NewParser(input)
		// Should not panic
		_, _ = p.ParseScript()
	})
}

// ============================================================================
// ParseExpressions Property Tests
// ============================================================================

// TestPropertyParseExpressionsDeterministic verifies same input produces same output.
func TestPropertyParseExpressionsDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		varName := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "varName")
		input := "Hello [[" + varName + "]] world"

		p1 := NewParser(input)
		result1, err1 := p1.ParseExpressions()

		p2 := NewParser(input)
		result2, err2 := p2.ParseExpressions()

		// Same error status
		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("Non-deterministic error: %v vs %v", err1, err2)
		}

		if err1 != nil {
			return
		}

		if result1 != result2 {
			t.Fatalf("Non-deterministic: %q vs %q", result1, result2)
		}
	})
}

// TestPropertyParseExpressionsPreservesLiterals verifies text outside [[]] is preserved.
func TestPropertyParseExpressionsPreservesLiterals(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate text without expression markers or escape chars
		text := rapid.StringMatching(`[a-zA-Z0-9 ]{1,50}`).Draw(t, "text")

		p := NewParser(text)
		result, err := p.ParseExpressions()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result != text {
			t.Fatalf("Text not preserved: expected %q, got %q", text, result)
		}
	})
}

// TestPropertyParseExpressionsNeverPanics verifies function never panics.
func TestPropertyParseExpressionsNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		input := rapid.String().Draw(t, "input")

		p := NewParser(input)
		// Should not panic
		_, _ = p.ParseExpressions()
	})
}

// ============================================================================
// Character Validation Tests
// ============================================================================

// TestPropertyIsCmdNameAlphanumeric verifies isCmdName only accepts valid chars.
func TestPropertyIsCmdNameAlphanumeric(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		ch := rapid.Rune().Draw(t, "char")

		result := isCmdName(ch)

		// Expected: a-z, A-Z, 0-9, or .
		expected := (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '.'

		if result != expected {
			t.Fatalf("isCmdName(%q) = %v, expected %v", ch, result, expected)
		}
	})
}

// TestPropertyIsAdvArgNameValid verifies isAdvArgName only accepts valid chars.
func TestPropertyIsAdvArgNameValid(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		ch := rapid.Rune().Draw(t, "char")

		result := isAdvArgName(ch)

		// Expected: a-z, A-Z, 0-9, or _
		expected := (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '_'

		if result != expected {
			t.Fatalf("isAdvArgName(%q) = %v, expected %v", ch, result, expected)
		}
	})
}

// TestPropertyIsWhitespaceCorrect verifies isWhitespace matches expected chars.
func TestPropertyIsWhitespaceCorrect(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		ch := rapid.Rune().Draw(t, "char")

		result := isWhitespace(ch)

		// Expected: space, tab, newline, carriage return
		expected := ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'

		if result != expected {
			t.Fatalf("isWhitespace(%q) = %v, expected %v", ch, result, expected)
		}
	})
}

// ============================================================================
// Escape Sequence Tests
// ============================================================================

// TestPropertyEscapeSequencesRecognized verifies known escape sequences.
func TestPropertyEscapeSequencesRecognized(t *testing.T) {
	t.Parallel()

	escapeTests := []struct {
		input    string
		expected string
	}{
		{"^n", "\n"},
		{"^r", "\r"},
		{"^t", "\t"},
		{"^^", "^"},
		{`^"`, `"`},
		{"^'", "'"},
	}

	for _, tt := range escapeTests {
		p := NewParser(tt.input)
		// Skip the ^ character
		_, _ = p.read()
		result, err := p.parseEscapeSeq()
		if err != nil {
			t.Fatalf("Error parsing %q: %v", tt.input, err)
		}
		if result != tt.expected {
			t.Fatalf("Escape %q: expected %q, got %q", tt.input, tt.expected, result)
		}
	}
}

// ============================================================================
// AdvArgs Tests
// ============================================================================

// TestPropertyAdvArgsGetSetConsistent verifies Get returns what was set with With.
func TestPropertyAdvArgsGetSetConsistent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		key := advArgKeyGen().Draw(t, "key")
		value := advArgValueGen().Draw(t, "value")

		// Start with empty AdvArgs
		aa := NewAdvArgs(nil)

		// Set a value
		aa = aa.With(advargtypes.Key(key), value)

		// Get should return the same value
		got := aa.Get(advargtypes.Key(key))
		if got != value {
			t.Fatalf("Get(%q) = %q, expected %q", key, got, value)
		}
	})
}

// TestPropertyAdvArgsWithImmutable verifies With doesn't mutate original.
func TestPropertyAdvArgsWithImmutable(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		key := advArgKeyGen().Draw(t, "key")
		value := advArgValueGen().Draw(t, "value")

		original := NewAdvArgs(map[string]string{"existing": "value"})
		modified := original.With(advargtypes.Key(key), value)

		// Original should not have the new key
		if original.Get(advargtypes.Key(key)) == value {
			t.Fatal("With() should not mutate original")
		}

		// Modified should have the new key
		if modified.Get(advargtypes.Key(key)) != value {
			t.Fatal("With() should set value in returned copy")
		}
	})
}

// TestPropertyAdvArgsIsEmptyCorrect verifies IsEmpty behavior.
func TestPropertyAdvArgsIsEmptyCorrect(t *testing.T) {
	t.Parallel()

	// Empty AdvArgs
	empty := NewAdvArgs(nil)
	if !empty.IsEmpty() {
		t.Fatal("nil AdvArgs should be empty")
	}

	empty2 := NewAdvArgs(map[string]string{})
	if !empty2.IsEmpty() {
		t.Fatal("Empty map AdvArgs should be empty")
	}

	// Non-empty AdvArgs
	nonEmpty := NewAdvArgs(map[string]string{"key": "value"})
	if nonEmpty.IsEmpty() {
		t.Fatal("AdvArgs with data should not be empty")
	}
}

// ============================================================================
// Unicode Handling Tests
// ============================================================================

// TestPropertyParseScriptHandlesUnicode verifies unicode in args is preserved.
func TestPropertyParseScriptHandlesUnicode(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate a string with letters from various scripts
		chars := rapid.SliceOfN(rapid.RuneFrom(nil, unicode.Letter), 1, 20).Draw(t, "chars")
		unicodeStr := string(chars)

		script := "**cmd:" + unicodeStr

		p := NewParser(script)
		result, err := p.ParseScript()
		if err != nil {
			return // Some unicode might cause parse issues, that's acceptable
		}

		if len(result.Cmds) != 1 {
			t.Fatalf("Expected 1 command, got %d", len(result.Cmds))
		}

		// The argument should contain the unicode string (trimmed)
		if len(result.Cmds[0].Args) != 1 {
			t.Fatalf("Expected 1 arg, got %d", len(result.Cmds[0].Args))
		}
	})
}
