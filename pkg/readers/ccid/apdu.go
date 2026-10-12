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

package ccid

import (
	"context"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/rs/zerolog/log"
)

// PC/SC Part 3 storage card commands. The reader's firmware carries them out,
// so they work the same through a PC/SC service and over raw CCID.
const (
	claPCSC         = 0xFF
	insGetData      = 0xCA
	insReadBinary   = 0xB0
	insUpdateBinary = 0xD6
	insLoadKey      = 0x82
	insAuthenticate = 0x86

	swSuccess = 0x9000

	apduHeaderLen = 5
)

// errTagNotReady is a reader that reports a tag before it has identified it.
// The bR500 does this when powered on straight after a power-off: the UID
// comes back empty with a success status.
var errTagNotReady = errors.New("reader has not identified the tag yet")

// statusError is a command the reader answered with anything but 90 00.
// Readers disagree on the code for the same fault, so the code is reported
// and never interpreted.
type statusError struct {
	command string
	sw      uint16
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s failed with status %04X", e.command, e.sw)
}

// transmitter sends one APDU to the tag in the field.
type transmitter interface {
	Transmit(ctx context.Context, apdu []byte) ([]byte, error)
}

// exchange sends a command and returns the response data without its status
// word. The header of a failed command is logged at the default level: with
// the status, it is what is needed to support a reader nobody has tested.
func exchange(ctx context.Context, card transmitter, command string, apdu []byte) ([]byte, error) {
	resp, err := card.Transmit(ctx, apdu)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", command, err)
	}
	if len(resp) < 2 {
		return nil, fmt.Errorf("%s: response of %d bytes has no status", command, len(resp))
	}
	sw := uint16(resp[len(resp)-2])<<8 | uint16(resp[len(resp)-1])
	if sw != swSuccess {
		log.Info().
			Str("command", command).
			Hex("header", apdu[:min(len(apdu), apduHeaderLen)]).
			Str("status", fmt.Sprintf("%04X", sw)).
			Msg("ccid: reader rejected command")
		return nil, &statusError{command: command, sw: sw}
	}
	return resp[:len(resp)-2], nil
}

// readUID returns the tag's UID. Only the lengths ISO 14443 defines are
// accepted, so a reader that is not ready never produces a token.
func readUID(ctx context.Context, card transmitter) ([]byte, error) {
	uid, err := exchange(ctx, card, "get UID", []byte{claPCSC, insGetData, 0x00, 0x00, 0x00})
	if err != nil {
		return nil, err
	}
	switch len(uid) {
	case 4, 7, 10:
		return uid, nil
	default:
		return nil, fmt.Errorf("%w: UID of %d bytes", errTagNotReady, len(uid))
	}
}

// apduTag gives the shared tag layer block access through the storage card
// commands. Transfers use the tag's native sizes: every reader tested accepts
// those, and two of three reject anything larger. A length of 00 is never
// sent, because some readers answer it with success and no data.
type apduTag struct {
	card transmitter
}

var (
	_ nfctag.BlockIO       = (*apduTag)(nil)
	_ nfctag.Authenticator = (*apduTag)(nil)
)

func (t *apduTag) ReadBlock(ctx context.Context, addr int) ([]byte, error) {
	if addr < 0 || addr > 0xFF {
		return nil, fmt.Errorf("block %d out of range", addr)
	}
	return exchange(ctx, t.card, "read", []byte{claPCSC, insReadBinary, 0x00, byte(addr), nfctag.BlockSize})
}

func (t *apduTag) WriteBlock(ctx context.Context, addr int, data []byte) error {
	if addr < 0 || addr > 0xFF {
		return fmt.Errorf("block %d out of range", addr)
	}
	if len(data) == 0 || len(data) > nfctag.BlockSize {
		return fmt.Errorf("write of %d bytes not supported", len(data))
	}
	apdu := make([]byte, 0, apduHeaderLen+len(data))
	apdu = append(apdu, claPCSC, insUpdateBinary, 0x00, byte(addr), byte(len(data))) //nolint:gosec // at most 16
	apdu = append(apdu, data...)
	_, err := exchange(ctx, t.card, "write", apdu)
	return err
}

// Authenticate loads the key into the reader's volatile slot 0 and uses it.
// All three readers tested stay usable after a failed attempt.
func (t *apduTag) Authenticate(ctx context.Context, block int, keyType nfctag.KeyType, key nfctag.Key) error {
	if block < 0 || block > 0xFF {
		return fmt.Errorf("block %d out of range", block)
	}
	load := make([]byte, 0, apduHeaderLen+len(key))
	load = append(load, claPCSC, insLoadKey, 0x00, 0x00, byte(len(key)))
	load = append(load, key[:]...)
	if _, err := exchange(ctx, t.card, "load key", load); err != nil {
		return err
	}
	auth := []byte{claPCSC, insAuthenticate, 0x00, 0x00, 0x05, 0x01, 0x00, byte(block), byte(keyType), 0x00}
	_, err := exchange(ctx, t.card, "authenticate", auth)
	return err
}
