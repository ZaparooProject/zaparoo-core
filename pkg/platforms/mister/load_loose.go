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
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cpcdsk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/decb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/trs80cas"
	"github.com/spf13/afero"
)

const (
	// looseSettle is how long a machine gets to reach its prompt once the core
	// has its media.
	looseSettle = 4 * time.Second
	// diskLoadWait is how long a floppy load gets before RUN is typed.
	diskLoadWait = 8 * time.Second
	// maxTapeWait bounds the wait for a tape to finish loading.
	maxTapeWait = 10 * time.Minute
	// electronMenuSettle is how long the Electron takes to boot into its menu
	// and draw its list before the menu is left.
	electronMenuSettle = 25 * time.Second
	// petTapeSettle is how long the PET takes to boot to BASIC; its tape is
	// already playing, and the leader lasts about ten seconds.
	petTapeSettle = 2500 * time.Millisecond
	// c64Settle is how long the C64 can take after the core has its media to
	// reach BASIC; keys typed sooner are lost.
	c64Settle = 10 * time.Second
	// c128Settle is how long the C128 takes to boot before it takes keys.
	c128Settle = 9 * time.Second
	// zx81LoadWait is how long the ZX81 core takes to load a program before RUN.
	zx81LoadWait = 6 * time.Second
	// tapeMargin is added to the estimated tape load time.
	tapeMargin = 5 * time.Second
	// trs80TapeRate is how fast the TRS-80 core reads a CAS file, in bytes a
	// second. The core does not play the tape at 500 baud: a 13 KB tape loads
	// in about a second.
	trs80TapeRate = 4000
)

// chordHold is how long a modifier is held before and after the key it
// modifies. The cores read the keyboard slowly, and a combination whose keys
// go down and up together is missed.
const chordHold = 400 * time.Millisecond

// chord types modifiers and a key with the overlap the cores need.
func chord(modifier, key string, more ...string) string {
	if len(key) > 1 {
		key = "{" + key + "}"
	}
	mods := append([]string{modifier}, more...)
	var b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&b, "{press:%s}", m)
	}
	fmt.Fprintf(&b, "{delay:%s}%s{delay:%s}", chordHold, key, chordHold)
	for i := len(mods) - 1; i >= 0; i-- {
		fmt.Fprintf(&b, "{release:%s}", mods[i])
	}
	return b.String()
}

// qlKeyHold is how long the QL's F1 is held. Its keyboard is read by a slow
// second processor that misses the input's usual short taps.
const qlKeyHold = 200 * time.Millisecond

// looseRule is the plan for media of certain extensions that the core loads as
// a file of its own. The steps are built per file for formats whose command
// depends on what the media holds.
type looseRule struct {
	build   func(fs afero.Fs, path string) (steps []loadStep, ok bool, err error)
	layout  keyLayout
	exts    []string
	options []mgls.Setting
	settle  time.Duration
}

func fixedSteps(steps ...loadStep) func(afero.Fs, string) ([]loadStep, bool, error) {
	return func(afero.Fs, string) ([]loadStep, bool, error) { return steps, true, nil }
}

