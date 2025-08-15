/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
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

package tty2oled

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/rs/zerolog/log"
	"go.bug.st/serial"
)

// Reader represents a tty2oled display reader
type Reader struct {
	port                serial.Port
	platform            platforms.Platform
	healthCheckCtx      context.Context
	cfg                 *config.Instance
	currentMedia        *models.ActiveMedia
	pictureManager      *PictureManager
	healthCheckCancel   context.CancelFunc
	deviceConfig        config.ReadersConnect
	path                string
	mu                  sync.RWMutex
	connected           bool
	operationInProgress bool
}

func NewReader(cfg *config.Instance, pl platforms.Platform) *Reader {
	ctx, cancel := context.WithCancel(context.Background())
	return &Reader{
		cfg:               cfg,
		platform:          pl,
		pictureManager:    NewPictureManager(cfg, pl),
		healthCheckCtx:    ctx,
		healthCheckCancel: cancel,
	}
}

func (*Reader) IDs() []string {
	return []string{"tty2oled"}
}

func (r *Reader) Open(device config.ReadersConnect, _ chan<- readers.Scan) error {
	// Set device config without holding mutex
	r.mu.Lock()
	r.deviceConfig = device
	r.path = device.Path
	r.mu.Unlock()

	// Open serial port with proper configuration
	port, err := serial.Open(r.path, &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return fmt.Errorf("failed to open serial port: %w", err)
	}

	// Set read timeout for detection
	err = port.SetReadTimeout(500 * time.Millisecond)
	if err != nil {
		_ = port.Close()
		return fmt.Errorf("failed to set read timeout: %w", err)
	}

	// Set port without holding mutex during handshake
	r.mu.Lock()
	r.port = port
	r.mu.Unlock()

	if err := r.handshake(); err != nil {
		r.mu.Lock()
		_ = r.port.Close()
		r.port = nil
		r.mu.Unlock()
		return err
	}

	// Mark as connected after successful handshake
	r.mu.Lock()
	r.connected = true
	r.mu.Unlock()

	log.Info().Str("device", r.path).Msg("tty2oled display connected")

	// Start background health check
	r.startHealthCheck()

	return nil
}

func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Stop health check goroutine
	if r.healthCheckCancel != nil {
		r.healthCheckCancel()
	}

	if r.port != nil {
		_ = r.port.Close()
		r.port = nil
	}

	r.connected = false
	log.Info().Str("device", r.path).Msg("tty2oled display disconnected")

	return nil
}

func (r *Reader) Detect(connected []string) string {
	ports, err := helpers.GetSerialDeviceList()
	if err != nil {
		log.Error().Err(err).Msg("failed to get serial ports")
		return ""
	}

	log.Debug().Int("port_count", len(ports)).Msg("tty2oled: checking serial ports")

	for _, name := range ports {
		device := "tty2oled:" + name

		log.Debug().Str("port", name).Str("device", device).Msg("tty2oled: checking port")

		if helpers.Contains(connected, device) {
			log.Debug().Str("device", device).Msg("tty2oled: skipping already connected device")
			continue
		}

		// Check if this port is in use by ANY connected reader (not just tty2oled)
		portInUse := false
		for _, connectedDevice := range connected {
			// Parse connected device string (format: "driver:path")
			parts := strings.SplitN(connectedDevice, ":", 2)
			if len(parts) == 2 && parts[1] == name {
				log.Debug().
					Str("port", name).
					Str("connected_as", connectedDevice).
					Str("attempted_as", device).
					Msg("tty2oled: skipping port, already in use by another reader")
				portInUse = true
				break
			}
		}

		if portInUse {
			continue
		}

		// try to detect tty2oled device by attempting handshake
		log.Debug().Str("port", name).Msg("tty2oled: attempting detection")
		if r.detectDevice(name) {
			log.Debug().Str("device", device).Msg("tty2oled: device detected successfully")
			return device
		}
		log.Debug().Str("port", name).Msg("tty2oled: detection failed for port")
	}

	return ""
}

func (r *Reader) Device() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.path
}

func (r *Reader) Connected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Just return cached connection status - no hardware probing
	// Connection status is updated in sendCommand() when write errors occur
	return r.connected && r.port != nil
}

func (r *Reader) Info() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.connected {
		return "tty2oled (disconnected)"
	}

	return "tty2oled (" + r.path + ")"
}

