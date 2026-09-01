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
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
)

const testPort = "/dev/ttyACM-test"

func testMedia(systemID, name string) *models.ActiveMedia {
	return &models.ActiveMedia{
		SystemID:   systemID,
		SystemName: strings.ToUpper(systemID),
		Name:       name,
		Path:       "/games/" + systemID + "/" + name + ".rom",
	}
}

// stubArtwork stands in for Core's media image pipeline.
type stubArtwork struct {
	err         error
	clock       *clockwork.FakeClock
	contentType string
	data        []byte
	// resolveFor advances the fake clock, standing in for a slow lookup.
	resolveFor time.Duration
	calls      atomic.Int32
}

func (s *stubArtwork) Artwork(_ context.Context, _, _ string, _ int) (*readers.MediaArtwork, error) {
	s.calls.Add(1)
	if s.clock != nil && s.resolveFor > 0 {
		s.clock.Advance(s.resolveFor)
	}
	if s.err != nil {
		return nil, s.err
	}
	return &readers.MediaArtwork{ContentType: s.contentType, Data: s.data, TypeTag: "property:image-boxart"}, nil
}

// newTestReader builds a reader wired to dev and a fake clock, and guarantees
// it is closed so goleak can verify no goroutine survives the test.
func newTestReader(t *testing.T, dev *fakeDevice) (*Reader, *clockwork.FakeClock) {
	t.Helper()

	clock := clockwork.NewFakeClock()
	r := NewReader(&config.Instance{})
	r.clock = clock
	r.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return dev, nil
	}
	t.Cleanup(func() {
		require.NoError(t, r.Close())
	})
	return r, clock
}

func openTestReader(t *testing.T, r *Reader) {
	t.Helper()
	err := r.Open(config.ReadersConnect{Driver: driverID, Path: testPort}, nil, readers.OpenOpts{})
	require.NoError(t, err)
	require.True(t, r.Connected())
}

// waitForSettledScene blocks until the most recently rendered scene satisfies
// want, and returns it.
//
// The driver renders a launching frame before the playing one whenever artwork
// still has to be fetched, so a test that waits on a scene count can observe
// the intermediate frame. Waiting for the state under test instead keeps these
// assertions independent of how many frames it takes to get there.
func waitForSettledScene(t *testing.T, dev *fakeDevice, want func(fakeScene) bool) fakeScene {
	t.Helper()
	var last fakeScene
	require.Eventually(t, func() bool {
		scene, ok := dev.lastScene()
		if !ok {
			return false
		}
		last = scene
		return want(scene)
	}, 5*time.Second, time.Millisecond, "display never settled on the expected scene")
	return last
}

// waitForScenes blocks until the device has rendered at least n scenes.
func waitForScenes(t *testing.T, dev *fakeDevice, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return len(dev.renderedScenes()) >= n
	}, 5*time.Second, time.Millisecond, "expected at least %d rendered scenes", n)
}

func sceneIs(name string) func(fakeScene) bool {
	return func(s fakeScene) bool { return s.scene == name }
}

// countScenes returns how many rendered scenes have the given name.
func countScenes(dev *fakeDevice, name string) int {
	n := 0
	for _, s := range dev.renderedScenes() {
		if s.scene == name {
			n++
		}
	}
	return n
}

func TestMetadataAndCapabilities(t *testing.T) {
	t.Parallel()

	r := NewReader(&config.Instance{})
	meta := r.Metadata()

	assert.Equal(t, driverID, meta.ID)
	assert.False(t, meta.DefaultEnabled, "probing writes to serial ports, so stay opt-in")
	assert.True(t, meta.DefaultAutoDetect)
	assert.Equal(t, []string{driverID}, r.IDs())
	assert.Equal(t, []readers.Capability{readers.CapabilityDisplay}, r.Capabilities())
}

func TestWriteIsRejected(t *testing.T) {
	t.Parallel()

	r := NewReader(&config.Instance{})
	token, err := r.Write("**launch.random:snes")
	require.Error(t, err)
	assert.Nil(t, token)
	r.CancelWrite()
}

func TestNewReaderDoesNotStartGoroutines(t *testing.T) {
	t.Parallel()

	// Auto-detect builds every driver once a second and discards the ones it
	// does not use, so construction must be allocation-only. goleak in
	// TestMain is what actually enforces this.
	for range 50 {
		r := NewReader(&config.Instance{})
		require.NoError(t, r.Close())
	}
}

func TestOpenPerformsHandshakeAndRendersIdle(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	commands := dev.commandLog()
	require.GreaterOrEqual(t, len(commands), 3)
	assert.Equal(t, "HELLO "+protocolVersion, commands[0])
	assert.Equal(t, "INFO", commands[1])
	assert.Equal(t, "QUIET", commands[2], "the READY beacon must be silenced before rendering")

	scene, ok := dev.lastScene()
	require.True(t, ok, "opening should render an idle scene")
	assert.Equal(t, "idle", scene.scene)
	assert.Contains(t, r.Info(), "fw=")
}

