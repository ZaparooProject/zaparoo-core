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

// Command arcadenames builds pkg/database/arcadenames/names.tsv.gz from a
// MAME release's machine list, the mameNNNNlx.zip asset MAME publishes with
// every release (https://github.com/mamedev/mame/releases). It keeps only
// runnable arcade machines and only their set name, description, year and
// manufacturer.
//
// Usage: go run ./scripts/arcadenames mame0289lx.zip pkg/database/arcadenames/names.tsv.gz
package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

type control struct {
	Type string `xml:"type,attr"`
}

// present counts an element without reading it.
type present struct{}

type machine struct {
	Name         string    `xml:"name,attr"`
	IsBIOS       string    `xml:"isbios,attr"`
	IsDevice     string    `xml:"isdevice,attr"`
	IsMechanical string    `xml:"ismechanical,attr"`
	Runnable     string    `xml:"runnable,attr"`
	Description  string    `xml:"description"`
	Year         string    `xml:"year"`
	Manufacturer string    `xml:"manufacturer"`
	ROMs         []present `xml:"rom"`
	Disks        []present `xml:"disk"`
	Displays     []present `xml:"display"`
	SoftLists    []present `xml:"softwarelist"`
	Controls     []control `xml:"input>control"`
}

// arcade keeps machines a set archive can hold and that play like arcade
// games: not BIOS, device or mechanical entries, with ROMs and a display,
// and without the software lists or keyboards of consoles and computers.
func arcade(m *machine) bool {
	if m.IsBIOS == "yes" || m.IsDevice == "yes" || m.IsMechanical == "yes" || m.Runnable == "no" {
		return false
	}
	if len(m.ROMs) == 0 && len(m.Disks) == 0 || len(m.Displays) == 0 || len(m.SoftLists) > 0 {
		return false
	}
	for _, c := range m.Controls {
		if c.Type == "keyboard" || c.Type == "keypad" {
			return false
		}
	}
	return true
}

func open(path string) (io.ReadCloser, error) {
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		f, err := os.Open(path) //nolint:gosec // developer-supplied input path
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		return f, nil
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	for _, file := range archive.File {
		if strings.HasSuffix(file.Name, ".xml") {
			r, openErr := file.Open()
			if openErr != nil {
				_ = archive.Close()
				return nil, fmt.Errorf("open %s in %s: %w", file.Name, path, openErr)
			}
			return struct {
				io.Reader
				io.Closer
			}{r, archive}, nil
		}
	}
	_ = archive.Close()
	return nil, fmt.Errorf("%s holds no .xml machine list", path)
}

func clean(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\t", " ", "\n", " ").Replace(s)), " ")
}

func run(in, out string) error {
	src, err := open(in)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	version := "unknown"
	decoder := xml.NewDecoder(src)
	decoder.Strict = false
	var rows []string
	for {
		token, tokenErr := decoder.Token()
		if errors.Is(tokenErr, io.EOF) {
			break
		}
		if tokenErr != nil {
			return fmt.Errorf("parse %s: %w", in, tokenErr)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local == "mame" {
			for _, attr := range start.Attr {
				if attr.Name.Local == "build" {
					version, _, _ = strings.Cut(attr.Value, " ")
				}
			}
			continue
		}
		if start.Name.Local != "machine" {
			continue
		}
		var m machine
		if decodeErr := decoder.DecodeElement(&m, &start); decodeErr != nil {
			return fmt.Errorf("parse machine: %w", decodeErr)
		}
		if !arcade(&m) || m.Name == "" || clean(m.Description) == "" {
			continue
		}
		rows = append(rows, strings.Join([]string{
			m.Name, clean(m.Description), clean(m.Year), clean(m.Manufacturer),
		}, "\t"))
	}
	slices.Sort(rows)
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	if _, err := fmt.Fprintf(zw, "# MAME %s\n%s\n", version, strings.Join(rows, "\n")); err != nil {
		return fmt.Errorf("write catalog: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil { //nolint:gosec // checked-in data file
		return fmt.Errorf("write %s: %w", out, err)
	}
	//nolint:forbidigo // CLI output
	_, _ = fmt.Printf("wrote %d arcade sets from MAME %s to %s\n", len(rows), version, out)
	return nil
}

func main() {
	if len(os.Args) != 3 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: arcadenames <mameNNNNlx.zip|mame.xml> <names.tsv.gz>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
