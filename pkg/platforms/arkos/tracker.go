//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// esComm is the kernel comm of the ES binary: "emulationstation" cut to the
// 15 characters comm holds.
const esComm = "emulationstatio"

// esGameMatcher recognises the game process EmulationStation started itself.
// ES runs its command line through sh -c, so the game is a child of that
// shell (or of ES when the shell execs a lone command), and ES puts the ROM
// path last on the command line of every ArkOS launcher.
type esGameMatcher struct {
	fs      afero.Fs
	procDir string
	roots   []string
}

func (m *esGameMatcher) romPath(proc procscanner.ProcessInfo) (string, bool) {
	args := strings.Split(strings.TrimRight(proc.Cmdline, "\x00"), "\x00")
	if len(args) < 2 {
		return "", false
	}
	// "sudo perfmax <governor> <rom>" runs before the emulator and also ends
	// with the ROM path.
	switch filepath.Base(args[0]) {
	case "sudo", "perfmax", "perfnorm":
		return "", false
	}
	rom := args[len(args)-1]
	if !filepath.IsAbs(rom) || filepath.Clean(rom) != rom {
		return "", false
	}
	if topFolder(rom, m.roots) == "" {
		return "", false
	}
	return rom, true
}

// Match implements procscanner.Matcher.
func (m *esGameMatcher) Match(proc procscanner.ProcessInfo) bool {
	if _, ok := m.romPath(proc); !ok {
		return false
	}
	ppid, ok := m.parent(proc.PID)
	if !ok {
		return false
	}
	parentComm := m.comm(ppid)
	if strings.HasPrefix(parentComm, esComm) {
		return true
	}
	if parentComm != "sh" && parentComm != "dash" && parentComm != "bash" {
		return false
	}
	gpid, ok := m.parent(ppid)
	if !ok {
		return false
	}
	return strings.HasPrefix(m.comm(gpid), esComm)
}

func (m *esGameMatcher) comm(pid int) string {
	data, err := afero.ReadFile(m.fs, filepath.Join(m.procDir, strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// parent reads the parent PID from /proc/<pid>/stat. The comm field can hold
// spaces and parentheses, so fields are counted from the last ')'.
func (m *esGameMatcher) parent(pid int) (int, bool) {
	data, err := afero.ReadFile(m.fs, filepath.Join(m.procDir, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	s := string(data)
	end := strings.LastIndexByte(s, ')')
	if end < 0 {
		return 0, false
	}
	fields := strings.Fields(s[end+1:])
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil || ppid <= 0 {
		return 0, false
	}
	return ppid, true
}

// esGameStarted publishes a game ES launched itself.
func (p *Platform) esGameStarted(proc procscanner.ProcessInfo) {
	rom, ok := p.esMatcher.romPath(proc)
	if !ok {
		return
	}
	media := p.mediaForPath(rom)
	if media == nil {
		log.Debug().Str("path", rom).Msg("ES-launched game is not in a known system")
		return
	}

	p.esGameMu.Lock()
	p.esGamePID = proc.PID
	p.esGamePath = rom
	p.esGameMu.Unlock()

	log.Info().Int("pid", proc.PID).Str("path", rom).Msg("detected game launched by EmulationStation")
	if p.setActiveMedia != nil {
		p.setActiveMedia(media)
	}
}

func (p *Platform) esGameStopped(pid int) {
	p.esGameMu.Lock()
	if p.esGamePID != pid {
		p.esGameMu.Unlock()
		return
	}
	path := p.esGamePath
	p.esGamePID = 0
	p.esGamePath = ""
	p.esGameMu.Unlock()

	log.Info().Int("pid", pid).Str("path", path).Msg("game launched by EmulationStation exited")
	if p.setActiveMedia == nil {
		return
	}
	if p.activeMedia != nil {
		if current := p.activeMedia(); current != nil && current.Path != path {
			return
		}
	}
	p.setActiveMedia(nil)
}

// takeESGame forgets the tracked ES game and returns its PID, or 0.
func (p *Platform) takeESGame() int {
	p.esGameMu.Lock()
	defer p.esGameMu.Unlock()
	pid := p.esGamePID
	p.esGamePID = 0
	p.esGamePath = ""
	return pid
}

// mediaForPath builds the ActiveMedia for a ROM from the ES system that holds
// it.
func (p *Platform) mediaForPath(rom string) *models.ActiveMedia {
	systems, err := loadESSystems(p.fs)
	if err != nil {
		log.Debug().Err(err).Msg("cannot read es_systems.cfg")
		return nil
	}
	sys, err := systemForPath(systems, rom, romRoots)
	if err != nil {
		return nil
	}
	info, ok := lookupSystem(sys)
	if !ok {
		return nil
	}
	systemName := info.SystemID
	if meta, metaErr := assets.GetSystemMetadata(info.SystemID); metaErr == nil {
		systemName = meta.Name
	}
	name := strings.TrimSuffix(filepath.Base(rom), filepath.Ext(rom))
	media := models.NewActiveMedia(info.SystemID, systemName, rom, name, info.GetLauncherID())
	launch := p.resolveGameLaunch(sys, rom, frontendHome)
	media.LauncherControls = platforms.ControlNames(&platforms.Launcher{
		Controls: p.retroArchControls(&launch),
	})
	return media
}

// arkosSystems maps ArkOS system names no other ES variant uses. alg is
// mapped by algSystem instead.
//
//nolint:gochecknoglobals // Package-level configuration
var arkosSystems = map[string]esde.SystemInfo{
	"c16":      {SystemID: systemdefs.SystemC16},
	"gametank": {SystemID: systemdefs.SystemGameTank},
	"palm":     {SystemID: systemdefs.SystemPalmOS},
	"vmu":      {SystemID: systemdefs.SystemVMU},
}

// algSystem maps the American Laser Games system by what its command runs.
// Stock ArkOS and dArkOS play these games in hypseus-singe through
// /usr/local/bin/singe.sh, a laserdisc player, so they are DAPHNE games; a
// command that runs anything else is taken to be an arcade emulator such as
// MAME.
func algSystem(sys *esSystem) (esde.SystemInfo, bool) {
	commands := make([]string, 0, 1+len(sys.Emulators))
	commands = append(commands, sys.Command)
	for i := range sys.Emulators {
		commands = append(commands, sys.Emulators[i].Command)
	}
	for _, c := range commands {
		c = strings.ToLower(c)
		if strings.Contains(c, "singe") || strings.Contains(c, "hypseus") || strings.Contains(c, "daphne") {
			return esde.LookupByFolderName("daphne")
		}
	}
	return esde.LookupByFolderName("arcade")
}

// lookupSystem maps an ES system to Zaparoo by name, then by folder, then
// by its theme platform. ArkOS shares the ELEC names for systems ES-DE
// spells differently, such as genesis and sfc, and has a few of its own.
func lookupSystem(sys *esSystem) (esde.SystemInfo, bool) {
	platformNames := strings.FieldsFunc(sys.Platform, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	names := make([]string, 0, 2+len(platformNames))
	names = append(names, sys.Name, sys.folder())
	names = append(names, platformNames...)
	for _, name := range names {
		if name == "" {
			continue
		}
		if info, ok := esde.LookupByFolderName(name); ok {
			return info, true
		}
		if folder, ok := esbase.ELECSystemAliases[name]; ok {
			if info, ok := esde.LookupByFolderName(folder); ok {
				return info, true
			}
		}
		if strings.EqualFold(name, "alg") {
			return algSystem(sys)
		}
		if info, ok := arkosSystems[strings.ToLower(name)]; ok {
			return info, true
		}
	}
	return esde.SystemInfo{}, false
}