func TestOpenRejectsWrongDriver(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)

	err := r.Open(config.ReadersConnect{Driver: "pn532", Path: testPort}, nil, readers.OpenOpts{})
	require.Error(t, err)
	assert.False(t, r.Connected())
	assert.Empty(t, dev.commandLog(), "a mismatched driver must not touch the port")
}

func TestOpenFailsCleanlyOnUnresponsivePort(t *testing.T) {
	t.Parallel()

	// A port that answers nothing: another device, or nothing at all.
	silent := &stallingPort{stalls: 1 << 30}
	r := NewReader(&config.Instance{})
	r.clock = clockwork.NewFakeClock()
	r.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return silent, nil
	}
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	err := r.Open(config.ReadersConnect{Driver: driverID, Path: testPort}, nil, readers.OpenOpts{})
	require.Error(t, err)
	assert.False(t, r.Connected())
}

func TestOnMediaChangeDoesNoSerialIO(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	// With writes gated, any serial I/O on this path would block forever.
	release := dev.gateWrites()
	defer release()

	done := make(chan error, 1)
	go func() { done <- r.OnMediaChange(testMedia("snes", "Super Metroid")) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("OnMediaChange blocked on the serial port; it runs on the media publish path")
	}
}

func TestOnMediaChangeCoalescesBursts(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	baseline := countScenes(dev, "playing")
	release := dev.gateWrites()

	for i := range 50 {
		require.NoError(t, r.OnMediaChange(testMedia("snes", fmt.Sprintf("Game %02d", i))))
	}
	release()

	waitForSettledScene(t, dev, func(s fakeScene) bool {
		return s.scene == "playing" && s.title == "Game 49"
	})

	rendered := countScenes(dev, "playing") - baseline
	assert.LessOrEqual(t, rendered, 2,
		"50 changes should collapse into at most one in-flight and one pending render")
}

func TestOnMediaChangeNilRendersIdle(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	waitForSettledScene(t, dev, sceneIs("playing"))

	// Stopping media must clear the display rather than leaving the last game
	// on screen forever.
	require.NoError(t, r.OnMediaChange(nil))
	waitForSettledScene(t, dev, sceneIs("idle"))

	assert.Contains(t, dev.commandLog(), "SCENE idle")
}

func TestOnMediaChangeRendersTitleAndSystem(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	// idle at open, then launching while the cover is fetched, then playing.
	scene := waitForSettledScene(t, dev, sceneIs("playing"))

	assert.Equal(t, "Super Metroid", scene.title)
	assert.Equal(t, "SNES", scene.system)
	assert.Empty(t, scene.coverID, "no artwork source means no cover")
	assert.Contains(t, scene.status, "no cover")
}

func TestRenderToleratesInterleavedDeviceLogs(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	// QUIET does not silence every firmware log line, so protocol responses
	// arrive interleaved with ESP-IDF output and must still be matched.
	dev.queueLogLine("I (12345) display: Render scene: playing")
	dev.queueLogLine("\x1b[0;33mW (12346) zapdisplay.protocol: noisy\x1b[0m")

	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	// idle at open, then launching while the cover is fetched, then playing.
	scene := waitForSettledScene(t, dev, sceneIs("playing"))

	assert.Equal(t, "Super Metroid", scene.title)
}

