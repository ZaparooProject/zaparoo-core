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

// Package nfctag reads and writes NFC tags for every reader driver, so the
// same physical tag gives the same token whichever reader it is presented to.
//
// A driver identifies the tag and supplies the few operations its hardware can
// perform. This package owns everything above that: where the NDEF data lives,
// how it is parsed, which special tags are recognised and what a write puts on
// the tag.
//
// Tag kinds are handled by one function pair each. A handler asserts the extra
// capability it needs from the driver's BlockIO, as the Classic handler does
// with Authenticator, so a new kind is a new handler and, at most, one new
// capability interface.
package nfctag

import (
	"context"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// Kind is the tag family a driver identified.
type Kind int

const (
	// KindUnknown is any tag this package cannot read. It gets a UID-only token.
	KindUnknown Kind = iota
	// KindType2 covers NTAG21x and MIFARE Ultralight.
	KindType2
	KindClassic1K
	KindClassic4K
)

// KeyType selects which MIFARE Classic sector key to authenticate with. The
// values are the tag's own command bytes.
type KeyType byte

const (
	KeyA KeyType = 0x60
	KeyB KeyType = 0x61
)

// Key is a MIFARE Classic sector key.
type Key [6]byte

var (
	// KeyNDEF is the public key NDEF-formatted sectors use.
	KeyNDEF = Key{0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7}
	// KeyFactory is the key a blank tag ships with.
	KeyFactory = Key{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
)

// BlockSize is the number of bytes one read returns on every supported kind.
const BlockSize = 16

var (
	// ErrVerify means the tag did not hold the written data when read back.
	ErrVerify = errors.New("tag contents do not match what was written")
	// ErrTooLarge means the text does not fit on the tag.
	ErrTooLarge = errors.New("text is too large for tag")
	// ErrEmptyText means there was nothing to write.
	ErrEmptyText = errors.New("text cannot be empty")
)

// BlockIO is the tag access every driver supplies.
//
// The sizes are the tags' native ones, which every reader tested accepts:
// larger transfers are rejected by some readers.
type BlockIO interface {
	// ReadBlock returns BlockSize bytes. On a Type 2 tag addr is a page and the
	// read covers four pages starting there. On a Classic tag addr is a block.
	ReadBlock(ctx context.Context, addr int) ([]byte, error)
	// WriteBlock writes one 4-byte page on a Type 2 tag or one 16-byte block
	// on a Classic tag.
	WriteBlock(ctx context.Context, addr int, data []byte) error
}

// Authenticator is the extra capability MIFARE Classic needs.
//
// A failed authentication must leave the tag usable, because a write tries the
// NDEF key and then the factory key on the same sector. Drivers whose hardware
// drops the tag on failure have to select it again before returning.
type Authenticator interface {
	Authenticate(ctx context.Context, block int, keyType KeyType, key Key) error
}

// Tag is one tag in a reader's field.
type Tag struct {
	IO   BlockIO
	UID  []byte
	Kind Kind
}

// Result is what a tag holds, in the form a token carries it.
type Result struct {
	// Type is always set, including when the read failed.
	Type string
	Text string
	// Data is tag memory from the first user page or block, in BlockSize
	// units, through the end of the NDEF data.
	Data []byte
}

// Read returns the tag's contents. On error the Result still carries the type,
// so the caller can report the tag by UID alone.
func Read(ctx context.Context, tag *Tag) (Result, error) {
	switch tag.Kind {
	case KindType2:
		return readType2(ctx, tag)
	case KindClassic1K, KindClassic4K:
		return readClassic(ctx, tag)
	case KindUnknown:
		return Result{Type: tokens.TypeUnknown}, nil
	default:
		return Result{Type: tokens.TypeUnknown}, nil
	}
}

// Write stores text on the tag as a single NDEF text record and reads it back.
// The Result describes the tag as it now is.
func Write(ctx context.Context, tag *Tag, text string) (Result, error) {
	if text == "" {
		return Result{}, ErrEmptyText
	}
	payload, err := buildTextMessage(text)
	if err != nil {
		return Result{}, err
	}

	switch tag.Kind {
	case KindType2:
		return writeType2(ctx, tag, text, payload)
	case KindClassic1K, KindClassic4K:
		return writeClassic(ctx, tag, text, payload)
	case KindUnknown:
		return Result{}, ErrUnsupported
	default:
		return Result{}, ErrUnsupported
	}
}

// readBlock reads one block and checks the driver returned a whole one.
func readBlock(ctx context.Context, io BlockIO, addr int) ([]byte, error) {
	data, err := io.ReadBlock(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("read block %d: %w", addr, err)
	}
	if len(data) != BlockSize {
		return nil, fmt.Errorf("read block %d: got %d bytes, want %d", addr, len(data), BlockSize)
	}
	return data, nil
}
