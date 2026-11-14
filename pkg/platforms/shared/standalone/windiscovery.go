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

package standalone

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// UninstallEntry is the part of a Windows Uninstall registry entry used to
// find an installed emulator.
type UninstallEntry struct {
	DisplayName     string
	InstallLocation string
	DisplayIcon     string
}

// WindowsEnv is everything Windows discovery reads from the machine. Every
// field is optional, so tests can supply only what they exercise.
type WindowsEnv struct {
	FS     afero.Fs
	Getenv func(key string) string
	// Configured returns the install_dir set for a launcher ID or an
	// emulator name, if any.
	Configured func(launcherID string) string
	// AppPath returns the registered App Paths location of an executable.
	AppPath          func(exe string) string
	UninstallEntries []UninstallEntry
	// FixedDrives are local fixed drive roots such as `C:\`.
	FixedDrives []string
	IndexRoots  []string
}

// WindowsInstall is a discovered emulator executable.
type WindowsInstall struct {
	Exe    string
	Dir    string
	Source string
}

type windowsDiscovery struct {
	env      *WindowsEnv
	listings map[string][]os.FileInfo
	parents  []string
}

// DiscoverWindows finds each emulator's executable. Emulators that are not
// found are left out of the result, which is keyed by emulator Name.
//
// Discovery only lists known folders and their direct children, each once,
// so its cost does not grow with the size of the drives searched.
func DiscoverWindows(env *WindowsEnv, emulators []WindowsEmulator) map[string]WindowsInstall {
	if env.FS == nil {
		env.FS = afero.NewOsFs()
	}
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	d := &windowsDiscovery{env: env, listings: make(map[string][]os.FileInfo)}
	d.parents = d.commonParents()
	result := make(map[string]WindowsInstall, len(emulators))
	for i := range emulators {
		if install, ok := d.find(&emulators[i]); ok {
			result[emulators[i].Name] = install
		}
	}
	return result
}

func (d *windowsDiscovery) find(emu *WindowsEmulator) (WindowsInstall, bool) {
	steps := []func(*WindowsEmulator) (string, string){
		d.fromConfig, d.fromAppPaths, d.fromUninstall, d.fromScoop, d.fromWinget, d.fromPath, d.fromParents,
	}
	for _, step := range steps {
		if exe, source := step(emu); exe != "" {
			return WindowsInstall{Exe: exe, Dir: filepath.Dir(exe), Source: source}, true
		}
	}
	return WindowsInstall{}, false
}

func (d *windowsDiscovery) fromConfig(emu *WindowsEmulator) (exe, source string) {
	if d.env.Configured == nil {
		return "", ""
	}
	ids := make([]string, 0, len(emu.Systems)+1)
	for i := range emu.Systems {
		ids = append(ids, emu.Systems[i].ID)
	}
	ids = append(ids, emu.Name)
	for _, id := range ids {
		configured := d.env.Configured(id)
		if configured == "" {
			continue
		}
		info, err := d.env.FS.Stat(configured)
		if err != nil {
			continue
		}
		if info.Mode().IsRegular() {
			return configured, "install_dir"
		}
		if info.IsDir() {
			if found := d.exeIn(emu, configured); found != "" {
				return found, "install_dir"
			}
		}
	}
	return "", ""
}

func (d *windowsDiscovery) fromAppPaths(emu *WindowsEmulator) (exe, source string) {
	if d.env.AppPath == nil {
		return "", ""
	}
	for _, name := range emu.Exes {
		if strings.HasSuffix(name, "*") {
			continue
		}
		if path := d.env.AppPath(name); path != "" && d.isFile(path) {
			return path, "app paths"
		}
	}
	return "", ""
}

