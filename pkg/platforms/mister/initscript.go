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

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

const (
	initScriptPath = "/etc/init.d/S11zaparoo"
	initRCSPath    = "/etc/init.d/rcS"
	initRootMount  = "/"
)

// initScriptContent returns the init script that starts Core early in MiSTer's
// boot, instead of from user-startup.sh at the very end. The start is
// backgrounded: rcS runs each script in turn and waits for it, and a foreground
// start held networking and Bluetooth back for as long as Core took to come up.
// The file lives in linux.img, so a MiSTer Linux update removes it; Core writes
// it again on its next start, and the user-startup.sh entry starts Core in the
// meantime.
func initScriptContent(zaparooPath string) string {
	return `#!/bin/sh
# Managed by Zaparoo Core. Starts Core early in boot; Core rewrites this file when it changes.
ZAPAROO=` + shellQuote(zaparooPath) + `
[ -x "$ZAPAROO" ] || exit 0
case "$1" in
  start) "$ZAPAROO" -service start >/dev/null 2>&1 & ;;
  stop) "$ZAPAROO" -service stop >/dev/null 2>&1 ;;
esac
`
}

// shellQuote quotes s for a POSIX shell, leaving plain paths unquoted.
func shellQuote(s string) string {
	plain := s != ""
	for _, r := range s {
		safe := r == '/' || r == '.' || r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !safe {
			plain = false
			break
		}
	}
	if plain {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// initScriptSource is the path the init script starts Core from. The service
// runs a hashed copy of the launcher, so os.Executable() names a file that is
// replaced on every update; ZAPAROO_APP holds the launcher it was copied from.
func initScriptSource() string {
	if p := os.Getenv(config.AppEnv); p != "" && filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(misterconfig.ScriptsDir, "zaparoo.sh")
}

// rootMount reads and changes the mount flags of the root filesystem.
type rootMount interface {
	// Flags returns the statfs flags (ST_*) of the filesystem holding path.
	Flags(path string) (int64, error)
	// Remount remounts target with the given mount flags (MS_*).
	Remount(target string, flags uintptr) error
}

type sysRootMount struct{}

func (sysRootMount) Flags(path string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return int64(st.Flags), nil //nolint:unconvert // Flags is int32 on 32-bit ARM
}

func (sysRootMount) Remount(target string, flags uintptr) error {
	if err := unix.Mount("", target, "", flags, ""); err != nil {
		return fmt.Errorf("remount %s: %w", target, err)
	}
	return nil
}

// keptMountFlags are the statfs flags carried over when the root filesystem
// is remounted, so a remount changes nothing but read-only.
var keptMountFlags = []struct {
	st int64
	ms uintptr
}{
	{unix.ST_NOSUID, unix.MS_NOSUID},
	{unix.ST_NODEV, unix.MS_NODEV},
	{unix.ST_NOEXEC, unix.MS_NOEXEC},
	{unix.ST_SYNCHRONOUS, unix.MS_SYNCHRONOUS},
	{unix.ST_MANDLOCK, unix.MS_MANDLOCK},
	{unix.ST_NOATIME, unix.MS_NOATIME},
	{unix.ST_NODIRATIME, unix.MS_NODIRATIME},
	{unix.ST_RELATIME, unix.MS_RELATIME},
}

func remountFlags(stFlags int64, readOnly bool) uintptr {
	flags := uintptr(unix.MS_REMOUNT)
	for _, f := range keptMountFlags {
		if stFlags&f.st != 0 {
			flags |= f.ms
		}
	}
	if readOnly {
		flags |= unix.MS_RDONLY
	}
	return flags
}

// installInitScript writes the early start init script if it is missing or
// out of date. Failure only costs the early start, so it is logged and
// startup carries on.
func installInitScript(ctx context.Context) {
	installInitScriptWith(ctx, afero.NewOsFs(), sysRootMount{}, initScriptContent(initScriptSource()))
}

func installInitScriptWith(ctx context.Context, fs afero.Fs, mount rootMount, content string) {
	if ctx.Err() != nil {
		return
	}
	written, err := ensureInitScript(fs, mount, content)
	switch {
	case err != nil:
		log.Warn().Err(err).Str("path", initScriptPath).Msg("could not install early start init script")
	case written:
		log.Info().Str("path", initScriptPath).Msg("installed early start init script")
	default:
		log.Debug().Str("path", initScriptPath).Msg("early start init script is up to date")
	}
}

// ensureInitScript reports whether it wrote the init script. When the root
// filesystem is mounted read-only, as current MiSTer Linux images do, it is
// remounted read-write for the write and put back to read-only afterwards,
// including when the write fails.
func ensureInitScript(fs afero.Fs, mount rootMount, content string) (written bool, err error) {
	if _, statErr := fs.Stat(initRCSPath); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			// Not a SysV init image: nothing would run the script.
			return false, nil
		}
		return false, fmt.Errorf("checking %s: %w", initRCSPath, statErr)
	}
	if current, readErr := afero.ReadFile(fs, initScriptPath); readErr == nil {
		if string(current) == content {
			info, statErr := fs.Stat(initScriptPath)
			if statErr == nil && info.Mode().Perm()&0o111 != 0 {
				return false, nil
			}
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return false, fmt.Errorf("reading %s: %w", initScriptPath, readErr)
	}

	stFlags, err := mount.Flags(initRootMount)
	if err != nil {
		return false, fmt.Errorf("reading root mount flags: %w", err)
	}
	if stFlags&unix.ST_RDONLY != 0 {
		if remountErr := mount.Remount(initRootMount, remountFlags(stFlags, false)); remountErr != nil {
			return false, fmt.Errorf("making root filesystem writable: %w", remountErr)
		}
		defer func() {
			if restoreErr := mount.Remount(initRootMount, remountFlags(stFlags, true)); restoreErr != nil {
				err = errors.Join(err, fmt.Errorf("restoring read-only root filesystem: %w", restoreErr))
			}
		}()
	}

	if err := writeInitScript(fs, content); err != nil {
		return false, err
	}
	return true, nil
}

func writeInitScript(fs afero.Fs, content string) error {
	dir := filepath.Dir(initScriptPath)
	tmp, err := afero.TempFile(fs, dir, "."+filepath.Base(initScriptPath)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary init script: %w", err)
	}
	tmpPath := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = fs.Remove(tmpPath)
		}
	}()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temporary init script: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing temporary init script: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temporary init script: %w", err)
	}
	if err := fs.Chmod(tmpPath, 0o755); err != nil { //nolint:gosec // init scripts must be executable by init
		return fmt.Errorf("making init script executable: %w", err)
	}
	if err := fs.Rename(tmpPath, initScriptPath); err != nil {
		return fmt.Errorf("replacing init script: %w", err)
	}
	renamed = true

	if d, openErr := fs.Open(dir); openErr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
