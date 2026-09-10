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
package pinup

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyEmulator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		emu  Emulator
		want EmulatorClass
	}{
		{name: "vpx by name", emu: Emulator{Name: "Visual Pinball X", GamesExt: "vpx"}, want: ClassVisualPinball},
		{name: "vpx alt install", emu: Emulator{Name: "VPX 10.8 x64", GamesExt: "vpx,vpt"}, want: ClassVisualPinball},
		{name: "vpx by extension only", emu: Emulator{Name: "Tables", GamesExt: ".VPX"}, want: ClassVisualPinball},
		{
			name: "vpx by script",
			emu:  Emulator{Name: "Cabinet", LaunchScript: `START "" VPinballX64.exe -play`},
			want: ClassVisualPinball,
		},
		{name: "future pinball", emu: Emulator{Name: "Future Pinball", GamesExt: "fpt"}, want: ClassFuturePinball},
		{
			name: "future pinball script using popper's vpx starter helper",
			emu: Emulator{
				Name: "Future Pinball", GamesExt: "fpt",
				LaunchScript: `START "" "[STARTDIR]Launch\VPXSTARTER.exe" 10 5 60 "BSP Software*"` +
					`START "" "[DIREMU]\BAM\FPLoader.exe" /open "[GAMEFULLNAME]"`,
			},
			want: ClassFuturePinball,
		},
		{
			name: "unnamed emulator whose script only has popper helpers",
			emu:  Emulator{Name: "Custom", LaunchScript: `"[STARTDIR]Launch\VPXSTARTER.exe" 30 10 60 "Player"`},
			want: ClassNone,
		},
		{
			name: "emulator name beats a script that mentions another program",
			emu:  Emulator{Name: "Pinball FX3", LaunchScript: `START "" "PopperKeepFocus.exe" "Visual Pinball Player"`},
			want: ClassPinballFX,
		},
		{name: "fx3", emu: Emulator{Name: "Pinball FX3", GamesExt: "pxp"}, want: ClassPinballFX},
		{
			name: "fx by steam id",
			emu:  Emulator{Name: "Pinball FX", LaunchScript: "steam.exe -applaunch 2328760"},
			want: ClassPinballFX,
		},
		{name: "fx2", emu: Emulator{Name: "FX2"}, want: ClassPinballFX},
		{name: "pinball m", emu: Emulator{Name: "PinballM", LaunchScript: "-applaunch 2337640"}, want: ClassPinballM},
		{
			name: "zaccaria",
			emu:  Emulator{Name: "Zaccaria", LaunchScript: `"Zaccaria Pinball.exe"`},
			want: ClassZaccaria,
		},
		{name: "pro pinball", emu: Emulator{Name: "Pro Pinball Ultra"}, want: ClassProPinball},
		{name: "other pinball", emu: Emulator{Name: "Pinball Arcade"}, want: ClassOtherPinball},
		{
			name: "mame",
			emu:  Emulator{Name: "MAME", GamesExt: "zip", LaunchScript: "mame64.exe [GAMENAME]"},
			want: ClassNone,
		},
		{name: "pc games", emu: Emulator{Name: "PC Games", GamesExt: "lnk;exe"}, want: ClassNone},
		{name: "empty", emu: Emulator{}, want: ClassNone},
		{
			// The usual Baller Installer tree is C:\vPinball\..., so any
			// emulator whose script names a path under it contains "pinball".
			// Classifying on that indexes its games as pinball tables.
			name: "non-pinball emulator installed under a vPinball tree",
			emu: Emulator{
				Name: "MAME", Display: "MAME",
				LaunchScript: `"C:\vPinball\Emulators\MAME\mame64.exe" -rompath "C:\vPinball\roms"`,
			},
			want: ClassNone,
		},
		{
			// The specific keywords still apply to a script.
			name: "future pinball script still classifies",
			emu:  Emulator{Name: "FP", LaunchScript: `"C:\Games\Future Pinball\Future Pinball.exe"`},
			want: ClassFuturePinball,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ClassifyEmulator(&tt.emu))
			assert.Equal(t, tt.want != ClassNone, IsPinball(&tt.emu))
		})
	}
}

