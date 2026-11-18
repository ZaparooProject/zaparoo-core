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
	parentElement  = "parent"
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

// Identity is what a descriptor says about which romset it is.
type Identity struct {
	// SetName is the descriptor's own MAME set, lower-cased.
	SetName string
	// Parent is the set this one is a clone of, lower-cased. Empty for a
	// parent set and for a descriptor that does not say.
	Parent string
}

// ReadSetName returns the descriptor's set name alone. See ReadIdentity.
func ReadSetName(fs afero.Fs, filename string) string {
	return ReadIdentity(fs, filename).SetName
}

// ReadIdentity requires one complete, bounded descriptor header. Unlike optional
// artwork lookup, this identity can select a different title's write target, so
// malformed documents and repeated setname elements must not select a row. A
// zero Identity means the descriptor is not an identity source.
func ReadIdentity(fs afero.Fs, filename string) Identity {
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
		return Identity{}
	}
	file, err := fs.Open(filename)
	if err != nil {
		return Identity{}
	}
	defer func() { _ = file.Close() }()
	decoder := xml.NewDecoder(&io.LimitedReader{R: file, N: MaxHeaderBytes})
	decoder.CharsetReader = charset.NewReaderLabel
	return NormalizeIdentity(decodeIdentity(decoder))
}

// NormalizeIdentity validates the raw text of a descriptor's set and parent
// elements. A set name that is not a plain romset name is no identity at all.
// A parent that is not one, or that only repeats the set, is dropped and the
// set name kept: the parent is a hint about other rows, not this row's key.
func NormalizeIdentity(setName, parent string) Identity {
	setName = strings.TrimSpace(setName)
	if setName == "" || SetStem(setName) != setName {
		return Identity{}
	}
	identity := Identity{SetName: strings.ToLower(setName)}
	parent = strings.TrimSpace(parent)
	if parent != "" && SetStem(parent) == parent && !strings.EqualFold(parent, setName) {
		identity.Parent = strings.ToLower(parent)
	}
	return identity
}

// decodeIdentity returns the descriptor's single <setname> and, when it names
// one, its <parent>. Every element ahead of the ROM payload is examined, so a
// repeated, nested or absent set name still yields nothing; parsing then stops
// rather than reading megabytes of base64 that can carry no identity. A header
// that is malformed, outruns the read bound, names another root, or is trailed
// by a second document is not an identity source. A repeated parent is
// ambiguous and reads as none.
func decodeIdentity(decoder *xml.Decoder) (setName, parent string) {
	var setNames, parents, depth int
	var rootClosed bool
	result := func(complete bool) (string, string) {
		if !complete || setNames != 1 {
			return "", ""
		}
		if parents != 1 {
			return setName, ""
		}
		return setName, parent
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return result(rootClosed)
		}
		if err != nil {
			return "", ""
		}
		switch data := token.(type) {
		case xml.StartElement:
			if rootClosed {
				return "", ""
			}
			depth++
			switch {
			case depth == 1:
				if data.Name.Local != rootElement {
					return "", ""
				}
			case depth == 2 && data.Name.Local == payloadElement:
				return result(true)
			case depth == 2 && data.Name.Local == setNameElement:
				setNames++
				if err := decoder.DecodeElement(&setName, &data); err != nil {
					return "", ""
				}
				setName = strings.TrimSpace(setName)
				depth--
			case depth == 2 && data.Name.Local == parentElement:
				parents++
				if err := decoder.DecodeElement(&parent, &data); err != nil {
					return "", ""
				}
				depth--
			default:
				if err := decoder.Skip(); err != nil {
					return "", ""
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
				return "", ""
			}
		case xml.Comment, xml.ProcInst:
		case xml.Directive:
			// Tolerated only ahead of the document it declares.
			if rootClosed {
				return "", ""
			}
		}
	}
}
