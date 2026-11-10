//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/spf13/afero"
)

const (
	// userESSystemsPath takes precedence over systemESSystemsPath, matching
	// SystemData::getConfigPath in EmulationStation-fcamod.
	userESSystemsPath   = "/home/ark/.emulationstation/es_systems.cfg"
	systemESSystemsPath = "/etc/emulationstation/es_systems.cfg"
	esSettingsPath      = "/home/ark/.emulationstation/es_settings.cfg"

	// maxConfigSize bounds how much of an ES config file is read. Stock
	// es_systems.cfg is around 100 KiB.
	maxConfigSize = 8 << 20

	defaultGovernor = "performance"
)

type esEmulator struct {
	Name      string   `xml:"name,attr"`
	Command   string   `xml:"command,attr"`
	Cores     []string `xml:"cores>core"`
	Governors []string `xml:"governors>governor"`
}

type esSystem struct {
	Name      string       `xml:"name"`
	FullName  string       `xml:"fullname"`
	Path      string       `xml:"path"`
	Extension string       `xml:"extension"`
	Command   string       `xml:"command"`
	Platform  string       `xml:"platform"`
	Emulators []esEmulator `xml:"emulators>emulator"`
}

type esSystemList struct {
	Systems []esSystem `xml:"system"`
}

// folder is the directory name ES reads the system's games from.
func (s *esSystem) folder() string {
	if p := strings.TrimSpace(s.Path); p != "" {
		return filepath.Base(filepath.Clean(p))
	}
	return s.Name
}

// extensions returns the system's extensions lowercased and deduplicated, as
// ES-fcamod compares them case-insensitively.
func (s *esSystem) extensions() []string {
	fields := strings.Fields(s.Extension)
	exts := make([]string, 0, len(fields))
	for _, f := range fields {
		ext := strings.ToLower(f)
		if !strings.HasPrefix(ext, ".") || slices.Contains(exts, ext) {
			continue
		}
		exts = append(exts, ext)
	}
	return exts
}

func readLimited(fs afero.Fs, path string) ([]byte, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxConfigSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxConfigSize {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxConfigSize)
	}
	return data, nil
}

// loadESSystems reads the es_systems.cfg ES-fcamod would load.
func loadESSystems(fs afero.Fs) ([]esSystem, error) {
	path := userESSystemsPath
	if _, err := fs.Stat(path); err != nil {
		path = systemESSystemsPath
	}
	data, err := readLimited(fs, path)
	if err != nil {
		return nil, err
	}
	return parseESSystems(data)
}

func parseESSystems(data []byte) ([]esSystem, error) {
	// ES-fcamod's parser accepts commands with a raw "&", as in the "2>&1"
	// stock ArkOS images ship, so the decoder is not strict either.
	var list esSystemList
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	if err := dec.Decode(&list); err != nil {
		return nil, fmt.Errorf("parse es_systems.cfg: %w", err)
	}
	systems := make([]esSystem, 0, len(list.Systems))
	for i := range list.Systems {
		sys := list.Systems[i]
		sys.Name = strings.TrimSpace(sys.Name)
		sys.Path = strings.TrimSpace(sys.Path)
		sys.Command = strings.TrimSpace(sys.Command)
		sys.Platform = strings.TrimSpace(sys.Platform)
		if sys.Name == "" || sys.Command == "" {
			continue
		}
		for j := range sys.Emulators {
			sys.Emulators[j].Name = strings.TrimSpace(sys.Emulators[j].Name)
			sys.Emulators[j].Command = strings.TrimSpace(sys.Emulators[j].Command)
			for k := range sys.Emulators[j].Cores {
				sys.Emulators[j].Cores[k] = strings.TrimSpace(sys.Emulators[j].Cores[k])
			}
			for k := range sys.Emulators[j].Governors {
				sys.Emulators[j].Governors[k] = strings.TrimSpace(sys.Emulators[j].Governors[k])
			}
		}
		systems = append(systems, sys)
	}
	return systems, nil
}

// loadESSettings reads es_settings.cfg, a flat list of <string name= value=>
// style elements with no root element. A missing file yields no settings.
func loadESSettings(fs afero.Fs) map[string]string {
	data, err := readLimited(fs, esSettingsPath)
	if err != nil {
		return map[string]string{}
	}
	return parseESSettings(data)
}

func parseESSettings(data []byte) map[string]string {
	settings := map[string]string{}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			return settings
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		var name, value string
		hasValue := false
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "name":
				name = attr.Value
			case "value":
				value = attr.Value
				hasValue = true
			}
		}
		if name != "" && hasValue {
			settings[name] = value
		}
	}
}

// esLaunch is the emulator, core and governor ES-fcamod would pick for a game,
// and the command template they fill in.
type esLaunch struct {
	system   *esSystem
	command  string
	emulator string
	core     string
	governor string
}

// esBuiltinGovernors are the governors every ES-fcamod emulator offers before
// any listed in its <governors> element (EmulatorData::mGovernors).
var esBuiltinGovernors = []string{"performance", "ondemand", "powersave"}

// esGame holds a game's own launch choices: the emulator, core and governor
// stored in its gamelist metadata, and its ES display name, which keys the
// "<name>.governor" setting. name is only called when that setting is
// consulted.
type esGame struct {
	name     func() string
	emulator string
	core     string
	governor string
}