func (*Reader) Write(string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on display-only reader")
}

func (*Reader) CancelWrite() {
	// no-op, writing not supported
}

func (*Reader) Capabilities() []readers.Capability {
	return []readers.Capability{readers.CapabilityDisplay}
}

func (r *Reader) OnMediaChange(media *models.ActiveMedia) error {
	r.mu.Lock()

	if !r.connected {
		r.mu.Unlock()
		return nil // Device not connected, nothing to do
	}

	r.currentMedia = media

	if media == nil {
		log.Debug().Msg("tty2oled: clearing display (no active media)")
		err := r.clearDisplay()
		r.mu.Unlock()
		return err
	}

	log.Debug().
		Str("system", media.SystemID).
		Str("name", media.Name).
		Msg("tty2oled: updating display for media change")

	// Copy media to avoid race conditions in goroutine
	mediaCopy := *media
	r.mu.Unlock()

	// Move picture download to background to avoid blocking readers
	go func() {
		if err := r.displayMedia(&mediaCopy); err != nil {
			log.Warn().
				Err(err).
				Str("system", mediaCopy.SystemID).
				Msg("tty2oled: failed to update display in background")
		}
	}()

	return nil
}

func (r *Reader) handshake() error {
	if r.port == nil {
		return errors.New("port not open")
	}

	// Add timeout for entire handshake process to prevent hangs during autodetection
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return r.handshakeWithContext(ctx)
}

func (r *Reader) handshakeWithContext(ctx context.Context) error {
	// Send QWERTZ as first transmission - exactly like bash script
	// The bash script does: echo "QWERTZ" > $TTYDEV; sleep $WAITSECS
	// Arduino code shows QWERTZ is one-way: just clears buffer, no response expected
	if err := r.sendCommand(CmdHandshake); err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	// Initialize device with same sequence as bash script
	if err := r.initializeDevice(); err != nil {
		return fmt.Errorf("failed to initialize device: %w", err)
	}

	return nil
}

// initializeDevice sends the initialization commands that the bash script sends after handshake
func (r *Reader) initializeDevice() error {
	// Bash script sequence after QWERTZ: sendcontrast, sendrotation, sendtime, sendscreensaver

	// sendcontrast: echo "CMDCON,${CONTRAST}" > ${TTYDEV}; sleep ${WAITSECS}
	contrastCmd := fmt.Sprintf("%s,%d", CmdContrast, ContrastDefault)
	if err := r.sendCommand(contrastCmd); err != nil {
		return fmt.Errorf("failed to send contrast command: %w", err)
	}

	// sendrotation: if rotation enabled, echo "CMDROT,1" > ${TTYDEV}; sleep ${WAITSECS}
	// then echo "CMDSORG" > ${TTYDEV}; sleep 4
	// TODO: Add rotation support based on config - for now skip rotation
	// When implemented:
	// rotateCmd := fmt.Sprintf("%s,1", CmdRotate)
	// if err := r.sendCommand(rotateCmd); err != nil {
	//     return fmt.Errorf("failed to send rotation command: %w", err)
	// }
	// if err := r.sendCommand(CmdOrgLogo); err != nil {
	//     return fmt.Errorf("failed to send original logo command: %w", err)
	// }
	// time.Sleep(4 * time.Second) // bash script uses sleep 4 after CMDSORG

	// sendtime: echo "CMDSETTIME,${localtime}" > ${TTYDEV}; sleep ${WAITSECS}
	timestamp := time.Now().Unix()
	timeCmd := fmt.Sprintf("%s,%d", CmdSetTime, timestamp)
	if err := r.sendCommand(timeCmd); err != nil {
		return fmt.Errorf("failed to send time command: %w", err)
	}

	// sendscreensaver: echo "CMDSAVER,mode,interval,start" > ${TTYDEV}; sleep ${WAITSECS}
	// TODO: Implement proper screensaver mode calculation like bash script
	screensaverCmd := fmt.Sprintf("%s,0,0,0", CmdScreensaver)
	if err := r.sendCommand(screensaverCmd); err != nil {
		return fmt.Errorf("failed to send screensaver command: %w", err)
	}

	return nil
}

