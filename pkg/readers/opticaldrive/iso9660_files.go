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

package opticaldrive

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf16"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/sidecar"
	"github.com/rs/zerolog/log"
)

const (
	iso9660DescriptorTypeSupplementary = 0x02
	iso9660EscapeSequencesOffset       = 88
	// iso9660MaxDirectoryBytes bounds one directory read while walking a
	// path, and iso9660MaxPathDepth the components walked.
	iso9660MaxDirectoryBytes = 1 << 20
	iso9660MaxPathDepth      = 8
)

var (
	errISO9660NotFound  = errors.New("not found on disc")
	errISO9660NotFile   = errors.New("not a regular file")
	errISO9660TooLarge  = errors.New("file too large")
	errISO9660BadRecord = errors.New("iso9660 directory record crosses sector boundary")
)

// discSidecar is zaparoo.json read off a disc, with the cover it names.
type discSidecar struct {
	Fields   sidecar.Fields
	ImageExt string
	Image    []byte
}

// iso9660Tree is one directory hierarchy on a disc: the primary one with
// 8.3 upper-case names, or the Joliet one with long Unicode names.
type iso9660Tree struct {
	root   iso9660DirectoryRecord
	joliet bool
}

// isJolietDescriptor reports whether a supplementary volume descriptor
// declares one of the three Joliet UCS-2 levels.
func isJolietDescriptor(desc []byte) bool {
	esc := desc[iso9660EscapeSequencesOffset : iso9660EscapeSequencesOffset+3]
	return esc[0] == '%' && esc[1] == '/' && (esc[2] == '@' || esc[2] == 'C' || esc[2] == 'E')
}

// iso9660Name decodes a file identifier, dropping the ";1" version suffix
// and the trailing dot of extensionless 8.3 names.
func iso9660Name(identifier []byte, joliet bool) string {
	name := string(identifier)
	if joliet {
		units := make([]uint16, 0, len(identifier)/2)
		for i := 0; i+1 < len(identifier); i += 2 {
			units = append(units, binary.BigEndian.Uint16(identifier[i:]))
		}
		name = string(utf16.Decode(units))
	}
	if i := strings.LastIndexByte(name, ';'); i >= 0 {
		name = name[:i]
	}
	return strings.TrimSuffix(name, ".")
}

// walkISO9660Directory calls visit for each record in dir, skipping the
// self and parent entries, until visit returns false.
func walkISO9660Directory(
	ctx context.Context,
	r contextReaderAt,
	dir iso9660DirectoryRecord,
	visit func(iso9660DirectoryRecord) bool,
) error {
	if dir.Flags&iso9660DirectoryFlag == 0 {
		return errors.New("iso9660 record is not a directory")
	}
	if dir.DataLength > iso9660MaxDirectoryBytes {
		return fmt.Errorf("iso9660 directory: %w", errISO9660TooLarge)
	}
	start := iso9660RecordDataOffset(dir)
	remaining := int64(dir.DataLength)
	sector := make([]byte, iso9660SectorSize)
	for offset := int64(0); remaining > 0; offset += iso9660SectorSize {
		size := min(remaining, int64(len(sector)))
		n, err := r.ReadAtContext(ctx, sector[:size], start+offset)
		if err != nil {
			return fmt.Errorf("read iso9660 directory: %w", err)
		}
		if int64(n) < size {
			return io.ErrUnexpectedEOF
		}
		for pos := 0; pos < n; {
			length := int(sector[pos])
			if length == 0 {
				break
			}
			if pos+length > n {
				return errISO9660BadRecord
			}
			record, err := parseISO9660DirectoryRecord(sector[pos : pos+length])
			if err != nil {
				return fmt.Errorf("parse iso9660 directory record: %w", err)
			}
			pos += length
			if len(record.Identifier) == 1 && record.Identifier[0] <= 1 {
				continue
			}
			if !visit(record) {
				return nil
			}
		}
		remaining -= size
	}
	return nil
}

