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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

// MiSTer Moonlight CRT (manfredi90/MiSTer_Moonlight_CRT) streams games from a
// PC running Sunshine. The client runs on the ARM and loads the H264 core
// itself, which decodes the stream in the FPGA.
//
// Paths:
//   - moonlight:///Moonlight (or a bare moonlight://) opens the client's menu.
//   - moonlight://<pc>/<App> streams App from a PC, named as in the client's
//     list of PCs or given by its address.

const (
	moonlightBinaryName = "mister_moonlight_crt"
	moonlightMenuName   = "Moonlight"
	// moonlightDefaultSync is the client's menu default when menu.cfg has no
	// sync= line. Its stream action defaults to neither, so it is passed.
	moonlightDefaultSync = moonlightSyncGenlock
	// moonlightMaxBitrate is the cap the client's menu applies to bitrate=.
	moonlightMaxBitrate = 10000
	// moonlightMaxConfigSize bounds menu.cfg. The client writes a few
	// settings and at most 16 PCs, well under 4 KiB.
	moonlightMaxConfigSize = 16 * 1024
	// moonlightMaxConfigLine matches the client's own line buffer.
	moonlightMaxConfigLine = 159
	moonlightMaxPCs        = 16
	moonlightMaxHost       = 63
	moonlightMaxPCName     = 31
	moonlightMaxAppName    = 255
)

// moonlightDir is the client's folder, where its install instructions put
// it. Variables so tests can point them at temporary folders.
var (
	moonlightDir           = filepath.Join(misterconfig.SDRootDir, "MiSTer_moonlight_crt")
	moonlightOtherCoresDir = filepath.Join(misterconfig.SDRootDir, "_Other")
)

type moonlightSync int

const (
	moonlightSyncGenlock moonlightSync = iota
	moonlightSyncGenlockLow
	moonlightSync60Hz
)

// moonlightSyncNames maps the client's sync= values, including the names it
// still reads from older versions, to its sync modes.
var moonlightSyncNames = map[string]moonlightSync{
	"genlock":             moonlightSyncGenlock,
	"genlock low latency": moonlightSyncGenlockLow,
	"60 Hz":               moonlightSync60Hz,
	"genlock +1":          moonlightSyncGenlock,
	"guard":               moonlightSync60Hz,
	"none":                moonlightSync60Hz,
	"guardia":             moonlightSync60Hz,
	"nessuna":             moonlightSync60Hz,
}

// moonlightPCResolutions are the client's pcres= values other than "off".
var moonlightPCResolutions = map[string]struct{}{
	"640x480":  {},
	"1024x768": {},
	"1280x960": {},
	"1280x720": {},
}

type moonlightPC struct {
	host string
	name string
}

// moonlightSettings are the stream settings the client's menu keeps in
// menu.cfg. Its stream action does not read that file, so Core passes them
// as options to make a token launch match the menu.
type moonlightSettings struct {
	pcres     string
	pcs       []moonlightPC
	bitrate   int
	sync      moonlightSync
	audio     bool
	quitAfter bool
}

func defaultMoonlightSettings() moonlightSettings {
	return moonlightSettings{audio: true, sync: moonlightDefaultSync}
}

