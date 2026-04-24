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

package helpers

import (
	"bufio"
	"context"
	"crypto/md5" //nolint:gosec // Used for game file hashing/matching against existing retro gaming databases
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

// MaxResponseBodySize is the default maximum number of bytes to read from an
// HTTP response body. Use with io.LimitReader to prevent memory exhaustion
// from malicious or misconfigured servers. 1MB covers legitimate API responses.
const MaxResponseBodySize = 1 << 20 // 1 MiB

func TokensEqual(a, b *tokens.Token) bool {
	if a == nil && b == nil {
		return true
	} else if a == nil || b == nil {
		return false
	}

	return a.UID == b.UID && a.Text == b.Text
}

func GetMd5Hash(filePath string) (string, error) {
	//nolint:gosec // Safe: opens files for MD5 hashing, used for game file identification
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file for MD5 hash: %w", err)
	}
	//nolint:gosec // Used for game file hashing/matching against existing retro gaming databases
	hash := md5.New()
	_, _ = io.Copy(hash, file)
	_ = file.Close()
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func GetFileSize(filePath string) (int64, error) {
	//nolint:gosec // Safe: opens files to get file size, used for game file analysis
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open file for size check: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return 0, fmt.Errorf("failed to get file stat: %w", err)
	}

	size := stat.Size()
	_ = file.Close()

	return size, nil
}

// Contains returns true if slice contains value.
func Contains[T comparable](xs []T, x T) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// EqualStringSlices compares two string slices for equality
func EqualStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Sort both slices for comparison
	aCopy := make([]string, len(a))
	copy(aCopy, a)
	sort.Strings(aCopy)

	bCopy := make([]string, len(b))
	copy(bCopy, b)
	sort.Strings(bCopy)

	for i, v := range aCopy {
		if v != bCopy[i] {
			return false
		}
	}
	return true
}

// MapKeys returns a list of all keys in a map.
func MapKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	return keys
}

func AlphaMapKeys[V any](m map[string]V) []string {
	keys := MapKeys(m)
	sort.Strings(keys)
	return keys
}

func WaitForInternet(maxTries int) bool {
	for range maxTries {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com", http.NoBody)
		if err != nil {
			cancel()
			continue
		}

		resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: hardcoded URL https://api.github.com
		cancel()
		if err == nil && resp != nil {
			if err := resp.Body.Close(); err != nil {
				log.Error().Err(err).Msg("error closing response body")
			}
			return true
		}
		time.Sleep(1 * time.Second)
	}
	return false
}

func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok &&
			!ipnet.IP.IsLoopback() && ipnet.IP.IsPrivate() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return ""
}

// GetAllLocalIPs returns all non-loopback private IPv4 addresses
func GetAllLocalIPs() []string {
	var ips []string

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok &&
			!ipnet.IP.IsLoopback() && ipnet.IP.IsPrivate() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}

	return ips
}

func IsZip(filePath string) bool {
	return strings.EqualFold(filepath.Ext(filePath), ".zip")
}

// ZIP format constants for central directory parsing.
const (
	zipSigCentralDir = uint32(0x02014b50)
	zipSigEOCD       = uint32(0x06054b50)
	zipSigEOCD64Loc  = uint32(0x07064b50)
	zipSigEOCD64     = uint32(0x06064b50)
	zipEOCDMinSize   = 22
	zipEOCD64LocSize = 20
	zipEOCD64Size    = 56
	zipCDHdrSize     = 46
)

// zipTailPool provides pooled tail buffers for EOCD scanning.
// Most zip files have no comment, so an initial 1 KB read finds the EOCD.
// The pool holds the occasional full 64 KB+ fallback buffer.
var zipTailPool = sync.Pool{New: func() any { b := make([]byte, zipEOCDMinSize+65535); return &b }}

// findEOCD scans tail (read from the end of the file) backward for the EOCD
// signature. Returns the offset within tail or -1 if not found.
func findEOCD(tail []byte) int {
	for i := len(tail) - zipEOCDMinSize; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) == zipSigEOCD {
			return i
		}
	}
	return -1
}