func (r *Reader) sendCommand(command string) error {
	if r.port == nil {
		return errors.New("port not open")
	}

	// Mark operation as in progress to prevent health check interference
	r.mu.Lock()
	r.operationInProgress = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.operationInProgress = false
		r.mu.Unlock()
	}()

	// Send command exactly like bash script: echo "COMMAND" > ${TTYDEV}; sleep ${WAITSECS}
	data := command + CommandTerminator
	_, err := r.port.Write([]byte(data))
	if err != nil {
		// Check for disconnection errors
		if r.isDisconnectionError(err) {
			r.mu.Lock()
			r.connected = false
			r.mu.Unlock()
			log.Info().Str("device", r.path).Err(err).Msg("tty2oled device disconnected - write error")
		}
		return fmt.Errorf("failed to write to port: %w", err)
	}

	log.Debug().Str("command", command).Msg("tty2oled: sent command")

	// Bash script timing: sleep ${WAITSECS} (0.2 seconds)
	time.Sleep(WaitDuration)

	// Optional: Arduino sends "ttyack;" after processing commands
	// For now, we don't wait for acknowledgment to maintain compatibility with shell script
	// TODO: Add config option to enable acknowledgment checking for better reliability

	return nil
}

// isDisconnectionError checks if an error indicates device disconnection
func (*Reader) isDisconnectionError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific serial library error types first
	var portErr serial.PortError
	if errors.As(err, &portErr) {
		switch portErr.Code() {
		case serial.PortNotFound:
			return true // Device was unplugged/removed
		case serial.PortClosed:
			return true // Port was closed unexpectedly
		case serial.InvalidSerialPort:
			return true // Device is no longer a valid serial port
		case serial.PortBusy, serial.PermissionDenied, serial.InvalidSpeed,
			serial.InvalidDataBits, serial.InvalidParity, serial.InvalidStopBits,
			serial.InvalidTimeoutValue, serial.ErrorEnumeratingPorts, serial.FunctionNotImplemented:
			return false // Configuration or permission errors, not disconnection
		default:
			return false
		}
	}

	// Fallback to string matching for OS-level errors that aren't wrapped
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "device not configured") ||
		strings.Contains(errStr, "input/output error") ||
		strings.Contains(errStr, "no such device") ||
		strings.Contains(errStr, "device not found") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "device disconnected")
}

func (r *Reader) clearDisplay() error {
	// Use standard tty2oled protocol to display blank screen
	// CMDCOR with "BLANK" corename and no transition - just like bash script
	command := CmdCore + ",BLANK," + TransitionNone
	return r.sendCommand(command)
}

func (r *Reader) displayMedia(media *models.ActiveMedia) error {
	if media == nil || media.SystemID == "" {
		return r.clearDisplay()
	}

	// Try to find picture for the system
	picturePath, err := r.pictureManager.GetPictureForSystem(media.SystemID)
	if err != nil {
		log.Warn().
			Err(err).
			Str("system", media.SystemID).
			Msg("failed to get picture for system, using system name fallback")

		// Fallback to displaying system name without picture
		return r.sendCommand(media.SystemID)
	}

	// Use the base filename (without extension) as corename
	baseName := filepath.Base(picturePath)
	coreName := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	// Exactly replicate bash script senddata() function:
	// echo "CMDCOR,${1},${TRANSITION}" > ${TTYDEV}
	// sleep ${WAITSECS}
	// tail -n +4 "${picfnam}" | xxd -r -p > ${TTYDEV}

	command := CmdCore + "," + coreName + "," + TransitionSlide

	if err := r.sendCommand(command); err != nil {
		return fmt.Errorf("failed to send CMDCOR command: %w", err)
	}

	// Send picture data exactly like bash script
	if err := r.sendPictureData(picturePath); err != nil {
		return fmt.Errorf("failed to send picture data: %w", err)
	}

	return nil
}

