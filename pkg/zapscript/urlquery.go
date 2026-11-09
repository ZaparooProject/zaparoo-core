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
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/ZaparooProject/go-zapscript"
)

// launchArgKeys are the advanced argument names the launch command takes.
var launchArgKeys = sync.OnceValue(func() map[string]bool {
	keys := make(map[string]bool)
	var collect func(t reflect.Type)
	collect = func(t reflect.Type) {
		for i := range t.NumField() {
			f := t.Field(i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				collect(f.Type)
				continue
			}
			if tag := f.Tag.Get("advarg"); tag != "" {
				keys[tag] = true
			}
		}
	}
	collect(reflect.TypeFor[zapscript.LaunchArgs]())
	return keys
})

// foldURLQuery gives a launched http(s) URL back the query string the parser
// read as advanced arguments. A stream such as
// https://host/live.m3u8?id=station keeps ?id=station, while launch
// arguments written alongside it, such as slot, still apply.
func foldURLQuery(cmd zapscript.Command) zapscript.Command {
	if cmd.Name != zapscript.ZapScriptCmdLaunch || len(cmd.Args) == 0 || cmd.AdvArgs.IsEmpty() {
		return cmd
	}
	lower := strings.ToLower(cmd.Args[0])
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return cmd
	}
	known := launchArgKeys()
	kept := make(map[string]string)
	var query []string
	for key, value := range cmd.AdvArgs.Raw() {
		if known[key] {
			kept[key] = value
			continue
		}
		query = append(query, key+"="+value)
	}
	if len(query) == 0 {
		return cmd
	}
	slices.Sort(query)
	sep := "?"
	if strings.Contains(cmd.Args[0], "?") {
		sep = "&"
	}
	cmd.Args = slices.Clone(cmd.Args)
	cmd.Args[0] += sep + strings.Join(query, "&")
	cmd.AdvArgs = zapscript.NewAdvArgs(kept)
	return cmd
}

// LocationZapScript returns ZapScript that launches exactly location: the
// location itself when it already reads that way, so a stream URL stays
// readable on a card, or an escaped launch of it when the parser would read
// part of it as something else.
func LocationZapScript(location string) string {
	script, err := zapscript.NewParser(location).ParseScript()
	if err == nil && len(script.Cmds) == 1 {
		cmd := foldURLQuery(script.Cmds[0])
		if cmd.Name == zapscript.ZapScriptCmdLaunch && len(cmd.Args) == 1 &&
			cmd.Args[0] == location && cmd.AdvArgs.IsEmpty() {
			return location
		}
	}
	return literalLaunch(location)
}