// ListZip returns a slice of all filenames in a zip file.
// It reads only the central directory, avoiding the zip.File/FileHeader
// allocations that archive/zip performs per entry.
func ListZip(filePath string) ([]string, error) {
	f, err := os.Open(filePath) //nolint:gosec // G304: caller-controlled path, same as archive/zip.OpenReader
	if err != nil {
		return nil, fmt.Errorf("failed to open zip file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("close zip failed")
		}
	}()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to seek to end of zip: %w", err)
	}
	if size < zipEOCDMinSize {
		return nil, errors.New("file too small to be a valid zip")
	}

	// Attempt a cheap 1 KB read first — covers all zips with no or a short comment.
	// Only pay for the full 64 KB fallback when necessary.
	const smallTail = 1024
	var (
		eocdOff int
		tail    []byte
	)

	firstLen := int64(smallTail)
	if firstLen > size {
		firstLen = size
	}
	firstBuf := make([]byte, firstLen)
	if _, err := f.ReadAt(firstBuf, size-firstLen); err != nil {
		return nil, fmt.Errorf("failed to read zip tail: %w", err)
	}
	eocdOff = findEOCD(firstBuf)
	if eocdOff >= 0 {
		tail = firstBuf
	} else {
		// Fall back to the full max-comment search (22 + 65535 bytes).
		const maxSearch = zipEOCDMinSize + 65535
		searchLen := int64(maxSearch)
		if searchLen > size {
			searchLen = size
		}
		raw := zipTailPool.Get()
		bufPtr, ok := raw.(*[]byte)
		if !ok || bufPtr == nil {
			b := make([]byte, maxSearch)
			bufPtr = &b
		}
		buf := (*bufPtr)[:searchLen]
		if _, err := f.ReadAt(buf, size-searchLen); err != nil {
			zipTailPool.Put(bufPtr)
			return nil, fmt.Errorf("failed to read zip tail: %w", err)
		}
		eocdOff = findEOCD(buf)
		if eocdOff < 0 {
			zipTailPool.Put(bufPtr)
			return nil, errors.New("zip EOCD signature not found")
		}
		// Preserve the ZIP64 locator that immediately precedes the EOCD so that
		// the ZIP64 path below can still find it after we release the pool buffer.
		copyStart := eocdOff - zipEOCD64LocSize
		if copyStart < 0 {
			copyStart = 0
		}
		tail = make([]byte, len(buf)-copyStart)
		copy(tail, buf[copyStart:])
		eocdOff -= copyStart
		zipTailPool.Put(bufPtr)
	}

	eocd := tail[eocdOff:]
	entryCount := int(binary.LittleEndian.Uint16(eocd[10:]))
	cdSize := int64(binary.LittleEndian.Uint32(eocd[12:]))
	cdOffset := int64(binary.LittleEndian.Uint32(eocd[16:]))

	// Sentinel values in the standard EOCD indicate ZIP64 — look for the
	// ZIP64 EOCD locator that must appear immediately before the standard EOCD.
	if entryCount == 0xFFFF || cdSize == 0xFFFFFFFF || cdOffset == 0xFFFFFFFF {
		if eocdOff >= zipEOCD64LocSize {
			locOff := eocdOff - zipEOCD64LocSize
			if binary.LittleEndian.Uint32(tail[locOff:]) == zipSigEOCD64Loc {
				eocd64Abs := int64(binary.LittleEndian.Uint64(tail[locOff+8:])) //nolint:gosec // ZIP64 offset from spec
				buf64 := make([]byte, zipEOCD64Size)
				if _, readErr := f.ReadAt(buf64, eocd64Abs); readErr == nil &&
					binary.LittleEndian.Uint32(buf64) == zipSigEOCD64 {
					entryCount = int(binary.LittleEndian.Uint64(buf64[32:])) //nolint:gosec // G115: ZIP64 spec
					cdSize = int64(binary.LittleEndian.Uint64(buf64[40:]))   //nolint:gosec // G115: ZIP64 spec
					cdOffset = int64(binary.LittleEndian.Uint64(buf64[48:])) //nolint:gosec // G115: ZIP64 spec
				}
			}
		}
	}

	// On 32-bit platforms int(uint64) truncates — clamp to zero so the
	// maxEntries cap below produces a safe capacity hint.
	if entryCount < 0 {
		entryCount = 0
	}

	if cdOffset < 0 || cdSize < 0 || cdOffset+cdSize > size {
		return nil, fmt.Errorf("invalid zip central directory: offset=%d size=%d fileSize=%d", cdOffset, cdSize, size)
	}

	cd := make([]byte, cdSize)
	if _, err := f.ReadAt(cd, cdOffset); err != nil {
		return nil, fmt.Errorf("failed to read central directory: %w", err)
	}

	// Cap capacity to the theoretical maximum — each entry occupies at least
	// zipCDHdrSize bytes, so the central directory bounds entryCount.
	maxEntries := len(cd) / zipCDHdrSize
	if entryCount > maxEntries {
		entryCount = maxEntries
	}
	names := make([]string, 0, entryCount)
	pos := 0
	for pos+zipCDHdrSize <= len(cd) {
		if binary.LittleEndian.Uint32(cd[pos:]) != zipSigCentralDir {
			break
		}
		nameLen := int(binary.LittleEndian.Uint16(cd[pos+28:]))
		extraLen := int(binary.LittleEndian.Uint16(cd[pos+30:]))
		commentLen := int(binary.LittleEndian.Uint16(cd[pos+32:]))

		nameStart := pos + zipCDHdrSize
		nameEnd := nameStart + nameLen
		if nameEnd > len(cd) {
			break
		}
		names = append(names, string(cd[nameStart:nameEnd]))
		pos = nameEnd + extraLen + commentLen
	}

	return names, nil
}

