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

package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// appEncodings returns relative gzip and identity preferences in thousandths.
// Identity is acceptable by default, except for an explicit exclusion or *;q=0.
func appEncodings(values []string) (gzipPreference, identityPreference int) {
	qualities := map[string]int{}
	for _, value := range values {
		for item := range strings.SplitSeq(value, ",") {
			parts := strings.Split(item, ";")
			coding := strings.ToLower(strings.TrimSpace(parts[0]))
			if coding != "gzip" && coding != "identity" && coding != "*" {
				continue
			}
			quality := 1000
			if len(parts) > 2 {
				quality = 0
				parts = parts[:1]
			}
			for _, param := range parts[1:] {
				key, val, ok := strings.Cut(strings.TrimSpace(param), "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
					quality = 0
					break
				}
				quality = appQuality(strings.TrimSpace(val))
			}
			// Conflicting duplicate codings never override an exclusion.
			if previous, ok := qualities[coding]; ok {
				quality = min(previous, quality)
			}
			qualities[coding] = quality
		}
	}
	gzipQuality, ok := qualities["gzip"]
	if !ok {
		gzipQuality = qualities["*"]
	}
	identityQuality, ok := qualities["identity"]
	if !ok {
		identityQuality = 1000
		if wildcard, exists := qualities["*"]; exists && wildcard == 0 {
			identityQuality = 0
		}
	}
	return gzipQuality, identityQuality
}

func appQuality(value string) int {
	whole, fractional, _ := strings.Cut(value, ".")
	if whole != "0" && whole != "1" || len(fractional) > 3 {
		return 0
	}
	quality := 0
	for _, digit := range fractional {
		if digit < '0' || digit > '9' || whole == "1" && digit != '0' {
			return 0
		}
		quality = quality*10 + int(digit-'0')
	}
	for i := len(fractional); i < 3; i++ {
		quality *= 10
	}
	if whole == "1" {
		return 1000
	}
	return quality
}

func serveCompressedAppContent(
	w http.ResponseWriter, r *http.Request, name string, info os.FileInfo, file http.File,
) {
	gzipQuality, identityQuality := appEncodings(r.Header.Values("Accept-Encoding"))
	if gzipQuality == 0 && identityQuality == 0 {
		http.Error(w, http.StatusText(http.StatusNotAcceptable), http.StatusNotAcceptable)
		return
	}
	useGzip := gzipQuality > 0 && gzipQuality >= identityQuality
	// Byte ranges are most useful on the decoded representation. If identity
	// is forbidden, serve a complete gzip response rather than multipart gzip.
	if r.Header.Get("Range") != "" && identityQuality > 0 {
		useGzip = false
	}
	var decoded []byte
	if !useGzip || w.Header().Get("Content-Type") == "" {
		reader, err := gzip.NewReader(file)
		if err != nil {
			appDecompressionError(w, err)
			return
		}
		var source io.Reader = reader
		if useGzip {
			source = io.LimitReader(reader, 512)
		}
		decoded, err = io.ReadAll(source)
		_ = reader.Close()
		if err != nil {
			appDecompressionError(w, err)
			return
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(decoded))
		}
	}
	if !useGzip {
		http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(decoded))
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		appDecompressionError(w, err)
		return
	}
	if r.Header.Get("Range") != "" {
		r = r.Clone(r.Context())
		r.Header.Del("Range")
		r.Header.Del("If-Range")
	}
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func appDecompressionError(w http.ResponseWriter, err error) {
	log.Error().Err(err).Msg("decompress embedded app asset")
	w.Header().Del("Content-Type")
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
