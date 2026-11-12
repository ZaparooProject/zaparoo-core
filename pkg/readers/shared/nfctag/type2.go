/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package nfctag

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

const (
	type2PageSize      = 4
	type2PagesPerBlock = BlockSize / type2PageSize
	type2CCPage        = 3
	type2UserPage      = 4
	type2CCMagic       = 0xE1
	type2CCVersion     = 0x10

	// Capability container size bytes, which identify the NTAG variant.
	ccSizeNTAG213 = 0x12
	ccSizeNTAG215 = 0x3E
	ccSizeNTAG216 = 0x6D

	amiiboIDPage = 21
	amiiboIDLen  = 8
)

// amiiboMatcher is bytes 9 to 15 of an Amiibo: an internal byte, the static
// lock bytes and the capability container.
var amiiboMatcher = []byte{0x48, 0x0F, 0xE0, 0xF1, 0x10, 0xFF, 0xEE}

// legoDimensionsMatcher is the start of user memory on a Lego Dimensions tag.
// https://github.com/RfidResearchGroup/proxmark3/blob/master/client/src/cmdhfmfu.c
var legoDimensionsMatcher = []byte{
	0x01, 0x03, 0xA0, 0x0C,
	0x34, 0x03, 0x13, 0xD1,
	0x01, 0x0F, 0x54, 0x02,
	0x65, 0x6E,
}

// type2Variants gives each NTAG variant's first configuration page, from the
// largest down. It identifies a tag whose capability container is blank.
var type2Variants = []struct {
	configPage int
	ccSize     byte
}{
	{configPage: 227, ccSize: ccSizeNTAG216},
	{configPage: 131, ccSize: ccSizeNTAG215},
	{configPage: 41, ccSize: ccSizeNTAG213},
}

// type2AUTH0 is where the AUTH0 byte sits in a configuration page, and the
// value it has on a tag as delivered.
const (
	type2AUTH0Offset  = 3
	type2AUTH0Default = 0xFF
)

// type2UserBytes returns the user memory size for a capability container size
// byte. The NTAG variants have slightly more usable memory than the container
// declares.
func type2UserBytes(ccSize byte) int {
	switch ccSize {
	case ccSizeNTAG213:
		return 144
	case ccSizeNTAG215:
		return 504
	case ccSizeNTAG216:
		return 888
	default:
		return int(ccSize) * 8
	}
}

func isAmiibo(header []byte) bool {
	return bytes.Equal(header[9:BlockSize], amiiboMatcher)
}

func readType2(ctx context.Context, tag *Tag) (Result, error) {
	res := Result{Type: tokens.TypeNTAG}

	header, err := readBlock(ctx, tag.IO, 0)
	if err != nil {
		return res, err
	}

	if isAmiibo(header) {
		res.Type = tokens.TypeAmiibo
		id, idErr := readBlock(ctx, tag.IO, amiiboIDPage)
		if idErr != nil {
			return res, idErr
		}
		res.Data = id[:amiiboIDLen]
		return res, nil
	}

	mem, err := readBlock(ctx, tag.IO, type2UserPage)
	if err != nil {
		return res, err
	}
	res.Data = mem

	isLego := bytes.HasPrefix(mem, legoDimensionsMatcher)
	if isLego {
		res.Type = tokens.TypeLegoDimensions
	}

	cc := header[type2CCPage*type2PageSize:]
	if cc[0] != type2CCMagic {
		return res, nil
	}

	blocks := (type2UserBytes(cc[2]) + BlockSize - 1) / BlockSize
	read := 1
	mem, err = readThroughNDEF(mem, func() ([]byte, error) {
		if read >= blocks {
			return nil, errEndOfMemory
		}
		block, readErr := readBlock(ctx, tag.IO, type2UserPage+read*type2PagesPerBlock)
		if readErr != nil {
			return nil, readErr
		}
		read++
		return block, nil
	})
	res.Data = mem
	if err != nil {
		return res, err
	}

	// A toy's text is not ZapScript. It stays available in Data.
	if !isLego {
		res.Text = TextFromMemory(mem)
	}
	return res, nil
}

func writeType2(ctx context.Context, tag *Tag, text string, payload []byte) (Result, error) {
	header, err := readBlock(ctx, tag.IO, 0)
	if err != nil {
		return Result{}, err
	}
	if isAmiibo(header) {
		return Result{}, ErrUnsupported
	}

	cc := header[type2CCPage*type2PageSize:]
	ccSize := cc[2]
	needsContainer := false
	switch {
	case cc[0] == type2CCMagic:
	case bytes.Equal(cc, make([]byte, type2PageSize)):
		// The container is one-time programmable, so it can only be written
		// while it is still blank. Without it other readers ignore the tag.
		ccSize, err = blankType2Size(ctx, tag.IO)
		if err != nil {
			return Result{}, err
		}
		needsContainer = true
	default:
		return Result{}, ErrUnsupported
	}

	capacity := type2UserBytes(ccSize)
	if len(payload) > capacity {
		return Result{}, fmt.Errorf("%w: %d of %d bytes", ErrTooLarge, len(payload), capacity)
	}

	padded := padTo(payload, type2PageSize)
	for offset := 0; offset < len(padded); offset += type2PageSize {
		page := type2UserPage + offset/type2PageSize
		if writeErr := tag.IO.WriteBlock(ctx, page, padded[offset:offset+type2PageSize]); writeErr != nil {
			return Result{}, fmt.Errorf("write page %d: %w", page, writeErr)
		}
	}

	mem := make([]byte, 0, len(padded)+BlockSize)
	for page := type2UserPage; len(mem) < len(padded); page += type2PagesPerBlock {
		block, readErr := readBlock(ctx, tag.IO, page)
		if readErr != nil {
			return Result{}, readErr
		}
		mem = append(mem, block...)
	}
	if !bytes.Equal(mem[:len(padded)], padded) {
		return Result{}, ErrVerify
	}

	// The container is written last: it cannot be undone, so a tag only
	// becomes an NDEF tag once it holds a complete, verified message.
	if needsContainer {
		container := []byte{type2CCMagic, type2CCVersion, ccSize, 0x00}
		if writeErr := tag.IO.WriteBlock(ctx, type2CCPage, container); writeErr != nil {
			return Result{}, fmt.Errorf("write capability container: %w", writeErr)
		}
	}

	return Result{Type: tokens.TypeNTAG, Text: text, Data: mem}, nil
}

// blankType2Size returns the capability container size byte for a tag whose
// container is blank, or ErrUnsupported when the tag cannot be identified.
//
// A wrong size could never be corrected, so the variant has to be shown by the
// tag, not inferred from a read that failed: a larger variant's page failing
// to read can be a passing RF fault. Each variant keeps its configuration at a
// different page, and the first of those pages ends in the AUTH0 byte, which
// is FFh on a tag as delivered. On any larger variant the same page is blank
// user memory.
func blankType2Size(ctx context.Context, io BlockIO) (byte, error) {
	for _, variant := range type2Variants {
		config, err := io.ReadBlock(ctx, variant.configPage)
		if err != nil || len(config) <= type2AUTH0Offset {
			continue
		}
		if config[type2AUTH0Offset] == type2AUTH0Default {
			return variant.ccSize, nil
		}
	}
	return 0, ErrUnsupported
}