// RandomElem picks and returns a random element from a slice.
func RandomElem[T any](xs []T) (T, error) {
	var item T
	if len(xs) == 0 {
		return item, errors.New("empty slice")
	}
	randInt, err := rand.Int(rand.Reader, big.NewInt(int64(len(xs))))
	if err != nil {
		return item, fmt.Errorf("failed to generate random number: %w", err)
	}
	item = xs[randInt.Int64()]
	return item, nil
}

// RandomInt returns a random integer between 0 and maxVal-1 (inclusive).
func RandomInt(maxVal int) (int, error) {
	if maxVal <= 0 {
		return 0, errors.New("maxVal must be positive")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(maxVal)))
	if err != nil {
		return 0, fmt.Errorf("failed to generate random number: %w", err)
	}
	return int(n.Int64()), nil
}

// CopyFile copies a file from sourcePath to destPath.
// Optional perm parameter sets file permissions (uses 0644 if not specified).
func CopyFile(sourcePath, destPath string, perm ...os.FileMode) error {
	//nolint:gosec // Safe: utility function for copying files with controlled paths
	inputFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", sourcePath, err)
	}
	defer func(inputFile *os.File) {
		_ = inputFile.Close()
	}(inputFile)

	//nolint:gosec // Safe: utility function for copying files with controlled paths
	outputFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func(outputFile *os.File) {
		_ = outputFile.Close()
	}(outputFile)

	_, err = io.Copy(outputFile, inputFile)
	if err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}
	err = outputFile.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// Set permissions if provided, otherwise use default 0644
	fileMode := os.FileMode(0o644)
	if len(perm) > 0 {
		fileMode = perm[0]
	}
	if err := os.Chmod(destPath, fileMode); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	return nil
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandSeq(n int) (string, error) {
	b := make([]rune, n)
	for i := range b {
		randInt, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "", fmt.Errorf("failed to generate secure random sequence: %w", err)
		}
		b[i] = letters[randInt.Int64()]
	}
	return string(b), nil
}

func YesNoPrompt(label string, def bool) bool {
	choices := "Y/n"
	if !def {
		choices = "y/N"
	}

	r := bufio.NewReader(os.Stdin)
	var s string

	for {
		_, _ = fmt.Fprintf(os.Stderr, "%s [%s] ", label, choices)
		s, _ = r.ReadString('\n')
		s = strings.TrimSpace(s)
		if s == "" {
			return def
		}
		s = strings.ToLower(s)
		if s == "y" || s == "yes" {
			return true
		}
		if s == "n" || s == "no" {
			return false
		}
	}
}

func IsTruthy(s string) bool {
	return strings.EqualFold(s, "true") || strings.EqualFold(s, "yes")
}

func IsFalsey(s string) bool {
	return strings.EqualFold(s, "false") || strings.EqualFold(s, "no")
}

func MaybeJSON(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\n', '\t', '\r':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}

// PadNumber formats a number with leading zeros to the specified width.
// Examples:
//   - PadNumber(5, 2) → "05"
//   - PadNumber(42, 4) → "0042"
//   - PadNumber(123, 2) → "123"
func PadNumber(num, width int) string {
	format := fmt.Sprintf("%%0%dd", width)
	return fmt.Sprintf(format, num)
}
