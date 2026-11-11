//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

const (
	frontendUser = "ark"
	frontendHome = "/home/ark"
	shellPath    = "/bin/sh"
	// shellArgv0 names $0 in the game shell so it is recognisable in ps.
	shellArgv0 = "zaparoo-arkos"
)

// safeToken matches the values ES substitutes unquoted into a command line
// (emulator, core, governor, system names). They come from config files, so
// anything else is refused rather than handed to the shell.
var safeToken = regexp.MustCompile(`^[A-Za-z0-9._+-]*$`)

// account is the frontend user games run as.
type account struct {
	home   string
	name   string
	groups []uint32
	uid    uint32
	gid    uint32
}

func lookupFrontendAccount() (*account, error) {
	u, err := user.Lookup(frontendUser)
	if err != nil {
		return nil, fmt.Errorf("look up user %s: %w", frontendUser, err)
	}
	return accountFromUser(u)
}

func accountFromUser(u *user.User) (*account, error) {
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("parse uid %q: %w", u.Uid, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("parse gid %q: %w", u.Gid, err)
	}
	acct := &account{
		name: u.Username,
		home: u.HomeDir,
		uid:  uint32(uid),
		gid:  uint32(gid),
	}
	if acct.home == "" {
		acct.home = frontendHome
	}
	groupIDs, err := u.GroupIds()
	if err != nil {
		return nil, fmt.Errorf("look up groups of %s: %w", u.Username, err)
	}
	for _, g := range groupIDs {
		id, parseErr := strconv.ParseUint(g, 10, 32)
		if parseErr != nil {
			continue
		}
		acct.groups = append(acct.groups, uint32(id))
	}
	return acct, nil
}

// launchSpec is one game process: a shell script built from the ES command
// template, with the ROM path and basename passed as positional parameters so
// the path never becomes shell source.
type launchSpec struct {
	acct   *account
	script string
	args   []string
}

// buildScript turns an ES command template into a shell script. Path
// placeholders become quoted positional parameters ("$1" is the ROM, "$2" its
// basename); the remaining placeholders are substituted as ES does, but only
// when they are plain tokens.
func buildScript(launch *esLaunch, home string) (string, error) {
	tokens := map[string]string{
		"%EMULATOR%": launch.emulator,
		"%CORE%":     launch.core,
		"%GOVERNOR%": launch.governor,
		"%SYSTEM%":   launch.system.Name,
	}
	for name, value := range tokens {
		if !safeToken.MatchString(value) {
			return "", fmt.Errorf("refusing %s value %q from ES config", name, value)
		}
	}
	if launch.command == "" {
		return "", errors.New("ES system has no launch command")
	}
	replacer := strings.NewReplacer(
		"%HOME%", home,
		"%EMULATOR%", launch.emulator,
		"%CORE%", launch.core,
		"%GOVERNOR%", launch.governor,
		"%SYSTEM%", launch.system.Name,
	)
	return replacer.Replace(substitutePathParams(launch.command)), nil
}

// pathParams maps the path placeholders to the positional parameter that
// carries each one.
var pathParams = []struct{ placeholder, param string }{
	{"%ROM_RAW%", "$1"},
	{"%ROM%", "$1"},
	{"%BASENAME%", "$2"},
}

// substitutePathParams replaces the path placeholders with positional
// parameters quoted for where each one sits, since ES templates may already
// quote them: bare $1 inside double quotes, "$1" outside quotes, and closing
// and reopening a single-quoted string around "$1".
func substitutePathParams(command string) string {
	var out strings.Builder
	var inSingle, inDouble bool
	for i := 0; i < len(command); {
		c := command[i]
		switch {
		case c == '\\' && !inSingle && i+1 < len(command):
			out.WriteString(command[i : i+2])
			i += 2
			continue
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '%':
			if param, n := matchPathParam(command[i:]); n > 0 {
				switch {
				case inDouble:
					out.WriteString(param)
				case inSingle:
					out.WriteString(`'"` + param + `"'`)
				default:
					out.WriteString(`"` + param + `"`)
				}
				i += n
				continue
			}
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}

func matchPathParam(s string) (param string, n int) {
	for _, p := range pathParams {
		if strings.HasPrefix(s, p.placeholder) {
			return p.param, len(p.placeholder)
		}
	}
	return "", 0
}

func newLaunchSpec(launch *esLaunch, acct *account, romPath string) (*launchSpec, error) {
	script, err := buildScript(launch, acct.home)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(filepath.Base(romPath), filepath.Ext(romPath))
	return &launchSpec{
		acct:   acct,
		script: script,
		args:   []string{romPath, base},
	}, nil
}

// gameEnv is the environment emulationstation.service and emulationstation.sh
// give the games ES starts.
func gameEnv(acct *account) []string {
	env := []string{
		"HOME=" + acct.home,
		"USER=" + acct.name,
		"LOGNAME=" + acct.name,
		"SHELL=/bin/bash",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"TERM=linux",
		"XDG_RUNTIME_DIR=/run/user/" + strconv.FormatUint(uint64(acct.uid), 10) + "/",
		"SDL_ASSERT=always_ignore",
		"SDL_VIDEO_EGL_DRIVER=libEGL.so",
	}
	if lang := os.Getenv("LANG"); lang != "" {
		env = append(env, "LANG="+lang)
	}
	return env
}

func gameCommand(spec *launchSpec) *exec.Cmd {
	argv := append([]string{"-c", spec.script, shellArgv0}, spec.args...)
	//nolint:gosec // G204: the script comes from ES config; the ROM path is a positional parameter.
	cmd := exec.CommandContext(context.Background(), shellPath, argv...)
	cmd.Dir = spec.acct.home
	cmd.Env = gameEnv(spec.acct)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:    spec.acct.uid,
			Gid:    spec.acct.gid,
			Groups: spec.acct.groups,
		},
	}
	return cmd
}

func startGameProcess(spec *launchSpec) (*os.Process, error) {
	cmd := gameCommand(spec)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start game: %w", err)
	}
	return cmd.Process, nil
}
