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
	"strings"

	"github.com/rs/zerolog/log"
)

const (
	iso9660SectorSize       = 0x800
	iso9660SuperblockOffset = 0x8000
	iso9660MaxDescriptors   = 16
	iso9660DescriptorSize   = iso9660SectorSize

	iso9660DescriptorTypePrimary = 0x01
	iso9660DescriptorTypeEnd     = 0xff

	iso9660VolumeIDOffset            = 40
	iso9660VolumeIDSize              = 32
	iso9660RootDirectoryRecordOffset = 156
	iso9660CreatedOffset             = 813
	iso9660ModifiedOffset            = 830
	iso9660DateSize                  = 17

	iso9660DirectoryRecordMinSize     = 34
	iso9660ExtentLocationOffset       = 2
	iso9660DataLengthOffset           = 10
	iso9660FileFlagsOffset            = 25
	iso9660FileIdentifierLengthOffset = 32
	iso9660FileIdentifierOffset       = 33
	iso9660DirectoryFlag              = 0x02
	iso9660MultiExtentFlag            = 0x80
	iso9660MaxTokenFileSize           = 1 * 1024 * 1024
	iso9660TokenFileName              = "zaparoo.txt" //nolint:gosec // Filename, not a credential.
)

type discTokenFileState uint8

const (
	discTokenFileUnknown discTokenFileState = iota
	discTokenFileAbsent
	discTokenFilePresent
)

var errISO9660IdentityNotFound = errors.New("iso9660 identity not found")

type discIdentity struct {
	UUID      string
	Label     string
	TokenFile discTokenFile
}

type discTokenFile struct {
	Data  []byte
	State discTokenFileState
}

type iso9660DirectoryRecord struct {
	Identifier              []byte
	ExtentLocation          uint32
	DataLength              uint32
	ExtendedAttributeLength uint8
	Flags                   uint8
}

type contextReaderAt interface {
	ReadAtContext(context.Context, []byte, int64) (int, error)
}

func readISO9660IdentityContext(ctx context.Context, r contextReaderAt) (discIdentity, bool, error) {
	buf := make([]byte, iso9660DescriptorSize)
	for i := range iso9660MaxDescriptors {
		offset := int64(iso9660SuperblockOffset + i*iso9660SectorSize)
		n, err := r.ReadAtContext(ctx, buf, offset)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return discIdentity{}, false, nil
			}
			return discIdentity{}, false, fmt.Errorf("read iso9660 descriptor: %w", err)
		}
		if n < len(buf) {
			return discIdentity{}, false, nil
		}

		if string(buf[1:6]) != "CD001" {
			continue
		}
		if buf[0] == iso9660DescriptorTypeEnd {
			break
		}
		if buf[0] != iso9660DescriptorTypePrimary {
			continue
		}

		label := trimISO9660String(buf[iso9660VolumeIDOffset : iso9660VolumeIDOffset+iso9660VolumeIDSize])
		uuid := iso9660DateUUID(buf[iso9660ModifiedOffset : iso9660ModifiedOffset+iso9660DateSize])
		if uuid == "" {
			uuid = iso9660DateUUID(buf[iso9660CreatedOffset : iso9660CreatedOffset+iso9660DateSize])
		}

		tokenFile, tokenErr := readISO9660TokenFileContext(ctx, r, buf)
		if tokenErr != nil {
			log.Debug().
				Err(tokenErr).
				Str("uuid", uuid).
				Str("label", label).
				Msg("failed to read ISO9660 token file")
			tokenFile = discTokenFile{State: discTokenFileUnknown}
		}
		return discIdentity{UUID: uuid, Label: label, TokenFile: tokenFile}, true, nil
	}
	return discIdentity{}, false, nil
}

