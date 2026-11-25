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

package externaldrive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/sidecar"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

// resolveSidecarImage turns the image field into a path on the drive, or ""
// when it does not name a PNG or JPEG regular file under mount.
func resolveSidecarImage(mount, rel string) string {
	if rel == "" {
		return ""
	}
	if !sidecar.ImageExtension(rel) {
		log.Warn().Str("image", rel).Msg("ignoring zaparoo.json image: not a .png, .jpg or .jpeg file")
		return ""
	}
	path, info, err := pathutil.LocalFileUnder(mount, rel)
	if err != nil {
		log.Warn().Err(err).Str("image", rel).Msg("ignoring zaparoo.json image: not a file on the drive")
		return ""
	}
	if info.Size() > imagefiles.MaxBytes {
		log.Warn().Str("image", rel).Int64("size", info.Size()).Msg("ignoring zaparoo.json image: too large")
		return ""
	}
	return path
}

// loadSidecar reads zaparoo.json from the root of mount. It returns nil when
// there is no usable file or nothing in it survives validation. Any problem
// is logged and never stops the token from being scanned.
func loadSidecar(mount string) *tokens.Metadata {
	name := findFileInDir(mount, sidecar.FileName)
	if name == "" {
		return nil
	}
	path, info, err := pathutil.LocalFileUnder(mount, filepath.Base(name))
	if err != nil {
		log.Warn().Err(err).Str("path", name).Msg("ignoring zaparoo.json: not a regular file")
		return nil
	}
	if info.Size() > sidecar.MaxBytes {
		log.Warn().Str("path", path).Int64("size", info.Size()).Msg("ignoring zaparoo.json: exceeds maximum size")
		return nil
	}
	data, err := readSidecarFile(path)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("ignoring zaparoo.json: read failed")
		return nil
	}
	fields, err := sidecar.Parse(data)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("ignoring zaparoo.json")
		return nil
	}
	meta := &tokens.Metadata{
		Name:        fields.Name,
		Description: fields.Description,
		ImagePath:   resolveSidecarImage(mount, fields.Image),
	}
	if *meta == (tokens.Metadata{}) {
		return nil
	}
	return meta
}

func readSidecarFile(path string) ([]byte, error) {
	//nolint:gosec // path was checked to be a regular file under the mount
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, sidecar.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(data) > sidecar.MaxBytes {
		return nil, errors.New("exceeds maximum size")
	}
	return data, nil
}
