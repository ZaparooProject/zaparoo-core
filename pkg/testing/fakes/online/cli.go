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

package online

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
)

const cliUsage = `usage: online <command>
  link-requests
  link-approve [id]
  link-deny [id]
  account [set [--premium] [--limited]]
  devices
  op enqueue <kind> [json-params]
  ops
  backups ls
  fault set <path-prefix> <401|403|429|500|503|timeout|slow>
  fault clear [path-prefix]
  faults
  requests [clear]`

// ErrUsage is wrapped by CLI errors for a command it could not parse.
var ErrUsage = errors.New(cliUsage)

// CLI runs a control command against the fake through call and prints the
// JSON reply, indented. See cliUsage for the commands.
func CLI(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error {
	if len(args) == 0 {
		return ErrUsage
	}
	run := func(method, path string, body any) error {
		return printCall(ctx, call, stdout, method, path, body)
	}
	cmd, rest := args[0], args[1:]
	switch {
	case cmd == "link-requests" && len(rest) == 0:
		return run(http.MethodGet, "link-requests", nil)
	case (cmd == "link-approve" || cmd == "link-deny") && len(rest) <= 1:
		id := ""
		if len(rest) == 1 {
			id = rest[0]
		}
		return run(http.MethodPost, "link-requests/"+strings.TrimPrefix(cmd, "link-"), map[string]string{"id": id})
	case cmd == "account" && len(rest) == 0:
		return run(http.MethodGet, "account", nil)
	case cmd == "account" && rest[0] == "set":
		account, err := parseAccount(rest[1:])
		if err != nil {
			return err
		}
		return run(http.MethodPost, "account", account)
	case cmd == "devices" && len(rest) == 0:
		return run(http.MethodGet, "devices", nil)
	case cmd == "op" && len(rest) >= 2 && len(rest) <= 3 && rest[0] == "enqueue":
		body := map[string]any{"kind": rest[1]}
		if len(rest) == 3 {
			if !json.Valid([]byte(rest[2])) {
				return fmt.Errorf("op params are not valid JSON: %s", rest[2])
			}
			body["params"] = json.RawMessage(rest[2])
		}
		return run(http.MethodPost, "ops", body)
	case cmd == "ops" && len(rest) == 0:
		return run(http.MethodGet, "ops", nil)
	case cmd == "backups" && len(rest) == 1 && rest[0] == "ls":
		return run(http.MethodGet, "backups", nil)
	case cmd == "fault" && len(rest) == 3 && rest[0] == "set":
		return run(http.MethodPost, "faults", Fault{PathPrefix: rest[1], Kind: rest[2]})
	case cmd == "fault" && len(rest) >= 1 && len(rest) <= 2 && rest[0] == "clear":
		path := "faults"
		if len(rest) == 2 {
			path += "?prefix=" + url.QueryEscape(rest[1])
		}
		return run(http.MethodDelete, path, nil)
	case cmd == "faults" && len(rest) == 0:
		return run(http.MethodGet, "faults", nil)
	case cmd == "requests" && len(rest) == 0:
		return run(http.MethodGet, "requests", nil)
	case cmd == "requests" && len(rest) == 1 && rest[0] == "clear":
		return run(http.MethodDelete, "requests", nil)
	}
	return fmt.Errorf("%w\nunknown or malformed command: %s", ErrUsage, strings.Join(args, " "))
}

func parseAccount(args []string) (Account, error) {
	flags := flag.NewFlagSet("account set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	premium := flags.Bool("premium", false, "give the account cloud backup")
	limited := flags.Bool("limited", false, "shrink the backup quota")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return Account{}, fmt.Errorf("%w\naccount set takes only --premium and --limited", ErrUsage)
	}
	return Account{Premium: *premium, Limited: *limited}, nil
}

func printCall(
	ctx context.Context, call fakectl.Caller, stdout io.Writer, method, path string, body any,
) error {
	var data []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding control request: %w", err)
		}
		data = encoded
	}
	out, err := call(ctx, method, path, data)
	if err != nil {
		return err
	}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		out = []byte(`{"ok":true}`)
	}
	var indented bytes.Buffer
	if json.Indent(&indented, out, "", "  ") != nil {
		indented.Reset()
		indented.Write(out)
	}
	indented.WriteByte('\n')
	if _, err := stdout.Write(indented.Bytes()); err != nil {
		return fmt.Errorf("writing control output: %w", err)
	}
	return nil
}
