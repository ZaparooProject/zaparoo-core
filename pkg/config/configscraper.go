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

package config

import (
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
)

// Scraper configures metadata scraper behavior.
type Scraper struct {
	LibretroThumbnails ScraperLibretroThumbnails `toml:"libretro_thumbnails,omitempty"`
	GamelistXML        ScraperGamelistXML        `toml:"gamelist_xml,omitempty"`
}

// DefaultLibretroThumbnailsBaseURL is the libretro thumbnail server RetroArch
// downloads from.
const DefaultLibretroThumbnailsBaseURL = "https://thumbnails.libretro.com"

// ScraperLibretroThumbnails configures the libretro thumbnail scraper.
type ScraperLibretroThumbnails struct {
	// BaseURL replaces the thumbnail server, such as a local mirror.
	BaseURL string `toml:"base_url,omitempty"`
}

// ScraperGamelistXML configures the EmulationStation gamelist.xml scraper.
type ScraperGamelistXML struct {
	CustomPath string `toml:"custom_path,omitempty"`
}

// ScraperGamelistXMLCustomPath returns the optional directory containing
// per-system gamelist bundles at {custom_path}/{system_id}/gamelist.xml.
func (c *Instance) ScraperGamelistXMLCustomPath() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Scraper.GamelistXML.CustomPath
}

// ScraperLibretroThumbnailsBaseURL returns the thumbnail server the libretro
// scraper downloads from, without a trailing slash: the configured base_url
// when it is an http(s) URL, otherwise the public libretro server. Image paths
// are appended to it, so a query or fragment would corrupt every lookup.
func (c *Instance) ScraperLibretroThumbnailsBaseURL() string {
	if c == nil {
		return DefaultLibretroThumbnailsBaseURL
	}
	c.mu.RLock()
	raw := strings.TrimSpace(c.vals.Scraper.LibretroThumbnails.BaseURL)
	c.mu.RUnlock()
	if raw == "" {
		return DefaultLibretroThumbnailsBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		strings.ContainsAny(raw, "?#") {
		log.Warn().Str("base_url", raw).Msg("ignoring invalid scraper.libretro_thumbnails base_url")
		return DefaultLibretroThumbnailsBaseURL
	}
	return strings.TrimRight(raw, "/")
}

// SetScraperLibretroThumbnailsBaseURL sets the thumbnail server the libretro
// scraper downloads from; "" restores the public server.
func (c *Instance) SetScraperLibretroThumbnailsBaseURL(baseURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Scraper.LibretroThumbnails.BaseURL = baseURL
}
