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

// Package romm reads a self-hosted RomM server: its platforms and ROMs become
// indexed media, and a ROM is downloaded onto the device when launched.
package romm

// Platform is one entry of GET /api/platforms.
//
//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
type Platform struct {
	Slug      string `json:"slug"`
	FSSlug    string `json:"fs_slug"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updated_at"`
	ID        int    `json:"id"`
	ROMCount  int    `json:"rom_count"`
}

// RomFile is one file belonging to a ROM.
//
//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
type RomFile struct {
	FileName      string `json:"file_name"`
	FilePath      string `json:"file_path"`
	ID            int    `json:"id"`
	FileSizeBytes int64  `json:"file_size_bytes"`
}

// Metadatum is RomM's merged metadata for a ROM, drawn from whichever
// metadata providers the server has enabled.
//
//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
type Metadatum struct {
	AverageRating    *float64 `json:"average_rating"`
	FirstReleaseDate *int64   `json:"first_release_date"`
	PlayerCount      string   `json:"player_count"`
	Genres           []string `json:"genres"`
	Franchises       []string `json:"franchises"`
	Companies        []string `json:"companies"`
	Publishers       []string `json:"publishers"`
	Developers       []string `json:"developers"`
	GameModes        []string `json:"game_modes"`
	AgeRatings       []string `json:"age_ratings"`
}

// Rom is one ROM as returned by the ROM list and detail endpoints. Only the
// fields Core uses are decoded.
//
//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
type Rom struct {
	Metadatum         Metadatum `json:"metadatum"`
	HasFileOnDisk     *bool     `json:"has_file_on_disk"`
	PlatformSlug      string    `json:"platform_slug"`
	PlatformFSSlug    string    `json:"platform_fs_slug"`
	FSName            string    `json:"fs_name"`
	FSNameNoTags      string    `json:"fs_name_no_tags"`
	FSNameNoExt       string    `json:"fs_name_no_ext"`
	FSExtension       string    `json:"fs_extension"`
	Name              string    `json:"name"`
	Summary           string    `json:"summary"`
	PathCoverLarge    string    `json:"path_cover_large"`
	PathCoverSmall    string    `json:"path_cover_small"`
	UpdatedAt         string    `json:"updated_at"`
	Regions           []string  `json:"regions"`
	Languages         []string  `json:"languages"`
	MergedScreenshots []string  `json:"merged_screenshots"`
	Files             []RomFile `json:"files"`
	ID                int       `json:"id"`
	PlatformID        int       `json:"platform_id"`
	FSSizeBytes       int64     `json:"fs_size_bytes"`
	HasMultipleFiles  bool      `json:"has_multiple_files"`
	MissingFromFS     bool      `json:"missing_from_fs"`
}

// Downloadable reports whether the server has the ROM's files to serve.
func (r *Rom) Downloadable() bool {
	if r.MissingFromFS {
		return false
	}
	return r.HasFileOnDisk == nil || *r.HasFileOnDisk
}

type romPage struct {
	Items  []Rom `json:"items"`
	Total  int   `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}
