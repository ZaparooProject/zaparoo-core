//go:build linux && !android

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

// Package tags gives the shared tag layer access to a tag through libnfc.
// Everything about what is on a tag and how it is written lives in nfctag;
// this package only sends the tag's own commands.
package tags

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/clausecker/nfc/v2"
)

// The tags' native commands.
const (
	cmdRead         = 0x30
	cmdWritePage    = 0xA2
	cmdWriteBlock   = 0xA0
	type2PageSize   = 4
	classicUIDBytes = 4
)

// typeA is the only modulation polled for: NTAG and MIFARE Classic.
var typeA = nfc.Modulation{Type: nfc.ISO14443a, BaudRate: nfc.Nbr106}

var SupportedCardTypes = []nfc.Modulation{typeA}

// UID returns a target's UID, or nil for a target this driver cannot use.
func UID(target nfc.Target) []byte {
	card, ok := target.(*nfc.ISO14443aTarget)
	if !ok || card.UIDLen <= 0 || card.UIDLen > len(card.UID) {
		return nil
	}
	return append([]byte(nil), card.UID[:card.UIDLen]...)
}

// GetTagUID returns a target's UID as the hex string a token carries.
func GetTagUID(target nfc.Target) string {
	return hex.EncodeToString(UID(target))
}

// Kind identifies the tag family from the values a Type A tag answers
// selection with.
func Kind(target nfc.Target) nfctag.Kind {
	card, ok := target.(*nfc.ISO14443aTarget)
	if !ok {
		return nfctag.KindUnknown
	}
	switch {
	case card.Atqa == [2]byte{0x00, 0x44} && card.Sak == 0x00:
		// https://www.nxp.com/docs/en/data-sheet/NTAG213_215_216.pdf page 33
		return nfctag.KindType2
	case card.Atqa == [2]byte{0x00, 0x04} && card.Sak == 0x08:
		// https://www.nxp.com/docs/en/application-note/AN10833.pdf page 9
		return nfctag.KindClassic1K
	case card.Atqa == [2]byte{0x00, 0x02} && card.Sak == 0x18:
		return nfctag.KindClassic4K
	default:
		return nfctag.KindUnknown
	}
}

// Transceiver is the part of a libnfc device the tag commands need.
type Transceiver interface {
	InitiatorTransceiveBytes(tx, rx []byte, timeout int) (int, error)
	InitiatorSelectPassiveTarget(m nfc.Modulation, initData []byte) (nfc.Target, error)
}

// IO sends a tag's native commands through libnfc.
type IO struct {
	dev Transceiver
	uid []byte
}

var (
	_ nfctag.BlockIO       = (*IO)(nil)
	_ nfctag.Authenticator = (*IO)(nil)
)

// NewIO returns tag access for the tag with the given UID, which the caller
// has already selected by polling.
func NewIO(dev Transceiver, uid []byte) *IO {
	return &IO{dev: dev, uid: uid}
}

func (t *IO) transceive(tx []byte, replySize int) ([]byte, error) {
	rx := make([]byte, replySize)
	n, err := t.dev.InitiatorTransceiveBytes(tx, rx, 0)
	if err != nil {
		return nil, fmt.Errorf("tag command %02X: %w", tx[0], err)
	}
	if n < 0 || n > len(rx) {
		return nil, fmt.Errorf("tag command %02X: reply of %d bytes", tx[0], n)
	}
	return rx[:n], nil
}

func (t *IO) ReadBlock(_ context.Context, addr int) ([]byte, error) {
	if addr < 0 || addr > 0xFF {
		return nil, fmt.Errorf("block %d out of range", addr)
	}
	return t.transceive([]byte{cmdRead, byte(addr)}, nfctag.BlockSize)
}

func (t *IO) WriteBlock(_ context.Context, addr int, data []byte) error {
	if addr < 0 || addr > 0xFF {
		return fmt.Errorf("block %d out of range", addr)
	}
	var command byte
	switch len(data) {
	case type2PageSize:
		command = cmdWritePage
	case nfctag.BlockSize:
		command = cmdWriteBlock
	default:
		return fmt.Errorf("write of %d bytes not supported", len(data))
	}
	tx := make([]byte, 0, 2+len(data))
	tx = append(tx, command, byte(addr))
	tx = append(tx, data...)
	_, err := t.transceive(tx, 2)
	return err
}

// Authenticate proves a sector key to the tag. A tag that is given the wrong
// key stops answering, so it is selected again before the failure is
// returned: the caller may want to try another key.
func (t *IO) Authenticate(_ context.Context, block int, keyType nfctag.KeyType, key nfctag.Key) error {
	if block < 0 || block > 0xFF {
		return fmt.Errorf("block %d out of range", block)
	}
	if len(t.uid) < classicUIDBytes {
		return errors.New("tag has no UID to authenticate with")
	}
	tx := make([]byte, 0, 2+len(key)+classicUIDBytes)
	tx = append(tx, byte(keyType), byte(block))
	tx = append(tx, key[:]...)
	tx = append(tx, t.uid[len(t.uid)-classicUIDBytes:]...)

	if _, err := t.transceive(tx, 2); err != nil {
		if _, selectErr := t.dev.InitiatorSelectPassiveTarget(typeA, t.uid); selectErr != nil {
			return fmt.Errorf("%w (and the tag could not be selected again: %w)", err, selectErr)
		}
		return err
	}
	return nil
}
