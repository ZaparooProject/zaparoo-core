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

package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTitleKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		systemID string
		slug     string
		want     string
	}{
		{
			name:     "basic key",
			systemID: "nes",
			slug:     "supermariobros",
			want:     "nes:supermariobros",
		},
		{
			name:     "empty slug",
			systemID: "snes",
			slug:     "",
			want:     "snes:",
		},
		{
			name:     "empty systemID",
			systemID: "",
			slug:     "zelda",
			want:     ":zelda",
		},
		{
			name:     "both empty",
			systemID: "",
			slug:     "",
			want:     ":",
		},
		{
			name:     "slug with special characters",
			systemID: "pc",
			slug:     "half-life-2",
			want:     "pc:half-life-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TitleKey(tt.systemID, tt.slug)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMediaKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		systemID string
		path     string
		want     string
	}{
		{
			name:     "basic key",
			systemID: "nes",
			path:     "/games/mario.nes",
			want:     "nes:/games/mario.nes",
		},
		{
			name:     "empty path",
			systemID: "snes",
			path:     "",
			want:     "snes:",
		},
		{
			name:     "empty systemID",
			systemID: "",
			path:     "/roms/zelda.sfc",
			want:     ":/roms/zelda.sfc",
		},
		{
			name:     "path with spaces",
			systemID: "pc",
			path:     "/games/My Game/game.exe",
			want:     "pc:/games/My Game/game.exe",
		},
		{
			name:     "windows-style path",
			systemID: "pc",
			path:     "C:/Games/Doom/doom.exe",
			want:     "pc:C:/Games/Doom/doom.exe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MediaKey(tt.systemID, tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTagKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tagType  string
		tagValue string
		want     string
	}{
		{
			name:     "basic key",
			tagType:  "region",
			tagValue: "usa",
			want:     "region:usa",
		},
		{
			name:     "extension tag",
			tagType:  "extension",
			tagValue: "nes",
			want:     "extension:nes",
		},
		{
			name:     "unknown tag",
			tagType:  "unknown",
			tagValue: "unknown",
			want:     "unknown:unknown",
		},
		{
			name:     "empty value",
			tagType:  "lang",
			tagValue: "",
			want:     "lang:",
		},
		{
			name:     "empty type",
			tagType:  "",
			tagValue: "en",
			want:     ":en",
		},
		{
			name:     "revision tag",
			tagType:  "rev",
			tagValue: "1-0",
			want:     "rev:1-0",
		},
		{
			name:     "disc tag",
			tagType:  "disc",
			tagValue: "1",
			want:     "disc:1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TagKey(tt.tagType, tt.tagValue)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildTitleZapScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		systemID string
		gameName string
		want     string
		tags     []TagInfo
	}{
		{
			name:     "no tags",
			systemID: "SNES",
			gameName: "Super Mario World",
			tags:     []TagInfo{},
			want:     "@SNES/Super Mario World",
		},
		{
			name:     "year tag only",
			systemID: "SNES",
			gameName: "Game",
			tags:     []TagInfo{{Tag: "1994", Type: "year"}},
			want:     "@SNES/Game (year:1994)",
		},
		{
			name:     "year tag with wrong length skipped",
			systemID: "SNES",
			gameName: "Game",
			tags:     []TagInfo{{Tag: "94", Type: "year"}},
			want:     "@SNES/Game",
		},
		{
			name:     "year tag with 5 chars skipped",
			systemID: "SNES",
			gameName: "Game",
			tags:     []TagInfo{{Tag: "19941", Type: "year"}},
			want:     "@SNES/Game",
		},
		{
			name:     "players tag",
			systemID: "SNES",
			gameName: "Game",
			tags:     []TagInfo{{Tag: "2", Type: "players"}},
			want:     "@SNES/Game (players:2)",
		},
		{
			name:     "publisher tag uses generic format",
			systemID: "SNES",
			gameName: "Donkey Kong Country",
			tags:     []TagInfo{{Tag: "nintendo", Type: "publisher"}},
			want:     "@SNES/Donkey Kong Country (publisher:nintendo)",
		},
		{
			name:     "developer tag uses generic format",
			systemID: "SNES",
			gameName: "Donkey Kong Country",
			tags:     []TagInfo{{Tag: "rare", Type: "developer"}},
			want:     "@SNES/Donkey Kong Country (developer:rare)",
		},
		{
			name:     "credit tag uses generic format",
			systemID: "SNES",
			gameName: "Game",
			tags:     []TagInfo{{Tag: "capcom", Type: "credit"}},
			want:     "@SNES/Game (credit:capcom)",
		},
		{
			name:     "year and publisher combined",
			systemID: "SNES",
			gameName: "Game",
			tags:     []TagInfo{{Tag: "1994", Type: "year"}, {Tag: "nintendo", Type: "publisher"}},
			want:     "@SNES/Game (year:1994) (publisher:nintendo)",
		},
		{
			name:     "edition tag uses generic format",
			systemID: "SNES",
			gameName: "Game",
			tags:     []TagInfo{{Tag: "special", Type: "edition"}},
			want:     "@SNES/Game (edition:special)",
		},
		{
			name:     "release tag uses generic format",
			systemID: "SNES",
			gameName: "Game",
			tags:     []TagInfo{{Tag: "homebrew", Type: "release"}},
			want:     "@SNES/Game (release:homebrew)",
		},
		{
			name:     "rev tag uses generic format",
			systemID: "SNES",
			gameName: "Game",
			tags:     []TagInfo{{Tag: "1", Type: "rev"}},
			want:     "@SNES/Game (rev:1)",
		},
		{
			name:     "empty tag is skipped",
			systemID: "SNES",
			gameName: "Game",
			tags:     []TagInfo{{Tag: "", Type: "publisher"}, {Tag: "nintendo", Type: "publisher"}},
			want:     "@SNES/Game (publisher:nintendo)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildTitleZapScript(tt.systemID, tt.gameName, tt.tags)
			assert.Equal(t, tt.want, got)
		})
	}
}
