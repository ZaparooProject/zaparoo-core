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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestATR(t *testing.T) {
	t.Parallel()

	ultralightC := append([]byte(nil), atrNTAG...)
	ultralightC[14] = 0x3A
	typeB := append([]byte(nil), atrNTAG...)
	typeB[12] = 0x07
	felica := append([]byte(nil), atrNTAG...)
	felica[12] = 0x11
	felica[14] = 0x3B

	tests := []struct {
		name        string
		atr         []byte
		contactless bool
		kind        nfctag.Kind
	}{
		{name: "NTAG", atr: atrNTAG, contactless: true, kind: nfctag.KindType2},
		{name: "Ultralight C", atr: ultralightC, contactless: true, kind: nfctag.KindType2},
		{name: "Classic 1K", atr: atrClassic1K, contactless: true, kind: nfctag.KindClassic1K},
		{name: "Classic 4K", atr: atrClassic4K, contactless: true, kind: nfctag.KindClassic4K},
		{name: "reader placeholder", atr: atrPlaceholder, contactless: true, kind: nfctag.KindUnknown},
		{name: "DESFire", atr: atrDESFire, contactless: true, kind: nfctag.KindUnknown},
		{name: "Type B storage card", atr: typeB, contactless: true, kind: nfctag.KindUnknown},
		{name: "FeliCa", atr: felica, contactless: true, kind: nfctag.KindUnknown},
		{name: "truncated storage card", atr: atrNTAG[:14], contactless: true, kind: nfctag.KindUnknown},
		{name: "security key", atr: atrSecurityKey, kind: nfctag.KindUnknown},
		{name: "contact card", atr: []byte{0x3B, 0x9F, 0x95, 0x81, 0x31, 0xFE, 0x9F}, kind: nfctag.KindUnknown},
		{name: "empty", atr: nil, kind: nfctag.KindUnknown},
		{name: "too short", atr: []byte{0x3B, 0x80, 0x80}, kind: nfctag.KindUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.contactless, isContactlessATR(tt.atr))
			assert.Equal(t, tt.kind, tagKind(tt.atr))
		})
	}
}

func FuzzATR(f *testing.F) {
	seeds := [][]byte{atrNTAG, atrClassic1K, atrClassic4K, atrPlaceholder, atrDESFire, atrSecurityKey, nil}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, atr []byte) {
		kind := tagKind(atr)
		if kind != nfctag.KindUnknown && !isContactlessATR(atr) {
			t.Fatalf("% X was given a tag kind without being a contactless ATR", atr)
		}
	})
}

// scriptedCard answers each command with the next queued response.
type scriptedCard struct {
	err       error
	responses [][]byte
	sent      [][]byte
}

func (c *scriptedCard) Transmit(_ context.Context, apdu []byte) ([]byte, error) {
	c.sent = append(c.sent, append([]byte(nil), apdu...))
	if c.err != nil {
		return nil, c.err
	}
	if len(c.responses) == 0 {
		return nil, nil
	}
	resp := c.responses[0]
	c.responses = c.responses[1:]
	return resp, nil
}

func TestReadUID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, length := range []int{4, 7, 10} {
		uid := make([]byte, length)
		card := &scriptedCard{responses: [][]byte{sw(swSuccess, uid...)}}
		got, err := readUID(ctx, card)
		require.NoError(t, err)
		assert.Len(t, got, length)
		assert.Equal(t, [][]byte{{0xFF, 0xCA, 0x00, 0x00, 0x00}}, card.sent)
	}

	// The bR500 answers with success and no UID before it is ready.
	for _, length := range []int{0, 1, 5, 8, 16} {
		card := &scriptedCard{responses: [][]byte{sw(swSuccess, make([]byte, length)...)}}
		_, err := readUID(ctx, card)
		require.ErrorIs(t, err, errTagNotReady, "a UID of %d bytes", length)
	}

	_, err := readUID(ctx, &scriptedCard{responses: [][]byte{sw(0x6300)}})
	var statusErr *statusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, uint16(0x6300), statusErr.sw)
	assert.Equal(t, "get UID failed with status 6300", statusErr.Error())
}