func (s *esSystem) emulatorNamed(name string) *esEmulator {
	for i := range s.Emulators {
		if s.Emulators[i].Name == name {
			return &s.Emulators[i]
		}
	}
	return nil
}

// defaultEmulator mirrors SystemEnvironmentData::getDefaultEmulator: the
// emulator chosen in ES settings when the system still offers it, otherwise
// the first listed.
func defaultEmulator(sys *esSystem, settings map[string]string) string {
	if emu := sys.emulatorNamed(settings[sys.Name+".emulator"]); emu != nil {
		return emu.Name
	}
	if len(sys.Emulators) > 0 {
		return sys.Emulators[0].Name
	}
	return ""
}

// defaultCore mirrors SystemEnvironmentData::getDefaultCore: the core chosen
// in ES settings when the emulator offers it, otherwise the emulator's first.
func defaultCore(sys *esSystem, settings map[string]string, emulator string) string {
	emu := sys.emulatorNamed(emulator)
	if emu == nil {
		return ""
	}
	if chosen := settings[sys.Name+".core"]; slices.Contains(emu.Cores, chosen) {
		return chosen
	}
	if len(emu.Cores) > 0 {
		return emu.Cores[0]
	}
	return ""
}

// emulatorGovernor mirrors SystemEnvironmentData::getDefaultGovernor: the
// governor chosen for the system in ES settings, but only when the emulator
// offers it.
func emulatorGovernor(sys *esSystem, settings map[string]string, emulator string) string {
	emu := sys.emulatorNamed(emulator)
	if emu == nil {
		return ""
	}
	chosen := settings[sys.Name+".governor"]
	if slices.Contains(esBuiltinGovernors, chosen) || slices.Contains(emu.Governors, chosen) {
		return chosen
	}
	return ""
}

// resolveLaunch mirrors FileData::launchGame in EmulationStation-fcamod. The
// game's gamelist emulator and core win over the system defaults. The
// governor is the first set of: the game's gamelist governor, the system
// setting when the emulator offers it, the "<game name>.governor" setting, the
// system setting, and GlobalPerformanceGovernor. The emulator's own command
// line replaces the system's when it has one.
//
// ES-fcamod reads <defaultCore> and <defaultGov> from es_systems.cfg but never
// uses them (SystemData::loadSystem keeps them in locals), so neither is
// honoured here.
func resolveLaunch(sys *esSystem, settings map[string]string, game *esGame) esLaunch {
	if game == nil {
		game = &esGame{}
	}
	launch := esLaunch{system: sys, command: sys.Command}

	launch.emulator = game.emulator
	if launch.emulator == "" {
		launch.emulator = defaultEmulator(sys, settings)
	}
	launch.core = game.core
	if launch.core == "" {
		launch.core = defaultCore(sys, settings, launch.emulator)
	}

	launch.governor = game.governor
	if launch.governor == "" {
		launch.governor = emulatorGovernor(sys, settings, launch.emulator)
	}
	if launch.governor == "" && game.name != nil {
		if name := game.name(); name != "" {
			launch.governor = settings[name+".governor"]
		}
	}
	if launch.governor == "" {
		launch.governor = settings[sys.Name+".governor"]
	}
	if launch.governor == "" {
		launch.governor = emulatorGovernor(sys, settings, defaultEmulator(sys, settings))
	}
	if launch.governor == "" {
		// ES-fcamod's Settings default GlobalPerformanceGovernor to
		// "performance"; an empty value is treated the same so perfmax always
		// gets a governor argument.
		launch.governor = settings["GlobalPerformanceGovernor"]
	}
	if launch.governor == "" {
		launch.governor = defaultGovernor
	}

	if emu := sys.emulatorNamed(launch.emulator); emu != nil && emu.Command != "" {
		launch.command = emu.Command
	}
	return launch
}

var errNoSystem = errors.New("no EmulationStation system for path")

// systemForPath finds the ES system a ROM belongs to: the system whose path
// holds the ROM, or failing that the system named for the ROM's top-level
// folder under a ROM root, so games on /roms2 still resolve while ES points at
// /roms and the other way round.
func systemForPath(systems []esSystem, romPath string, roots []string) (*esSystem, error) {
	var best *esSystem
	bestLen := -1
	for i := range systems {
		sysPath := filepath.Clean(systems[i].Path)
		if systems[i].Path == "" || !helpers.PathHasPrefix(romPath, sysPath) {
			continue
		}
		if len(sysPath) > bestLen {
			best = &systems[i]
			bestLen = len(sysPath)
		}
	}
	if best != nil {
		return best, nil
	}

	folder := topFolder(romPath, roots)
	if folder != "" {
		for i := range systems {
			if systems[i].folder() == folder {
				return &systems[i], nil
			}
		}
		for i := range systems {
			if systems[i].Name == folder {
				return &systems[i], nil
			}
		}
	}
	return nil, fmt.Errorf("%w: %s", errNoSystem, romPath)
}

// topFolder returns the first directory below whichever root holds path.
func topFolder(path string, roots []string) string {
	for _, root := range roots {
		rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		first, rest, found := strings.Cut(rel, string(filepath.Separator))
		if !found || rest == "" {
			continue
		}
		return first
	}
	return ""
}
