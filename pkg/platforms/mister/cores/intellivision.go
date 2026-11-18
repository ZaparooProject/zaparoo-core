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

package cores

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// The Intellivision core's OSD "Format" option and its two values. In CFG
// mapping mode the core parses a jzIntv .cfg loaded into the second file slot,
// which maps the ROM and sets the ECS and JLP options from its [vars].
const (
	intvFormatOption = "Format"
	intvFormatRaw    = "Raw & Intellicart"
	intvFormatCFG    = "CFG mapping"
	intvCFGIndex     = 2
)

func hookIntellivision(_ *config.Instance, system *Core, mediaPath string) (string, error) {
	return hookIntellivisionWithFS(afero.NewOsFs(), system, mediaPath)
}

// hookIntellivisionWithFS loads a raw ROM together with the .cfg file of the
// same name beside it, so each game gets the memory map, ECS and JLP settings
// it declares instead of whatever the core's OSD was last left on. Media
// without one is loaded in raw mode, so a saved CFG mapping setting cannot
// leave it waiting for a .cfg that never arrives.
func hookIntellivisionWithFS(fs afero.Fs, system *Core, mediaPath string) (string, error) {
	if mediaPath == "" {
		return "", nil
	}
	def, err := PathToMGLDef(system, mediaPath)
	if err != nil {
		return "", err
	}

	format := intvFormatRaw
	cfgPath := ""
	ext := strings.ToLower(filepath.Ext(mediaPath))
	if ext == ".bin" || ext == ".int" {
		cfgPath = intvSiblingCFG(fs, mediaPath)
		if cfgPath != "" {
			format = intvFormatCFG
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\t<option name=\"%s\" value=\"%s\"/>\n", xmlAttr(intvFormatOption), xmlAttr(format))
	fmt.Fprintf(&b, "\t<file delay=\"%d\" type=\"%s\" index=\"%d\" path=\"../../../../..%s\"/>\n",
		def.Delay, xmlAttr(def.Method), def.Index, xmlAttr(mediaPath))
	if cfgPath != "" {
		log.Debug().Str("path", mediaPath).Str("cfg", cfgPath).Msg("loading Intellivision ROM with CFG mapping")
		fmt.Fprintf(&b, "\t<file delay=\"%d\" type=\"%s\" index=\"%d\" path=\"../../../../..%s\"/>\n",
			def.Delay, xmlAttr(def.Method), intvCFGIndex, xmlAttr(cfgPath))
	}
	return b.String(), nil
}

// intvSiblingCFG returns the .cfg file sharing the ROM's name in the same
// folder, inside a zip when the ROM is in one, or "" when there is none. An
// empty .cfg is ignored because it holds no mapping for the core to apply.
func intvSiblingCFG(fs afero.Fs, mediaPath string) string {
	stem := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	if zipPath, inner, ok := splitZipPath(mediaPath); ok {
		innerStem := strings.TrimSuffix(inner, path.Ext(inner))
		entry := zipSiblingEntry(fs, zipPath, innerStem)
		if entry == "" {
			return ""
		}
		return zipPath + "/" + entry
	}
	for _, ext := range []string{".cfg", ".CFG", ".Cfg"} {
		candidate := stem + ext
		if info, err := fs.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return candidate
		}
	}
	return ""
}

// splitZipPath splits a path to a file inside a zip into the zip's path and
// the entry name within it. Like MiSTer Main, it takes the first ".zip/" in
// the path as the archive, so folders named *.zip inside it stay part of the
// entry name.
func splitZipPath(mediaPath string) (zipPath, inner string, ok bool) {
	slashed := filepath.ToSlash(mediaPath)
	const marker = ".zip/"
	for i := 0; i+len(marker) <= len(slashed); i++ {
		if strings.EqualFold(slashed[i:i+len(marker)], marker) {
			return slashed[:i+len(".zip")], slashed[i+len(marker):], true
		}
	}
	return "", "", false
}

// zipSiblingEntry returns the name of the .cfg entry for innerStem in a zip,
// matching the extension case-insensitively, or "" when there is none.
func zipSiblingEntry(fs afero.Fs, zipPath, innerStem string) string {
	file, err := fs.Open(zipPath)
	if err != nil {
		return ""
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("path", zipPath).Msg("failed to close zip")
		}
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		log.Debug().Err(err).Str("path", zipPath).Msg("failed to read zip for Intellivision CFG")
		return ""
	}
	for _, f := range reader.File {
		name := f.Name
		if f.FileInfo().IsDir() || f.UncompressedSize64 == 0 || !strings.EqualFold(path.Ext(name), ".cfg") {
			continue
		}
		if strings.TrimSuffix(name, path.Ext(name)) == innerStem {
			return name
		}
	}
	return ""
}

func xmlAttr(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value)) //nolint:errcheck // strings.Builder writes cannot fail.
	return b.String()
}
