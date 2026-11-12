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
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

const (
	classic1KSectors = 16
	classic4KSectors = 40

	// The first 32 sectors have 4 blocks each; the rest, on a 4K tag, have 16.
	classicSmallSectors     = 32
	classicSmallSectorSize  = 4
	classicLargeSectorSize  = 16
	classicFirstDataSector  = 1
	classicTrailerKeyAEnd   = 6
	classicTrailerAccessEnd = 10
)

var errNoAuthenticator = errors.New("reader cannot authenticate MIFARE Classic sectors")

func classicSectorCount(kind Kind) int {
	if kind == KindClassic4K {
		return classic4KSectors
	}
	return classic1KSectors
}

func classicSectorStart(sector int) int {
	if sector < classicSmallSectors {
		return sector * classicSmallSectorSize
	}
	return classicSmallSectors*classicSmallSectorSize + (sector-classicSmallSectors)*classicLargeSectorSize
}

// classicDataBlocks is the number of blocks in a sector before its trailer.
func classicDataBlocks(sector int) int {
	if sector < classicSmallSectors {
		return classicSmallSectorSize - 1
	}
	return classicLargeSectorSize - 1
}

func classicCapacity(kind Kind) int {
	total := 0
	for sector := classicFirstDataSector; sector < classicSectorCount(kind); sector++ {
		total += classicDataBlocks(sector) * BlockSize
	}
	return total
}

// classicCursor walks the data blocks of a Classic tag from sector 1, skipping
// trailers. NDEF data is stored there without a directory in sector 0, which is
// how every Zaparoo driver has always written these tags.
type classicCursor struct {
	sectors int
	sector  int
	index   int
}

func newClassicCursor(kind Kind) *classicCursor {
	return &classicCursor{sectors: classicSectorCount(kind), sector: classicFirstDataSector}
}

func (c *classicCursor) done() bool {
	return c.sector >= c.sectors
}

func (c *classicCursor) block() int {
	return classicSectorStart(c.sector) + c.index
}

func (c *classicCursor) atSectorStart() bool {
	return c.index == 0
}

func (c *classicCursor) trailer() int {
	return classicSectorStart(c.sector) + classicDataBlocks(c.sector)
}

func (c *classicCursor) advance() {
	c.index++
	if c.index >= classicDataBlocks(c.sector) {
		c.sector++
		c.index = 0
	}
}

func readClassic(ctx context.Context, tag *Tag) (Result, error) {
	res := Result{Type: tokens.TypeMifare}

	auth, ok := tag.IO.(Authenticator)
	if !ok {
		return res, errNoAuthenticator
	}

	cursor := newClassicCursor(tag.Kind)
	next := func() ([]byte, error) {
		if cursor.done() {
			return nil, errEndOfMemory
		}
		if cursor.atSectorStart() {
			if err := auth.Authenticate(ctx, cursor.block(), KeyA, KeyNDEF); err != nil {
				// A sector without the NDEF key is where NDEF data ends: a
				// blank tag has none, and a partly formatted one stops here.
				return nil, errEndOfMemory
			}
		}
		block, err := readBlock(ctx, tag.IO, cursor.block())
		if err != nil {
			return nil, err
		}
		cursor.advance()
		return block, nil
	}

	mem, err := next()
	if err != nil {
		return res, endOfMemoryIsNil(err)
	}

	mem, err = readThroughNDEF(mem, next)
	res.Data = mem
	if err != nil {
		return res, err
	}
	res.Text = TextFromMemory(mem)
	return res, nil
}