func TestOnMediaChangeRejectedWhenDisconnected(t *testing.T) {
	t.Parallel()

	r := NewReader(&config.Instance{})
	require.Error(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
}

func TestCoverUploadFramingMatchesDevice(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	r.SetArtworkSource(&stubArtwork{
		contentType: "image/png",
		data:        solidPNG(t, 200, 300, colorRed()),
	})
	openTestReader(t, r)

	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	// idle at open, then launching while the cover is fetched, then playing.
	scene := waitForSettledScene(t, dev, sceneIs("playing"))

	assert.NotEmpty(t, scene.coverID, "cover should have been selected")
	assert.Equal(t, "Playing", scene.status)

	// A 200x300 source fills the height and keeps its aspect: 192x288, which is
	// 110592 bytes. At 768 decoded bytes per chunk that is 144 whole chunks
	// with nothing left over.
	assert.Equal(t, 144, dev.countCommands("ASSET_CHUNK_DATA "))
	assert.Equal(t, 1, dev.countCommands("ASSET_BEGIN "))
	assert.Equal(t, 1, dev.countCommands("ASSET_END "))

	commands := dev.commandLog()
	assert.Contains(t, commands, "ASSET_CHUNK_DATA 143 1024", "final chunk is full")
	assert.Equal(t, 192*288*2, dev.committedSize())
	assert.Contains(t, commands, "ASSET_USE cover "+scene.coverID+" 192 288 rgb565")

	// CLEAR resets pending state, so it has to come before the fields.
	assert.Less(t, indexOf(commands, "CLEAR"), indexOf(commands, "SCENE playing"))
}

func TestCoverIsNotReuploadedForRepeatMedia(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	r.SetArtworkSource(&stubArtwork{contentType: "image/png", data: solidPNG(t, 200, 300, colorRed())})
	openTestReader(t, r)

	media := testMedia("snes", "Super Metroid")
	require.NoError(t, r.OnMediaChange(media))
	waitForScenes(t, dev, 2)

	require.NoError(t, r.OnMediaChange(nil))
	waitForScenes(t, dev, 3)

	// Returning to media the device already holds skips the launching frame:
	// re-selecting a committed cover is instant, so a "starting" scene would
	// only flash.
	require.NoError(t, r.OnMediaChange(media))
	waitForScenes(t, dev, 4)

	assert.Equal(t, 1, dev.countCommands("ASSET_BEGIN "), "the device still holds the cover; re-select it")
	assert.Equal(t, 2, dev.countCommands("ASSET_USE "))
}

func TestCoverReuploadedAfterDeviceForgetsIt(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	r.SetArtworkSource(&stubArtwork{contentType: "image/png", data: solidPNG(t, 200, 300, colorRed())})
	openTestReader(t, r)

	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	waitForSettledScene(t, dev, sceneIs("playing"))

	// The device rebooted: it kept the link but lost every asset.
	dev.forgetAssets()
	require.NoError(t, r.OnMediaChange(nil))
	waitForSettledScene(t, dev, sceneIs("idle"))
	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	scene := waitForSettledScene(t, dev, func(s fakeScene) bool {
		return s.scene == "playing" && s.coverID != ""
	})

	assert.NotEmpty(t, scene.coverID, "driver should recover by uploading the cover again")
	assert.Equal(t, 2, dev.countCommands("ASSET_BEGIN "))
}

func TestArtworkFailureStillRendersScene(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		source *stubArtwork
		name   string
	}{
		{name: "no artwork stored", source: &stubArtwork{err: readers.ErrNoArtwork}},
		{name: "lookup failed", source: &stubArtwork{err: errors.New("database is busy")}},
		{name: "undecodable image", source: &stubArtwork{contentType: "image/png", data: []byte("junk")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dev := newFakeDevice()
			r, _ := newTestReader(t, dev)
			r.SetArtworkSource(tc.source)
			openTestReader(t, r)

			require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
			scene := waitForSettledScene(t, dev, sceneIs("playing"))

			assert.Equal(t, "Super Metroid", scene.title)
			assert.Empty(t, scene.coverID)
			assert.Zero(t, dev.countCommands("ASSET_BEGIN "))
		})
	}
}

func TestRefreshResendsSceneWithoutReuploadingCover(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, clock := newTestReader(t, dev)
	r.SetArtworkSource(&stubArtwork{contentType: "image/png", data: solidPNG(t, 200, 300, colorRed())})
	openTestReader(t, r)

	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	waitForSettledScene(t, dev, sceneIs("playing"))

	uploadsBefore := dev.countCommands("ASSET_BEGIN ")
	scenesBefore := len(dev.renderedScenes())

	// The firmware's anti-retention offset only advances when the host
	// re-sends SHOW, so a long session needs a periodic nudge.
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(refreshInterval)
	require.Eventually(t, func() bool {
		return len(dev.renderedScenes()) > scenesBefore
	}, 5*time.Second, time.Millisecond, "the refresh should re-send the scene")

	assert.Equal(t, uploadsBefore, dev.countCommands("ASSET_BEGIN "), "a refresh must not re-upload artwork")
}

// panicArtwork stands in for anything in the render path that can panic. In
// production that is the PNG, JPEG and WebP decoders running on scraped files.
type panicArtwork struct{}

func (panicArtwork) Artwork(_ context.Context, _, _ string, _ int) (*readers.MediaArtwork, error) {
	panic("malformed cover")
}

func TestRenderPanicDoesNotKillTheProcess(t *testing.T) {
	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	r.SetArtworkSource(panicArtwork{})
	openTestReader(t, r)

	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))

	// A panic part way through a scene leaves the device stream desynced, so
	// the link is dropped and the service reconnects through a fresh
	// handshake. Reaching this assertion at all is the point: an unrecovered
	// panic on the worker goroutine would take the whole daemon down.
	require.Eventually(t, func() bool {
		return !r.Connected()
	}, 5*time.Second, time.Millisecond, "a panicking render should drop the link")
}

