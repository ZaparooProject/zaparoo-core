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

package sim

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
)

// Layout names every path a sim instance uses. All of them live under Root,
// so a sim never reads or writes the host's real Zaparoo directories.
type Layout struct {
	Root     string `json:"root"`
	Config   string `json:"configDir"`
	Data     string `json:"dataDir"`
	Logs     string `json:"logDir"`
	Temp     string `json:"tempDir"`
	ROMs     string `json:"romsDir"`
	Socket   string `json:"socket"`
	State    string `json:"stateFile"`
	Journal  string `json:"journal"`
	Marker   string `json:"-"`
	Snapshot string `json:"screenshot"`
}

// MarkerName is the file that marks a directory as a sim root.
const MarkerName = ".zaparoo-sim"

// NewLayout returns the layout for root. Root is made absolute.
func NewLayout(root string) (Layout, error) {
	if strings.TrimSpace(root) == "" {
		return Layout{}, errors.New("sim root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, fmt.Errorf("resolve sim root: %w", err)
	}
	return Layout{
		Root:     abs,
		Config:   filepath.Join(abs, "config"),
		Data:     filepath.Join(abs, "data"),
		Logs:     filepath.Join(abs, "logs"),
		Temp:     filepath.Join(abs, "tmp"),
		ROMs:     filepath.Join(abs, "roms"),
		Socket:   filepath.Join(abs, "sim.sock"),
		State:    filepath.Join(abs, "sim.json"),
		Journal:  filepath.Join(abs, "journal.jsonl"),
		Marker:   filepath.Join(abs, MarkerName),
		Snapshot: filepath.Join(abs, "tmp", "screenshot.png"),
	}, nil
}

// CheckRoot refuses roots that could be real data: the filesystem root, the
// home directory, the real Zaparoo directories or anything above or inside
// them, and a non-empty directory that is not already a sim root. Symlinks
// are followed, so a link cannot smuggle a real directory in as a sim root.
func (l *Layout) CheckRoot() error {
	root := resolvePath(l.Root)
	if root == "/" {
		return errors.New("refusing to use / as a sim root")
	}
	if home, err := os.UserHomeDir(); err == nil && samePath(root, resolvePath(home)) {
		return errors.New("refusing to use the home directory as a sim root")
	}
	zap := linuxbase.Settings()
	for _, dir := range []string{zap.DataDir, zap.ConfigDir, zap.LogDir} {
		if dir == "" {
			continue
		}
		realDir := resolvePath(dir)
		if within(root, realDir) || within(realDir, root) {
			return fmt.Errorf("refusing sim root %s: it overlaps the real Zaparoo directory %s", l.Root, dir)
		}
	}
	entries, err := os.ReadDir(l.Root)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("read sim root: %w", err)
	case len(entries) == 0:
		return nil
	}
	if _, err := os.Stat(l.Marker); err != nil {
		return fmt.Errorf("refusing sim root %s: directory is not empty and has no %s marker", l.Root, MarkerName)
	}
	return nil
}

// Ensure creates the root and its directories and writes the marker.
func (l *Layout) Ensure() error {
	for _, dir := range []string{l.Root, l.Config, l.Data, l.Logs, l.Temp, l.ROMs} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(l.Marker, []byte("zaparoo sim root\n"), 0o600); err != nil {
		return fmt.Errorf("write sim marker: %w", err)
	}
	return nil
}

// maxLinkHops bounds symlink chains so a loop cannot hang resolvePath.
const maxLinkHops = 40

// resolvePath returns path with every symlink followed, including links whose
// target does not exist yet and paths whose tail has not been created.
func resolvePath(path string) string {
	path = filepath.Clean(path)
	for range maxLinkHops {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		info, err := os.Lstat(path)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			target, linkErr := os.Readlink(path)
			if linkErr != nil {
				return path
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			path = filepath.Clean(target)
			continue
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		return filepath.Join(resolvePath(parent), filepath.Base(path))
	}
	return path
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// State is what a running sim publishes in sim.json for clients.
type State struct {
	Fakes   map[string]string `json:"fakes,omitempty"`
	Layout  Layout            `json:"layout"`
	APIURL  string            `json:"apiUrl"`
	Version string            `json:"version"`
	PID     int               `json:"pid"`
	APIPort int               `json:"apiPort"`
	Ready   bool              `json:"ready"`
}

// WriteState writes sim.json atomically.
func (l *Layout) WriteState(st *State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sim state: %w", err)
	}
	tmp := l.State + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write sim state: %w", err)
	}
	if err := os.Rename(tmp, l.State); err != nil {
		return fmt.Errorf("publish sim state: %w", err)
	}
	return nil
}

// ReadState reads sim.json.
func (l *Layout) ReadState() (*State, error) {
	data, err := os.ReadFile(l.State)
	if err != nil {
		return nil, fmt.Errorf("read sim state (is the sim running?): %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("decode sim state: %w", err)
	}
	return &st, nil
}

// DefaultsOptions sets what the first-boot config.toml contains.
type DefaultsOptions struct {
	Readers []string
	APIPort int
}

// Defaults returns the config values a sim boots with on first run. The API
// listens on loopback only; discovery, Bluetooth, update checks, internet
// probes and error reporting are off; the only index root is the sim ROM tree;
// and each name in opts.Readers gets a [[readers.connect]] entry (sim readers
// also open by auto-detect unless added with NoAutoDetect).
func (l *Layout) Defaults(opts DefaultsOptions) config.Values {
	off := false
	defaults := config.BaseDefaults
	port := opts.APIPort
	defaults.Service.APIPort = &port
	defaults.Service.APIListen = "127.0.0.1"
	defaults.Service.Discovery.Enabled = &off
	defaults.Service.BLE.Enabled = &off
	defaults.Service.InternetCheck = &off
	defaults.Updates.Check = &off
	defaults.Updates.Install = &off
	defaults.ErrorReporting = false
	defaults.DebugLogging = true
	defaults.Launchers.IndexRoot = []string{l.ROMs}
	// Real software drivers never probe the host for devices: the desktop may
	// have real readers in use by a real Core.
	defaults.Readers.Drivers = map[string]config.DriverConfig{}
	for _, id := range RealDrivers() {
		defaults.Readers.Drivers[id] = config.DriverConfig{AutoDetect: &off}
	}
	for _, name := range opts.Readers {
		defaults.Readers.Connect = append(defaults.Readers.Connect, config.ReadersConnect{
			Driver: "sim",
			Path:   name,
		})
	}
	return defaults
}
