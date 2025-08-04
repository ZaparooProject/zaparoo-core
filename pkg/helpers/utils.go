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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
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

func GetMd5Hash(path string) (string, error) {
	//nolint:gosec // Safe: opens files for MD5 hashing, used for game file identification
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file for MD5 hash: %w", err)
	}
	//nolint:gosec // Used for game file hashing/matching against existing retro gaming databases
	hash := md5.New()
	_, _ = io.Copy(hash, file)
	_ = file.Close()
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func GetFileSize(path string) (int64, error) {
	//nolint:gosec // Safe: opens files to get file size, used for game file analysis
	file, err := os.Open(path)
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
		if err == nil {
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

func IsZip(path string) bool {
	return filepath.Ext(strings.ToLower(path)) == ".zip"
}

// ListZip returns a slice of all filenames in a zip file.
func ListZip(path string) ([]string, error) {
	r, err := zip.OpenReader(path)
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

func RandSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		randInt, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			// Fallback to timestamp-based selection if crypto/rand fails
			b[i] = letters[int(time.Now().UnixNano())%len(letters)]
		} else {
			b[i] = letters[randInt.Int64()]
		}
	}
	return string(b)
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

func FilenameFromPath(path string) string {
	p := filepath.Clean(path)
	b := filepath.Base(p)
	e := filepath.Ext(p)
	if HasSpace(e) {
		e = ""
	}
	r, _ := strings.CutSuffix(b, e)
	return r
}

func SlugifyPath(path string) string {
	fn := FilenameFromPath(path)
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
