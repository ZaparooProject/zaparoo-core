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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawWriteFailPort passes commands through to a fake device but fails any
// write that is not a newline-terminated command: the binary half of a chunk.
type rawWriteFailPort struct {
	*fakeDevice
}

func (p rawWriteFailPort) Write(b []byte) (int, error) {
	if len(b) > 0 && b[len(b)-1] != '\n' {
		return 0, errors.New("usb transfer failed")
	}
	return p.fakeDevice.Write(b)
}

// newRenderSession builds a session on dev with the given features, the state
// claim would leave it in, without going through Open.
func newRenderSession(dev serialPort, features ...string) *session {
	sess := newSession(dev)
	sess.features = map[string]bool{}
	for _, feature := range features {
		sess.features[feature] = true
	}
	return sess
}

func TestRenderStopsAtTheFirstRejectedCommand(t *testing.T) {
	t.Parallel()

	media := testMedia("snes", "Super Metroid")
	media.Started = time.Now().Add(-time.Minute)

	renders := map[string]func(r *Reader, sess *session) error{
		"idle": func(r *Reader, sess *session) error {
			return r.renderIdle(context.Background(), sess)
		},
		"launching": func(r *Reader, sess *session) error {
			return r.renderLaunching(context.Background(), sess, media)
		},
		"playing": func(r *Reader, sess *session) error {
			return r.renderPlaying(context.Background(), sess, media, time.Now())
		},
	}

	for _, tc := range []struct {
		render string
		reject string
	}{
		{"idle", "CLEAR"},
		{"idle", "SCENE"},
		{"idle", "STATUS"},
		{"idle", "SHOW"},
		{"launching", "CLEAR"},
		{"launching", "SCENE"},
		{"launching", "TITLE"},
		{"launching", "SYSTEM"},
		{"launching", "STATUS"},
		{"launching", "SHOW"},
		{"playing", "CLEAR"},
		{"playing", "SCENE"},
		{"playing", "TITLE"},
		{"playing", "SYSTEM"},
		{"playing", "STATUS"},
		{"playing", "ELAPSED"},
		{"playing", "SHOW"},
	} {
		t.Run(tc.render+"/"+tc.reject, func(t *testing.T) {
			t.Parallel()

			dev := newFakeDevice()
			dev.rejectCommand(tc.reject)
			r := NewReader(&config.Instance{})
			r.clock = clockwork.NewFakeClock()

			err := renders[tc.render](r, newRenderSession(dev, "clock"))
			require.Error(t, err)

			var protoErr *protocolError
			require.ErrorAs(t, err, &protoErr)
			assert.Equal(t, "rejected", protoErr.reason)
			assert.Contains(t, err.Error(), tc.reject)
			assert.Contains(t, err.Error(), "rejected")

			// A half-built scene is never shown, and nothing is sent after
			// the device refused a step.
			assert.Empty(t, dev.renderedScenes())
			commands := dev.commandLog()
			require.NotEmpty(t, commands)
			assert.True(t, strings.HasPrefix(commands[len(commands)-1], tc.reject),
				"last command %q should be the rejected %q", commands[len(commands)-1], tc.reject)
		})
	}
}

func TestIdleRendersEvenWhenTheClockIsRefused(t *testing.T) {
	t.Parallel()

	// The clock screen is optional: a device that will not take the time
	// still has to show the idle scene.
	dev := newFakeDevice()
	dev.rejectCommand("TIME ")
	r := NewReader(&config.Instance{})

	require.NoError(t, r.renderIdle(context.Background(), newRenderSession(dev, "clock")))

	assert.Equal(t, 1, dev.countCommands("TIME "))
	scene, ok := dev.lastScene()
	require.True(t, ok)
	assert.Equal(t, "idle", scene.scene)
	assert.Equal(t, "Ready", scene.status)
}

func TestIdleSendsTheWallClockWhenSupported(t *testing.T) {
	t.Parallel()

	dev := newFakeDeviceWithFeatures("scenes,assets-b64,quiet,settings,clock")
	r, _ := newTestReader(t, dev)
	before := time.Now().Unix()
	openTestReader(t, r)
	after := time.Now().Unix()

	commands := dev.commandLog()
	timeAt := indexOfPrefix(commands, "TIME ")
	require.GreaterOrEqual(t, timeAt, 0, "a clock-capable display must be told the time")

	fields := strings.Fields(commands[timeAt])
	require.Len(t, fields, 3)
	unix, err := strconv.ParseInt(fields[1], 10, 64)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, unix, before)
	assert.LessOrEqual(t, unix, after)
	_, offset := time.Now().Zone()
	assert.Equal(t, strconv.Itoa(offset), fields[2], "the UTC offset lets the device show local time")

	// The time is part of the idle scene, so it has to land before SHOW.
	assert.Less(t, timeAt, indexOf(commands, "SHOW"))
}