func TestLinkFailureMarksReaderDisconnected(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	dev.setWriteError(errors.New("input/output error"))
	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))

	// readerManager prunes readers that report themselves disconnected.
	require.Eventually(t, func() bool {
		return !r.Connected()
	}, 5*time.Second, time.Millisecond, "a dead link should mark the reader disconnected")
}

func TestCloseIsIdempotentAndReopenable(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	require.NoError(t, r.Close())
	require.NoError(t, r.Close(), "closing twice must be safe")
	assert.False(t, r.Connected())

	// readerManager reconnects a pruned reader on its next tick.
	reopened := newFakeDevice()
	r.portFactory = func(_ string, _ *serial.Mode) (testutils.SerialPort, error) {
		return reopened, nil
	}
	openTestReader(t, r)
	assert.True(t, r.Connected())
}

func TestCloseWithoutOpenIsSafe(t *testing.T) {
	t.Parallel()

	r := NewReader(&config.Instance{})
	require.NoError(t, r.Close())
}

func TestReaderIDIsStableForAPath(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	openTestReader(t, r)

	first := r.ReaderID()
	assert.Equal(t, first, r.ReaderID())
	assert.Contains(t, first, driverID)
}

func TestPathClaimedIgnoresOtherDriversPorts(t *testing.T) {
	t.Parallel()

	connected := []string{"pn532:/dev/ttyACM0", "zapdisplay:/dev/ttyACM1"}

	assert.True(t, pathClaimed(connected, "/dev/ttyACM0"), "another driver already owns this port")
	assert.True(t, pathClaimed(connected, "/dev/ttyACM1"))
	assert.False(t, pathClaimed(connected, "/dev/ttyACM2"))
}

func indexOf(values []string, want string) int {
	for i, v := range values {
		if v == want {
			return i
		}
	}
	return -1
}

func TestFastLoadSkipsLaunchingFrame(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, _ := newTestReader(t, dev)
	// The fake clock never advances during the resolve, so this is the fastest
	// possible load.
	r.SetArtworkSource(&stubArtwork{contentType: "image/png", data: solidPNG(t, 200, 300, colorRed())})
	openTestReader(t, r)

	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	waitForSettledScene(t, dev, sceneIs("playing"))

	// A raw-chunk upload finishes quickly enough that a launching frame would
	// read as a flash rather than as feedback.
	assert.Zero(t, countScenes(dev, "launching"), "a fast load should go straight to playing")
	assert.NotContains(t, dev.commandLog(), "SCENE launching")
}

func TestSlowLoadShowsLaunchingFrame(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, clock := newTestReader(t, dev)
	r.SetArtworkSource(&stubArtwork{
		contentType: "image/png",
		data:        solidPNG(t, 200, 300, colorRed()),
		clock:       clock,
		resolveFor:  launchingDelay + 50*time.Millisecond,
	})
	openTestReader(t, r)

	require.NoError(t, r.OnMediaChange(testMedia("snes", "Super Metroid")))
	scene := waitForSettledScene(t, dev, sceneIs("playing"))

	// Once the work has genuinely taken a while, the display should not sit on
	// the previous game while the cover uploads.
	assert.Equal(t, 1, countScenes(dev, "launching"), "a slow resolve should be acknowledged")
	assert.Equal(t, "Super Metroid", scene.title)

	commands := dev.commandLog()
	launching := indexOf(commands, "SCENE launching")
	upload := indexOfPrefix(commands, "ASSET_BEGIN ")
	require.NotEqual(t, -1, launching)
	require.NotEqual(t, -1, upload)
	assert.Less(t, launching, upload,
		"the launching frame must land before the upload it is covering for")
}

func TestLaunchingFrameSkippedForCachedCover(t *testing.T) {
	t.Parallel()

	dev := newFakeDevice()
	r, clock := newTestReader(t, dev)
	r.SetArtworkSource(&stubArtwork{
		contentType: "image/png",
		data:        solidPNG(t, 200, 300, colorRed()),
		clock:       clock,
		resolveFor:  launchingDelay + 50*time.Millisecond,
	})
	openTestReader(t, r)

	media := testMedia("snes", "Super Metroid")
	require.NoError(t, r.OnMediaChange(media))
	waitForSettledScene(t, dev, sceneIs("playing"))
	require.NoError(t, r.OnMediaChange(nil))
	waitForSettledScene(t, dev, sceneIs("idle"))

	before := countScenes(dev, "launching")
	require.NoError(t, r.OnMediaChange(media))
	waitForSettledScene(t, dev, sceneIs("playing"))

	// Re-selecting a cover the device already holds does no resolve and no
	// upload, so there is nothing to acknowledge however slow the source is.
	assert.Equal(t, before, countScenes(dev, "launching"))
}

// indexOfPrefix returns the position of the first command starting with want.
func indexOfPrefix(values []string, want string) int {
	for i, v := range values {
		if strings.HasPrefix(v, want) {
			return i
		}
	}
	return -1
}
