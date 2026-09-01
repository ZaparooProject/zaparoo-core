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
	"encoding/base64"
	"errors"
	"fmt"
	"hash/crc32"
	"regexp"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
)

const (
	// protocolVersion is the wire contract this driver speaks. The device
	// echoes it back during the handshake and it is the only reliable way to
	// identify the accessory: it enumerates with Espressif's stock USB ID and
	// no product string.
	protocolVersion = "zapdisplay/1"

	baudRate    = 115200
	readTimeout = 250 * time.Millisecond

	// handshakeTimeout bounds the whole claim sequence during detection. Auto
	// detect probes once a second, so a wedged port must not hold it up.
	handshakeTimeout = 2 * time.Second
	// commandTimeout covers an ordinary command. SHOW is the slow one: the
	// device answers only after it has drawn a full 400x960 frame.
	commandTimeout = 5 * time.Second
	// uploadTimeout covers a single asset chunk exchange.
	uploadTimeout = 8 * time.Second

	// Artwork bounds, mirroring the firmware's main/ui/state.h. Artwork is not
	// a fixed slot: box art is portrait on some systems and landscape on
	// others, so the device is sent whatever aspect the source has and lays out
	// its detail column around it.
	coverMaxWidth  = 512
	coverMaxHeight = 288
	coverMaxBytes  = coverMaxWidth * coverMaxHeight * 2

	// chunkDecodedBytes is the payload per base64 asset chunk. The device
	// decodes into a 768 byte buffer and requires the base64 length to be a
	// multiple of 4, so 768 decoded bytes (1024 encoded) fills it exactly.
	chunkDecodedBytes = 768

	// chunkRawBytes is the payload per raw chunk on firmware that advertises
	// "assets-raw". Raw bytes go straight into the asset buffer, so the device
	// imposes no size limit and this is chosen purely to amortise the per-chunk
	// round trip: throughput flattens around here, and raw transfer is roughly
	// two and a half times faster than base64 end to end.
	chunkRawBytes = 32768

	// Field lengths the device silently truncates at, minus the terminator.
	maxTitleLen   = 63
	maxSystemLen  = 47
	maxStatusLen  = 47
	maxMessageLen = 95
)

// errNotZapDisplay reports that a port answered, but not as a display.
var errNotZapDisplay = errors.New("not a zapdisplay device")

// protocolError is an ERR response from the device. The reason is matched
// against known values, so callers can react to a missing asset without
// string-matching the whole message themselves.
type protocolError struct {
	command string
	reason  string
}

func (e *protocolError) Error() string {
	return fmt.Sprintf("zapdisplay %q failed: %s", e.command, e.reason)
}

// isAssetMissing reports whether err is the device saying it does not hold the
// asset the host asked it to display, which happens after it reboots.
func isAssetMissing(err error) bool {
	var protoErr *protocolError
	return errors.As(err, &protoErr) && protoErr.reason == "asset-missing"
}

// logLinePattern matches ESP-IDF log output, which shares the serial stream
// with protocol responses. Lines like "I (1234) display: Render scene: idle"
// are skipped wherever a response is expected.
var logLinePattern = regexp.MustCompile(`^[VDIWE] \(\d+\)`)

// ansiPattern strips terminal colour codes, which ESP-IDF adds to log lines
// when colour output is enabled in the firmware build.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func isProtocolNoise(line string) bool {
	if line == "" {
		return true
	}
	// An unsolicited READY beacon arrives every five seconds until QUIET is
	// accepted, and can land in the middle of any exchange.
	return logLinePattern.MatchString(line) || strings.HasPrefix(line, "READY "+protocolVersion)
}

// lineReader turns a serial port into newline-delimited lines.
//
// It cannot use bufio: go.bug.st/serial signals a read timeout by returning
// (0, nil), which bufio treats as lack of progress and turns into
// io.ErrNoProgress after a hundred such reads. Here a timed-out read is simply
// a retry until the caller's deadline passes.
type lineReader struct {
	port testutils.SerialPort
	buf  []byte
	tmp  [512]byte
}

func newLineReader(port testutils.SerialPort) *lineReader {
	return &lineReader{port: port}
}

// readLine returns the next complete line, without its terminator. The device
// writes "\n" but the USB Serial/JTAG console converts that to "\r\n", so the
// carriage return is stripped here.
func (l *lineReader) readLine(ctx context.Context, deadline time.Time) (string, error) {
	for {
		if idx := indexNewline(l.buf); idx >= 0 {
			line := string(l.buf[:idx])
			l.buf = l.buf[idx+1:]
			return strings.TrimRight(line, "\r"), nil
		}

		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("read line: %w", err)
		}
		if !time.Now().Before(deadline) {
			return "", context.DeadlineExceeded
		}

		n, err := l.port.Read(l.tmp[:])
		if err != nil {
			return "", fmt.Errorf("read from display: %w", err)
		}
		if n == 0 {
			// Read timeout. Nothing to do but try again until the deadline.
			continue
		}
		l.buf = append(l.buf, l.tmp[:n]...)
	}
}