func TestIdleOmitsTheClockWithoutTheFeature(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	assert.Zero(t, dev.countCommands("TIME "), "older firmware does not know TIME")
}

func TestPlayingSendsElapsedTimeWhenSupported(t *testing.T) {
	t.Parallel()

	dev := newFakeDeviceWithFeatures("scenes,assets-b64,quiet,settings,clock")
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	media := testMedia("snes", "Super Metroid")
	media.Started = time.Now().Add(-90 * time.Second)
	require.NoError(t, r.OnMediaChange(media))
	waitForSettledScene(t, dev, sceneIs("playing"))

	commands := dev.commandLog()
	at := indexOfPrefix(commands, "ELAPSED ")
	require.GreaterOrEqual(t, at, 0, "the device counts on from the session start it is given")
	elapsed, err := strconv.Atoi(strings.TrimPrefix(commands[at], "ELAPSED "))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 90)
	assert.Less(t, elapsed, 120)
}

func TestPlayingOmitsElapsedForAnUnknownStart(t *testing.T) {
	t.Parallel()

	dev := newFakeDeviceWithFeatures("scenes,assets-b64,quiet,settings,clock")
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	// testMedia leaves Started zero: there is no session start to count from.
	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	waitForSettledScene(t, dev, sceneIs("playing"))

	assert.Zero(t, dev.countCommands("ELAPSED"))
}

func TestCoverUploadUsesRawChunksWhenSupported(t *testing.T) {
	t.Parallel()

	dev := newFakeDeviceWithFeatures("scenes,assets-b64,assets-raw,quiet,settings")
	r, _ := newTestReader(t, dev)
	r.SetArtworkSource(&stubArtwork{
		contentType: "image/png",
		data:        solidPNG(t, 200, 300, colorRed()),
	})
	openTestReader(t, r)

	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	scene := waitForSettledScene(t, dev, sceneIs("playing"))

	assert.NotEmpty(t, scene.coverID)
	assert.Equal(t, "Playing", scene.status)

	// 192x288 is 110592 bytes: three full 32 KiB raw chunks and a 12288-byte
	// tail, with no base64 fallback.
	assert.Zero(t, dev.countCommands("ASSET_CHUNK_DATA "))
	assert.Equal(t, 4, dev.countCommands("ASSET_CHUNK_RAW "))
	commands := dev.commandLog()
	assert.Contains(t, commands, "ASSET_CHUNK_RAW 0 32768")
	assert.Contains(t, commands, "ASSET_CHUNK_RAW 3 12288", "the final chunk carries only the remainder")
	assert.Equal(t, 192*288*2, dev.committedSize())
}

func TestRawUploadIsBinaryClean(t *testing.T) {
	t.Parallel()

	// Every byte value, including CR and LF, must arrive unmodified: older
	// firmware translated CR, which is the corruption raw mode must not have.
	const width, height = 200, 100
	pixels := make([]byte, width*height*2)
	for i := range pixels {
		pixels[i] = byte(i % 256)
	}

	dev := newFakeDevice()
	sess := newRenderSession(dev, "assets-raw")
	ctx := context.Background()

	require.NoError(t, sess.uploadCover(ctx, "cover1", pixels))
	require.NoError(t, sess.useCover(ctx, "cover1", width, height))

	assert.Equal(t, pixels, dev.committedPayload())
	commands := dev.commandLog()
	assert.Contains(t, commands, "ASSET_CHUNK_RAW 0 32768")
	assert.Contains(t, commands, "ASSET_CHUNK_RAW 1 7232")
	assert.Zero(t, dev.countCommands("ASSET_CHUNK_DATA "))
}

func TestRawUploadStopsWhenTheDeviceRejectsAChunk(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	dev.rejectCommand("ASSET_CHUNK_RAW")
	sess := newRenderSession(dev, "assets-raw")

	err := sess.uploadCover(context.Background(), "cover1", make([]byte, 64))
	var protoErr *protocolError
	require.ErrorAs(t, err, &protoErr)
	assert.Equal(t, "rejected", protoErr.reason)
	assert.Zero(t, dev.countCommands("ASSET_END"), "a failed upload is never committed")
}

func TestRawUploadSurfacesAFailedPayloadWrite(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	sess := newRenderSession(rawWriteFailPort{dev}, "assets-raw")

	err := sess.uploadCover(context.Background(), "cover1", make([]byte, 64))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write payload")
	assert.Zero(t, dev.countCommands("ASSET_END"), "a failed upload is never committed")
}