// looseRules plans media by extension for one system. Media with no rule
// launches as it always has.
func looseRules(fs afero.Fs, rules ...looseRule) loadPlanner {
	return func(_ *config.Instance, path string) (loadPlan, bool, error) {
		ext := strings.ToLower(filepath.Ext(path))
		for i := range rules {
			rule := &rules[i]
			matched := false
			for _, e := range rule.exts {
				if e == ext {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			steps, ok, err := rule.build(fs, path)
			if err != nil || !ok {
				return loadPlan{}, false, err
			}
			return loadPlan{
				target:  path,
				layout:  rule.layout,
				settle:  rule.settle,
				options: rule.options,
				steps:   steps,
			}, true, nil
		}
		return loadPlan{}, false, nil
	}
}

// firstPlan tries each planner in turn and uses the first that has a plan.
func firstPlan(planners ...loadPlanner) loadPlanner {
	return func(cfg *config.Instance, path string) (loadPlan, bool, error) {
		for _, plan := range planners {
			resolved, ok, err := plan(cfg, path)
			if err != nil || ok {
				return resolved, ok, err
			}
		}
		return loadPlan{}, false, nil
	}
}

// commodoreDisk and commodoreTape are the BASIC commands that load the first
// program of a disk or a tape on the Commodore home computers.
func commodoreDisk(load string) func(afero.Fs, string) ([]loadStep, bool, error) {
	return fixedSteps(typeText(load+"{enter}"), typeAfter(diskLoadWait, "RUN{enter}"))
}

// tapeWait estimates how long a tape image takes to load from its size, so the
// command that starts the program is typed once the load has finished.
func tapeWait(fs afero.Fs, path string, bytesPerSecond int64) time.Duration {
	info, err := fs.Stat(path)
	if err != nil || bytesPerSecond <= 0 {
		return maxTapeWait
	}
	return min(time.Duration(info.Size()/bytesPerSecond)*time.Second+tapeMargin, maxTapeWait)
}

// maxZippedMedia bounds a file read out of a zip to look inside it.
const maxZippedMedia = 32 << 20

var errZippedMediaTooLarge = errors.New("file in the zip is too large to read")

// readableMedia returns a filesystem the media at path can be read from. A
// file inside a zip is read out into memory, since the format readers want a
// file of their own; anything else is read in place.
func readableMedia(fs afero.Fs, path string) (readFS afero.Fs, readPath string, err error) {
	at := strings.Index(strings.ToLower(path), ".zip/")
	if at < 0 {
		return fs, path, nil
	}
	archive, inner := path[:at+len(".zip")], path[at+len(".zip/"):]

	f, err := fs.Open(archive)
	if err != nil {
		return nil, "", fmt.Errorf("open zip: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("stat zip: %w", err)
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return nil, "", fmt.Errorf("read zip: %w", err)
	}
	for _, entry := range zr.File {
		if !strings.EqualFold(entry.Name, inner) {
			continue
		}
		if entry.UncompressedSize64 > maxZippedMedia {
			return nil, "", errZippedMediaTooLarge
		}
		rc, openErr := entry.Open()
		if openErr != nil {
			return nil, "", fmt.Errorf("open zip entry: %w", openErr)
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, maxZippedMedia+1))
		closeErr := rc.Close()
		if readErr != nil || closeErr != nil {
			return nil, "", fmt.Errorf("read zip entry: %w", errors.Join(readErr, closeErr))
		}
		mem := afero.NewMemMapFs()
		memPath := "/media" + strings.ToLower(filepath.Ext(inner))
		if writeErr := afero.WriteFile(mem, memPath, data, 0o600); writeErr != nil {
			return nil, "", fmt.Errorf("stage zip entry: %w", writeErr)
		}
		return mem, memPath, nil
	}
	return nil, "", fmt.Errorf("%w: %s", errNoSuchFile, inner)
}

// Fallback pulse clocks for a tap image whose header names no machine.
const (
	clockC64   = 985248
	clockVIC20 = 1108405
	clockC16   = 886724
	clockPET   = 1000000
)

// A Commodore .tap image counts its pulses in the cycles of the machine that
// wrote it, so the machine and video standard in its header give the clock.
const (
	tapHeader     = 20
	tapMachineOff = 13
	tapVideoOff   = 14
	tapNTSC       = 1
)

// tapClocks are the pulse clocks in hertz by machine byte, PAL then NTSC.
var tapClocks = map[byte][2]int{ //nolint:gochecknoglobals // constant table
	0: {985248, 1022730},  // C64
	1: {1108405, 1022727}, // VIC-20
	2: {886724, 894886},   // C16 and Plus/4
	3: {1000000, 1000000}, // PET
	4: {985248, 1022730},  // C128
}

func isTapImage(data []byte) bool {
	return len(data) >= tapHeader && string(data[4:12]) == "TAPE-RAW" &&
		(string(data[:4]) == "C64-" || string(data[:4]) == "C16-")
}

// tapDuration is how long a Commodore .tap image plays, from its pulse
// lengths: a byte is that many 8-cycle units, and a zero is a long pulse whose
// length the next three bytes give (version 1 images). fallbackHz applies when
// the header names no known machine.
func tapDuration(fs afero.Fs, path string, fallbackHz int) (time.Duration, bool) {
	fs, path, err := readableMedia(fs, path)
	if err != nil {
		return 0, false
	}
	data, err := afero.ReadFile(fs, path)
	if err != nil || !isTapImage(data) {
		return 0, false
	}
	clockHz := fallbackHz
	if clocks, ok := tapClocks[data[tapMachineOff]]; ok {
		clockHz = clocks[0]
		if data[tapVideoOff] == tapNTSC {
			clockHz = clocks[1]
		}
	}
	version := data[12]
	var cycles int64
	for i := tapHeader; i < len(data); i++ {
		b := data[i]
		switch {
		case b != 0:
			cycles += int64(b) * 8
		case version >= 1 && i+3 < len(data):
			cycles += int64(data[i+1]) | int64(data[i+2])<<8 | int64(data[i+3])<<16
			i += 3
		default:
			cycles += 256 * 8
		}
	}
	return time.Duration(cycles) * time.Second / time.Duration(clockHz), true
}

// commodoreTape loads the first program of a tape and runs it once the tape has
// played. The tape plays in real time, so the run command waits for its length.
func commodoreTape(clockHz int) func(afero.Fs, string) ([]loadStep, bool, error) {
	return commodoreTapeLoad(clockHz, `LOAD"",1{enter}`)
}

// commodoreTapeLoad is commodoreTape with the machine's own load command.
func commodoreTapeLoad(clockHz int, load string) func(afero.Fs, string) ([]loadStep, bool, error) {
	return func(fs afero.Fs, path string) ([]loadStep, bool, error) {
		steps := []loadStep{typeText(load)}
		if length, ok := tapDuration(fs, path, clockHz); ok {
			steps = append(steps, typeAfter(min(length+tapeMargin, maxTapeWait), "RUN{enter}"))
		}
		return steps, true, nil
	}
}

// c128Tape loads a tape in C64 mode: the tapes the C128 plays are C64 software,
// which crashes in 128 mode.
func c128Tape(fs afero.Fs, path string) ([]loadStep, bool, error) {
	steps, ok, err := commodoreTape(clockC64)(fs, path)
	if !ok || err != nil {
		return steps, ok, err
	}
	steps[0].pause = 3 * time.Second
	return append([]loadStep{typeText("GO64{enter}"), typeAfter(time.Second, "Y{enter}")}, steps...), true, nil
}

func buildTRS80Tape(fs afero.Fs, path string) ([]loadStep, bool, error) {
	fs, path, err := readableMedia(fs, path)
	if err != nil {
		return nil, false, nil //nolint:nilerr // media this reader cannot reach launches as it always has
	}
	program, err := trs80cas.ProgramName(fs, path)
	if err != nil {
		return nil, false, nil //nolint:nilerr // a tape this reader cannot place launches as it always has
	}
	if program.Kind == trs80cas.BASIC {
		return []loadStep{
			typeText("{enter}"),
			typeAfter(3*time.Second, "CLOAD{enter}"),
			typeAfter(tapeWait(fs, path, trs80TapeRate), "RUN{enter}"),
		}, true, nil
	}
	initial := "{enter}"
	if program.Name != "" {
		initial = program.Name[:1] + initial
	}
	return []loadStep{
		typeText("{enter}"), // the ROM first asks for the memory size
		typeAfter(3*time.Second, "SYSTEM{enter}"),
		typeAfter(2*time.Second, initial),
		typeAfter(tapeWait(fs, path, trs80TapeRate), "/{enter}"),
	}, true, nil
}

// bkProgramStart is where BK0010 programs start: those loaded below it keep
// their autostart data in front of the code.
const bkProgramStart = 0o1000

// buildBK0010Bin starts a program the core has put in memory. A .bin starts
// with its load address, and the monitor runs code with <octal address>G.
func buildBK0010Bin(fs afero.Fs, path string) ([]loadStep, bool, error) {
	fs, path, err := readableMedia(fs, path)
	if err != nil {
		return nil, false, nil //nolint:nilerr // media this reader cannot reach launches as it always has
	}
	f, err := fs.Open(path)
	if err != nil {
		return nil, false, nil //nolint:nilerr // media this reader cannot reach launches as it always has
	}
	defer func() { _ = f.Close() }()
	var header [2]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return nil, false, nil //nolint:nilerr // a file too short to be a program launches as it always has
	}
	start := max(int(binary.LittleEndian.Uint16(header[:])), bkProgramStart)
	return []loadStep{typeText(fmt.Sprintf("%oG", start))}, true, nil
}

const (
	// einsteinReservedTracks are the system tracks before an Einstein disk's
	// directory.
	einsteinReservedTracks = 2
	// einsteinBootWait is how long XtalDOS takes to boot from the disk.
	einsteinBootWait = 6 * time.Second
)

// buildEinsteinDisk boots the disk with Ctrl+Break, then runs its program when
// the disk has exactly one. A disk with several is left at the DOS prompt.
func buildEinsteinDisk(fs afero.Fs, path string) ([]loadStep, bool, error) {
	steps := []loadStep{typeText(chord("ctrl", "tab"))}
	readFS, readPath, err := readableMedia(fs, path)
	if err != nil {
		return steps, true, nil //nolint:nilerr // a disk this reader cannot reach is still booted
	}
	files, err := cpcdsk.Directory(readFS, readPath, einsteinReservedTracks)
	if err != nil {
		return steps, true, nil //nolint:nilerr // a disk this reader cannot read is still booted
	}
	var program string
	for _, f := range files {
		name, ext, _ := strings.Cut(f.Name, ".")
		if !strings.EqualFold(ext, "COM") {
			continue
		}
		if program != "" {
			return steps, true, nil
		}
		program = name
	}
	if program != "" {
		steps = append(steps, typeAfter(einsteinBootWait, program+"{enter}"))
	}
	return steps, true, nil
}

func buildCPCDisk(fs afero.Fs, path string) ([]loadStep, bool, error) {
	fs, path, err := readableMedia(fs, path)
	if err != nil {
		return nil, false, nil //nolint:nilerr // media this reader cannot reach launches as it always has
	}
	listing, err := cpcdsk.Programs(fs, path)
	if err != nil {
		return nil, false, nil //nolint:nilerr // a disk this reader cannot place launches as it always has
	}
	if name, ok := listing.Runnable(); ok {
		name = strings.TrimSuffix(strings.TrimSuffix(name, ".BAS"), ".BIN")
		return []loadStep{typeText(fmt.Sprintf(`RUN"%s{enter}`, name))}, true, nil
	}
	if listing.CPM {
		return []loadStep{typeText("|CPM{enter}")}, true, nil
	}
	return nil, false, nil
}

func buildDECBDisk(fs afero.Fs, path string) ([]loadStep, bool, error) {
	fs, path, err := readableMedia(fs, path)
	if err != nil {
		return nil, false, nil //nolint:nilerr // media this reader cannot reach launches as it always has
	}
	files, err := decb.Files(fs, path)
	if err != nil {
		return nil, false, nil //nolint:nilerr // a disk this reader cannot place launches as it always has
	}
	file, ok := decb.Runnable(files)
	if !ok {
		return nil, false, nil
	}
	command := file.RunCommand()
	if command == "" {
		return nil, false, nil
	}
	return []loadStep{typeText(command + "{enter}")}, true, nil
}

func buildCoCoTape(fs afero.Fs, path string) ([]loadStep, bool, error) {
	if readFS, readPath, err := readableMedia(fs, path); err == nil {
		fs, path = readFS, readPath
	}
	return []loadStep{
		typeText("CLOADM{enter}"),
		typeAfter(tapeWait(fs, path, 100), "EXEC{enter}"),
	}, true, nil
}

// loosePlanners are the systems whose media needs a command typed after
// loading. Each command starts a program the core has mounted but does not
// run by itself; media a core starts on its own has no rule.
func loosePlanners(fs afero.Fs) map[string]loadPlanner {
	disk := []string{".d64", ".g64", ".d81"}
	return map[string]loadPlanner{
		systemdefs.SystemC64: looseRules(fs,
			looseRule{
				layout: commodoreLayout(), exts: disk, settle: c64Settle,
				build: commodoreDisk(`LOAD"*",8,1`),
			},
			looseRule{
				layout: commodoreLayout(), exts: []string{".tap"}, settle: c64Settle,
				build:   commodoreTape(clockC64),
				options: []mgls.Setting{mgls.Option{Name: "System ROM", Value: "Standard C64"}},
			},
		),
		systemdefs.SystemC128: looseRules(fs,
			looseRule{
				layout: commodoreLayout(), exts: disk, settle: c128Settle,
				build: commodoreDisk(`LOAD"*",8,1`),
			},
			looseRule{
				layout: commodoreLayout(), exts: []string{".tap"}, settle: c128Settle,
				build: c128Tape,
			},
		),
		systemdefs.SystemVIC20: looseRules(fs,
			looseRule{
				layout: commodoreLayout(), exts: disk, settle: looseSettle,
				build: commodoreDisk(`LOAD"*",8,1`),
			},
			looseRule{
				layout: commodoreLayout(), exts: []string{".tap"}, settle: looseSettle,
				build: commodoreTape(clockVIC20),
			},
			looseRule{
				layout: commodoreLayout(), exts: []string{".crt"}, settle: looseSettle,
				build: fixedSteps(typeText(chord("ctrl", "f11"))),
			},
		),
		systemdefs.SystemC16: looseRules(fs,
			looseRule{
				layout: commodoreLayout(), exts: disk, settle: looseSettle,
				build: commodoreDisk(`LOAD"*",8,1`),
			},
			looseRule{
				layout: commodoreLayout(), exts: []string{".tap"}, settle: looseSettle,
				build: commodoreTape(clockC16),
			},
		),
		systemdefs.SystemPET2001: looseRules(fs,
			looseRule{
				// The core plays the tape as soon as it has it, with no motor
				// control, so LOAD goes in before the leader runs out. BASIC 4
				// loads from tape when LOAD names no device.
				layout: commodoreLayout(), exts: []string{".tap"}, settle: petTapeSettle,
				build: commodoreTapeLoad(clockPET, "LOAD{enter}"),
			},
		),
		systemdefs.SystemAcornElectron: looseRules(fs,
			looseRule{
				exts: []string{".uef"}, settle: electronMenuSettle, layout: acornLayout(),
				build: fixedSteps(
					// The card's boot.vhd autostarts its menu, which Func and 0 leave.
					typeText(chord("capslock", "0")),
					typeAfter(time.Second, "*TAPE{enter}"),
					// The card's filing system raises PAGE above where tape
					// programs expect to load.
					typeAfter(time.Second, "PAGE=&E00{enter}"),
					// CHAIN is Func and K on the Electron's keyboard.
					typeAfter(time.Second, chord("capslock", "k")+`""{enter}`),
				),
			},
		),
		systemdefs.SystemAmstrad: looseRules(fs,
			looseRule{exts: []string{".dsk"}, settle: looseSettle, layout: amstradLayout(), build: buildCPCDisk},
			looseRule{
				exts: []string{".cdt"}, settle: looseSettle, layout: amstradLayout(),
				build: fixedSteps(typeText(`|TAPE{enter}RUN"{enter}`), typeAfter(time.Second, "{enter}")),
			},
		),
		systemdefs.SystemZXSpectrum: looseRules(fs,
			looseRule{
				exts: []string{".tap", ".tzx", ".pzx"}, settle: looseSettle,
				build: fixedSteps(typeText("{enter}")),
			},
		),
		systemdefs.SystemZX81: looseRules(fs,
			looseRule{
				exts: []string{".p", ".0", ".o"}, settle: looseSettle, layout: zx81Layout(),
				build: fixedSteps(typeText(`J""{enter}`), typeAfter(zx81LoadWait, "R{enter}")),
			},
		),
		systemdefs.SystemTRS80: looseRules(fs,
			looseRule{exts: []string{".cas"}, settle: looseSettle, build: buildTRS80Tape},
			looseRule{
				// The ROM boots from drive 0 only at a reset, and the core
				// mounts the disk after the machine has started.
				exts: []string{".dsk", ".jv1", ".jvi"}, build: fixedSteps(),
				options: []mgls.Setting{mgls.Reset{Delay: 1, Hold: 1}},
			},
		),
		systemdefs.SystemBBCMicro: looseRules(fs,
			looseRule{
				// The machine can start booting before the disk is mounted and
				// stop at a disc fault, so it is reset once the disk is in.
				exts: []string{".ssd", ".dsd"}, build: fixedSteps(),
				options: []mgls.Setting{
					mgls.Option{Name: "Model", Value: "Master(R65SC12)"},
					mgls.Option{Name: "Dflt Boot", Value: "Floppy (SSD/DSD)"},
					mgls.Reset{Delay: 1, Hold: 1},
				},
			},
		),
		systemdefs.SystemOric: looseRules(fs,
			looseRule{
				// The disk controller only comes up at a reset, and the core
				// mounts the disk after the machine has booted.
				exts: []string{".dsk"}, build: fixedSteps(),
				options: []mgls.Setting{mgls.Reset{Delay: 1, Hold: 1}},
			},
		),
		systemdefs.SystemBK0011M: looseRules(fs,
			looseRule{
				exts: []string{".bin"}, settle: looseSettle, build: buildBK0010Bin,
				options: []mgls.Setting{mgls.Option{Name: "Model", Value: "BK0010"}},
			},
		),
		systemdefs.SystemCoCo3: looseRules(fs,
			looseRule{
				exts: []string{".ccc"}, build: fixedSteps(),
				options: []mgls.Setting{mgls.Option{Name: "Multi-Pak", Value: "(Slot3) ECB /Cart"}},
			},
		),
		systemdefs.SystemCoCo2: looseRules(fs,
			looseRule{
				exts: []string{".dsk"}, settle: looseSettle, build: buildDECBDisk,
				options: []mgls.Setting{mgls.Option{Name: "Cart Slot", Value: "Disk"}},
			},
			looseRule{exts: []string{".cas"}, settle: looseSettle, build: buildCoCoTape},
		),
		systemdefs.SystemQL: looseRules(fs,
			looseRule{
				exts: []string{".mdv"}, settle: looseSettle,
				// The ROM waits for F1 (monitor) or F2 (TV), then boots mdv1_boot
				// itself. Nothing else is typed: keys sent while it loads reach
				// the program it starts.
				build: fixedSteps(typeText(fmt.Sprintf("{hold:f1:%s}", qlKeyHold))),
			},
		),
		systemdefs.SystemTatungEinstein: looseRules(fs,
			looseRule{exts: []string{".dsk"}, settle: looseSettle, build: buildEinsteinDisk},
		),
		systemdefs.SystemVector06C: looseRules(fs,
			looseRule{
				exts: []string{".fdd", ".edd"}, settle: looseSettle,
				build: fixedSteps(typeText(chord("alt", "f11"))),
			},
		),
	}
}

// loadPlannersFor builds the planner of every system that types a command
// after loading, and the launcher changes each needs.
func loadPlannersFor(pl platforms.Platform) map[string]loadPlannerSet {
	fs := loadFilesystem(pl)
	sets := make(map[string]loadPlannerSet)

	for systemID, plan := range loosePlanners(fs) {
		sets[systemID] = loadPlannerSet{plan: plan}
	}

	for systemID, set := range fatPlanners(pl, fs) {
		prior := sets[systemID]
		if prior.plan != nil {
			set.plan = firstPlan(set.plan, prior.plan)
		}
		sets[systemID] = set
	}

	list := extendForImages(fs, listMMB, nil, discoverImages(pl, fs))
	for _, systemID := range []string{systemdefs.SystemAcornElectron, systemdefs.SystemBBCMicro} {
		prior := sets[systemID]
		plan := mmbPlanner(fs, systemID)
		if prior.plan != nil {
			plan = firstPlan(plan, prior.plan)
		}
		sets[systemID] = loadPlannerSet{plan: plan, extend: list}
	}
	return sets
}

// loadFilesystem is the filesystem plans read media from.
func loadFilesystem(pl platforms.Platform) afero.Fs {
	if p, ok := pl.(interface{ filesystem() afero.Fs }); ok {
		return p.filesystem()
	}
	return afero.NewOsFs()
}
