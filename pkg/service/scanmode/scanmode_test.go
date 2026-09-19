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

package scanmode

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testReaderID = "pn532-1234567890abcdef"

// newHoldReaderOnTapConfig is a tap device with one reader configured for hold.
func newHoldReaderOnTapConfig(t *testing.T) *config.Instance {
	t.Helper()
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	cfg.SetScanMode(config.ScanModeTap)
	require.NoError(t, cfg.LoadTOML(`
[[readers.connect]]
driver = "pn532"
path = "/dev/ttyUSB0"
scan_mode = "hold"
`))
	return cfg
}

func newHoldReader() *mocks.MockReader {
	r := mocks.NewMockReader()
	r.On("Metadata").Return(readers.DriverMetadata{ID: "pn532"})
	r.On("IDs").Return([]string{"pn532"})
	r.On("Path").Return("/dev/ttyUSB0")
	return r
}

func TestResolve(t *testing.T) {
	t.Parallel()

	cfg := newHoldReaderOnTapConfig(t)
	present := func(string) (readers.Reader, bool) { return newHoldReader(), true }
	gone := func(string) (readers.Reader, bool) { return nil, false }
	readerToken := &tokens.Token{Source: tokens.SourceReader, ReaderID: testReaderID}
	tapToken := &tokens.Token{
		Source: tokens.SourceReader, ReaderID: testReaderID,
		Traits: tokens.ResolveTraits(map[string]any{tokens.TraitTap: true}),
	}

	tests := []struct {
		lookup      readerLookup
		token       *tokens.Token
		name        string
		want        string
		keepRemoved bool
	}{
		{name: "nil token uses global", lookup: present, want: config.ScanModeTap},
		{
			name: "token without reader uses global", lookup: present,
			token: &tokens.Token{Source: tokens.SourceReader}, want: config.ScanModeTap,
		},
		{name: "present reader uses its override", lookup: present, token: readerToken, want: config.ScanModeHold},
		{name: "gone reader falls back to global", lookup: gone, token: readerToken, want: config.ScanModeTap},
		{
			name: "gone reader keeps hold after removal", lookup: gone, token: readerToken,
			keepRemoved: true, want: config.ScanModeHold,
		},
		{
			name: "token trait wins over reader", lookup: present, token: tapToken,
			keepRemoved: true, want: config.ScanModeTap,
		},
		{
			name: "token trait wins over gone reader", lookup: gone, token: tapToken,
			keepRemoved: true, want: config.ScanModeTap,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, resolve(cfg, tt.lookup, tt.token, tt.keepRemoved))
		})
	}
}

// TestResolveAfterRemoval_ReaderDisconnectingMidResolve is the exit timer
// firing while the owner's reader disconnects: the reader is present for the
// first lookup and gone for any after it. Looking it up twice read the second
// answer as "no reader" and fell back to the global tap mode, so the media the
// hold reader launched never exited.
func TestResolveAfterRemoval_ReaderDisconnectingMidResolve(t *testing.T) {
	t.Parallel()

	cfg := newHoldReaderOnTapConfig(t)
	lookups := 0
	lookup := func(string) (readers.Reader, bool) {
		lookups++
		if lookups == 1 {
			return newHoldReader(), true
		}
		return nil, false
	}

	token := &tokens.Token{Source: tokens.SourceReader, ReaderID: testReaderID}
	assert.Equal(t, config.ScanModeHold, resolve(cfg, lookup, token, true))
	assert.Equal(t, 1, lookups)
}
