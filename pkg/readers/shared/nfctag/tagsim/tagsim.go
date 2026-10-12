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

// Package tagsim provides in-memory NFC tags for tests. They behave like the
// physical tags at the level reader drivers see them: page and block
// addressing, the one-time-programmable capability container, and MIFARE
// Classic sector authentication.
package tagsim

import (
	"context"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
)

const (
	pageSize  = 4
	blockSize = 16
)

// Type 2 tag sizes, in pages.
const (
	PagesUltralight = 16
	PagesNTAG213    = 45
	PagesNTAG215    = 135
	PagesNTAG216    = 231
)

var (
	ErrOutOfRange       = errors.New("address out of range")
	ErrNotAuthenticated = errors.New("sector not authenticated")
	ErrAuthFailed       = errors.New("authentication failed")
	ErrReadOnly         = errors.New("page is read-only")
	ErrBadLength        = errors.New("wrong data length")
)

// Type2 is an NTAG or Ultralight tag.
type Type2 struct {
	Mem   []byte
	Reads int
}

var _ nfctag.BlockIO = (*Type2)(nil)

// NewType2 returns a tag of the given size as it leaves the factory: a 7-byte
// UID, a capability container for its size and empty user memory.
func NewType2(pages int) *Type2 {
	tag := &Type2{Mem: make([]byte, pages*pageSize)}
	copy(tag.Mem, []byte{0x04, 0x7A, 0x7E, 0x88, 0x0A, 0xC0, 0x1C, 0x91, 0x47, 0x48, 0x00, 0x00})

	var ccSize byte
	switch pages {
	case PagesNTAG213:
		ccSize = 0x12
	case PagesNTAG215:
		ccSize = 0x3E
	case PagesNTAG216:
		ccSize = 0x6D
	default:
		ccSize = byte((pages - 4) * pageSize / 8) //nolint:gosec // test tags are far below 2040 bytes
	}
	copy(tag.Mem[3*pageSize:], []byte{0xE1, 0x10, ccSize, 0x00})
	return tag
}

// UID returns the tag's 7-byte UID.
func (t *Type2) UID() []byte {
	uid := make([]byte, 0, 7)
	uid = append(uid, t.Mem[0:3]...)
	return append(uid, t.Mem[4:8]...)
}

// SetUser replaces user memory from page 4 with data.
func (t *Type2) SetUser(data []byte) {
	copy(t.Mem[4*pageSize:], data)
}

// User returns user memory from page 4.
func (t *Type2) User() []byte {
	return t.Mem[4*pageSize:]
}

// ClearCC blanks the capability container, as on a tag that was never
// initialised.
func (t *Type2) ClearCC() {
	copy(t.Mem[3*pageSize:], make([]byte, pageSize))
}

func (t *Type2) pages() int {
	return len(t.Mem) / pageSize
}

// ReadBlock returns four pages from addr, rolling over to page 0 past the end
// as the real tags do.
func (t *Type2) ReadBlock(_ context.Context, addr int) ([]byte, error) {
	if addr < 0 || addr >= t.pages() {
		return nil, fmt.Errorf("%w: page %d", ErrOutOfRange, addr)
	}
	t.Reads++
	out := make([]byte, 0, blockSize)
	for i := range blockSize / pageSize {
		page := (addr + i) % t.pages()
		out = append(out, t.Mem[page*pageSize:(page+1)*pageSize]...)
	}
	return out, nil
}

// WriteBlock writes one page. The capability container page can only have
// bits set, never cleared.
func (t *Type2) WriteBlock(_ context.Context, addr int, data []byte) error {
	if len(data) != pageSize {
		return fmt.Errorf("%w: %d bytes", ErrBadLength, len(data))
	}
	if addr < 0 || addr >= t.pages() {
		return fmt.Errorf("%w: page %d", ErrOutOfRange, addr)
	}
	if addr < 2 {
		return fmt.Errorf("%w: page %d", ErrReadOnly, addr)
	}
	page := t.Mem[addr*pageSize : (addr+1)*pageSize]
	if addr == 3 {
		for i := range page {
			page[i] |= data[i]
		}
		return nil
	}
	copy(page, data)
	return nil
}

// Classic is a MIFARE Classic 1K or 4K tag.
type Classic struct {
	keysA      map[int]nfctag.Key
	keysB      map[int]nfctag.Key
	uid        []byte
	Blocks     [][]byte
	authSector int
	Writes     int
	sectors    int
}

var (
	_ nfctag.BlockIO       = (*Classic)(nil)
	_ nfctag.Authenticator = (*Classic)(nil)
)

