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
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	iso9660SectorSize       = 0x800
	iso9660SuperblockOffset = 0x8000
	iso9660MaxDescriptors   = 16
	iso9660DescriptorSize   = iso9660SectorSize

	iso9660DescriptorTypePrimary = 0x01
	iso9660DescriptorTypeEnd     = 0xff

	iso9660VolumeIDOffset = 40
	iso9660VolumeIDSize   = 32
	iso9660CreatedOffset  = 813
	iso9660ModifiedOffset = 830
	iso9660DateSize       = 17
)

var errISO9660IdentityNotFound = errors.New("iso9660 identity not found")

type discIdentity struct {
	UUID  string
	Label string
}

type contextReaderAt interface {
	ReadAtContext(context.Context, []byte, int64) (int, error)
}

type readerAtContextAdapter struct {
	reader io.ReaderAt
}

func (r readerAtContextAdapter) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	n, err := r.reader.ReadAt(p, off)
	if err != nil {
		return n, fmt.Errorf("read at: %w", err)
	}
	return n, nil
}

func readISO9660Identity(r io.ReaderAt) (discIdentity, bool, error) {
	return readISO9660IdentityContext(context.Background(), readerAtContextAdapter{reader: r})
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
		return discIdentity{UUID: uuid, Label: label}, true, nil
	}
	return discIdentity{}, false, nil
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
