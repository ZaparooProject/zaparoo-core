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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// includeExt is the only kind of file include reads. A name given without it
// has it added.
const includeExt = ".txt"

// ErrScriptUnreadable is returned when an include names a file in the scripts
// folder that exists but cannot be used as a script: not a regular file, too
// large, not readable, or reached through a link that leaves the folder.
var ErrScriptUnreadable = errors.New("script file cannot be read")

// cmdInclude runs a ZapScript file from the scripts folder in Core's data
// directory at this point in the script. The file's commands are added to the
// run after this one, so they run under the same token, trust and command
// bound as the rest of it.
//
//nolint:gocritic // single-use parameter in command handler
func cmdInclude(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	spend := env.SpendExpansion
	if spend == nil {
		spend = (&ExpansionBudget{}).Spend
	}
	cmds, err := loadInclude(pl, env.Cmd.Args, spend)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	return platforms.CmdResult{NewCommands: cmds}, nil
}

// ExpandIncludes returns cmds with every include replaced by the commands of
// the file it names, nested includes too, as a run would insert them. Checks
// made before a run, such as whether it launches media, use it to see what
// the run will do. An include that cannot be loaded is kept as it is: running
// it fails the same way, and the run checks what it adds again. An include
// with a when condition is kept too, as it may not run at all.
func ExpandIncludes(pl platforms.Platform, cmds []zapscript.Command) []zapscript.Command {
	if !slices.ContainsFunc(cmds, isInclude) {
		return cmds
	}
	return expandIncludesWithin(pl, cmds, &ExpansionBudget{})
}

func expandIncludesWithin(
	pl platforms.Platform,
	cmds []zapscript.Command,
	budget *ExpansionBudget,
) []zapscript.Command {
	out := make([]zapscript.Command, 0, len(cmds))
	for _, cmd := range cmds {
		if !isInclude(cmd) || hasWhen(cmd) {
			out = append(out, cmd)
			continue
		}
		included, err := loadInclude(pl, cmd.Args, budget.Spend)
		if err != nil {
			out = append(out, cmd)
			continue
		}
		out = append(out, expandIncludesWithin(pl, included, budget)...)
	}
	return out
}

func isInclude(cmd zapscript.Command) bool {
	return cmd.Name == zapscript.ZapScriptCmdInclude
}

//nolint:gocritic // read-only command value
func hasWhen(cmd zapscript.Command) bool {
	_, ok := cmd.AdvArgs.GetWhen()
	return ok
}

// loadInclude reads and parses the script an include's args name, and
// charges it to the run's expansion budget.
func loadInclude(pl platforms.Platform, args []string, spend func(cmds, size int) error) ([]zapscript.Command, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, ErrRequiredArgs
	}
	if len(args) > 1 {
		return nil, ErrArgCount
	}
	name, err := includeScriptName(args[0])
	if err != nil {
		return nil, err
	}

	text, err := readIncludeScript(filepath.Join(helpers.DataDir(pl), config.ScriptsDir), name)
	if err != nil {
		return nil, err
	}

	// Errors name the file but never quote it. Parse errors from the
	// parser give only a position.
	script, err := zapscript.NewParser(text).ParseScript()
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalidScript, name, err)
	}
	if len(script.Cmds) == 0 {
		return nil, fmt.Errorf("%w: %s: script is empty", ErrInvalidScript, name)
	}

	if err := spend(len(script.Cmds), len(text)); err != nil {
		return nil, fmt.Errorf("include %s: %w", name, err)
	}
	return script.Cmds, nil
}

// includeScriptName turns the name on a card into a path relative to the
// scripts folder. It must stay inside the folder, and it always ends in .txt.
func includeScriptName(arg string) (string, error) {
	name := strings.TrimSpace(arg)
	if !strings.EqualFold(filepath.Ext(name), includeExt) {
		name += includeExt
	}
	rel := filepath.FromSlash(name)
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%w: script name must be a path inside the scripts folder: %s",
			ErrInvalidArguments, arg)
	}
	return rel, nil
}

// readIncludeScript reads rel from the scripts folder at dir. The folder is
// opened as a root, so neither ".." nor a link can lead outside it, even if
// the file is swapped while it is read. The open does not wait, so a named
// pipe in the folder cannot hold the run.
func readIncludeScript(dir, rel string) (string, error) {
	root, err := os.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrFileNotFound, filepath.ToSlash(rel))
	}
	if err != nil {
		return "", fmt.Errorf("%w: open scripts folder: %w", ErrScriptUnreadable, err)
	}
	defer func() { _ = root.Close() }()

	file, err := root.OpenFile(rel, includeOpenFlags, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrFileNotFound, filepath.ToSlash(rel))
	}
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScriptUnreadable, err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScriptUnreadable, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s: not a regular file", ErrScriptUnreadable, filepath.ToSlash(rel))
	}
	if info.Size() > MaxScriptLength {
		return "", fmt.Errorf("%w: %s: %w: %d bytes (max %d)",
			ErrScriptUnreadable, filepath.ToSlash(rel), ErrScriptTooLong, info.Size(), MaxScriptLength)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxScriptLength+1))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScriptUnreadable, err)
	}
	if lenErr := ValidateScriptLength(string(data)); lenErr != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrScriptUnreadable, filepath.ToSlash(rel), lenErr)
	}
	return string(data), nil
}