// cAtoi parses like C's atoi, which the client uses for numeric settings:
// leading spaces, an optional sign, then digits up to the first non-digit.
func cAtoi(s string) int {
	s = strings.TrimLeft(s, " \t")
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	n := 0
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
		if n > 1_000_000 {
			break
		}
	}
	if neg {
		return -n
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// parseMoonlightConfig reads menu.cfg the way the client does, keeping only
// the keys Core uses. The file is untrusted: its size and lines are bounded,
// unknown keys and values are dropped, and nothing in it reaches a shell.
func parseMoonlightConfig(r io.Reader) (moonlightSettings, error) {
	settings := defaultMoonlightSettings()
	data, err := io.ReadAll(io.LimitReader(r, moonlightMaxConfigSize+1))
	if err != nil {
		return settings, fmt.Errorf("read menu.cfg: %w", err)
	}
	if len(data) > moonlightMaxConfigSize {
		return settings, fmt.Errorf("menu.cfg exceeds %d bytes", moonlightMaxConfigSize)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, moonlightMaxConfigSize), moonlightMaxConfigSize)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if len(line) > moonlightMaxConfigLine {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch key {
		case "bitrate":
			settings.bitrate = min(cAtoi(value), moonlightMaxBitrate)
		case "audio":
			settings.audio = cAtoi(value) != 0
		case "quitafter":
			settings.quitAfter = cAtoi(value) != 0
		case "sync":
			if mode, ok := moonlightSyncNames[value]; ok {
				settings.sync = mode
			}
		case "pcres":
			settings.pcres = ""
			if _, ok := moonlightPCResolutions[value]; ok {
				settings.pcres = value
			}
		case "pc":
			if len(settings.pcs) >= moonlightMaxPCs {
				continue
			}
			host, name, _ := strings.Cut(value, "|")
			if host == "" {
				continue
			}
			if name == "" {
				name = host
			}
			settings.pcs = append(settings.pcs, moonlightPC{
				host: truncate(host, moonlightMaxHost),
				name: truncate(name, moonlightMaxPCName),
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return settings, fmt.Errorf("scan menu.cfg: %w", err)
	}
	return settings, nil
}

// loadMoonlightSettings reads the client's menu.cfg. A missing file gives the
// menu's defaults.
func loadMoonlightSettings() (moonlightSettings, error) {
	f, err := os.Open(filepath.Join(moonlightDir, "menu.cfg")) //nolint:gosec // G304: fixed file in the client's folder
	if errors.Is(err, os.ErrNotExist) {
		return defaultMoonlightSettings(), nil
	}
	if err != nil {
		return defaultMoonlightSettings(), fmt.Errorf("open menu.cfg: %w", err)
	}
	defer func() { _ = f.Close() }()
	return parseMoonlightConfig(f)
}

// validMoonlightHost applies the client's own address rule, and refuses a
// leading '-', which the client would read as an option.
func validMoonlightHost(host string) bool {
	if host == "" || len(host) > moonlightMaxHost || host[0] == '-' {
		return false
	}
	for i := range len(host) {
		c := host[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c == '.', c == '-', c == '_', c == ':':
		default:
			return false
		}
	}
	return true
}

// resolveMoonlightHost finds a PC by its name in the client's list, then by
// its address, and otherwise takes pc as an address.
func resolveMoonlightHost(pcs []moonlightPC, pc string) (string, bool) {
	for _, entry := range pcs {
		if strings.EqualFold(entry.name, pc) {
			return entry.host, validMoonlightHost(entry.host)
		}
	}
	for _, entry := range pcs {
		if strings.EqualFold(entry.host, pc) {
			return entry.host, validMoonlightHost(entry.host)
		}
	}
	return pc, validMoonlightHost(pc)
}

// moonlightTarget is what a moonlight:// path asks for. An empty pc is the
// client's menu.
type moonlightTarget struct {
	pc  string
	app string
}

func (t moonlightTarget) menu() bool { return t.pc == "" }

// parseMoonlightPath reads a moonlight:// path. A PC with no app is refused,
// since the client's stream action would quietly pick its default app.
func parseMoonlightPath(path string) (moonlightTarget, error) {
	parts := virtualpath.ParseURIComponents(path)
	if !strings.EqualFold(parts.Scheme, shared.SchemeMoonlight) {
		return moonlightTarget{}, virtualpath.ErrSchemeMismatch
	}
	if strings.Trim(parts.Rest, "/") == "" {
		return moonlightTarget{}, nil
	}
	parsed, err := virtualpath.ParseVirtualPathStr(path)
	if err != nil {
		return moonlightTarget{}, fmt.Errorf("parse moonlight path: %w", err)
	}
	if parsed.ID == "" {
		return moonlightTarget{}, nil
	}
	switch {
	case parsed.Name == "":
		return moonlightTarget{}, errors.New("moonlight path has a PC but no app")
	case len(parsed.ID) > moonlightMaxHost:
		return moonlightTarget{}, errors.New("moonlight PC name is too long")
	case len(parsed.Name) > moonlightMaxAppName:
		return moonlightTarget{}, errors.New("moonlight app name is too long")
	case virtualpath.ContainsControlChar(parsed.ID), virtualpath.ContainsControlChar(parsed.Name):
		return moonlightTarget{}, errors.New("moonlight path has control characters")
	}
	return moonlightTarget{pc: parsed.ID, app: parsed.Name}, nil
}

// moonlightTest accepts only paths the launcher can act on, so a malformed
// one never stops the running media. See shared.SchemeIDTest.
func moonlightTest(_ *config.Instance, path string) bool {
	_, err := parseMoonlightPath(path)
	return err == nil
}

// moonlightStreamArgs builds the client's stream action with the menu's
// settings. Every value is its own argument; nothing goes through a shell.
func moonlightStreamArgs(host, app string, settings *moonlightSettings) []string {
	args := []string{"stream", host, "-app", app}
	if settings.bitrate > 0 {
		args = append(args, "-bitrate", strconv.Itoa(settings.bitrate))
	}
	if !settings.audio {
		args = append(args, "-noaudio")
	}
	if settings.quitAfter {
		args = append(args, "-quitappafter")
	}
	switch settings.sync {
	case moonlightSyncGenlock:
		args = append(args, "-genlock")
	case moonlightSyncGenlockLow:
		args = append(args, "-genlock-low")
	case moonlightSync60Hz:
		args = append(args, "-60hz")
	}
	if settings.pcres != "" {
		args = append(args, "-pcres", settings.pcres)
	}
	return args
}

func moonlightBinary() string {
	return filepath.Join(moonlightDir, moonlightBinaryName)
}

// isH264Core reports whether name is a dated H264 core file, such as
// "H264_20260101.rbf". The client looks only for H264_*.rbf, so an undated
// H264.rbf would not start.
func isH264Core(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "h264_") && strings.HasSuffix(lower, ".rbf")
}

func h264CoreIn(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && isH264Core(entry.Name()) {
			return true
		}
	}
	return false
}

// moonlightInstalled reports whether the client and an H264 core it can
// load are both on the SD card.
func moonlightInstalled() error {
	if !executableFile(moonlightBinary()) {
		//nolint:wrapcheck // the repair error is the whole message; a wrapper would only prefix it
		return platforms.NewLaunchRepairErrorWithReason(
			platforms.LaunchRepairLauncherNotInstalled,
			map[string]string{platforms.LaunchRepairParamLauncher: "Moonlight"},
			"MiSTer Moonlight CRT is not installed",
		)
	}
	if !h264CoreIn(filepath.Join(moonlightDir, "core")) && !h264CoreIn(moonlightOtherCoresDir) {
		//nolint:wrapcheck // the repair error is the whole message; a wrapper would only prefix it
		return platforms.NewLaunchRepairErrorWithReason(
			platforms.LaunchRepairLauncherComponentMissing,
			map[string]string{
				platforms.LaunchRepairParamLauncher: "Moonlight",
				platforms.LaunchRepairParamPlugin:   tracker.MoonlightCoreName,
			},
			"the MiSTer core H264 is not installed",
		)
	}
	return nil
}

// createMoonlightLauncher streams from a Sunshine PC with MiSTer Moonlight CRT.
func createMoonlightLauncher(pl *Platform) platforms.Launcher {
	detected := moonlightInstalled() == nil
	return platforms.Launcher{
		ID:                 tracker.MoonlightLauncherID,
		SystemID:           systemdefs.SystemPC,
		Schemes:            []string{shared.SchemeMoonlight},
		Test:               moonlightTest,
		SkipFilesystemScan: true,
		Lifecycle:          platforms.LifecycleTracked,
		Detected:           &detected,
		Availability:       func(*config.Instance) error { return moonlightInstalled() },
		Preflight:          moonlightPreflight,
		Scanner:            scanMoonlight,
		Launch:             launchMoonlight(pl),
	}
}

// scanMoonlight indexes the client's menu. Sunshine's apps live on the PC and
// the client keeps no list of them, so there is nothing more to index.
func scanMoonlight(
	_ context.Context,
	_ *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	if err := moonlightInstalled(); err != nil {
		log.Debug().Err(err).Msg("Moonlight client not found, skipping scan")
		return results, nil
	}
	return append(results, platforms.ScanResult{
		Path:  virtualpath.CreateVirtualPath(shared.SchemeMoonlight, "", moonlightMenuName),
		Name:  moonlightMenuName,
		NoExt: true,
	}), nil
}

// moonlightPreflight refuses a path whose PC resolves to no usable address
// before Core stops the running media for it.
func moonlightPreflight(_ *config.Instance, path string, _ *platforms.LaunchOptions) error {
	_, err := moonlightArgs(path)
	return err
}

// moonlightArgs turns a path into the client's arguments.
func moonlightArgs(path string) ([]string, error) {
	target, err := parseMoonlightPath(path)
	if err != nil {
		return nil, err
	}
	if target.menu() {
		return []string{"menu"}, nil
	}
	settings, err := loadMoonlightSettings()
	if err != nil {
		log.Warn().Err(err).Msg("moonlight: using default settings")
		settings = defaultMoonlightSettings()
	}
	host, ok := resolveMoonlightHost(settings.pcs, target.pc)
	if !ok {
		return nil, errors.New("moonlight PC is not in the client's list and is not a valid address")
	}
	return moonlightStreamArgs(host, target.app, &settings), nil
}

func buildMoonlightCommand(ctx context.Context, args []string) *exec.Cmd {
	//nolint:gosec // G204: fixed binary, arguments passed without a shell
	cmd := exec.CommandContext(ctx, moonlightBinary(), args...)
	cmd.Dir = moonlightDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	terminateOnCancel(cmd)
	return cmd
}

// moonlightReturnToMenu loads the menu core after the client exits if the
// H264 core is still loaded, as the client's own script does. It does nothing
// after Core stopped the client for another launch: that launch loads its own
// core, and a menu load at the same time could make Main drop it.
func moonlightReturnToMenu(preempted bool, coreName string, launchMenu func() error) {
	if preempted || !strings.EqualFold(coreName, tracker.MoonlightCoreName) {
		return
	}
	if err := launchMenu(); err != nil {
		log.Warn().Err(err).Msg("moonlight: failed to return to menu")
	}
}

func launchMoonlight(pl *Platform) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
		if err := moonlightInstalled(); err != nil {
			log.Warn().Str("path", moonlightDir).Msg("Moonlight client or H264 core not found")
			return nil, err
		}
		args, err := moonlightArgs(path)
		if err != nil {
			return nil, err
		}

		cmd := buildMoonlightCommand(pl.launcherManager.GetContext(), args)
		restore := func() {
			if cmd.Process == nil {
				return
			}
			moonlightReturnToMenu(
				pl.processPreempted(cmd.Process),
				mistermain.GetActiveCoreName(),
				mistermain.LaunchMenu,
			)
		}

		log.Info().Strs("args", args).Msg("launching Moonlight")
		// The client loads the H264 core, so this is a core launch: it
		// supersedes a stop still waiting for an earlier launch's core.
		pl.coreLoads.launched(time.Now())
		return runTrackedProcess(pl, cmd, restore, "moonlight", trackedExit{
			launcherID: tracker.MoonlightLauncherID,
		})
	}
}