func readISO9660TokenFileContext(
	ctx context.Context,
	r contextReaderAt,
	primaryVolumeDescriptor []byte,
) (discTokenFile, error) {
	rootRecord, err := parseISO9660DirectoryRecord(primaryVolumeDescriptor[iso9660RootDirectoryRecordOffset:])
	if err != nil {
		return discTokenFile{}, fmt.Errorf("parse iso9660 root directory record: %w", err)
	}
	if rootRecord.Flags&iso9660DirectoryFlag == 0 {
		return discTokenFile{}, errors.New("iso9660 root directory record is not a directory")
	}

	rootOffset := iso9660RecordDataOffset(rootRecord)
	remaining := int64(rootRecord.DataLength)
	sector := make([]byte, iso9660SectorSize)
	for directoryOffset := int64(0); remaining > 0; directoryOffset += iso9660SectorSize {
		readSize := min(remaining, int64(len(sector)))
		n, readErr := r.ReadAtContext(ctx, sector[:readSize], rootOffset+directoryOffset)
		if readErr != nil {
			return discTokenFile{}, fmt.Errorf("read iso9660 root directory: %w", readErr)
		}
		if int64(n) < readSize {
			return discTokenFile{}, io.ErrUnexpectedEOF
		}

		for recordOffset := 0; recordOffset < n; {
			recordLength := int(sector[recordOffset])
			if recordLength == 0 {
				break
			}
			if recordOffset+recordLength > n {
				return discTokenFile{}, errors.New("iso9660 directory record crosses sector boundary")
			}

			record, parseErr := parseISO9660DirectoryRecord(sector[recordOffset : recordOffset+recordLength])
			if parseErr != nil {
				return discTokenFile{}, fmt.Errorf("parse iso9660 directory record: %w", parseErr)
			}
			if iso9660TokenFileIdentifier(record.Identifier) {
				return readISO9660TokenFileRecord(ctx, r, record)
			}
			recordOffset += recordLength
		}
		remaining -= readSize
	}

	return discTokenFile{State: discTokenFileAbsent}, nil
}

func readISO9660TokenFileRecord(
	ctx context.Context,
	r contextReaderAt,
	record iso9660DirectoryRecord,
) (discTokenFile, error) {
	if record.Flags&iso9660DirectoryFlag != 0 || record.Flags&iso9660MultiExtentFlag != 0 {
		return discTokenFile{State: discTokenFilePresent}, nil
	}
	if record.DataLength > iso9660MaxTokenFileSize {
		return discTokenFile{State: discTokenFilePresent}, nil
	}
	if record.DataLength == 0 {
		return discTokenFile{State: discTokenFilePresent}, nil
	}

	contents := make([]byte, int(record.DataLength))
	n, err := r.ReadAtContext(ctx, contents, iso9660RecordDataOffset(record))
	if err != nil {
		return discTokenFile{}, fmt.Errorf("read iso9660 token file: %w", err)
	}
	if n < len(contents) {
		return discTokenFile{}, io.ErrUnexpectedEOF
	}
	return discTokenFile{Data: contents, State: discTokenFilePresent}, nil
}

func parseISO9660DirectoryRecord(raw []byte) (iso9660DirectoryRecord, error) {
	if len(raw) == 0 {
		return iso9660DirectoryRecord{}, io.ErrUnexpectedEOF
	}
	recordLength := int(raw[0])
	if recordLength < iso9660DirectoryRecordMinSize || recordLength > len(raw) {
		return iso9660DirectoryRecord{}, errors.New("invalid iso9660 directory record length")
	}

	identifierLength := int(raw[iso9660FileIdentifierLengthOffset])
	identifierEnd := iso9660FileIdentifierOffset + identifierLength
	if identifierEnd > recordLength {
		return iso9660DirectoryRecord{}, errors.New("invalid iso9660 file identifier length")
	}

	return iso9660DirectoryRecord{
		Identifier:              raw[iso9660FileIdentifierOffset:identifierEnd],
		ExtentLocation:          binary.LittleEndian.Uint32(raw[iso9660ExtentLocationOffset:]),
		DataLength:              binary.LittleEndian.Uint32(raw[iso9660DataLengthOffset:]),
		ExtendedAttributeLength: raw[1],
		Flags:                   raw[iso9660FileFlagsOffset],
	}, nil
}

func iso9660RecordDataOffset(record iso9660DirectoryRecord) int64 {
	block := int64(record.ExtentLocation) + int64(record.ExtendedAttributeLength)
	return block * iso9660SectorSize
}

func iso9660TokenFileIdentifier(identifier []byte) bool {
	name := strings.TrimSuffix(string(identifier), ";1")
	return strings.EqualFold(name, iso9660TokenFileName)
}

func trimISO9660String(raw []byte) string {
	return strings.TrimRight(string(raw), " \x00")
}

func iso9660DateUUID(raw []byte) string {
	if len(raw) < iso9660DateSize {
		return ""
	}

	zeros := 0
	for _, b := range raw[:16] {
		if b == '0' {
			zeros++
		}
	}
	if zeros == 16 && raw[16] == 0 {
		return ""
	}

	return fmt.Sprintf(
		"%c%c%c%c-%c%c-%c%c-%c%c-%c%c-%c%c-%c%c",
		raw[0], raw[1], raw[2], raw[3],
		raw[4], raw[5],
		raw[6], raw[7],
		raw[8], raw[9],
		raw[10], raw[11],
		raw[12], raw[13],
		raw[14], raw[15],
	)
}
