//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

const (
	blockBegin = "# BEGIN ZAPAROO CORE"
	blockEnd   = "# END ZAPAROO CORE"

	autostartFile = "autostart.sh"
	onStartFile   = "runcommand-onstart.sh"
	onEndFile     = "runcommand-onend.sh"

	eventStart = "start"
	eventEnd   = "end"
)

// shellQuote single-quotes s for bash.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func validStateDir(dir string) error {
	if !filepath.IsAbs(dir) || strings.ContainsAny(dir, "\x00\n\r") {
		return fmt.Errorf("unusable Zaparoo state directory: %q", dir)
	}
	return nil
}

// autostartBlock wraps EmulationStation in a loop that runs launch requests
// from Core on the console that owns the display. RetroPie's autostart.sh is
// run by bash and starts EmulationStation with a bare "emulationstation"
// command, so defining a function of that name ahead of it takes over the
// launch without editing RetroPie's own line. RetroPie deletes every line
// containing its auto marker when autostart is reconfigured, so the block
// must never contain that marker.
//
// When EmulationStation exits with a request pending, the request is run and
// EmulationStation starts again afterwards. With no request, the loop ends
// as before, so quitting EmulationStation still leaves a shell. A request
// is two NUL-terminated fields read straight into variables and passed as
// quoted arguments; it is never evaluated.
func autostartBlock(stateDir string) string {
	return autostartBlockFor(stateDir, runcommandPath)
}

func autostartBlockFor(stateDir, runcommand string) string {
	return blockBegin + `
# Lets Zaparoo Core launch games on this console. Remove with: zaparoo -uninstall
zaparoo_dir=` + shellQuote(stateDir) + `
zaparoo_take_request() {
    local work="$zaparoo_dir/request.$$" kind system rom
    mv -f -- "$zaparoo_dir/request" "$work" 2>/dev/null || return 1
    { IFS= read -r -d '' kind && IFS= read -r -d '' system && IFS= read -r -d '' rom; } <"$work"
    rm -f -- "$work"
    [[ "$system" =~ ^[a-z0-9][a-z0-9_.+-]*$ && -f "$rom" ]] || return 0
    case "$kind" in
        sys) ` + shellQuote(runcommand) + ` 0 _SYS_ "$system" "$rom" ;;
        port) bash "$rom" ;;
    esac
    return 0
}
zaparoo_wait_hold() {
    local i
    for ((i = 0; i < 50; i++)); do
        [[ -e "$zaparoo_dir/hold" && ! -e "$zaparoo_dir/request" ]] || break
        sleep 0.2
    done
    rm -f -- "$zaparoo_dir/hold"
}
emulationstation() {
    local ret=0
    mkdir -p -- "$zaparoo_dir"
    rm -f -- "$zaparoo_dir/request" "$zaparoo_dir/hold"
    echo "$$" >"$zaparoo_dir/loop.pid"
    while true; do
        zaparoo_wait_hold
        zaparoo_take_request && continue
        command emulationstation "$@"
        ret=$?
        [[ -e "$zaparoo_dir/request" ]] || break
    done
    rm -f -- "$zaparoo_dir/loop.pid"
    return "$ret"
}
` + blockEnd + "\n"
}

// hookBlock reports a runcommand start or end to Core. runcommand runs its
// hooks with bash as its own children, so $PPID is the runcommand process.
// The subshell keeps the block from touching the user's script state, and
// the rename means Core never reads a half-written state file.
func hookBlock(event, stateDir string) string {
	return blockBegin + `
# Reports games to Zaparoo Core. Remove with: zaparoo -uninstall
(
    zaparoo_dir=` + shellQuote(stateDir) + `
    [ -d "$zaparoo_dir" ] || exit 0
    printf '%s\0' ` + event + ` "$PPID" "$1" "$2" "$3" >"$zaparoo_dir/state.$$" &&
        mv -f -- "$zaparoo_dir/state.$$" "$zaparoo_dir/state"
) 2>/dev/null
` + blockEnd + "\n"
}

// findBlock returns the byte range of the managed block, including the end
// marker's newline, or a negative start when there is no block.
func findBlock(content []byte) (start, end int, err error) {
	offset := 0
	start = -1
	for offset < len(content) {
		lineEnd := bytes.IndexByte(content[offset:], '\n')
		next := len(content)
		if lineEnd >= 0 {
			next = offset + lineEnd + 1
		}
		line := strings.TrimRight(string(content[offset:next]), "\r\n")
		switch {
		case line == blockBegin && start < 0:
			start = offset
		case line == blockBegin:
			return 0, 0, errors.New("nested zaparoo block")
		case line == blockEnd && start >= 0:
			return start, next, nil
		case line == blockEnd:
			return 0, 0, errors.New("zaparoo block end without a start")
		}
		offset = next
	}
	if start >= 0 {
		return 0, 0, errors.New("zaparoo block start without an end")
	}
	return -1, 0, nil
}

