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

package steam

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/rs/zerolog/log"
)

// Binary VDF magic numbers (version identifiers)
const (
	magic27 uint32 = 0x07564427 // v27 format
	magic28 uint32 = 0x07564428 // v28 format (adds binaryDataHash)
	magic29 uint32 = 0x07564429 // v29 format (adds string table)
)

// Binary VDF type markers
const (
	vdfTypeNested uint8 = 0x00 // Nested object
	vdfTypeString uint8 = 0x01 // Null-terminated string
	vdfTypeUint32 uint8 = 0x02 // 32-bit unsigned integer
	vdfTypeEnd    uint8 = 0x08 // End of object marker
)

var (
	ErrInvalidMagic   = errors.New("invalid appinfo.vdf magic header")
	ErrAppNotFound    = errors.New("app not found in appinfo.vdf")
	ErrInvalidFormat  = errors.New("invalid binary VDF format")
	ErrNoLaunchConfig = errors.New("no launch config found")
)

// LaunchConfig contains launch configuration from appinfo.vdf.
type LaunchConfig struct {
	Executable string // Relative executable path (e.g., "game.exe")
	Arguments  string // Launch arguments
	Type       string // Launch type ("default", "none", "option1", etc.)
	OSList     string // Target OS ("windows", "linux", "macos")
	WorkingDir string // Relative working directory
}

// binaryVDFReader wraps a byte slice for little-endian binary reading.
type binaryVDFReader struct {
	data  []byte
	pool  []string
	pos   int
	magic uint32
}

func newBinaryVDFReader(data []byte) *binaryVDFReader {
	return &binaryVDFReader{data: data}
}

func (r *binaryVDFReader) readUint8() (uint8, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	v := r.data[r.pos]
	r.pos++
	return v, nil
}

func (r *binaryVDFReader) readUint32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, io.EOF
	}
	v := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *binaryVDFReader) readUint64() (uint64, error) {
	if r.pos+8 > len(r.data) {
		return 0, io.EOF
	}
	v := binary.LittleEndian.Uint64(r.data[r.pos:])
	r.pos += 8
	return v, nil
}

func (r *binaryVDFReader) readInt64() (int64, error) {
	if r.pos+8 > len(r.data) {
		return 0, io.EOF
	}
	// Read as signed int64 directly to avoid overflow conversion
	buf := r.data[r.pos : r.pos+8]
	v := int64(buf[0]) | int64(buf[1])<<8 | int64(buf[2])<<16 | int64(buf[3])<<24 |
		int64(buf[4])<<32 | int64(buf[5])<<40 | int64(buf[6])<<48 | int64(buf[7])<<56
	r.pos += 8
	return v, nil
}

func (r *binaryVDFReader) readBytes(n int) ([]byte, error) {
	if r.pos+n > len(r.data) {
		return nil, io.EOF
	}
	v := r.data[r.pos : r.pos+n]
	r.pos += n
	return v, nil
}

func (r *binaryVDFReader) readNullString() (string, error) {
	start := r.pos
	for r.pos < len(r.data) && r.data[r.pos] != 0 {
		r.pos++
	}
	if r.pos >= len(r.data) {
		return "", io.EOF
	}
	s := string(r.data[start:r.pos])
	r.pos++ // skip null terminator
	return s, nil
}

// readKey reads a key based on the VDF version.
// v27/v28: null-terminated string inline
// v29: uint32 index into string pool
func (r *binaryVDFReader) readKey() (string, error) {
	if r.magic == magic29 && len(r.pool) > 0 {
		idx, err := r.readUint32()
		if err != nil {
			return "", err
		}
		if int(idx) >= len(r.pool) {
			return "", ErrInvalidFormat
		}
		return r.pool[idx], nil
	}
	return r.readNullString()
}

// readObject reads a binary VDF object into a map.
func (r *binaryVDFReader) readObject() (map[string]any, error) {
	result := make(map[string]any)

	for {
		typeMarker, err := r.readUint8()
		if err != nil {
			return nil, err
		}

		if typeMarker == vdfTypeEnd {
			break
		}

		key, err := r.readKey()
		if err != nil {
			return nil, err
		}

		switch typeMarker {
		case vdfTypeNested:
			nested, err := r.readObject()
			if err != nil {
				return nil, err
			}
			result[key] = nested

		case vdfTypeString:
			s, err := r.readNullString()
			if err != nil {
				return nil, err
			}
			result[key] = s

		case vdfTypeUint32:
			v, err := r.readUint32()
			if err != nil {
				return nil, err
			}
			result[key] = v

		default:
			return nil, ErrInvalidFormat
		}
	}

	return result, nil
}

