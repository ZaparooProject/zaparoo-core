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
	"fmt"
	"strings"
	"unicode/utf8"

	gozapscript "github.com/ZaparooProject/go-zapscript"
)

// RedactedPlaceholder replaces a bearer credential in text that is logged,
// stored, or returned to API clients.
const RedactedPlaceholder = "[redacted]"

// redactedScript replaces an entire script whose credentials could not be
// isolated. Losing the readable text is preferable to leaking a credential.
// It deliberately carries no command prefix: this value is stored and
// displayed, never executed, and should not read as a runnable command.
const redactedScript = "[redacted script]"

// scriptCredentials returns the bearer credential values carried by a parsed
// script. Both commands that take a profile switch ID are covered: the
// profile card's positional argument, and the profile argument authorizing a
// playtime extension.
//
// Values already replaced by RedactedPlaceholder are skipped, so redacting
// text twice is a no-op and the verification pass below does not reject its
// own output.
func scriptCredentials(script *gozapscript.Script) []string {
	var found []string
	add := func(value string) {
		if value != "" && value != RedactedPlaceholder {
			found = append(found, value)
		}
	}
	for i := range script.Cmds {
		cmd := &script.Cmds[i]
		switch cmd.Name {
		case gozapscript.ZapScriptCmdProfile:
			if len(cmd.Args) > 0 {
				add(cmd.Args[0])
			}
		case gozapscript.ZapScriptCmdPlaytimeExtend:
			for _, value := range advArgsFold(cmd.AdvArgs, gozapscript.KeyProfile) {
				add(value)
			}
		}
	}
	return found
}

// advArgsFold returns every advanced argument value whose key matches name
// with ASCII case folded.
//
// The parser stores advanced argument keys exactly as written, while the
// command decoder matches them into struct fields case-insensitively. So
// `?PROFILE=` authorizes an extension just as `?profile=` does, and an exact
// map lookup would leave that credential in the clear. Every match is
// returned because a script may carry several spellings of the key at once.
func advArgsFold(args gozapscript.AdvArgs, name gozapscript.Key) []string {
	var values []string
	args.Range(func(key gozapscript.Key, value string) bool {
		if strings.EqualFold(string(key), string(name)) {
			values = append(values, value)
		}
		return true
	})
	return values
}

// hasCredentialCommand reports whether any command in the script is one that
// carries a bearer credential, whether or not its value has already been
// replaced.
func hasCredentialCommand(script *gozapscript.Script) bool {
	for i := range script.Cmds {
		switch script.Cmds[i].Name {
		case gozapscript.ZapScriptCmdProfile, gozapscript.ZapScriptCmdPlaytimeExtend:
			return true
		}
	}
	return false
}

// credentialCommands are the commands whose arguments carry a bearer
// credential. Kept beside scriptCredentials because the two must be changed
// together: a command added there and missed here would never be redacted.
var credentialCommands = []string{
	gozapscript.ZapScriptCmdProfile,
	gozapscript.ZapScriptCmdPlaytimeExtend,
}

// mayCarryCredential reports whether text could possibly parse into a
// credential-bearing command.
//
// A command name reaches the parse tree verbatim: the grammar admits only
// [a-zA-Z0-9.] in a name and normalization is a lowercase, so a name absent
// from the source cannot appear in the result. A false return is therefore
// proof that no credential is present, whether or not the text parses, and
// the parse can be skipped entirely. That matters because this runs on every
// token scanned and on every history row read.
func mayCarryCredential(text string) bool {
	for _, name := range credentialCommands {
		if containsFoldASCII(text, name) {
			return true
		}
	}
	return false
}

// containsFoldASCII reports whether s contains needle, comparing ASCII
// letters case-insensitively. needle must already be lowercase ASCII, which
// every command name is.
//
// strings.Contains(strings.ToLower(s), needle) would copy the whole script on
// every call, and real media paths carry capitals, so that copy would be the
// common case rather than the exception.
func containsFoldASCII(s, needle string) bool {
	n := len(needle)
	if n == 0 {
		return true
	}
	if len(s) < n {
		return false
	}

	lower := needle[0]
	upper := lower - ('a' - 'A')
	for i := 0; i+n <= len(s); i++ {
		if c := s[i]; c != lower && c != upper {
			continue
		}
		if equalFoldASCII(s[i:i+n], needle) {
			return true
		}
	}
	return false
}