// insertBlock puts block at the top of a script, after a shebang line, so
// it runs even when the user's own content exits early. An existing block is
// replaced in place, so installing again only refreshes it.
func insertBlock(content []byte, block string) ([]byte, error) {
	start, end, err := findBlock(content)
	if err != nil {
		return nil, err
	}
	if start >= 0 {
		out := make([]byte, 0, len(content)-(end-start)+len(block))
		out = append(out, content[:start]...)
		out = append(out, block...)
		return append(out, content[end:]...), nil
	}

	at := 0
	if bytes.HasPrefix(content, []byte("#!")) {
		if nl := bytes.IndexByte(content, '\n'); nl >= 0 {
			at = nl + 1
		}
	}
	out := make([]byte, 0, len(content)+len(block))
	out = append(out, content[:at]...)
	out = append(out, block...)
	return append(out, content[at:]...), nil
}

// removeBlock cuts the managed block out, leaving every other byte as it was
// before insertBlock.
func removeBlock(content []byte) (out []byte, found bool, err error) {
	start, end, err := findBlock(content)
	if err != nil || start < 0 {
		return content, false, err
	}
	out = make([]byte, 0, len(content)-(end-start))
	out = append(out, content[:start]...)
	return append(out, content[end:]...), true, nil
}

// writeFileReplacing writes data beside path and renames it into place,
// keeping the existing file's mode.
func writeFileReplacing(fs afero.Fs, path string, data []byte, mode os.FileMode) error {
	tmp, err := afero.TempFile(fs, filepath.Dir(path), "."+filepath.Base(path)+"-")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = fs.Remove(tmpPath)
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err = tmp.Close(); err != nil {
		_ = fs.Remove(tmpPath)
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}
	if err = fs.Chmod(tmpPath, mode); err != nil {
		_ = fs.Remove(tmpPath)
		return fmt.Errorf("chmod %s: %w", tmpPath, err)
	}
	if err = fs.Rename(tmpPath, path); err != nil {
		_ = fs.Remove(tmpPath)
		return fmt.Errorf("rename %s to %s: %w", tmpPath, path, err)
	}
	return nil
}

func addBlockToFile(fs afero.Fs, path, block string, mustExist bool) error {
	content, err := afero.ReadFile(fs, path)
	mode := os.FileMode(0o644)
	switch {
	case err == nil:
		if info, statErr := fs.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
	case errors.Is(err, os.ErrNotExist) && !mustExist:
		content = nil
	default:
		return fmt.Errorf("read %s: %w", path, err)
	}
	updated, err := insertBlock(content, block)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(updated, content) {
		return nil
	}
	return writeFileReplacing(fs, path, updated, mode)
}

// removeBlockFromFile removes the block. With deleteIfEmpty, a file left
// empty is removed too, since the install created it.
func removeBlockFromFile(fs afero.Fs, path string, deleteIfEmpty bool) error {
	content, err := afero.ReadFile(fs, path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	updated, found, err := removeBlock(content)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if !found {
		return nil
	}
	if len(updated) == 0 && deleteIfEmpty {
		if err := fs.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil
	}
	mode := os.FileMode(0o644)
	if info, statErr := fs.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	return writeFileReplacing(fs, path, updated, mode)
}

// installFrontendHooks adds the autostart loop and the runcommand hooks. The
// autostart script must already exist: without it EmulationStation does not
// start at boot and there is no console session to launch games on.
func installFrontendHooks(fs afero.Fs, configsDir, stateDir string) error {
	if err := validStateDir(stateDir); err != nil {
		return err
	}
	allDir := filepath.Join(configsDir, "all")
	autostart := filepath.Join(allDir, autostartFile)
	if _, err := fs.Stat(autostart); err != nil {
		return fmt.Errorf(
			"%s not found; enable \"Boot to EmulationStation\" in RetroPie-Setup first: %w", autostart, err,
		)
	}
	if err := addBlockToFile(fs, autostart, autostartBlock(stateDir), true); err != nil {
		return err
	}
	onStart := filepath.Join(allDir, onStartFile)
	if err := addBlockToFile(fs, onStart, hookBlock(eventStart, stateDir), false); err != nil {
		return err
	}
	return addBlockToFile(fs, filepath.Join(allDir, onEndFile), hookBlock(eventEnd, stateDir), false)
}

func uninstallFrontendHooks(fs afero.Fs, configsDir string) error {
	allDir := filepath.Join(configsDir, "all")
	return errors.Join(
		removeBlockFromFile(fs, filepath.Join(allDir, autostartFile), false),
		removeBlockFromFile(fs, filepath.Join(allDir, onStartFile), true),
		removeBlockFromFile(fs, filepath.Join(allDir, onEndFile), true),
	)
}
