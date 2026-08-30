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
	for i := range script.Cmds {
		cmd := &script.Cmds[i]
		var value string
		switch cmd.Name {
		case gozapscript.ZapScriptCmdProfile:
			if len(cmd.Args) > 0 {
				value = cmd.Args[0]
			}
		case gozapscript.ZapScriptCmdPlaytimeExtend:
			value = cmd.AdvArgs.Get(gozapscript.KeyProfile)
		}
		if value != "" && value != RedactedPlaceholder {
			found = append(found, value)
		}
	}
	return found
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

// HasSensitiveScript reports whether text involves a bearer credential, so
// callers can also drop adjacent raw copies such as a token's data payload.
//
// This keys off the command rather than the value: a token whose text has
// already been redacted may still have an unredacted raw payload beside it.
// Text that cannot be parsed is treated as sensitive, since an unreadable
// script cannot be shown to be free of credentials.
func HasSensitiveScript(text string) bool {
	if strings.TrimSpace(text) == "" {
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
// It fails closed. Text that cannot be parsed, or whose credentials survive
// the replacement, is replaced wholesale.
func RedactScript(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}

	script, err := gozapscript.NewParser(text).ParseScript()
	if err != nil {
		// An unparseable script cannot be shown to be free of credentials.
		return redactedScript
	}

	credentials := scriptCredentials(&script)
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
func RedactToken(text, data string) (redactedText, redactedData string) {
	redactedText = RedactScript(text)
	if HasSensitiveScript(text) {
		return redactedText, ""
	}
	return redactedText, data
}
