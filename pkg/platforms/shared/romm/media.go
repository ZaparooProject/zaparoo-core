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

package romm

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// ErrInvalidPath is returned for a romm:// path without a usable ROM ID.
var ErrInvalidPath = errors.New("invalid romm path")

// RomPath is the virtual path a ROM is indexed under.
func RomPath(id int, name string) string {
	return virtualpath.CreateVirtualPath(shared.SchemeRomM, strconv.Itoa(id), name)
}

// IsRomPath reports whether path uses the romm:// scheme, valid or not.
func IsRomPath(p string) bool {
	scheme, _, ok := strings.Cut(p, "://")
	return ok && strings.EqualFold(scheme, shared.SchemeRomM)
}

// ParseRomPath returns the ROM ID a romm:// path names.
func ParseRomPath(p string) (int, error) {
	raw, err := virtualpath.ExtractSchemeID(p, shared.SchemeRomM)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidPath, err)
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 || strconv.Itoa(id) != raw {
		return 0, fmt.Errorf("%w: rom id %q", ErrInvalidPath, raw)
	}
	return id, nil
}

// Layout is how a ROM is stored on the device.
type Layout struct {
	// Name is the file a single-file ROM is saved as, or the folder a
	// multi-file ROM is unpacked into.
	Name string
	// Archive marks a multi-file ROM, which RomM serves as one zip.
	Archive bool
}

// LayoutFor works out where a ROM goes on the device. A single-file ROM keeps
// its file's own name: for a ROM nested in a folder on the server, fs_name is
// that folder, not the file. A multi-file ROM is named after the ROM without
// its extension, which is also the name RomM gives a generated .m3u.
func LayoutFor(rom *Rom) (Layout, error) {
	var l Layout
	switch {
	case len(rom.Files) == 1:
		l = Layout{Name: rom.Files[0].FileName}
	case len(rom.Files) > 1, rom.HasMultipleFiles:
		name := rom.FSNameNoExt
		if name == "" {
			name = rom.FSName
		}
		l = Layout{Name: name, Archive: true}
	default:
		l = Layout{Name: rom.FSName}
	}
	l.Name = strings.TrimSpace(l.Name)
	if !safeName(l.Name) {
		return Layout{}, fmt.Errorf("romm rom %d has an unusable file name %q", rom.ID, l.Name)
	}
	return l, nil
}

// safeName reports whether a server-chosen name is a single path segment that
// can be joined onto a directory on any platform.
func safeName(name string) bool {
	if name == "" || name == "." || name == ".." || virtualpath.ContainsControlChar(name) {
		return false
	}
	if strings.ContainsAny(name, `/\:`) {
		return false
	}
	return filepath.Base(name) == name
}

// Title is the name a ROM is indexed under: its file name without extension,
// the same name the file gets once downloaded, so filename tags such as
// region and revision parse the same way they do for local files.
func Title(l Layout) string {
	if l.Archive {
		return l.Name
	}
	if name := strings.TrimSuffix(l.Name, path.Ext(l.Name)); name != "" {
		return name
	}
	return l.Name
}

// LocalNames are the lowercase file names that mean a ROM is already on the
// device: any of its files, and the playlist a multi-file ROM is launched by.
func LocalNames(rom *Rom, l Layout) []string {
	names := make([]string, 0, len(rom.Files)+1)
	for i := range rom.Files {
		if n := path.Base(strings.ReplaceAll(rom.Files[i].FileName, `\`, "/")); n != "" && n != "." {
			names = append(names, strings.ToLower(n))
		}
	}
	if l.Archive {
		names = append(names, strings.ToLower(l.Name+".m3u"))
	} else if len(rom.Files) == 0 {
		names = append(names, strings.ToLower(l.Name))
	}
	return names
}

// extensions are the lowercase extensions a launcher would need to accept
// for the ROM to be playable once downloaded.
func extensions(rom *Rom, l Layout) []string {
	if !l.Archive {
		return []string{strings.ToLower(path.Ext(l.Name))}
	}
	// A generated playlist is not counted: the server only adds one to some
	// downloads, so the game has to be startable from its own files.
	exts := make([]string, 0, len(rom.Files))
	for i := range rom.Files {
		if ext := strings.ToLower(path.Ext(rom.Files[i].FileName)); ext != "" {
			exts = append(exts, ext)
		}
	}
	return exts
}
