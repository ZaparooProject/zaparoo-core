//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	eventGameStart = "game-start"
	eventGameEnd   = "game-end"

	// defaultScriptTimeout bounds each event script. ES-fcamod waits for its
	// scripts without a limit; the stock ArkOS ones finish at once and leave
	// any long-running work in the background.
	defaultScriptTimeout = 10 * time.Second
	scriptWaitDelay      = 2 * time.Second
)

// scriptSpec is one event script run: the script and the two arguments ES
// passes it.
type scriptSpec struct {
	acct *account
	path string
	args [2]string
}

// eventScripts lists the scripts Scripting::fireEvent runs for an event: every
// entry of the event folder beside the ES binary, then of the one in the
// frontend user's ~/.emulationstation/scripts. Dotfiles are included, as ES
// includes them; folders are skipped since they cannot run. ES takes entries
// in directory order, which is unspecified, so they are sorted by name here.
func eventScripts(fs afero.Fs, home, event string) []string {
	dirs := []string{
		filepath.Join(esExeDir, "scripts", event),
		filepath.Join(home, ".emulationstation", "scripts", event),
	}
	var scripts []string
	for _, dir := range dirs {
		entries, err := afero.ReadDir(fs, dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			scripts = append(scripts, filepath.Join(dir, name))
		}
	}
	return scripts
}

// esEscapedPath mirrors Utils::FileSystem::getEscapedPath on Linux, which
// backslash-escapes characters that would break a shell word.
func esEscapedPath(p string) string {
	const special = "\\ '\"!$^&*(){}[]?;<>"
	b := []byte(p)
	for i := range len(special) {
		c := special[i]
		for off := 0; off < len(b); off++ {
			if b[off] != c {
				continue
			}
			if off == 0 || b[off-1] != '\\' {
				b = append(b[:off], append([]byte{'\\'}, b[off:]...)...)
				off++
			}
		}
	}
	return string(b)
}

// dquoted returns the word the shell makes of s between double quotes, which
// is how ES-fcamod hands each argument to an event script: a backslash is
// dropped only before $, `, ", \ or a newline. Expansions are not performed;
// ES would run a command substitution in a ROM name, Core passes it literally.
func dquoted(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '$', '`', '"', '\\':
				out.WriteByte(s[i+1])
				i++
				continue
			case '\n':
				i++
				continue
			}
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

// gameStartArgs are the arguments of the game-start event: the escaped ROM
// path and the ROM's stem, each double-quoted on ES's command line.
func gameStartArgs(romPath string) [2]string {
	stem := strings.TrimSuffix(filepath.Base(romPath), filepath.Ext(romPath))
	return [2]string{dquoted(esEscapedPath(romPath)), dquoted(stem)}
}

// fireEvent runs an event's scripts one after another as the frontend user,
// as ES-fcamod does around a game. A script that fails or overruns its time
// limit is logged and the next one runs; the launch never waits on it beyond
// the limit.
func (p *Platform) fireEvent(acct *account, event string, args [2]string) {
	for _, script := range eventScripts(p.fs, acct.home, event) {
		spec := &scriptSpec{acct: acct, path: script, args: args}
		ctx, cancel := context.WithTimeout(context.Background(), p.scriptTimeout)
		err := p.runScript(ctx, spec)
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancel()
		switch {
		case timedOut:
			log.Warn().Str("event", event).Str("script", script).
				Dur("timeout", p.scriptTimeout).Msg("ES event script timed out")
		case err != nil:
			log.Warn().Err(err).Str("event", event).Str("script", script).
				Msg("ES event script failed")
		default:
			log.Debug().Str("event", event).Str("script", script).Msg("ran ES event script")
		}
	}
}

// scriptCommand builds the process for an event script. ES runs
// `<script> "<arg1>" "<arg2>"` through system(), so the script goes through
// /bin/sh, which also runs scripts without a shebang. It gets its own process
// group so a timeout stops everything it started in the foreground.
func scriptCommand(ctx context.Context, spec *scriptSpec) *exec.Cmd {
	//nolint:gosec // G204: the script path comes from ES's own script folders.
	cmd := exec.CommandContext(ctx, shellPath, "-c", `"$0" "$@"`, spec.path, spec.args[0], spec.args[1])
	cmd.Dir = spec.acct.home
	cmd.Env = gameEnv(spec.acct)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Credential: &syscall.Credential{
			Uid:    spec.acct.uid,
			Gid:    spec.acct.gid,
			Groups: spec.acct.groups,
		},
	}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = scriptWaitDelay
	return cmd
}

func runScriptProcess(ctx context.Context, spec *scriptSpec) error {
	if err := scriptCommand(ctx, spec).Run(); err != nil {
		return fmt.Errorf("run %s: %w", spec.path, err)
	}
	return nil
}
