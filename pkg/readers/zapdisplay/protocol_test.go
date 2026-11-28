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

package zapdisplay

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stallingPort returns (0, nil) a fixed number of times before delivering data,
// reproducing how go.bug.st/serial reports a read timeout.
type stallingPort struct {
	data   []byte
	mu     syncutil.Mutex
	stalls int
	reads  int
}

func (p *stallingPort) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	if p.stalls > 0 {
		p.stalls--
		return 0, nil
	}
	if len(p.data) == 0 {
		return 0, nil
	}
	n := copy(b, p.data)
	p.data = p.data[n:]
	return n, nil
}

func (*stallingPort) Write(b []byte) (int, error)        { return len(b), nil }
func (*stallingPort) Close() error                       { return nil }
func (*stallingPort) SetReadTimeout(time.Duration) error { return nil }

func TestLineReaderTreatsEmptyReadsAsTimeoutNotFailure(t *testing.T) {
	t.Parallel()

	// More than bufio's 100-empty-read limit, which is why this driver cannot
	// use bufio.Reader on a serial port.
	port := &stallingPort{stalls: 250, data: []byte("OK zapdisplay/1\r\n")}
	reader := newLineReader(port)

	line, err := reader.readLine(context.Background(), time.Now().Add(2*time.Second))
	require.NoError(t, err)
	assert.Equal(t, "OK zapdisplay/1", line, "carriage return must be stripped")
	assert.Greater(t, port.reads, 250)
}

func TestLineReaderTimesOutWithoutData(t *testing.T) {
	t.Parallel()

	reader := newLineReader(&stallingPort{stalls: 1 << 30})
	_, err := reader.readLine(context.Background(), time.Now().Add(20*time.Millisecond))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestLineReaderSplitsMultipleLinesFromOneRead(t *testing.T) {
	t.Parallel()

	reader := newLineReader(&stallingPort{data: []byte("OK\r\nPONG\r\n")})
	deadline := time.Now().Add(time.Second)

	first, err := reader.readLine(context.Background(), deadline)
	require.NoError(t, err)
	second, err := reader.readLine(context.Background(), deadline)
	require.NoError(t, err)

	assert.Equal(t, "OK", first)
	assert.Equal(t, "PONG", second)
}

func TestExpectSkipsLogLinesAndBeacons(t *testing.T) {
	t.Parallel()

	noise := strings.Join([]string{
		"I (1234) display: Render scene: playing",
		"READY zapdisplay/1 fw=0.1.0 display=hd458002c40_320x960",
		"W (1240) zapdisplay.protocol: something",
		"OK",
	}, "\r\n") + "\r\n"

	sess := newSession(&stallingPort{data: []byte(noise)})
	line, err := sess.expect(context.Background(), "SHOW", time.Second, "OK")
	require.NoError(t, err)
	assert.Equal(t, "OK", line)
}

func TestExpectSkipsColouredLogLines(t *testing.T) {
	t.Parallel()

	noise := "\x1b[0;32mI (99) display: hello\x1b[0m\r\nOK\r\n"
	sess := newSession(&stallingPort{data: []byte(noise)})

	line, err := sess.expect(context.Background(), "SHOW", time.Second, "OK")
	require.NoError(t, err)
	assert.Equal(t, "OK", line)
}

func TestExpectSurfacesDeviceErrors(t *testing.T) {
	t.Parallel()

	sess := newSession(&stallingPort{data: []byte("ERR asset-missing\r\n")})
	_, err := sess.expect(context.Background(), "ASSET_USE cover x 192 288 rgb565", time.Second, "OK")

	require.Error(t, err)
	var protoErr *protocolError
	require.ErrorAs(t, err, &protoErr)
	assert.Equal(t, "asset-missing", protoErr.reason)
	assert.True(t, isAssetMissing(err))
}

func TestIsAssetMissingIgnoresOtherFailures(t *testing.T) {
	t.Parallel()

	assert.False(t, isAssetMissing(errors.New("boom")))
	assert.False(t, isAssetMissing(&protocolError{command: "SHOW", reason: "render-failed"}))
}

func TestExpectTimeoutReportsWhatItSaw(t *testing.T) {
	t.Parallel()

	sess := newSession(&stallingPort{data: []byte("SOMETHING ELSE\r\n")})
	_, err := sess.expect(context.Background(), "SHOW", 50*time.Millisecond, "OK")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SHOW")
	assert.Contains(t, err.Error(), "SOMETHING ELSE")
}

func TestWriteLineRejectsEmbeddedNewlines(t *testing.T) {
	t.Parallel()

	sess := newSession(&stallingPort{})
	require.Error(t, sess.writeLine("TITLE bad\ninjected"))
	require.Error(t, sess.writeLine("TITLE bad\rinjected"))
}

func TestUploadCoverRejectsSizesTheDeviceCannotHold(t *testing.T) {
	t.Parallel()

	// Any aspect within the device's bounds is legal now, so only an empty
	// buffer or one that overruns the asset limit is rejected up front.
	sess := newSession(&stallingPort{})
	err := sess.uploadCover(context.Background(), "abc", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bytes")

	err = sess.uploadCover(context.Background(), "abc", make([]byte, coverMaxBytes+2))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bytes")
}

func TestDisplayFieldFoldsAccentsAndDropsUnrenderable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{"accents folded to ascii", "Pokémon Rouge", "Pokemon Rouge"},
		{"plain ascii untouched", "Super Metroid", "Super Metroid"},
		{"unrenderable dropped", "Ys Ⅰ ドラゴン", "Ys"},
		{"control characters removed", "Bad\x07Title", "BadTitle"},
		{"whitespace collapsed", "  Sonic   the   Hedgehog  ", "Sonic the Hedgehog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, displayField(tc.input, maxTitleLen, "UNKNOWN"))
		})
	}
}

func TestDisplayFieldFallsBackWhenNothingRenderable(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "UNKNOWN", displayField("", maxTitleLen, "UNKNOWN"))
	assert.Equal(t, "UNKNOWN", displayField("ドラゴン", maxTitleLen, "UNKNOWN"))
}

func TestDisplayFieldTruncatesToDeviceBuffer(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 200)
	assert.Len(t, displayField(long, maxTitleLen, "UNKNOWN"), maxTitleLen)
	assert.Len(t, displayField(long, maxSystemLen, "UNKNOWN"), maxSystemLen)
}

func TestMediaKeyDistinguishesIdleFromMedia(t *testing.T) {
	t.Parallel()

	assert.Empty(t, mediaKey(nil))
	assert.NotEmpty(t, mediaKey(testMedia("snes", "Super Metroid")))
	assert.NotEqual(t,
		mediaKey(testMedia("snes", "Super Metroid")),
		mediaKey(testMedia("snes", "Chrono Trigger")),
	)
}
