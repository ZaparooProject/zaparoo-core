//go:build linux && !android

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

// Command zaparoo-sim runs Zaparoo Core on a simulated platform for
// development and testing, and drives a running instance: virtual readers,
// launch journal, fault injection, fixtures and offline network fakes.
// See docs/sim.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const usage = `zaparoo-sim: run and drive a simulated Zaparoo Core.

Usage: zaparoo-sim [--root DIR] [--json] <command> [args]

The root defaults to $ZAPAROO_SIM_ROOT. Every command but "serve" talks to the
instance running from that root.

Instance:
  serve [--port N] [--online-url URL] [--fakes libretro,romm,logs,online]
        [--readers r1,r2] [--foreground]   start Core (detached unless --foreground)
  status                                   instance, readers, active media
  stop                                     shut the instance down

Readers:
  readers ls | rm NAME
  readers add NAME [--no-write] [--no-removable] [--no-auto-detect] [--write-wait-ms N]
  tap [--reader R] [--uid U] [--data HEX] [--type T] [--hold-ms N] [--keep] [TEXT]
                                           tap a tag; removed after --hold-ms (default 200) unless --keep
  hold  [tap flags] [TEXT]                 tap and leave the tag on the reader
  remove [--reader R]                      take the tag off
  reader-error [--reader R]                report a reader error
  scan-error [--reader R] [--message M]    report a failed scan
  unplug | plug [--reader R]               disconnect or reconnect a reader
  write-complete [--reader R] [--uid U]    present a card for a pending write
  write-fail [--reader R] [--mode error|removed|no-tag|unsupported]
                                           make the next write fail

Journal:
  launches [--since SEQ] [--follow] [--timeout D]   launch events only
  journal [--since SEQ] [--kind K,K] [--follow] [--timeout D]

Faults:
  fault ls | set --launcher ID [flags] | clear [--launcher ID]

Core API:
  api METHOD [JSON_PARAMS]                 JSON-RPC call to this instance
  wait NOTIFICATION [--match K=V] [--timeout D]
                                           wait for an API notification

Fixtures:
  fixtures gen [flags]                     write a ROM tree under <root>/roms
  fixtures index [--systems S,S] [--timeout D]
                                           run media.generate and wait for it
  fixtures bigdb [--entries N] [--seed S] [--systems S,S]
                                           stage a large MediaDB directly (perf only)

Fakes (need serve --fakes):
  fake NAME ARGS...                        drive a fake: libretro, romm, logs, online
`

type globals struct {
	root string
	json bool
}

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, errUsage) {
			_, _ = fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		_, _ = fmt.Fprintln(os.Stderr, "zaparoo-sim:", err)
		os.Exit(1)
	}
}

var errUsage = errors.New("usage")

type command func(ctx context.Context, g globals, args []string, stdout io.Writer) error

func commands() map[string]command {
	return map[string]command{
		"serve":          cmdServe,
		"status":         cmdStatus,
		"stop":           cmdStop,
		"readers":        cmdReaders,
		"tap":            cmdTap,
		"hold":           cmdHold,
		"remove":         cmdRemove,
		"reader-error":   cmdReaderError,
		"scan-error":     cmdScanError,
		"unplug":         cmdUnplug,
		"plug":           cmdPlug,
		"write-complete": cmdWriteComplete,
		"write-fail":     cmdWriteFail,
		"launches":       cmdLaunches,
		"journal":        cmdJournal,
		"fault":          cmdFault,
		"api":            cmdAPI,
		"wait":           cmdWait,
		"fixtures":       cmdFixtures,
		"fake":           cmdFake,
	}
}

func run(ctx context.Context, args []string, stdout, _ io.Writer) error {
	g := globals{root: os.Getenv("ZAPAROO_SIM_ROOT")}
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch arg := args[0]; {
		case arg == "--json" || arg == "-json":
			g.json = true
			args = args[1:]
		case arg == "--root" || arg == "-root":
			if len(args) < 2 {
				return errors.New("--root needs a directory")
			}
			g.root = args[1]
			args = args[2:]
		case strings.HasPrefix(arg, "--root="):
			g.root = strings.TrimPrefix(arg, "--root=")
			args = args[1:]
		case arg == "-h" || arg == "--help" || arg == "help":
			_, _ = fmt.Fprint(stdout, usage)
			return nil
		default:
			return fmt.Errorf("unknown flag %s: %w", arg, errUsage)
		}
	}
	if len(args) == 0 {
		return errUsage
	}
	if args[0] == "help" {
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	}
	cmds := commands()
	cmd, ok := cmds[args[0]]
	if !ok {
		names := make([]string, 0, len(cmds))
		for name := range cmds {
			names = append(names, name)
		}
		sort.Strings(names)
		return fmt.Errorf("unknown command %q (have %s): %w", args[0], strings.Join(names, ", "), errUsage)
	}
	rest, jsonOut := extractJSONFlag(args[1:])
	g.json = g.json || jsonOut
	if g.root == "" {
		return errors.New("no sim root: pass --root DIR or set ZAPAROO_SIM_ROOT")
	}
	return cmd(ctx, g, rest, stdout)
}

// extractJSONFlag lets --json follow the command as well as precede it.
func extractJSONFlag(args []string) ([]string, bool) {
	out := make([]string, 0, len(args))
	found := false
	for i, arg := range args {
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
		if arg == "--json" || arg == "-json" {
			found = true
			continue
		}
		out = append(out, arg)
	}
	return out, found
}