// detectDevice attempts to detect a tty2oled device by sending CMDHWINF and waiting for a response
func (*Reader) detectDevice(portName string) bool {
	// Add overall timeout for device detection to prevent hanging autodetection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		detected bool
	}

	resultCh := make(chan result, 1)

	go func() {
		// Open serial port for detection
		port, err := serial.Open(portName, &serial.Mode{
			BaudRate: 115200,
			DataBits: 8,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
		})
		if err != nil {
			log.Debug().Err(err).Str("port", portName).Msg("failed to open serial port for tty2oled detection")
			resultCh <- result{false}
			return
		}
		defer func() {
			if closeErr := port.Close(); closeErr != nil {
				log.Debug().Err(closeErr).Str("port", portName).Msg("failed to close port after detection")
			}
		}()

		// Set short read timeout for detection
		err = port.SetReadTimeout(500 * time.Millisecond)
		if err != nil {
			log.Debug().Err(err).Str("port", portName).Msg("failed to set read timeout for detection")
			resultCh <- result{false}
			return
		}

		// Send hardware info probe command
		_, err = port.Write([]byte(CmdHardwareInfo + CommandTerminator))
		if err != nil {
			log.Debug().Err(err).Str("port", portName).Msg("failed to send probe command")
			resultCh <- result{false}
			return
		}

		// Try to read response with timeout
		buffer := make([]byte, 256)
		n, err := port.Read(buffer)
		if err != nil {
			log.Debug().Err(err).Str("port", portName).Msg("no response from device")
			resultCh <- result{false}
			return
		}

		if n > 0 {
			response := strings.TrimSpace(string(buffer[:n]))
			log.Debug().Str("port", portName).Str("response", response).Msg("tty2oled probe response")

			// Check for expected hardware response patterns from Arduino code
			// Examples: "HWESP32DE;", "HWESP8266;", "HWTTYNANO;", etc.
			if (strings.HasPrefix(response, "HW") && strings.HasSuffix(response, ";")) ||
				strings.Contains(response, "ttyack") || // Arduino sends ttyack; after commands
				strings.Contains(response, "ttyrdy") { // Arduino sends ttyrdy; on startup
				log.Debug().Str("port", portName).Str("response", response).Msg("detected tty2oled device")
				resultCh <- result{true}
				return
			}
		}

		log.Debug().Str("port", portName).Msg("no valid tty2oled response received")
		resultCh <- result{false}
	}()

	select {
	case res := <-resultCh:
		return res.detected
	case <-ctx.Done():
		log.Debug().Str("port", portName).Msg("tty2oled detection timeout")
		return false
	}
}

// sendPictureData reads a picture file and sends the binary data to the tty2oled device
func (r *Reader) sendPictureData(picturePath string) error {
	if r.port == nil {
		return errors.New("port not open")
	}

	// Mark operation as in progress to prevent health check interference
	r.mu.Lock()
	r.operationInProgress = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.operationInProgress = false
		r.mu.Unlock()
	}()

	// Read the picture file - exactly like bash script: tail -n +4 "$picfnam" | xxd -r -p
	data, err := r.readPictureFile(picturePath)
	if err != nil {
		return fmt.Errorf("failed to read picture file: %w", err)
	}

	// Convert hex data to binary - exactly like bash script: xxd -r -p
	binData, err := r.hexToBinary(data)
	if err != nil {
		return fmt.Errorf("failed to convert hex to binary: %w", err)
	}

	// Send the binary data directly - exactly like bash script: > ${TTYDEV}
	bytesWritten, err := r.port.Write(binData)
	if err != nil {
		return fmt.Errorf("failed to write picture data to port: %w", err)
	}

	if bytesWritten != len(binData) {
		return fmt.Errorf("incomplete picture data write: wrote %d of %d bytes", bytesWritten, len(binData))
	}

	log.Debug().
		Int("bytes_written", bytesWritten).
		Msg("tty2oled: picture data sent")

	// That's it! Bash script doesn't wait or do anything after sending the data
	return nil
}

// readPictureFile reads a picture file and extracts the hex data (skipping first 3 lines)
func (r *Reader) readPictureFile(filePath string) ([]string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) < 4 {
		return nil, fmt.Errorf("picture file too short (expected at least 4 lines, got %d)", len(lines))
	}

	// Skip first 3 lines (comment lines and array declaration) as per bash script: `tail -n +4`
	dataLines := lines[3:]

	// Filter out empty lines and extract hex values
	var hexValues []string
	for _, line := range dataLines {
		line = strings.TrimSpace(line)
		if line == "" || line == "};" || line == "}" {
			continue
		}

		// Extract hex values from C array format: 0XFF,0XFF,0XFF -> FF FF FF
		hexValues = append(hexValues, r.extractHexFromLine(line)...)
	}

	if len(hexValues) == 0 {
		return nil, errors.New("no hex data found in picture file")
	}

	return hexValues, nil
}