func (d *windowsDiscovery) fromUninstall(emu *WindowsEmulator) (exe, source string) {
	hints := emulatorHints(emu)
	for i := range d.env.UninstallEntries {
		entry := &d.env.UninstallEntries[i]
		if icon := iconPath(entry.DisplayIcon); icon != "" && exeMatches(emu, filepath.Base(icon)) &&
			d.isFile(icon) {
			// The icon can be a lower-priority build, such as PPSSPP's
			// 32-bit exe next to the 64-bit one.
			if preferred := d.exeIn(emu, filepath.Dir(icon)); preferred != "" {
				return preferred, "uninstall"
			}
			return icon, "uninstall"
		}
		location := strings.Trim(strings.TrimSpace(entry.InstallLocation), `"`)
		if location == "" {
			continue
		}
		name := strings.ToLower(entry.DisplayName)
		base := strings.ToLower(filepath.Base(location))
		matched := false
		for _, hint := range hints {
			if strings.Contains(name, hint) || strings.HasPrefix(base, hint) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if found := d.exeIn(emu, location); found != "" {
			return found, "uninstall"
		}
	}
	return "", ""
}

func (d *windowsDiscovery) fromScoop(emu *WindowsEmulator) (exe, source string) {
	if len(emu.Scoop) == 0 {
		return "", ""
	}
	var roots []string
	if scoop := d.env.Getenv("SCOOP"); scoop != "" {
		roots = append(roots, scoop)
	}
	if home := d.env.Getenv("USERPROFILE"); home != "" {
		roots = append(roots, filepath.Join(home, "scoop"))
	}
	if global := d.env.Getenv("SCOOP_GLOBAL"); global != "" {
		roots = append(roots, global)
	}
	if programData := d.env.Getenv("ProgramData"); programData != "" {
		roots = append(roots, filepath.Join(programData, "scoop"))
	}
	for _, root := range roots {
		for _, app := range emu.Scoop {
			if found := d.exeIn(emu, filepath.Join(root, "apps", app, "current")); found != "" {
				return found, "scoop"
			}
		}
	}
	return "", ""
}

func (d *windowsDiscovery) fromWinget(emu *WindowsEmulator) (exe, source string) {
	local := d.env.Getenv("LOCALAPPDATA")
	if local == "" {
		return "", ""
	}
	winget := filepath.Join(local, "Microsoft", "WinGet")
	if found := d.exeIn(emu, filepath.Join(winget, "Links")); found != "" {
		return found, "winget"
	}
	packages := filepath.Join(winget, "Packages")
	for _, child := range d.subdirs(packages) {
		if found := d.exeInTree(emu, child); found != "" {
			return found, "winget"
		}
	}
	return "", ""
}

func (d *windowsDiscovery) fromPath(emu *WindowsEmulator) (exe, source string) {
	systemRoot := strings.ToLower(d.env.Getenv("SystemRoot"))
	for _, dir := range strings.Split(d.env.Getenv("PATH"), string(os.PathListSeparator)) {
		dir = strings.Trim(strings.TrimSpace(dir), `"`)
		if dir == "" || !filepath.IsAbs(dir) ||
			(systemRoot != "" && strings.HasPrefix(strings.ToLower(dir), systemRoot)) {
			continue
		}
		if found := d.exeIn(emu, dir); found != "" {
			return found, "path"
		}
	}
	return "", ""
}

func (d *windowsDiscovery) fromParents(emu *WindowsEmulator) (exe, source string) {
	hints := emulatorHints(emu)
	for _, parent := range d.parents {
		for _, child := range d.subdirs(parent) {
			name := strings.ToLower(filepath.Base(child))
			for _, hint := range hints {
				if !strings.HasPrefix(name, hint) {
					continue
				}
				if found := d.exeInTree(emu, child); found != "" {
					return found, "search"
				}
				break
			}
		}
	}
	return "", ""
}

// commonParents lists the folders whose children are searched for emulator
// folders, most specific first.
func (d *windowsDiscovery) commonParents() []string {
	env := d.env.Getenv
	var parents []string
	add := func(parts ...string) {
		if len(parts) == 0 || parts[0] == "" {
			return
		}
		parents = append(parents, filepath.Join(parts...))
	}
	home := env("USERPROFILE")
	add(home, "Emulators")
	add(env("APPDATA"), "EmuDeck", "Emulators")
	for _, drive := range d.env.FixedDrives {
		add(drive, "Emulators")
		add(drive, "Emulation", "Emulators")
		add(drive, "ES-DE", "Emulators")
	}
	for _, root := range d.env.IndexRoots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		parent := filepath.Dir(filepath.Clean(root))
		add(parent, "Emulators")
		add(parent)
	}
	add(env("ProgramFiles"))
	add(env("ProgramW6432"))
	add(env("ProgramFiles(x86)"))
	add(env("LOCALAPPDATA"), "Programs")
	add(env("LOCALAPPDATA"))
	add(env("APPDATA"))
	for _, drive := range d.env.FixedDrives {
		add(drive, "Games")
		add(drive)
	}
	add(home, "Games")
	add(home)
	add(home, "Desktop")
	add(home, "Downloads")
	add(home, "Documents")

	seen := make(map[string]bool, len(parents))
	unique := parents[:0]
	for _, parent := range parents {
		key := strings.ToLower(filepath.Clean(parent))
		if !seen[key] {
			seen[key] = true
			unique = append(unique, parent)
		}
	}
	return unique
}

