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
	"encoding/base64"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// fakeScene is the state the device would have rendered on a SHOW.
type fakeScene struct {
	scene   string
	title   string
	system  string
	status  string
	message string
	accent  string
	coverID string
}

// fakeDevice simulates the zapdisplay firmware over an in-memory serial port.
//
// It implements the same command set, asset state machine and CRC checks as the
// real device, so the driver can be exercised end to end without hardware. It
// also reproduces the two transport quirks that matter: responses terminated
// with CRLF, and reads that return (0, nil) when there is nothing to deliver.
type fakeDevice struct {
	// writeErr and failNextAssetUse inject faults: a dead link and a device
	// that has forgotten the asset it was asked to show.
	writeErr error
	// gate, when non-nil, blocks every Write until it is closed. Tests use it
	// to prove the driver does no serial I/O on the media notify path.
	gate chan struct{}
	// pending is the scene being assembled; it is committed on SHOW.
	pending          fakeScene
	committedID      string
	failNextAssetUse string
	stagingID        string
	stagingMime      string
	chunkBuf         []byte
	stagingData      []byte
	partial          []byte
	// commands records every complete command line received, in order.
	commands []string
	scenes   []fakeScene
	out      []byte
	// unsolicited is emitted before the next response, standing in for the
	// ESP-IDF log output that shares the serial stream.
	unsolicited    []string
	committedBytes int
	// awaitingBytes is non-zero while the device is mid-chunk, mirroring the
	// firmware's blocking read after OK ASSET_CHUNK_DATA.
	awaitingBytes int
	awaitingIndex int
	stagingBytes  int
	nextChunk     int
	committedW    int
	committedH    int
	gateMu        syncutil.RWMutex
	mu            syncutil.Mutex
	stagingCRC    uint32
	stagingDone   bool
	closed        bool
	// awaitingRaw distinguishes a binary chunk from a base64 one; the raw path
	// exists because the firmware's console passes bytes through unmodified.
	awaitingRaw bool
}

func newFakeDevice() *fakeDevice {
	return &fakeDevice{}
}

func (f *fakeDevice) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, errors.New("port closed")
	}
	if len(f.out) == 0 {
		// Read timeout: the real driver library reports this as (0, nil).
		return 0, nil
	}
	n := copy(p, f.out)
	f.out = f.out[n:]
	return n, nil
}

func (f *fakeDevice) Write(p []byte) (int, error) {
	f.gateMu.RLock()
	gate := f.gate
	f.gateMu.RUnlock()
	if gate != nil {
		<-gate
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, errors.New("port closed")
	}
	if f.writeErr != nil {
		return 0, f.writeErr
	}

	for _, b := range p {
		if f.awaitingBytes > 0 {
			f.chunkBuf = append(f.chunkBuf, b)
			f.awaitingBytes--
			if f.awaitingBytes == 0 {
				f.finishChunkLocked()
			}
			continue
		}
		if b == '\n' {
			line := strings.TrimRight(string(f.partial), "\r")
			f.partial = f.partial[:0]
			f.handleLineLocked(line)
			continue
		}
		f.partial = append(f.partial, b)
	}
	return len(p), nil
}

func (*fakeDevice) SetReadTimeout(time.Duration) error { return nil }

func (f *fakeDevice) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// reply queues a response the way the device emits it: LF written by the
// firmware, converted to CRLF by the USB Serial/JTAG console.
func (f *fakeDevice) replyLocked(format string, args ...any) {
	for _, line := range f.unsolicited {
		f.out = append(f.out, []byte(line+"\r\n")...)
	}
	f.unsolicited = nil
	f.out = append(f.out, []byte(fmt.Sprintf(format, args...)+"\r\n")...)
}

func (f *fakeDevice) finishChunkLocked() {
	raw := f.awaitingRaw
	f.awaitingRaw = false

	decoded := f.chunkBuf
	if !raw {
		var err error
		decoded, err = base64.StdEncoding.DecodeString(string(f.chunkBuf))
		if err != nil {
			f.chunkBuf = nil
			f.replyLocked("ERR bad-base64")
			return
		}
	}
	f.chunkBuf = nil
	if len(f.stagingData)+len(decoded) > f.stagingBytes {
		f.replyLocked("ERR overflow")
		return
	}
	f.stagingData = append(f.stagingData, decoded...)
	f.nextChunk++
	f.replyLocked("OK ASSET_CHUNK %d", f.awaitingIndex)
}

