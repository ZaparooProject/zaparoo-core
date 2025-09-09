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

package helpers

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/md5" //nolint:gosec // Used for game file hashing/matching against existing retro gaming databases
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

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
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
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
	for i := 0; i < maxTries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com", http.NoBody)
		if err != nil {
			cancel()
			continue
		}

		resp, err := http.DefaultClient.Do(req)
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
	return filepath.Ext(strings.ToLower(filePath)) == ".zip"
}

// ListZip returns a slice of all filenames in a zip file.
func ListZip(filePath string) ([]string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip file: %w", err)
	}
	defer func(r *zip.ReadCloser) {
		err := r.Close()
		if err != nil {
			log.Warn().Err(err).Msg("close zip failed")
		}
	}(r)

	files := make([]string, 0, len(r.File))
	for _, f := range r.File {
		files = append(files, f.Name)
	}

	return files, nil
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

func CopyFile(sourcePath, destPath string) error {
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

var reSlug = regexp.MustCompile(`(\(.*\))|(\[.*])|[^a-z0-9A-Z]`)

func SlugifyString(input string) string {
	rep := reSlug.ReplaceAllStringFunc(input, func(_ string) string {
		return ""
	})
	return strings.ToLower(rep)
}

// CreateVirtualPath creates a properly encoded virtual path for media
// Example: "kodi-show", "123", "Some Hot/Cold" -> "kodi-show://123/Some%20Hot%2FCold"
func CreateVirtualPath(scheme, id, name string) string {
	return fmt.Sprintf("%s://%s/%s", scheme, id, url.PathEscape(name))
}

// VirtualPathResult holds parsed virtual path components
type VirtualPathResult struct {
	Scheme string
	ID     string
	Name   string
}

// ParseVirtualPathStr parses a virtual path and returns its components with string ID
func ParseVirtualPathStr(virtualPath string) (result VirtualPathResult, err error) {
	if !strings.Contains(virtualPath, "://") {
		return result, errors.New("not a virtual path")
	}

	parts := strings.SplitN(virtualPath, "://", 2)
	if len(parts) != 2 {
		return result, errors.New("invalid virtual path format")
	}

	result.Scheme = parts[0]
	idAndName := strings.SplitN(parts[1], "/", 2)
	if len(idAndName) < 1 {
		return result, errors.New("missing ID in virtual path")
	}

	result.ID = idAndName[0]
	if len(idAndName) == 2 {
		decoded, decodeErr := url.PathUnescape(idAndName[1])
		if decodeErr == nil {
			result.Name = decoded
		} else {
			result.Name = idAndName[1] // Fallback to undecoded
		}
	}

	return result, nil
}

func FilenameFromPath(p string) string {
	if p == "" {
		return ""
	}

	// Try to parse as virtual path first
	if strings.Contains(p, "://") {
		result, err := ParseVirtualPathStr(p)
		if err == nil && result.Name != "" {
			return result.Name
		}
	}

	// Regular file path - use existing logic
	// Convert to forward slash format for consistent cross-platform parsing
	// Replace backslashes with forward slashes to handle Windows paths on any OS
	normalizedPath := strings.ReplaceAll(p, "\\", "/")
	b := path.Base(normalizedPath)
	e := path.Ext(normalizedPath)
	if HasSpace(e) {
		e = ""
	}
	r, _ := strings.CutSuffix(b, e)
	return r
}

func SlugifyPath(filePath string) string {
	fn := FilenameFromPath(filePath)
	return SlugifyString(fn)
}

func HasSpace(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			return true
		}
	}
	return false
}

func IsServiceRunning(cfg *config.Instance) bool {
	_, err := client.LocalClient(context.Background(), cfg, models.MethodVersion, "")
	if err != nil {
		log.Debug().Err(err).Msg("error checking if service running")
		return false
	}
	return true
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
