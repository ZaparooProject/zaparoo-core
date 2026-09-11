//go:build windows

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

package steamtracker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

// candidateTier ranks how confidently a process was matched to an AppID.
// Lower is better.
type candidateTier int

const (
	// tierSteamEnv is a process Steam itself stamped with the AppID.
	tierSteamEnv candidateTier = iota
	// tierExactExe is a process running the executable appinfo.vdf names.
	tierExactExe
	// tierInstallDir is any process running from the app's install directory.
	tierInstallDir
)

type gameCandidate struct {
	exe  string
	pid  int32
	ppid int32
	tier candidateTier
}

// gamePaths are the on-disk locations an app can be recognised by. Resolving
// them reads appinfo.vdf and the library manifests, which is far too expensive
// to repeat per attempt while waiting for a game to start, and neither value
// changes during a launch.
type gamePaths struct {
	installDir string
	exePath    string
}

// resolveGamePaths looks up where an app is installed and which executable it
// declares. Both are best-effort; an empty value simply disables that match.
func resolveGamePaths(steamRoot string, appID int) gamePaths {
	installDir, _ := steam.FindInstallDirByAppIDInSteamDir(steamRoot, appID)
	exePath, _ := steam.GetGameExecutable(steamRoot, appID)
	return gamePaths{installDir: installDir, exePath: exePath}
}

// FindGameProcess finds the root process of a running Steam game, resolving the
// app's paths first. Prefer resolveGamePaths plus scanForGameProcess when
// calling repeatedly, so the manifests are only read once.
func FindGameProcess(steamRoot string, appID int) (*os.Process, int, error) {
	return scanForGameProcess(resolveGamePaths(steamRoot, appID), appID, true)
}

// scanForGameProcess finds the root process of a running Steam game.
//
// Steam on Windows offers no equivalent of the Linux reaper, so the root is
// recovered by matching processes against the app's executable and install
// directory and keeping the topmost match. Returning the topmost match means a
// launcher stub that spawns the real game is stopped along with its children
// rather than left behind.
//
// allowEnvSweep permits the last-resort scan of process environments for
// Steam's SteamAppId marker. That reads every process's memory, so it is only
// worth doing once the cheap path-based matches have been given a fair chance.
//
// Returns a nil process when nothing matched, which is not an error.
func scanForGameProcess(paths gamePaths, appID int, allowEnvSweep bool) (*os.Process, int, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, 0, fmt.Errorf("enumerate processes: %w", err)
	}

	candidates := collectCandidates(procs, paths)
	if len(candidates) == 0 && allowEnvSweep {
		candidates = collectEnvCandidates(procs, appID)
	}
	if len(candidates) == 0 {
		log.Debug().Int("appID", appID).Str("installDir", paths.installDir).
			Msg("no candidate processes matched Steam app")
		return nil, 0, nil
	}

	best := pickRootCandidate(candidates)
	proc, err := os.FindProcess(int(best.pid))
	if err != nil {
		return nil, 0, fmt.Errorf("open process %d: %w", best.pid, err)
	}

	log.Debug().
		Int("appID", appID).
		Int32("pid", best.pid).
		Str("exe", best.exe).
		Int("tier", int(best.tier)).
		Msg("found Steam game process")
	return proc, int(best.pid), nil
}

// collectCandidates matches processes against the app by install path, which
// only costs an Exe() call each.
func collectCandidates(procs []*process.Process, paths gamePaths) []gameCandidate {
	candidates := make([]gameCandidate, 0, 4)
	for _, p := range procs {
		exe, exeErr := p.Exe()
		if exeErr != nil || exe == "" {
			continue
		}

		tier, matched := matchCandidateTier(exe, paths)
		if !matched {
			continue
		}

		ppid, _ := p.Ppid()
		candidates = append(candidates, gameCandidate{pid: p.Pid, ppid: ppid, exe: exe, tier: tier})
	}
	return candidates
}

// matchCandidateTier classifies an executable path against the app's known
// locations, reporting whether it matched at all.
func matchCandidateTier(exe string, paths gamePaths) (tier candidateTier, matched bool) {
	switch {
	case paths.exePath != "" && strings.EqualFold(exe, paths.exePath):
		return tierExactExe, true
	case paths.installDir != "" && pathWithin(exe, paths.installDir):
		return tierInstallDir, true
	default:
		return 0, false
	}
}

// collectEnvCandidates finds processes Steam tagged with the AppID. This is the
// last resort: reading another process's environment means reading its memory,
// for every process on the machine, which is both slow and the kind of thing
// endpoint protection objects to. It only runs when no path-based match was
// found. Failures are skipped quietly because they are expected for anything
// at a higher integrity level.
func collectEnvCandidates(procs []*process.Process, appID int) []gameCandidate {
	want := "SteamAppId=" + strconv.Itoa(appID)
	candidates := make([]gameCandidate, 0, 4)
	for _, p := range procs {
		env, envErr := p.EnvironWithContext(context.Background())
		if envErr != nil {
			continue
		}
		for _, entry := range env {
			if !strings.EqualFold(entry, want) {
				continue
			}
			exe, _ := p.Exe()
			ppid, _ := p.Ppid()
			candidates = append(candidates, gameCandidate{
				pid: p.Pid, ppid: ppid, exe: exe, tier: tierSteamEnv,
			})
			break
		}
	}
	return candidates
}

// pickRootCandidate returns the highest process in the tree of matches, so
// killing it also takes down the children. Candidates whose parent is also a
// match are skipped; ties break on tier, then on lowest PID for determinism.
func pickRootCandidate(candidates []gameCandidate) gameCandidate {
	byPID := make(map[int32]struct{}, len(candidates))
	for _, c := range candidates {
		byPID[c.pid] = struct{}{}
	}

	roots := make([]gameCandidate, 0, len(candidates))
	for _, c := range candidates {
		if _, parentMatched := byPID[c.ppid]; parentMatched {
			continue
		}
		roots = append(roots, c)
	}
	// Every candidate having a matched parent means the matches form a cycle,
	// which cannot happen for a real process tree, but fall back rather than
	// index an empty slice.
	if len(roots) == 0 {
		roots = candidates
	}

	sort.Slice(roots, func(i, j int) bool {
		if roots[i].tier != roots[j].tier {
			return roots[i].tier < roots[j].tier
		}
		return roots[i].pid < roots[j].pid
	})
	return roots[0]
}

// pathWithin reports whether path sits inside dir, comparing whole path
// segments so "C:\\Games\\Portal2" is not treated as inside
// "C:\\Games\\Portal". filepath.Rel compares Windows path segments with
// strings.EqualFold, so case differences match without extra handling.
func pathWithin(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}