// equalFoldASCII compares s against a lowercase ASCII needle of the same
// length, folding ASCII case in s.
func equalFoldASCII(s, needle string) bool {
	for i := range len(needle) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != needle[i] {
			return false
		}
	}
	return true
}

// HasSensitiveScript reports whether text involves a bearer credential, so
// callers can also drop adjacent raw copies such as a token's data payload.
//
// This keys off the command rather than the value: a token whose text has
// already been redacted may still have an unredacted raw payload beside it.
// Text that names a credential-bearing command but cannot be parsed is
// treated as sensitive, since an unreadable script cannot be shown to be free
// of credentials.
func HasSensitiveScript(text string) bool {
	if strings.TrimSpace(text) == "" || !mayCarryCredential(text) {
		return false
	}
	script, err := gozapscript.NewParser(text).ParseScript()
	if err != nil {
		return true
	}
	return hasCredentialCommand(&script)
}

// RedactScript removes bearer credentials from ZapScript text while leaving
// everything else readable, so logs and history stay useful for diagnosis.
// A profile card keeps its command name; an extension card additionally
// keeps its amount, which is the part worth auditing.
//
// Credential values are replaced in the original text rather than the script
// being re-rendered from the parse tree, so traits, spacing and any command
// this function does not know about survive untouched.
//
// It fails closed. Text that names a credential-bearing command but cannot be
// parsed, or whose credentials survive the replacement, is replaced wholesale.
func RedactScript(text string) string {
	if strings.TrimSpace(text) == "" || !mayCarryCredential(text) {
		return text
	}

	script, err := gozapscript.NewParser(text).ParseScript()
	if err != nil {
		// An unparseable script cannot be shown to be free of credentials.
		return redactedScript
	}

	return redactParsedScript(text, &script)
}

// redactParsedScript replaces the credentials of an already-parsed script in
// its own source text.
func redactParsedScript(text string, script *gozapscript.Script) string {
	credentials := scriptCredentials(script)
	if len(credentials) == 0 {
		return text
	}

	redacted := text
	for _, credential := range credentials {
		redacted = strings.ReplaceAll(redacted, credential, RedactedPlaceholder)
	}

	// A quoted or escaped credential does not appear literally in the
	// source, so the replacement above can miss it. Re-parse and confirm
	// nothing sensitive survived rather than trusting the substitution.
	verified, err := gozapscript.NewParser(redacted).ParseScript()
	if err != nil || len(scriptCredentials(&verified)) > 0 {
		return redactedScript
	}

	return redacted
}

// RedactToken returns a copy of a token safe to log, store, or return to API
// clients. The raw data payload is dropped entirely for sensitive tokens: it
// is an unparsed copy of the same content, so it cannot be redacted in place.
//
// Both answers come from a single parse. This is the hottest redaction entry
// point: it runs twice per scanned token on the service worker and once per
// row on every history read.
func RedactToken(text, data string) (redactedText, redactedData string) {
	if strings.TrimSpace(text) == "" || !mayCarryCredential(text) {
		return text, data
	}

	script, err := gozapscript.NewParser(text).ParseScript()
	if err != nil {
		// Neither the script nor the raw payload beside it can be shown to
		// be free of credentials.
		return redactedScript, ""
	}

	redactedText = redactParsedScript(text, &script)
	if hasCredentialCommand(&script) {
		return redactedText, ""
	}
	return redactedText, data
}

// ForLog returns text safe to write to a log line: bearer credentials
// replaced, and anything longer than MaxScriptLength truncated.
//
// The truncation is what makes this usable from a reader driver, which logs
// whatever a tag holds before anything has bounded it. A legitimate script is
// never affected, because a longer one is rejected rather than run, so the
// only text this shortens is text that was never going to do anything except
// fill a log that lives in tmpfs.
func ForLog(text string) string {
	if len(text) <= MaxScriptLength {
		return RedactScript(text)
	}
	// Cut on a rune boundary so the log line stays valid UTF-8. Redaction
	// runs on the kept portion, which is the only part that reaches the log.
	cut := MaxScriptLength
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return fmt.Sprintf("%s\u2026 (%d bytes)", RedactScript(text[:cut]), len(text))
}
