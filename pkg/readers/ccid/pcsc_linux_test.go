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

package ccid

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyPCSC(t *testing.T) {
	t.Parallel()

	require.NoError(t, classifyPCSC(pcscSuccess, "list readers"))

	for _, code := range []pcscError{pcscUnknownReader, pcscReaderUnavailable, pcscNoService, pcscServiceStopped} {
		err := classifyPCSC(code, "reader status")
		require.ErrorIs(t, err, errReaderGone, "code %08X", uint32(code))
		require.ErrorIs(t, err, code)
		assert.Contains(t, err.Error(), "reader status")
	}

	// "Card was removed": the reader is still there.
	removed := pcscError(0x80100069)
	err := classifyPCSC(removed, "transmit")
	require.ErrorIs(t, err, removed)
	require.NotErrorIs(t, err, errReaderGone)
}

func TestPCSCError_Message(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "PC/SC service is not running", pcscError(pcscNoService).Error())
	assert.Equal(t, "PC/SC service has stopped", pcscError(pcscServiceStopped).Error())
	assert.Equal(t, "PC/SC error 0x8010006A", pcscError(0x8010006A).Error())
}

func TestSplitReaderNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{"ACS ACR122U PICC Interface 00 00", "Feitian bR500 (VER03) 01 00"},
		splitReaderNames([]byte("ACS ACR122U PICC Interface 00 00\x00Feitian bR500 (VER03) 01 00\x00\x00")))
	assert.Empty(t, splitReaderNames([]byte{0x00}))
	assert.Empty(t, splitReaderNames(nil))
}

// This runs against whatever the machine has. With no library, or a library
// and no service, establishing a context fails and says why. With a service
// it works, and a reader that does not exist is reported as gone.
func TestEstablishPCSC_OnThisMachine(t *testing.T) {
	t.Parallel()

	ctx, err := establishPCSC()
	if err != nil {
		var code pcscError
		if !errors.Is(err, errNoPCSCLibrary) && !errors.As(err, &code) {
			t.Fatalf("unexpected error establishing a context: %v", err)
		}
		require.NotErrorIs(t, err, errReaderGone, "no service is not a missing reader")
		return
	}
	defer func() { require.NoError(t, ctx.Release()) }()

	_, err = ctx.ListReaders()
	require.NoError(t, err, "no readers attached is an empty list, not an error")

	const missing = "Zaparoo Test Reader That Does Not Exist 00 00"
	_, err = ctx.Status(missing)
	require.ErrorIs(t, err, errReaderGone)
	_, err = ctx.Connect(missing)
	require.Error(t, err)
}