// drain consumes and discards whatever is already buffered, giving boot logs a
// moment to finish before the handshake starts.
func (l *lineReader) drain(ctx context.Context, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if _, err := l.readLine(ctx, deadline); err != nil {
			return
		}
	}
}

func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}

// session is a claimed connection to a display.
type session struct {
	port     testutils.SerialPort
	lines    *lineReader
	info     string
	features map[string]bool
	buf      []byte
}

func newSession(port testutils.SerialPort) *session {
	return &session{port: port, lines: newLineReader(port)}
}

// writeLine sends one command. The device matches commands on an exact prefix
// plus a single space, so the caller is responsible for well-formed text.
func (s *session) writeLine(text string) error {
	if strings.ContainsAny(text, "\r\n") {
		return fmt.Errorf("command %q contains a line break", text)
	}
	s.buf = append(s.buf[:0], text...)
	s.buf = append(s.buf, '\n')
	if _, err := s.port.Write(s.buf); err != nil {
		return fmt.Errorf("write command %q: %w", text, err)
	}
	return nil
}

// writeRaw sends payload bytes that are not a command, used for the framed
// portion of an asset chunk.
func (s *session) writeRaw(payload []byte) error {
	if _, err := s.port.Write(payload); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

// expect reads until a line matches one of the wanted prefixes, skipping log
// output and stray beacons. An ERR line ends the wait with a protocolError.
func (s *session) expect(ctx context.Context, command string, timeout time.Duration, wanted ...string) (string, error) {
	deadline := time.Now().Add(timeout)
	seen := make([]string, 0, 4)

	for {
		line, err := s.lines.readLine(ctx, deadline)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return "", fmt.Errorf("timed out waiting for %v after %q (saw %v)", wanted, command, seen)
			}
			return "", err
		}
		line = ansiPattern.ReplaceAllString(line, "")
		if isProtocolNoise(line) {
			continue
		}
		if len(seen) < cap(seen) {
			seen = append(seen, line)
		}
		if reason, ok := strings.CutPrefix(line, "ERR "); ok {
			return "", &protocolError{command: command, reason: reason}
		}
		for _, want := range wanted {
			if strings.HasPrefix(line, want) {
				return line, nil
			}
		}
	}
}

// command sends a line and waits for the matching response.
func (s *session) command(ctx context.Context, text string, timeout time.Duration, wanted ...string) (string, error) {
	if err := s.writeLine(text); err != nil {
		return "", err
	}
	return s.expect(ctx, text, timeout, wanted...)
}

// claim performs the handshake that identifies a display and takes ownership of
// the stream. QUIET stops the five-second READY beacon so later exchanges are
// not interleaved with it.
func (s *session) claim(ctx context.Context) error {
	s.lines.drain(ctx, 200*time.Millisecond)

	if _, err := s.command(ctx, "HELLO "+protocolVersion, handshakeTimeout, "OK "+protocolVersion); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timed out") {
			return errNotZapDisplay
		}
		return err
	}

	info, err := s.command(ctx, "INFO", handshakeTimeout, "INFO "+protocolVersion)
	if err != nil {
		return err
	}
	s.info = info
	s.features = parseFeatures(info)

	if _, err := s.command(ctx, "QUIET", handshakeTimeout, "OK"); err != nil {
		return err
	}
	return nil
}

// parseFeatures pulls the capability tokens out of an INFO line.
//
// Parsed rather than substring-matched: features= is one field among several,
// and a display name or firmware version containing a capability token would
// otherwise claim a capability the device does not have.
func parseFeatures(info string) map[string]bool {
	features := map[string]bool{}
	for _, field := range strings.Fields(info) {
		rest, ok := strings.CutPrefix(field, "features=")
		if !ok {
			continue
		}
		for _, token := range strings.Split(rest, ",") {
			if token != "" {
				features[token] = true
			}
		}
	}
	return features
}

