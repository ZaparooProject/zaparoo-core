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

// Package mra reads the MAME set name out of a MiSTer arcade descriptor.
//
// An MRA filename is a display title, so the set name inside the descriptor is
// the only stable identity an arcade row has. Scrapers keying external arcade
// metadata to indexed media share this reader so they agree on which
// descriptors are identity sources and which are not.
package mra

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path"
	"strings"

	"github.com/spf13/afero"
	"golang.org/x/net/html/charset"
)

// MaxHeaderBytes bounds the descriptor header a set name may be read from. It
// is not a file size limit: MiSTer MRAs embed their ROM payload as base64
// <part> data and run to tens of megabytes, and refusing those would leave real
// arcade games with no identity at all.
const MaxHeaderBytes = 256 * 1024

// Ext is the MiSTer arcade descriptor extension.
const Ext = ".mra"

const (
	rootElement    = "misterromdescription"
	setNameElement = "setname"
	// payloadElement opens the embedded ROM data. Everything that identifies
	// the set is written before it.
	payloadElement = "rom"
)

// SetStem treats ZIP paths as identities, never as files to open. Source
// bundles may contain absolute paths from a different machine, including Windows.
func SetStem(sourcePath string) string {
	if strings.ContainsAny(sourcePath, "\x00\r\n") || strings.Contains(sourcePath, "://") {
		return ""
	}
	base := path.Base(strings.ReplaceAll(strings.TrimSpace(sourcePath), `\`, "/"))
	ext := path.Ext(base)
	switch strings.ToLower(ext) {
	case ".zip", ".7z":
		base = strings.TrimSuffix(base, ext)
	case "":
	default:
		return ""
	}
	if base == "" || len(base) > 128 {
		return ""
	}
	for _, c := range base {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return ""
		}
	}
	return base
}

// ReadSetName requires one complete, bounded descriptor header. Unlike optional
// artwork lookup, this identity can select a different title's write target, so
// malformed documents and repeated setname elements must not select a row. The
// returned set name is lower-cased; an empty string means no identity.
func ReadSetName(fs afero.Fs, filename string) string {
	// Lstat, not Stat: Arcade Organizer aliases the same descriptor under its
	// category folders, and an alias that reached the index would read as a
	// second row for one set and block it as ambiguous. Skipping links keeps
	// the canonical `_Arcade` row as the only identity source.
	var info os.FileInfo
	var err error
	if lstater, ok := fs.(afero.Lstater); ok {
		info, _, err = lstater.LstatIfPossible(filename)
	} else {
		info, err = fs.Stat(filename)
	}
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	file, err := fs.Open(filename)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	decoder := xml.NewDecoder(&io.LimitedReader{R: file, N: MaxHeaderBytes})
	decoder.CharsetReader = charset.NewReaderLabel
	setName, ok := decodeSetName(decoder)
	if !ok || SetStem(setName) != setName {
		return ""
	}
	return strings.ToLower(setName)
}

// decodeSetName returns the descriptor's single <setname>. Every element ahead
// of the ROM payload is examined, so a repeated, nested or absent set name still
// yields nothing; parsing then stops rather than reading megabytes of base64
// that can carry no identity. A header that is malformed, outruns the read
// bound, names another root, or is trailed by a second document is not an
// identity source.
func decodeSetName(decoder *xml.Decoder) (setName string, ok bool) {
	var setNames, depth int
	var rootClosed bool
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return setName, rootClosed && setNames == 1
		}
		if err != nil {
			return "", false
		}
		switch data := token.(type) {
		case xml.StartElement:
			if rootClosed {
				return "", false
			}
			depth++
			switch {
			case depth == 1:
				if data.Name.Local != rootElement {
					return "", false
				}
			case depth == 2 && data.Name.Local == payloadElement:
				return setName, setNames == 1
			case depth == 2 && data.Name.Local == setNameElement:
				setNames++
				if err := decoder.DecodeElement(&setName, &data); err != nil {
					return "", false
				}
				setName = strings.TrimSpace(setName)
				depth--
			default:
				if err := decoder.Skip(); err != nil {
					return "", false
				}
				depth--
			}
		case xml.EndElement:
			depth--
			if depth == 0 {
				rootClosed = true
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(data)) != "" {
				return "", false
			}
		case xml.Comment, xml.ProcInst:
		case xml.Directive:
			// Tolerated only ahead of the document it declares.
			if rootClosed {
				return "", false
			}
		}
	}
}
