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

package proto_test

import (
	"encoding/hex"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/ccid/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Raw descriptors read from sysfs on the three readers the driver was
// developed against.
const (
	descACR122U = "12011001000000082f07002214020102000109025d00010100806409040000030b00000036210001000702" +
		"000000a00f0000a00f000000002a000090d0030000000100000000000000000000400002000f010000000000000001" +
		"070581030800320705020240000107058202400001"
	descACR1552 = "12010002000000402f0703230501010203010902c30003010080fa09040000030b0000043621100100070300" +
		"000088130000f834000000d29d01008cee0c0000fa0100000000000000000000ba04040004020000ffff0000000107" +
		"050102400000070581024000000705840308000a09040100020b000005362110010001030000008813000088130000" +
		"00813400006889090000fa0100000000000000000000ba04040004020000ffff000000010705030240000007058302" +
		"40000009040200010300000609211101210122150007058603080007"
	descBR500 = "12011001000000086e09230602020102030109025d00010100809609040000030b0000003621100100070300" +
		"0000740e0000740e000000da26000048db040011040200000700000000000000be04040004020000ffff0000000107" +
		"0583024000000705040240000007058103080018"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(s)
	require.NoError(t, err)
	return raw
}

func TestParseInterfaces_ACR122U(t *testing.T) {
	t.Parallel()

	ifaces := proto.ParseInterfaces(mustHex(t, descACR122U))

	require.Len(t, ifaces, 1)
	assert.Equal(t, proto.Interface{
		Number:           0,
		BulkIn:           0x82,
		BulkOut:          0x02,
		Interrupt:        0x81,
		BulkInPacket:     64,
		MaxSlotIndex:     0,
		Protocols:        0x00000002,
		Features:         0x00020040,
		MaxMessageLength: 271,
	}, ifaces[0])
	assert.True(t, ifaces[0].APDULevel())
	assert.Equal(t, "short APDU", ifaces[0].ExchangeLevel())
}

func TestParseInterfaces_ACR1552HasTwoSmartCardInterfacesAndAKeyboard(t *testing.T) {
	t.Parallel()

	ifaces := proto.ParseInterfaces(mustHex(t, descACR1552))

	require.Len(t, ifaces, 2, "the keyboard interface also has a 0x21 descriptor and must not be taken for a reader")
	assert.Equal(t, uint8(0), ifaces[0].Number)
	assert.Equal(t, uint8(0x81), ifaces[0].BulkIn)
	assert.Equal(t, uint8(0x01), ifaces[0].BulkOut)
	assert.Equal(t, uint8(0x84), ifaces[0].Interrupt)
	assert.Equal(t, uint32(0x000404BA), ifaces[0].Features)
	assert.Equal(t, "extended APDU", ifaces[0].ExchangeLevel())

	assert.Equal(t, uint8(1), ifaces[1].Number)
	assert.Equal(t, uint8(0x83), ifaces[1].BulkIn)
	assert.Equal(t, uint8(0x03), ifaces[1].BulkOut)
	assert.Zero(t, ifaces[1].Interrupt)
}

func TestParseInterfaces_BR500(t *testing.T) {
	t.Parallel()

	ifaces := proto.ParseInterfaces(mustHex(t, descBR500))

	require.Len(t, ifaces, 1)
	assert.Equal(t, uint8(0x83), ifaces[0].BulkIn)
	assert.Equal(t, uint8(0x04), ifaces[0].BulkOut)
	assert.Equal(t, uint8(0x81), ifaces[0].Interrupt)
	assert.Equal(t, uint32(516), ifaces[0].MaxMessageLength)
	assert.True(t, ifaces[0].APDULevel())
}

func TestParseInterfaces_ExchangeLevels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		want     string
		features uint32
		apdu     bool
	}{
		{name: "character", features: 0x00000000, want: "character"},
		{name: "TPDU", features: 0x00010000, want: "TPDU"},
		{name: "short APDU", features: 0x00020000, want: "short APDU", apdu: true},
		{name: "extended APDU", features: 0x00040000, want: "extended APDU", apdu: true},
		{name: "reserved", features: 0x00030000, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			iface := proto.Interface{Features: tt.features}
			assert.Equal(t, tt.want, iface.ExchangeLevel())
			assert.Equal(t, tt.apdu, iface.APDULevel())
		})
	}
}

func TestParseInterfaces_IgnoresUnusableInput(t *testing.T) {
	t.Parallel()

	assert.Empty(t, proto.ParseInterfaces(nil))
	assert.Empty(t, proto.ParseInterfaces([]byte{0x00, 0x00, 0x00}), "a zero length must not loop forever")

	truncated := mustHex(t, descBR500)
	assert.Empty(t, proto.ParseInterfaces(truncated[:60]), "an interface cut off before its endpoints is unusable")

	// A smart card interface with a class descriptor and no bulk endpoints.
	noEndpoints := mustHex(t, descBR500)[:18+9+9+54]
	assert.Empty(t, proto.ParseInterfaces(noEndpoints))
}

func FuzzParseInterfaces(f *testing.F) {
	for _, seed := range []string{descACR122U, descACR1552, descBR500} {
		raw, err := hex.DecodeString(seed)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
	f.Add([]byte{0x09, 0x04, 0x00, 0x00, 0x00, 0x0B})

	f.Fuzz(func(t *testing.T, raw []byte) {
		for _, iface := range proto.ParseInterfaces(raw) {
			if iface.BulkIn == 0 || iface.BulkOut == 0 || iface.BulkInPacket > 0x07FF {
				t.Fatalf("interface %d returned without both bulk endpoints", iface.Number)
			}
		}
	})
}