func writeClassic(ctx context.Context, tag *Tag, text string, payload []byte) (Result, error) {
	auth, ok := tag.IO.(Authenticator)
	if !ok {
		return Result{}, errNoAuthenticator
	}

	capacity := classicCapacity(tag.Kind)
	if len(payload) > capacity {
		return Result{}, fmt.Errorf("%w: %d of %d bytes", ErrTooLarge, len(payload), capacity)
	}
	padded := padTo(payload, BlockSize)

	cursor := newClassicCursor(tag.Kind)
	for offset := 0; offset < len(padded); offset += BlockSize {
		if cursor.atSectorStart() {
			if err := openClassicSector(ctx, tag.IO, auth, cursor); err != nil {
				return Result{}, err
			}
		}
		if err := tag.IO.WriteBlock(ctx, cursor.block(), padded[offset:offset+BlockSize]); err != nil {
			return Result{}, fmt.Errorf("write block %d: %w", cursor.block(), err)
		}
		cursor.advance()
	}

	clearClassicLeftovers(ctx, tag.IO, auth, cursor)

	mem, err := verifyClassic(ctx, tag, auth, padded)
	if err != nil {
		return Result{}, err
	}
	return Result{Type: tokens.TypeMifare, Text: text, Data: mem}, nil
}

// openClassicSector authenticates the cursor's sector for writing, formatting
// it first when it still has the factory key.
func openClassicSector(ctx context.Context, io BlockIO, auth Authenticator, cursor *classicCursor) error {
	first := cursor.block()
	if auth.Authenticate(ctx, first, KeyA, KeyNDEF) == nil {
		return nil
	}
	if err := auth.Authenticate(ctx, first, KeyA, KeyFactory); err != nil {
		return fmt.Errorf("%w: sector %d has an unknown key", ErrUnsupported, cursor.sector)
	}

	trailer, err := readBlock(ctx, io, cursor.trailer())
	if err != nil {
		return err
	}
	// Both keys become the NDEF key. The access bits are kept: a wrong value
	// there locks the sector for good.
	formatted := make([]byte, 0, BlockSize)
	formatted = append(formatted, KeyNDEF[:]...)
	formatted = append(formatted, trailer[classicTrailerKeyAEnd:classicTrailerAccessEnd]...)
	formatted = append(formatted, KeyNDEF[:]...)
	if err := io.WriteBlock(ctx, cursor.trailer(), formatted); err != nil {
		return fmt.Errorf("format sector %d: %w", cursor.sector, err)
	}

	if err := auth.Authenticate(ctx, first, KeyA, KeyNDEF); err != nil {
		return fmt.Errorf("authenticate sector %d after format: %w", cursor.sector, err)
	}
	return nil
}

// clearClassicLeftovers zeroes what an earlier, longer message left after the
// new one. It is best effort and stops at the first block that is already
// empty, since a message is written without gaps.
func clearClassicLeftovers(ctx context.Context, io BlockIO, auth Authenticator, cursor *classicCursor) {
	zero := make([]byte, BlockSize)
	for !cursor.done() {
		if cursor.atSectorStart() && auth.Authenticate(ctx, cursor.block(), KeyA, KeyNDEF) != nil {
			return
		}
		block, err := readBlock(ctx, io, cursor.block())
		if err != nil || bytes.Equal(block, zero) {
			return
		}
		if io.WriteBlock(ctx, cursor.block(), zero) != nil {
			return
		}
		cursor.advance()
	}
}

func verifyClassic(ctx context.Context, tag *Tag, auth Authenticator, padded []byte) ([]byte, error) {
	cursor := newClassicCursor(tag.Kind)
	mem := make([]byte, 0, len(padded))
	for len(mem) < len(padded) {
		if cursor.atSectorStart() {
			if err := auth.Authenticate(ctx, cursor.block(), KeyA, KeyNDEF); err != nil {
				return nil, fmt.Errorf("authenticate sector %d to verify: %w", cursor.sector, err)
			}
		}
		block, err := readBlock(ctx, tag.IO, cursor.block())
		if err != nil {
			return nil, err
		}
		mem = append(mem, block...)
		cursor.advance()
	}
	if !bytes.Equal(mem, padded) {
		return nil, ErrVerify
	}
	return mem, nil
}