// NewClassic returns a factory-blank tag with 16 sectors (1K) or 40 (4K).
func NewClassic(sectors int) *Classic {
	tag := &Classic{
		keysA:      make(map[int]nfctag.Key),
		keysB:      make(map[int]nfctag.Key),
		uid:        []byte{0x03, 0x8C, 0xC3, 0x0F},
		authSector: -1,
		sectors:    sectors,
	}
	blocks := 0
	for sector := range sectors {
		blocks += sectorSize(sector)
		tag.keysA[sector] = nfctag.KeyFactory
		tag.keysB[sector] = nfctag.KeyFactory
	}
	tag.Blocks = make([][]byte, blocks)
	for i := range tag.Blocks {
		tag.Blocks[i] = make([]byte, blockSize)
	}
	copy(tag.Blocks[0], []byte{0x03, 0x8C, 0xC3, 0x0F, 0x43, 0x08, 0x04, 0x00})
	for sector := range sectors {
		trailer := tag.Blocks[sectorStart(sector)+sectorSize(sector)-1]
		copy(trailer[6:10], []byte{0xFF, 0x07, 0x80, 0x69})
	}
	return tag
}

// UID returns the tag's 4-byte UID.
func (c *Classic) UID() []byte {
	return append([]byte(nil), c.uid...)
}

// Format gives sectors 1 and up the NDEF key, as a tag Zaparoo has written.
func (c *Classic) Format() {
	for sector := 1; sector < c.sectors; sector++ {
		c.SetKeys(sector, nfctag.KeyNDEF)
	}
}

// SetKeys sets both keys of a sector.
func (c *Classic) SetKeys(sector int, key nfctag.Key) {
	c.keysA[sector] = key
	c.keysB[sector] = key
}

// KeyA returns a sector's key A.
func (c *Classic) KeyA(sector int) nfctag.Key {
	return c.keysA[sector]
}

// SetData writes data across the data blocks from sector 1, skipping trailers.
func (c *Classic) SetData(data []byte) {
	for block := sectorStart(1); len(data) > 0 && block < len(c.Blocks); block++ {
		if isTrailer(block) {
			continue
		}
		n := copy(c.Blocks[block], data)
		data = data[n:]
	}
}

// Data returns the data blocks from sector 1 joined together.
func (c *Classic) Data() []byte {
	var out []byte
	for block := sectorStart(1); block < len(c.Blocks); block++ {
		if !isTrailer(block) {
			out = append(out, c.Blocks[block]...)
		}
	}
	return out
}

func sectorSize(sector int) int {
	if sector < 32 {
		return 4
	}
	return 16
}

func sectorStart(sector int) int {
	if sector < 32 {
		return sector * 4
	}
	return 128 + (sector-32)*16
}

func sectorOf(block int) int {
	if block < 128 {
		return block / 4
	}
	return 32 + (block-128)/16
}

func isTrailer(block int) bool {
	sector := sectorOf(block)
	return block == sectorStart(sector)+sectorSize(sector)-1
}

// Authenticate checks the key for the sector holding block. A failure drops
// any earlier authentication, as on a real tag.
func (c *Classic) Authenticate(_ context.Context, block int, keyType nfctag.KeyType, key nfctag.Key) error {
	if block < 0 || block >= len(c.Blocks) {
		return fmt.Errorf("%w: block %d", ErrOutOfRange, block)
	}
	sector := sectorOf(block)
	want := c.keysA[sector]
	if keyType == nfctag.KeyB {
		want = c.keysB[sector]
	}
	if key != want {
		c.authSector = -1
		return fmt.Errorf("%w: sector %d", ErrAuthFailed, sector)
	}
	c.authSector = sector
	return nil
}

func (c *Classic) checkAccess(block int) error {
	if block < 0 || block >= len(c.Blocks) {
		return fmt.Errorf("%w: block %d", ErrOutOfRange, block)
	}
	if sectorOf(block) != c.authSector {
		return fmt.Errorf("%w: block %d", ErrNotAuthenticated, block)
	}
	return nil
}

// ReadBlock returns one block. A trailer reads back with key A hidden.
func (c *Classic) ReadBlock(_ context.Context, addr int) ([]byte, error) {
	if err := c.checkAccess(addr); err != nil {
		return nil, err
	}
	out := append([]byte(nil), c.Blocks[addr]...)
	if isTrailer(addr) {
		sector := sectorOf(addr)
		keyB := c.keysB[sector]
		copy(out[0:6], make([]byte, 6))
		copy(out[10:16], keyB[:])
	}
	return out, nil
}

// WriteBlock writes one block. Writing a trailer changes the sector's keys.
func (c *Classic) WriteBlock(_ context.Context, addr int, data []byte) error {
	if len(data) != blockSize {
		return fmt.Errorf("%w: %d bytes", ErrBadLength, len(data))
	}
	if err := c.checkAccess(addr); err != nil {
		return err
	}
	c.Writes++
	copy(c.Blocks[addr], data)
	if isTrailer(addr) {
		sector := sectorOf(addr)
		var keyA, keyB nfctag.Key
		copy(keyA[:], data[0:6])
		copy(keyB[:], data[10:16])
		c.keysA[sector] = keyA
		c.keysB[sector] = keyB
	}
	return nil
}
