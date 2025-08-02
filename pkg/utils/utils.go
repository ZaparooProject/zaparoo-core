/*
Zaparoo Core
Copyright (C) 2023, 2024 Callan Barrett

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

package utils

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"math/rand"
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

var r = rand.New(rand.NewSource(time.Now().UnixNano()))

func TokensEqual(a, b *tokens.Token) bool {
	if a == nil && b == nil {
		return true
	} else if a == nil || b == nil {
		return false
	}

	return a.UID == b.UID && a.Text == b.Text
}

func GetMd5Hash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := md5.New()
	_, _ = io.Copy(hash, file)
	_ = file.Close()
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func GetFileSize(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}

	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return 0, err
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
		_, err := http.Get("https://api.github.com")
		if err == nil {
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
		return nil, err
	}
	defer func(r *zip.ReadCloser) {
		err := r.Close()
		if err != nil {
			log.Warn().Err(err).Msg("close zip failed")
		}
	}(r)

	var files []string
	for _, f := range r.File {
		files = append(files, f.Name)
	}

	return files, nil
}

// RandomElem picks and returns a random element from a slice.
func RandomElem[T any](xs []T) (T, error) {
	var item T
	if len(xs) == 0 {
		return item, fmt.Errorf("empty slice")
	} else {
		item = xs[r.Intn(len(xs))]
		return item, nil
	}
}

func CopyFile(sourcePath, destPath string) error {
	inputFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func(inputFile *os.File) {
		_ = inputFile.Close()
	}(inputFile)

	outputFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer func(outputFile *os.File) {
		_ = outputFile.Close()
	}(outputFile)

	_, err = io.Copy(outputFile, inputFile)
	if err != nil {
		return err
	}
	err = outputFile.Sync()
	if err != nil {
		return err
	}
	return inputFile.Close()
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
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
	rep := reSlug.ReplaceAllStringFunc(input, func(m string) string {
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
