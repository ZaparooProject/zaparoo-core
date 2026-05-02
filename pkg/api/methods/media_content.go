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

package methods

import (
	"mime"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

var contentTypeExtensions = map[string]string{
	"image/gif":     "gif",
	"image/jpeg":    "jpg",
	"image/jpg":     "jpg",
	"image/png":     "png",
	"image/svg+xml": "svg",
	"image/webp":    "webp",
}

func mediaContentExtension(contentType, text string) *string {
	if ext := extensionFromContentType(contentType); ext != "" {
		return &ext
	}
	if ext := extensionFromTextPath(text); ext != "" {
		return &ext
	}
	return nil
}

func extensionFromContentType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(contentType))
	}
	if ext, ok := contentTypeExtensions[mediaType]; ok {
		return ext
	}

	exts, err := mime.ExtensionsByType(mediaType)
	if err != nil || len(exts) == 0 {
		return ""
	}
	return strings.TrimPrefix(exts[0], ".")
}

func extensionFromTextPath(text string) string {
	if text == "" {
		return ""
	}

	pathText := text
	if u, err := url.Parse(text); err == nil && u.Path != "" {
		pathText = u.Path
	}

	ext := filepath.Ext(pathText)
	if ext == "" {
		ext = path.Ext(pathText)
	}
	return strings.TrimPrefix(strings.ToLower(ext), ".")
}