// extractHexFromLine extracts hex values from a C array format line
func (*Reader) extractHexFromLine(line string) []string {
	var hexValues []string

	// Remove common C formatting: commas, spaces, semicolons
	line = strings.ReplaceAll(line, ",", " ")
	line = strings.ReplaceAll(line, ";", " ")

	// Split by whitespace and extract hex values
	parts := strings.Fields(line)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "0X") || strings.HasPrefix(part, "0x") {
			// Extract hex value without 0X prefix
			hexValue := part[2:]
			// Ensure hex value is exactly 2 characters (pad with leading zero if needed)
			if len(hexValue) == 1 {
				hexValue = "0" + hexValue
			}
			if len(hexValue) == 2 {
				hexValues = append(hexValues, strings.ToUpper(hexValue))
			}
		}
	}

	return hexValues
}

// hexToBinary converts hex string array to binary data (equivalent to `xxd -r -p`)
func (*Reader) hexToBinary(hexValues []string) ([]byte, error) {
	result := make([]byte, 0, len(hexValues))

	for _, hexStr := range hexValues {
		// Parse hex string to byte
		if len(hexStr) != 2 {
			return nil, fmt.Errorf("invalid hex value: %s (expected 2 characters)", hexStr)
		}

		val, err := strconv.ParseUint(hexStr, 16, 8)
		if err != nil {
			return nil, fmt.Errorf("failed to parse hex value %s: %w", hexStr, err)
		}

		result = append(result, byte(val))
	}

	log.Debug().
		Int("hex_values", len(hexValues)).
		Int("binary_bytes", len(result)).
		Msg("tty2oled: converted hex data to binary")

	return result, nil
}

// startHealthCheck starts a background goroutine that performs health checks every 5 seconds
// but only when no other operations are in progress
func (r *Reader) startHealthCheck() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		log.Debug().Str("device", r.path).Msg("tty2oled: health check started")

		for {
			select {
			case <-r.healthCheckCtx.Done():
				log.Debug().Str("device", r.path).Msg("tty2oled: health check stopped")
				return
			case <-ticker.C:
				r.performBackgroundHealthCheck()
			}
		}
	}()
}

// performBackgroundHealthCheck checks device health only if no operations are in progress
func (r *Reader) performBackgroundHealthCheck() {
	r.mu.Lock()

	// Skip health check if not connected or if any operation is in progress
	if !r.connected || r.port == nil || r.operationInProgress {
		r.mu.Unlock()
		return
	}

	// Perform the health check while holding the lock to prevent interference
	if !r.doHealthCheck() {
		log.Info().Str("device", r.path).Msg("tty2oled device disconnected - background health check failed")
		r.connected = false
	}

	r.mu.Unlock()
}

// doHealthCheck performs the actual hardware health check
// NOTE: This method should only be called with the mutex held
func (r *Reader) doHealthCheck() bool {
	if r.port == nil {
		return false
	}

	// Set a short timeout for health check
	if err := r.port.SetReadTimeout(1 * time.Second); err != nil {
		log.Debug().Err(err).Msg("tty2oled: failed to set health check timeout")
		return false
	}

	// Send hardware info command - safe, read-only operation
	_, err := r.port.Write([]byte(CmdHardwareInfo + CommandTerminator))
	if err != nil {
		if r.isDisconnectionError(err) {
			log.Debug().Err(err).Msg("tty2oled: health check write failed - device disconnected")
		} else {
			log.Debug().Err(err).Msg("tty2oled: health check write failed")
		}
		return false
	}

	// Try to read response - Arduino should send hardware info like "HWESP32DE;"
	buffer := make([]byte, 64)
	n, err := r.port.Read(buffer)
	if err != nil {
		log.Debug().Err(err).Msg("tty2oled: health check read failed")
		return false
	}

	if n > 0 {
		response := strings.TrimSpace(string(buffer[:n]))
		// Check for valid hardware response patterns
		if (strings.HasPrefix(response, "HW") && strings.HasSuffix(response, ";")) ||
			strings.Contains(response, "ttyack") {
			log.Debug().Str("response", response).Msg("tty2oled: background health check passed")
			return true
		}
		log.Debug().Str("response", response).Msg("tty2oled: health check got unexpected response")
	}

	log.Debug().Msg("tty2oled: background health check failed - no valid response")
	return false
}