func TestExchange_RejectsAnythingButSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	errGone := errors.New("gone")

	// Three readers, three codes for the same fault.
	for _, code := range []uint16{0x6982, 0x6A82, 0x6300, 0x9001, 0x6100} {
		_, err := exchange(ctx, &scriptedCard{responses: [][]byte{sw(code)}}, "read", []byte{0xFF, 0xB0, 0, 4, 16})
		var statusErr *statusError
		require.ErrorAs(t, err, &statusErr, "status %04X", code)
		assert.Equal(t, code, statusErr.sw)
	}

	_, err := exchange(ctx, &scriptedCard{responses: [][]byte{{0x90}}}, "read", []byte{0xFF})
	require.Error(t, err, "a response too short to hold a status")
	_, err = exchange(ctx, &scriptedCard{}, "read", []byte{0xFF})
	require.Error(t, err)
	_, err = exchange(ctx, &scriptedCard{err: errGone}, "read", []byte{0xFF})
	require.ErrorIs(t, err, errGone)
}

func TestAPDUTag_Commands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	block := make([]byte, 16)
	card := &scriptedCard{responses: [][]byte{
		sw(swSuccess, block...),
		sw(swSuccess),
		sw(swSuccess),
		sw(swSuccess),
		sw(swSuccess),
	}}
	tag := &apduTag{card: card}

	got, err := tag.ReadBlock(ctx, 4)
	require.NoError(t, err)
	assert.Equal(t, block, got)
	require.NoError(t, tag.WriteBlock(ctx, 4, []byte{1, 2, 3, 4}))
	require.NoError(t, tag.WriteBlock(ctx, 0x3C, block))
	require.NoError(t, tag.Authenticate(ctx, 4, nfctag.KeyB, nfctag.KeyNDEF))

	// The exact bytes sent to the readers on the bench.
	assert.Equal(t, [][]byte{
		{0xFF, 0xB0, 0x00, 0x04, 0x10},
		{0xFF, 0xD6, 0x00, 0x04, 0x04, 1, 2, 3, 4},
		append([]byte{0xFF, 0xD6, 0x00, 0x3C, 0x10}, block...),
		{0xFF, 0x82, 0x00, 0x00, 0x06, 0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7},
		{0xFF, 0x86, 0x00, 0x00, 0x05, 0x01, 0x00, 0x04, 0x61, 0x00},
	}, card.sent)
}

func TestAPDUTag_RejectsBadArguments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	card := &scriptedCard{}
	tag := &apduTag{card: card}

	_, err := tag.ReadBlock(ctx, 256)
	require.Error(t, err)
	_, err = tag.ReadBlock(ctx, -1)
	require.Error(t, err)
	require.Error(t, tag.WriteBlock(ctx, 256, []byte{1, 2, 3, 4}))
	require.Error(t, tag.WriteBlock(ctx, 4, nil))
	require.Error(t, tag.WriteBlock(ctx, 4, make([]byte, 17)))
	require.Error(t, tag.Authenticate(ctx, 256, nfctag.KeyA, nfctag.KeyNDEF))

	assert.Empty(t, card.sent, "nothing is sent for an argument that cannot be encoded")
}

func TestAPDUTag_AuthenticationFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	loadFails := &scriptedCard{responses: [][]byte{sw(0x6300)}}
	require.Error(t, (&apduTag{card: loadFails}).Authenticate(ctx, 4, nfctag.KeyA, nfctag.KeyNDEF))
	assert.Len(t, loadFails.sent, 1, "authentication is not attempted with a key that did not load")

	authFails := &scriptedCard{responses: [][]byte{sw(swSuccess), sw(0x6982)}}
	require.Error(t, (&apduTag{card: authFails}).Authenticate(ctx, 4, nfctag.KeyA, nfctag.KeyNDEF))
}
