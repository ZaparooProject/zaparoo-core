//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package launchers

import "testing"

func FuzzParseMoonlightTarget(f *testing.F) {
	f.Add([]byte(`{"host":"gaming-pc","app":"Game"}`))
	f.Add([]byte("gaming-pc\nGame\n"))
	f.Add([]byte("--help\nGame\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxMoonlightTargetSize {
			t.Skip()
		}
		target, err := parseMoonlightTargetData(data)
		if err == nil && (!validApplicationField(target.Host, maxMoonlightFieldLength) ||
			!validApplicationField(target.App, maxMoonlightFieldLength)) {
			t.Fatal("accepted invalid Moonlight target")
		}
	})
}

func FuzzParseBottlesMetadata(f *testing.F) {
	f.Add([]byte(`["Gaming"]`), []byte(`[{"id":"game-1","name":"Game"}]`))
	f.Add([]byte(`{"Gaming":{}}`), []byte(`[]`))
	f.Add([]byte(`not json`), []byte(`not json`))

	f.Fuzz(func(t *testing.T, bottlesData, programsData []byte) {
		if len(bottlesData) > maxBottlesOutputSize || len(programsData) > maxBottlesOutputSize {
			t.Skip()
		}
		bottles, err := parseBottlesList(bottlesData)
		if err == nil && len(bottles) > maxBottles {
			t.Fatal("accepted too many bottles")
		}
		programs, err := parseBottlesPrograms("Gaming", programsData)
		if err == nil && len(programs) > maxBottlesPrograms {
			t.Fatal("accepted too many programs")
		}
	})
}