//nolint:gocritic // a command dispatch table would be less readable than this chain
func (f *fakeDevice) handleLineLocked(line string) {
	if line == "" {
		return
	}
	f.commands = append(f.commands, line)

	switch {
	case line == "HELLO "+protocolVersion:
		f.replyLocked("OK " + protocolVersion)
	case line == "INFO":
		f.replyLocked("INFO %s fw=0.1.0 display=hd458002c40 visible=320x960 transport=cdc "+
			"features=scenes,assets-b64,quiet", protocolVersion)
	case line == "PING":
		f.replyLocked("PONG")
	case line == "QUIET", line == "VERBOSE":
		f.replyLocked("OK")
	case line == "CLEAR":
		f.pending = fakeScene{}
		f.replyLocked("OK")
	case line == "SHOW":
		f.scenes = append(f.scenes, f.pending)
		f.replyLocked("OK")
	case strings.HasPrefix(line, "SCENE "):
		f.handleSceneLocked(strings.TrimPrefix(line, "SCENE "))
	case strings.HasPrefix(line, "TITLE "):
		f.pending.title = truncate(strings.TrimPrefix(line, "TITLE "), maxTitleLen)
		f.replyLocked("OK")
	case strings.HasPrefix(line, "SYSTEM "):
		f.pending.system = truncate(strings.TrimPrefix(line, "SYSTEM "), maxSystemLen)
		f.replyLocked("OK")
	case strings.HasPrefix(line, "STATUS "):
		f.pending.status = truncate(strings.TrimPrefix(line, "STATUS "), maxStatusLen)
		f.replyLocked("OK")
	case strings.HasPrefix(line, "MESSAGE "):
		f.pending.message = truncate(strings.TrimPrefix(line, "MESSAGE "), maxMessageLen)
		f.replyLocked("OK")
	case strings.HasPrefix(line, "ACCENT "):
		f.handleAccentLocked(strings.TrimPrefix(line, "ACCENT "))
	case strings.HasPrefix(line, "ELAPSED"), strings.HasPrefix(line, "TIME "):
		f.replyLocked("OK")
	case strings.HasPrefix(line, "ASSET_CHUNK_RAW "):
		f.handleRawChunkHeaderLocked(strings.TrimPrefix(line, "ASSET_CHUNK_RAW "))
	case strings.HasPrefix(line, "ASSET_BEGIN "):
		f.handleAssetBeginLocked(strings.TrimPrefix(line, "ASSET_BEGIN "))
	case strings.HasPrefix(line, "ASSET_CHUNK_DATA "):
		f.handleChunkHeaderLocked(strings.TrimPrefix(line, "ASSET_CHUNK_DATA "))
	case strings.HasPrefix(line, "ASSET_END "):
		f.handleAssetEndLocked(strings.TrimPrefix(line, "ASSET_END "))
	case strings.HasPrefix(line, "ASSET_USE "):
		f.handleAssetUseLocked(strings.TrimPrefix(line, "ASSET_USE "))
	default:
		f.replyLocked("ERR unknown-command")
	}
}

func (f *fakeDevice) handleSceneLocked(name string) {
	switch name {
	case "idle", "launching", "playing", "error":
		f.pending.scene = name
		f.replyLocked("OK")
	default:
		f.replyLocked("ERR bad-scene")
	}
}

func (f *fakeDevice) handleAccentLocked(value string) {
	trimmed := strings.TrimPrefix(value, "#")
	if len(trimmed) != 6 {
		f.replyLocked("ERR bad-color")
		return
	}
	for _, c := range trimmed {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			f.replyLocked("ERR bad-color")
			return
		}
	}
	f.pending.accent = value
	f.replyLocked("OK")
}

func (f *fakeDevice) handleAssetBeginLocked(args string) {
	parts := strings.Fields(args)
	if len(parts) != 4 {
		f.replyLocked("ERR bad-asset-begin")
		return
	}
	size, err := strconv.Atoi(parts[2])
	if err != nil || size <= 0 {
		f.replyLocked("ERR bad-asset-begin")
		return
	}
	crc, err := strconv.ParseUint(parts[3], 16, 32)
	if err != nil {
		f.replyLocked("ERR bad-asset-begin")
		return
	}
	f.stagingID = parts[0]
	f.stagingMime = parts[1]
	f.stagingBytes = size
	f.stagingCRC = uint32(crc)
	f.stagingData = nil
	f.stagingDone = false
	f.nextChunk = 0
	f.replyLocked("OK ASSET_BEGIN")
}

func (f *fakeDevice) handleChunkHeaderLocked(args string) {
	parts := strings.Fields(args)
	if len(parts) != 2 {
		f.replyLocked("ERR bad-asset-chunk")
		return
	}
	index, err1 := strconv.Atoi(parts[0])
	length, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || length <= 0 || length%4 != 0 {
		f.replyLocked("ERR bad-asset-chunk")
		return
	}
	if index != f.nextChunk {
		f.replyLocked("ERR bad-chunk-index")
		return
	}
	f.awaitingIndex = index
	f.awaitingBytes = length
	f.chunkBuf = nil
	f.replyLocked("OK ASSET_CHUNK_DATA")
}

