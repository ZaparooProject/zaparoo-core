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

// Compress-app prepares deterministic, compressed-only embedded web assets.
package main

import (
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

func main() {
	source := flag.String("source", "pkg/assets/_app/dist", "uncompressed app directory")
	output := flag.String("output", "pkg/assets/_app/packed/dist", "generated compressed directory")
	flag.Parse()
	if err := compressApp(afero.NewOsFs(), *source, *output); err != nil {
		log.Fatal().Err(err).Msg("compress embedded app")
	}
}

func compressApp(fs afero.Fs, source, output string) error {
	source, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve app source: %w", err)
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve app output: %w", err)
	}
	if within(source, output) || within(output, source) {
		return errors.New("app source and output must not overlap")
	}
	if _, err = fs.Stat(source); errors.Is(err, os.ErrNotExist) {
		// A source-less checkout must not silently reuse a previously built app.
		if err = fs.RemoveAll(output); err != nil {
			return fmt.Errorf("remove stale app output: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("stat app source: %w", err)
	}
	if err = fs.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create app output parent: %w", err)
	}
	// Keep staging invisible to go:embed while another command inspects assets.
	tmp, err := afero.TempDir(fs, filepath.Dir(output), ".compress-app-")
	if err != nil {
		return fmt.Errorf("create app staging directory: %w", err)
	}
	defer func() { _ = fs.RemoveAll(tmp) }()

	err = afero.Walk(fs, source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			if !info.IsDir() {
				return errors.New("app source must be a directory")
			}
			return nil
		}
		// Match go:embed's previous treatment of hidden files and directories.
		if strings.HasPrefix(info.Name(), ".") || strings.HasPrefix(info.Name(), "_") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("app asset is not a regular file: %s", path)
		}
		rel, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return fmt.Errorf("resolve app asset: %w", relErr)
		}
		return compressFile(fs, path, filepath.Join(tmp, rel+".gz"))
	})
	if err != nil {
		return fmt.Errorf("compress app assets: %w", err)
	}
	if err := fs.RemoveAll(output); err != nil {
		return fmt.Errorf("remove previous app output: %w", err)
	}
	if err := fs.Rename(tmp, output); err != nil {
		return fmt.Errorf("install compressed app: %w", err)
	}
	return nil
}

func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func compressFile(fs afero.Fs, source, output string) error {
	in, err := fs.Open(source)
	if err != nil {
		return fmt.Errorf("open app asset: %w", err)
	}
	defer func() { _ = in.Close() }()
	if err = fs.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create asset directory: %w", err)
	}
	out, err := fs.Create(output)
	if err != nil {
		return fmt.Errorf("create compressed asset: %w", err)
	}
	defer func() { _ = out.Close() }()
	gz, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	if _, err := io.Copy(gz, in); err != nil {
		_ = gz.Close()
		return fmt.Errorf("compress asset: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("finish compressed asset: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close compressed asset: %w", err)
	}
	return nil
}