// uploadCover sends a cover image and selects it for the playing scene.
//
// The ordering matters. The device holds the committed cover separately from
// the upload buffer, so ASSET_USE is what makes a freshly uploaded image
// visible, and re-sending ASSET_USE alone re-selects an image it already has.
func (s *session) uploadCover(ctx context.Context, assetID string, pixels []byte) error {
	if len(pixels) == 0 || len(pixels) > coverMaxBytes {
		return fmt.Errorf("cover must be 1..%d bytes, got %d", coverMaxBytes, len(pixels))
	}

	sum := crc32.ChecksumIEEE(pixels)
	begin := fmt.Sprintf("ASSET_BEGIN %s image/rgb565 %d %08x", assetID, len(pixels), sum)
	if _, err := s.command(ctx, begin, uploadTimeout, "OK ASSET_BEGIN"); err != nil {
		return err
	}

	var err error
	if s.features["assets-raw"] {
		err = s.uploadRawChunks(ctx, pixels)
	} else {
		err = s.uploadBase64Chunks(ctx, pixels)
	}
	if err != nil {
		return err
	}

	end := "ASSET_END " + assetID
	want := fmt.Sprintf("OK ASSET %s bytes=%d crc=%08x", assetID, len(pixels), sum)
	if _, err := s.command(ctx, end, uploadTimeout, want); err != nil {
		return err
	}
	return nil
}

// uploadRawChunks sends payload bytes with no encoding. The firmware sets its
// console to pass received bytes through unmodified, which is what makes this
// safe; older firmware translated CR and silently corrupted binary payloads,
// which is why base64 remains the fallback.
func (s *session) uploadRawChunks(ctx context.Context, pixels []byte) error {
	for index, offset := 0, 0; offset < len(pixels); index, offset = index+1, offset+chunkRawBytes {
		end := min(offset+chunkRawBytes, len(pixels))
		chunk := pixels[offset:end]

		header := fmt.Sprintf("ASSET_CHUNK_RAW %d %d", index, len(chunk))
		if _, err := s.command(ctx, header, uploadTimeout, "OK ASSET_CHUNK_RAW"); err != nil {
			return err
		}
		// The device is now blocking on exactly this many bytes, so the chunk
		// has to be treated as atomic: abandoning it here would wedge the
		// firmware until its read deadline expires.
		if err := s.writeRaw(chunk); err != nil {
			return err
		}
		want := fmt.Sprintf("OK ASSET_CHUNK %d", index)
		if _, err := s.expect(ctx, header, uploadTimeout, want); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) uploadBase64Chunks(ctx context.Context, pixels []byte) error {
	encoded := make([]byte, base64.StdEncoding.EncodedLen(chunkDecodedBytes))
	for index, offset := 0, 0; offset < len(pixels); index, offset = index+1, offset+chunkDecodedBytes {
		end := min(offset+chunkDecodedBytes, len(pixels))
		chunk := encoded[:base64.StdEncoding.EncodedLen(end-offset)]
		base64.StdEncoding.Encode(chunk, pixels[offset:end])

		header := fmt.Sprintf("ASSET_CHUNK_DATA %d %d", index, len(chunk))
		if _, err := s.command(ctx, header, uploadTimeout, "OK ASSET_CHUNK_DATA"); err != nil {
			return err
		}
		if err := s.writeRaw(chunk); err != nil {
			return err
		}
		want := fmt.Sprintf("OK ASSET_CHUNK %d", index)
		if _, err := s.expect(ctx, header, uploadTimeout, want); err != nil {
			return err
		}
	}
	return nil
}

// useCover selects an already-uploaded cover for the playing scene. The size is
// part of the command because the device stores whatever aspect it was sent.
func (s *session) useCover(ctx context.Context, assetID string, width, height int) error {
	text := fmt.Sprintf("ASSET_USE cover %s %d %d rgb565", assetID, width, height)
	_, err := s.command(ctx, text, commandTimeout, "OK")
	return err
}

// setRotation turns the panel for a display mounted the other way up.
//
// The device owns this setting and keeps it in NVS, so it also comes up the
// right way round with no host attached. Core sends it anyway on every connect,
// because Core is where the user configured it and the device's copy is only a
// cache.
//
// The firmware takes 0 and 180 and answers ERR bad-value for anything else: the
// panel has one landscape layout, so a quarter turn would need a UI it does not
// have rather than a transform.
func (s *session) setRotation(ctx context.Context, degrees int) error {
	if !s.features["settings"] {
		return fmt.Errorf("display firmware has no settings support, cannot rotate to %d", degrees)
	}
	_, err := s.command(ctx, fmt.Sprintf("SET rotation %d", degrees), commandTimeout, "OK")
	return err
}

// pushClock gives the device a wall clock for its idle screens. Failure is not
// worth propagating: the device simply skips the clock screen when it has never
// been told the time.
func (s *session) pushClock(ctx context.Context) {
	if !s.features["clock"] {
		return
	}
	now := time.Now()
	_, offset := now.Zone()
	text := fmt.Sprintf("TIME %d %d", now.Unix(), offset)
	// A device that will not take the clock simply never shows a clock screen,
	// which is the same as never having been told.
	_, _ = s.command(ctx, text, commandTimeout, "OK")
}

func (s *session) close() error {
	if err := s.port.Close(); err != nil {
		return fmt.Errorf("close display port: %w", err)
	}
	return nil
}