// handleRawChunkHeaderLocked mirrors the firmware's binary-clean path: the
// length is arbitrary and the bytes go straight into the asset buffer.
func (f *fakeDevice) handleRawChunkHeaderLocked(args string) {
	parts := strings.Fields(args)
	if len(parts) != 2 {
		f.replyLocked("ERR bad-asset-chunk")
		return
	}
	index, err1 := strconv.Atoi(parts[0])
	length, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || length <= 0 {
		f.replyLocked("ERR bad-asset-chunk")
		return
	}
	if index != f.nextChunk {
		f.replyLocked("ERR bad-chunk-index")
		return
	}
	f.awaitingIndex = index
	f.awaitingBytes = length
	f.awaitingRaw = true
	f.chunkBuf = nil
	f.replyLocked("OK ASSET_CHUNK_RAW")
}

func (f *fakeDevice) handleAssetEndLocked(id string) {
	switch {
	case f.stagingID == "" || id != f.stagingID:
		f.replyLocked("ERR bad-asset-id")
	case len(f.stagingData) != f.stagingBytes:
		f.replyLocked("ERR bad-asset-size")
	case crc32.ChecksumIEEE(f.stagingData) != f.stagingCRC:
		f.replyLocked("ERR crc")
	default:
		f.stagingDone = true
		f.replyLocked("OK ASSET %s bytes=%d crc=%08x", id, len(f.stagingData), f.stagingCRC)
	}
}

func (f *fakeDevice) handleAssetUseLocked(args string) {
	parts := strings.Fields(args)
	if len(parts) != 5 || parts[0] != "cover" || parts[4] != "rgb565" {
		f.replyLocked("ERR bad-asset-use")
		return
	}
	id := parts[1]
	width, errW := strconv.Atoi(parts[2])
	height, errH := strconv.Atoi(parts[3])
	if errW != nil || errH != nil || width <= 0 || height <= 0 ||
		width > coverMaxWidth || height > coverMaxHeight {
		f.replyLocked("ERR bad-asset-size")
		return
	}
	f.committedW = width
	f.committedH = height

	if f.failNextAssetUse != "" {
		reason := f.failNextAssetUse
		f.failNextAssetUse = ""
		f.replyLocked("ERR %s", reason)
		return
	}

	// Already committed: re-selectable without another upload.
	if f.committedID != "" && f.committedID == id {
		f.pending.coverID = id
		f.replyLocked("OK")
		return
	}
	if !f.stagingDone || f.stagingID != id {
		f.replyLocked("ERR asset-missing")
		return
	}
	if f.stagingMime != "image/rgb565" {
		f.replyLocked("ERR bad-asset-mime")
		return
	}
	// The device accepts any aspect within its bounds, so the declared size is
	// what the payload has to match, not a fixed slot.
	if len(f.stagingData) != width*height*2 {
		f.replyLocked("ERR bad-asset-size")
		return
	}
	f.committedID = id
	f.committedBytes = len(f.stagingData)
	f.stagingDone = false
	f.pending.coverID = id
	f.replyLocked("OK")
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// --- test helpers ---

func (f *fakeDevice) commandLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.commands))
	copy(out, f.commands)
	return out
}

func (f *fakeDevice) renderedScenes() []fakeScene {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeScene, len(f.scenes))
	copy(out, f.scenes)
	return out
}

func (f *fakeDevice) lastScene() (fakeScene, bool) {
	scenes := f.renderedScenes()
	if len(scenes) == 0 {
		return fakeScene{}, false
	}
	return scenes[len(scenes)-1], true
}

func (f *fakeDevice) countCommands(prefix string) int {
	count := 0
	for _, cmd := range f.commandLog() {
		if strings.HasPrefix(cmd, prefix) {
			count++
		}
	}
	return count
}

// forgetAssets simulates the device rebooting: it keeps the link but loses
// every uploaded asset, which is the real cause of ERR asset-missing.
func (f *fakeDevice) forgetAssets() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.committedID = ""
	f.stagingDone = false
	f.stagingID = ""
}

func (f *fakeDevice) queueLogLine(line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unsolicited = append(f.unsolicited, line)
}

// gateWrites blocks all writes until the returned function is called.
func (f *fakeDevice) gateWrites() func() {
	gate := make(chan struct{})
	f.gateMu.Lock()
	f.gate = gate
	f.gateMu.Unlock()

	return func() {
		f.gateMu.Lock()
		f.gate = nil
		f.gateMu.Unlock()
		close(gate)
	}
}

func (f *fakeDevice) setWriteError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeErr = err
}

// committedSize returns the byte length of the cover the device is holding.
func (f *fakeDevice) committedSize() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.committedBytes
}