func TestCandidateExecutables(t *testing.T) {
	t.Parallel()

	t.Run("popper process name first then class prefixes", func(t *testing.T) {
		t.Parallel()
		got := CandidateExecutables(&Emulator{Name: "Visual Pinball X", ProcessName: "VPinballX"})
		assert.Equal(t, []string{"VPinballX.exe", "VPinballX*", "VPinball*"}, got)
	})

	t.Run("table alternate launcher first", func(t *testing.T) {
		t.Parallel()
		got := TableExecutables(
			&Table{AltExe: "VPinballX107_32bit.exe"},
			&Emulator{Name: "Visual Pinball X", ProcessName: "VPinballX"},
		)
		assert.Equal(t, []string{"VPinballX107_32bit.exe", "VPinballX.exe", "VPinballX*", "VPinball*"}, got)
	})

	t.Run("stock install with no process name", func(t *testing.T) {
		t.Parallel()
		got := CandidateExecutables(&Emulator{Name: "Visual Pinball X", GamesExt: "vpx"})
		assert.Equal(t, []string{"VPinballX*", "VPinball*"}, got)
	})

	t.Run("process name with path", func(t *testing.T) {
		t.Parallel()
		got := CandidateExecutables(&Emulator{
			Name: "Future Pinball", ProcessName: `C:\Games\Future Pinball\Future Pinball.exe`,
		})
		assert.Equal(t, []string{"Future Pinball.exe", "FPLoader.exe"}, got)
	})

	t.Run("other pinball relies on process name", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"PinballArcade.exe"},
			CandidateExecutables(&Emulator{Name: "Pinball Arcade", ProcessName: "PinballArcade"}))
		assert.Empty(t, CandidateExecutables(&Emulator{Name: "Pinball Arcade"}))
	})

	t.Run("non pinball has none", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, CandidateExecutables(&Emulator{Name: "MAME"}))
	})
}

func TestNormalizeExecutable(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"VPinballX":                    "VPinballX.exe",
		"  VPinballX.exe ":             "VPinballX.exe",
		`C:\vp\VPinballX64.exe`:        "VPinballX64.exe",
		"/mnt/c/vp/Future Pinball.EXE": "Future Pinball.EXE",
		"":                             "",
		"   ":                          "",
		`C:\vp\`:                       "",
	}
	for input, want := range tests {
		assert.Equal(t, want, NormalizeExecutable(input), "input %q", input)
	}
}

func TestMatchesExecutable(t *testing.T) {
	t.Parallel()

	candidates := CandidateExecutables(&Emulator{Name: "Visual Pinball X", ProcessName: "VPinballX"})
	assert.True(t, MatchesExecutable(`C:\Visual Pinball\VPinballX64.exe`, candidates))
	assert.True(t, MatchesExecutable("vpinballx.exe", candidates))
	assert.True(t, MatchesExecutable("VPinballX107_64bit.exe", candidates), "renamed builds match by prefix")
	assert.True(t, MatchesExecutable("VPinballX_GL64.exe", candidates))
	assert.False(t, MatchesExecutable("VPXStarter.exe", candidates), "Popper's helper is not the table")
	assert.False(t, MatchesExecutable(`C:\vPinball\PinUPSystem\PinUpMenu.exe`, candidates))
	assert.False(t, MatchesExecutable("", candidates))

	fx := CandidateExecutables(&Emulator{Name: "Pinball FX3", GamesExt: "pxp"})
	assert.True(t, MatchesExecutable("Pinball FX3.exe", fx))
	assert.True(t, MatchesExecutable("PinballFX.exe", fx))
	assert.False(t, MatchesExecutable("steam.exe", fx))
}