// lookupISO9660 walks parts from the tree root, comparing names without
// regard to case.
func lookupISO9660(
	ctx context.Context,
	r contextReaderAt,
	tree iso9660Tree,
	parts []string,
) (iso9660DirectoryRecord, error) {
	if len(parts) == 0 || len(parts) > iso9660MaxPathDepth {
		return iso9660DirectoryRecord{}, errISO9660NotFound
	}
	current := tree.root
	for i, part := range parts {
		var found *iso9660DirectoryRecord
		err := walkISO9660Directory(ctx, r, current, func(rec iso9660DirectoryRecord) bool {
			if strings.EqualFold(iso9660Name(rec.Identifier, tree.joliet), part) {
				found = &rec
				return false
			}
			return true
		})
		if err != nil {
			return iso9660DirectoryRecord{}, err
		}
		if found == nil {
			return iso9660DirectoryRecord{}, errISO9660NotFound
		}
		isDir := found.Flags&iso9660DirectoryFlag != 0
		last := i == len(parts)-1
		if last == isDir {
			if last {
				return iso9660DirectoryRecord{}, errISO9660NotFile
			}
			return iso9660DirectoryRecord{}, errISO9660NotFound
		}
		current = *found
	}
	return current, nil
}

// readISO9660File reads a whole single-extent file no larger than maxBytes.
func readISO9660File(
	ctx context.Context,
	r contextReaderAt,
	record iso9660DirectoryRecord,
	maxBytes int64,
) ([]byte, error) {
	if record.Flags&(iso9660DirectoryFlag|iso9660MultiExtentFlag) != 0 {
		return nil, errISO9660NotFile
	}
	if int64(record.DataLength) > maxBytes {
		return nil, errISO9660TooLarge
	}
	data := make([]byte, int(record.DataLength))
	if len(data) == 0 {
		return data, nil
	}
	n, err := r.ReadAtContext(ctx, data, iso9660RecordDataOffset(record))
	if err != nil {
		return nil, fmt.Errorf("read iso9660 file: %w", err)
	}
	if n < len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}

// readISO9660Sidecar reads zaparoo.json from the disc root and the cover it
// names. Problems are logged; they never stop the disc from being scanned.
func readISO9660Sidecar(ctx context.Context, r contextReaderAt, tree iso9660Tree) *discSidecar {
	record, err := lookupISO9660(ctx, r, tree, []string{sidecar.FileName})
	if errors.Is(err, errISO9660NotFound) {
		return nil
	}
	if err != nil {
		log.Warn().Err(err).Msg("ignoring disc zaparoo.json")
		return nil
	}
	data, err := readISO9660File(ctx, r, record, sidecar.MaxBytes)
	if err != nil {
		log.Warn().Err(err).Msg("ignoring disc zaparoo.json")
		return nil
	}
	fields, err := sidecar.Parse(data)
	if err != nil {
		log.Warn().Err(err).Msg("ignoring disc zaparoo.json")
		return nil
	}
	out := &discSidecar{Fields: fields}
	out.Image, out.ImageExt = readISO9660SidecarImage(ctx, r, tree, fields.Image)
	if out.Image == nil {
		out.Fields.Image = ""
	}
	if out.Fields == (sidecar.Fields{}) {
		return nil
	}
	return out
}

func readISO9660SidecarImage(
	ctx context.Context,
	r contextReaderAt,
	tree iso9660Tree,
	rel string,
) (data []byte, ext string) {
	if rel == "" {
		return nil, ""
	}
	if !sidecar.ImageExtension(rel) {
		log.Warn().Str("image", rel).Msg("ignoring disc zaparoo.json image: not a .png, .jpg or .jpeg file")
		return nil, ""
	}
	parts, ok := sidecar.ImagePathParts(rel)
	if !ok {
		log.Warn().Str("image", rel).Msg("ignoring disc zaparoo.json image: not a path on the disc")
		return nil, ""
	}
	record, err := lookupISO9660(ctx, r, tree, parts)
	if err != nil {
		log.Warn().Err(err).Str("image", rel).Msg("ignoring disc zaparoo.json image")
		return nil, ""
	}
	data, err = readISO9660File(ctx, r, record, imagefiles.MaxBytes)
	if err != nil {
		log.Warn().Err(err).Str("image", rel).Msg("ignoring disc zaparoo.json image")
		return nil, ""
	}
	if len(data) == 0 {
		return nil, ""
	}
	return data, strings.ToLower(path.Ext(rel))
}
