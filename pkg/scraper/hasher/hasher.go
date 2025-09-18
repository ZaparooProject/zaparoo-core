// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package hasher

import (
	"archive/zip"
	"crypto/md5"
	"crypto/sha1"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"strings"
)

// FileHash contains all hash information for a file
type FileHash struct {
	CRC32    string
	MD5      string
	SHA1     string
	FileSize int64
}

// ComputeFileHashes calculates all hashes for a file path
// Handles regular files and paths that include ZIP archives (MiSTer-style paths)
func ComputeFileHashes(filePath string) (*FileHash, error) {
	// Check if path contains a ZIP file (MiSTer-style path)
	// Example: /media/games/snes/roms.zip/Super Mario World.sfc
	for i := len(filePath) - 1; i >= 0; i-- {
		if i > 4 && strings.HasSuffix(filePath[:i+1], ".zip") {
			zipPath := filePath[:i+1]
			fileInZip := filePath[i+2:] // Skip the '/' after .zip

			if _, err := os.Stat(zipPath); err == nil {
				return computeFileInZip(zipPath, fileInZip)
			}
		}
	}

	// Regular file hashing
	return computeRegularFileHash(filePath)
}

// computeFileInZip extracts and hashes a specific file from a ZIP
func computeFileInZip(zipPath, fileInZip string) (*FileHash, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	// Find the specific file in the ZIP
	for _, f := range r.File {
		if f.Name == fileInZip {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open file in zip: %w", err)
			}
			defer rc.Close()

			return hashReader(rc, int64(f.UncompressedSize64))
		}
	}

	return nil, fmt.Errorf("file %s not found in archive %s", fileInZip, zipPath)
}

// computeRegularFileHash computes hashes for a regular file
func computeRegularFileHash(filePath string) (*FileHash, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file stats: %w", err)
	}

	return hashReader(file, stat.Size())
}

// hashReader computes all hashes from an io.Reader
func hashReader(r io.Reader, size int64) (*FileHash, error) {
	crc32Hash := crc32.NewIEEE()
	md5Hash := md5.New()
	sha1Hash := sha1.New()

	// Use io.MultiWriter to compute all hashes in one pass
	w := io.MultiWriter(crc32Hash, md5Hash, sha1Hash)

	if _, err := io.Copy(w, r); err != nil {
		return nil, fmt.Errorf("failed to read file for hashing: %w", err)
	}

	return &FileHash{
		CRC32:    fmt.Sprintf("%08x", crc32Hash.Sum32()),
		MD5:      fmt.Sprintf("%x", md5Hash.Sum(nil)),
		SHA1:     fmt.Sprintf("%x", sha1Hash.Sum(nil)),
		FileSize: size,
	}, nil
}

// ValidateHashes checks if the provided hashes match the file
func ValidateHashes(filePath string, expectedHash *FileHash) (bool, error) {
	computedHash, err := ComputeFileHashes(filePath)
	if err != nil {
		return false, err
	}

	// Check each hash type if provided
	if expectedHash.CRC32 != "" && computedHash.CRC32 != expectedHash.CRC32 {
		return false, nil
	}
	if expectedHash.MD5 != "" && computedHash.MD5 != expectedHash.MD5 {
		return false, nil
	}
	if expectedHash.SHA1 != "" && computedHash.SHA1 != expectedHash.SHA1 {
		return false, nil
	}
	if expectedHash.FileSize > 0 && computedHash.FileSize != expectedHash.FileSize {
		return false, nil
	}

	return true, nil
}

// FormatHashForScraper formats hash according to scraper requirements
func FormatHashForScraper(hash *FileHash, scraperName string) map[string]string {
	result := make(map[string]string)

	switch strings.ToLower(scraperName) {
	case "screenscraper":
		// ScreenScraper accepts CRC32, MD5, and SHA1
		if hash.CRC32 != "" {
			result["crc"] = strings.ToUpper(hash.CRC32)
		}
		if hash.MD5 != "" {
			result["md5"] = strings.ToUpper(hash.MD5)
		}
		if hash.SHA1 != "" {
			result["sha1"] = strings.ToUpper(hash.SHA1)
		}
		result["romsize"] = fmt.Sprintf("%d", hash.FileSize)

	case "thegamesdb":
		// TheGamesDB typically doesn't use hashes, but we can provide them
		result["md5"] = strings.ToLower(hash.MD5)

	case "igdb":
		// IGDB doesn't typically use file hashes
		// But we can provide them if the API supports it
		result["md5"] = strings.ToLower(hash.MD5)

	default:
		// Generic format - provide all hashes in lowercase
		result["crc32"] = strings.ToLower(hash.CRC32)
		result["md5"] = strings.ToLower(hash.MD5)
		result["sha1"] = strings.ToLower(hash.SHA1)
		result["size"] = fmt.Sprintf("%d", hash.FileSize)
	}

	return result
}