// ReadLaunchConfigs reads launch configurations for an app from appinfo.vdf.
func ReadLaunchConfigs(steamDir string, appID int) ([]LaunchConfig, error) {
	appInfoPath := filepath.Join(steamDir, "appcache", "appinfo.vdf")

	//nolint:gosec // Safe: reads Steam cache files
	data, err := os.ReadFile(appInfoPath)
	if err != nil {
		return nil, fmt.Errorf("read appinfo.vdf: %w", err)
	}

	r := newBinaryVDFReader(data)

	// Read header
	magic, err := r.readUint32()
	if err != nil {
		return nil, err
	}
	if magic != magic27 && magic != magic28 && magic != magic29 {
		return nil, ErrInvalidMagic
	}
	r.magic = magic

	// Skip universe
	_, err = r.readUint32()
	if err != nil {
		return nil, err
	}

	// For v29, read string table offset and parse string pool
	if magic == magic29 {
		stringTableOffset, err := r.readInt64()
		if err != nil {
			return nil, err
		}

		// Save position, read string table, restore position
		savedPos := r.pos
		r.pos = int(stringTableOffset)

		stringCount, err := r.readUint32()
		if err != nil {
			return nil, err
		}

		r.pool = make([]string, stringCount)
		for i := range stringCount {
			s, err := r.readNullString()
			if err != nil {
				return nil, err
			}
			r.pool[i] = s
		}

		r.pos = savedPos
	}

	// Iterate through app entries
	for {
		entryAppID, err := r.readUint32()
		if err != nil {
			return nil, err
		}
		if entryAppID == 0 {
			// End of entries
			break
		}

		size, err := r.readUint32()
		if err != nil {
			return nil, err
		}
		endPos := r.pos + int(size)

		// Skip metadata fields
		_, _ = r.readUint32()  // infoState
		_, _ = r.readUint32()  // lastUpdated
		_, _ = r.readUint64()  // token
		_, _ = r.readBytes(20) // hash
		_, _ = r.readUint32()  // changeNumber

		// v28/v29 have binaryDataHash
		if magic == magic28 || magic == magic29 {
			_, _ = r.readBytes(20)
		}

		if int(entryAppID) == appID {
			// Found our app, parse the VDF blob
			obj, err := r.readObject()
			if err != nil {
				return nil, err
			}
			return extractLaunchConfigs(obj), nil
		}

		// Skip to next entry
		r.pos = endPos
	}

	return nil, ErrAppNotFound
}

// extractLaunchConfigs extracts launch configurations from parsed VDF data.
func extractLaunchConfigs(obj map[string]any) []LaunchConfig {
	var configs []LaunchConfig

	// Navigate to config/launch
	configObj, ok := obj["config"].(map[string]any)
	if !ok {
		return configs
	}

	launchObj, ok := configObj["launch"].(map[string]any)
	if !ok {
		return configs
	}

	// Each numbered key is a launch config
	for _, v := range launchObj {
		launchEntry, ok := v.(map[string]any)
		if !ok {
			continue
		}

		config := LaunchConfig{}

		if exe, ok := launchEntry["executable"].(string); ok {
			config.Executable = exe
		}
		if args, ok := launchEntry["arguments"].(string); ok {
			config.Arguments = args
		}
		if t, ok := launchEntry["type"].(string); ok {
			config.Type = t
		}
		if workDir, ok := launchEntry["workingdir"].(string); ok {
			config.WorkingDir = workDir
		}

		// OS list can be in "config" sub-object
		if configSub, ok := launchEntry["config"].(map[string]any); ok {
			if oslist, ok := configSub["oslist"].(string); ok {
				config.OSList = oslist
			}
		}

		if config.Executable != "" {
			configs = append(configs, config)
		}
	}

	return configs
}

// GetGameExecutable returns the best executable path for the current OS.
// It reads appinfo.vdf to find the launch configuration.
// Returns the full path to the executable, or empty string if not found.
func GetGameExecutable(steamDir string, appID int) (string, bool) {
	configs, err := ReadLaunchConfigs(steamDir, appID)
	if err != nil {
		log.Debug().Err(err).Int("appID", appID).Msg("failed to read launch configs")
		return "", false
	}

	if len(configs) == 0 {
		return "", false
	}

	// Get install directory
	installDir, found := FindInstallDirByAppID(appID)
	if !found {
		return "", false
	}

	currentOS := getCurrentOSString()

	// Find best matching config:
	// 1. Prefer type "default" or empty for current OS
	// 2. Fall back to any config for current OS
	// 3. Fall back to first config with an executable
	var bestConfig *LaunchConfig
	var fallbackConfig *LaunchConfig

	for i := range configs {
		c := &configs[i]

		// Skip configs without executable
		if c.Executable == "" {
			continue
		}

		// Check if OS matches
		osMatches := c.OSList == "" || matchesOS(c.OSList, currentOS)

		if osMatches {
			// Prefer "default" type or empty type
			if c.Type == "" || c.Type == "default" {
				bestConfig = c
				break
			}
			if fallbackConfig == nil {
				fallbackConfig = c
			}
		}
	}

	if bestConfig == nil {
		bestConfig = fallbackConfig
	}

	if bestConfig == nil && len(configs) > 0 {
		// Last resort: first config with executable
		for i := range configs {
			if configs[i].Executable != "" {
				bestConfig = &configs[i]
				break
			}
		}
	}

	if bestConfig == nil {
		return "", false
	}

	// Build full path
	exePath := filepath.Join(installDir, bestConfig.Executable)
	return exePath, true
}

// getCurrentOSString returns the OS string used by Steam.
func getCurrentOSString() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	case "linux":
		return "linux"
	default:
		return runtime.GOOS
	}
}

// matchesOS checks if the oslist contains the target OS.
func matchesOS(oslist, target string) bool {
	// oslist can be comma-separated or single value
	return bytes.Contains([]byte(oslist), []byte(target))
}
