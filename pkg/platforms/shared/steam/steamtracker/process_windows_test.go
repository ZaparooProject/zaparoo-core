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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathWithin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		dir  string
		want bool
	}{
		{
			name: "file directly inside dir",
			path: `C:\Games\Portal\portal.exe`,
			dir:  `C:\Games\Portal`,
			want: true,
		},
		{
			name: "file nested deeper inside dir",
			path: `C:\Games\Portal\bin\portal.exe`,
			dir:  `C:\Games\Portal`,
			want: true,
		},
		{
			name: "sibling directory sharing a name prefix is not inside",
			path: `C:\Games\Portal2\portal2.exe`,
			dir:  `C:\Games\Portal`,
			want: false,
		},
		{
			name: "case differences still match on Windows",
			path: `c:\games\portal\PORTAL.EXE`,
			dir:  `C:\Games\Portal`,
			want: true,
		},
		{
			name: "parent directory is not inside",
			path: `C:\Games\other.exe`,
			dir:  `C:\Games\Portal`,
			want: false,
		},
		{
			name: "empty dir never matches",
			path: `C:\Games\Portal\portal.exe`,
			dir:  "",
			want: false,
		},
		{
			name: "empty path never matches",
			path: "",
			dir:  `C:\Games\Portal`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, pathWithin(tt.path, tt.dir))
		})
	}
}

func TestPickRootCandidatePrefersTreeRoot(t *testing.T) {
	t.Parallel()

	// A launcher stub (100) that spawned the real game (200). Stopping the
	// stub takes the child with it, so the stub must win.
	candidates := []gameCandidate{
		{pid: 200, ppid: 100, exe: `C:\Games\Portal\game.exe`, tier: tierExactExe},
		{pid: 100, ppid: 4, exe: `C:\Games\Portal\launcher.exe`, tier: tierInstallDir},
	}

	got := pickRootCandidate(candidates)
	assert.Equal(t, int32(100), got.pid)
}

func TestPickRootCandidatePrefersStrongerTierAmongRoots(t *testing.T) {
	t.Parallel()

	// Two unrelated roots. The executable appinfo.vdf names beats some other
	// binary that merely lives in the install directory, such as a crash
	// handler. Env-tagged candidates are only collected when no path matched,
	// so they never appear alongside these.
	candidates := []gameCandidate{
		{pid: 300, ppid: 4, exe: `C:\Games\Portal\crashhandler.exe`, tier: tierInstallDir},
		{pid: 400, ppid: 8, exe: `C:\Games\Portal\game.exe`, tier: tierExactExe},
	}

	got := pickRootCandidate(candidates)
	assert.Equal(t, int32(400), got.pid)
}

func TestPickRootCandidateHandlesAllChildrenMatched(t *testing.T) {
	t.Parallel()

	// Every candidate claims a matched parent, which cannot happen for a real
	// tree. The helper must still return something rather than panic.
	candidates := []gameCandidate{
		{pid: 10, ppid: 20, exe: "a.exe", tier: tierInstallDir},
		{pid: 20, ppid: 10, exe: "b.exe", tier: tierInstallDir},
	}

	got := pickRootCandidate(candidates)
	assert.Equal(t, int32(10), got.pid)
}

func TestCollectCandidateMatchRules(t *testing.T) {
	t.Parallel()

	// collectCandidates needs live processes to enumerate, so exercise the
	// matching rules it applies against a fixed set of executable paths.
	paths := gamePaths{
		installDir: `C:\Games\Portal`,
		exePath:    `C:\Games\Portal\game.exe`,
	}

	tests := []struct {
		name string
		exe  string
		want candidateTier
		hit  bool
	}{
		{name: "exact executable", exe: `C:\Games\Portal\game.exe`, want: tierExactExe, hit: true},
		{name: "exact match ignores case", exe: `c:\games\portal\GAME.EXE`, want: tierExactExe, hit: true},
		{name: "other binary in install dir", exe: `C:\Games\Portal\crash.exe`, want: tierInstallDir, hit: true},
		{name: "sibling directory does not match", exe: `C:\Games\Portal2\game.exe`, hit: false},
		{name: "unrelated path does not match", exe: `C:\Windows\notepad.exe`, hit: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tier, matched := matchCandidateTier(tt.exe, paths)
			require.Equal(t, tt.hit, matched)
			if matched {
				assert.Equal(t, tt.want, tier)
			}
		})
	}
}

func TestResolveGamePathsWithoutSteamRoot(t *testing.T) {
	t.Parallel()

	// A missing Steam root must degrade to "no path matches" rather than
	// yielding relative paths that could match unrelated processes.
	paths := resolveGamePaths("", 212680)
	assert.Empty(t, paths.installDir)
	assert.Empty(t, paths.exePath)
}