// exeInTree finds the executable in dir or one of its direct subfolders,
// which covers archives that extract into a nested folder, or in a bin
// folder inside either (VICE ships its executables in bin).
func (d *windowsDiscovery) exeInTree(emu *WindowsEmulator, dir string) string {
	if found := d.exeInOrBin(emu, dir); found != "" {
		return found
	}
	for _, child := range d.subdirs(dir) {
		if found := d.exeInOrBin(emu, child); found != "" {
			return found
		}
	}
	return ""
}

func (d *windowsDiscovery) exeInOrBin(emu *WindowsEmulator, dir string) string {
	if found := d.exeIn(emu, dir); found != "" {
		return found
	}
	return d.exeIn(emu, filepath.Join(dir, "bin"))
}

// exeIn returns the first of the emulator's executables found in dir, in
// the order the emulator lists them.
func (d *windowsDiscovery) exeIn(emu *WindowsEmulator, dir string) string {
	entries := d.list(dir)
	for _, pattern := range emu.Exes {
		for _, entry := range entries {
			if entry.Mode().IsRegular() && exePatternMatches(pattern, entry.Name()) {
				return filepath.Join(dir, entry.Name())
			}
		}
	}
	return ""
}

func (d *windowsDiscovery) subdirs(dir string) []string {
	entries := d.list(dir)
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, filepath.Join(dir, entry.Name()))
		}
	}
	return result
}

func (d *windowsDiscovery) list(dir string) []os.FileInfo {
	key := strings.ToLower(filepath.Clean(dir))
	if entries, ok := d.listings[key]; ok {
		return entries
	}
	entries, err := afero.ReadDir(d.env.FS, dir)
	if err != nil {
		entries = nil
	}
	d.listings[key] = entries
	return entries
}

func (d *windowsDiscovery) isFile(path string) bool {
	info, err := d.env.FS.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func exeMatches(emu *WindowsEmulator, name string) bool {
	for _, pattern := range emu.Exes {
		if exePatternMatches(pattern, name) {
			return true
		}
	}
	return false
}

func exePatternMatches(pattern, name string) bool {
	name = strings.ToLower(name)
	pattern = strings.ToLower(pattern)
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".exe")
	}
	return name == pattern
}

// iconPath turns a DisplayIcon value such as `"C:\App\app.exe",0` into a
// file path.
func iconPath(icon string) string {
	icon = strings.TrimSpace(icon)
	if strings.HasPrefix(icon, `"`) {
		if end := strings.Index(icon[1:], `"`); end >= 0 {
			return icon[1 : end+1]
		}
		return ""
	}
	if comma := strings.LastIndex(icon, ","); comma > 0 {
		icon = icon[:comma]
	}
	if !strings.EqualFold(filepath.Ext(icon), ".exe") {
		return ""
	}
	return icon
}

func emulatorHints(emu *WindowsEmulator) []string {
	hints := make([]string, 0, len(emu.DirHints)+2)
	name := strings.ToLower(emu.Name)
	hints = append(hints, name)
	if compact := strings.ReplaceAll(name, " ", ""); compact != name {
		hints = append(hints, compact)
	}
	return append(hints, emu.DirHints...)
}
