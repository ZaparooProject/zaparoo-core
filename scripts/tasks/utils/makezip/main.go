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

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const baseURL = "https://github.com/ZaparooProject/zaparoo.org/raw/refs/heads/main/docs/platforms/"

var platformDocs = map[string]string{
	"batocera":  "batocera/index.md",
	"bazzite":   "bazzite.mdx",
	"chimeraos": "chimeraos.mdx",
	"libreelec": "libreelec.md",
	"linux":     "linux/index.md",
	"mac":       "mac.mdx",
	"mister":    "mister/index.md",
	"mistex":    "mistex.md",
	"recalbox":  "recalbox.mdx",
	"steamos":   "steamos.md",
	"windows":   "windows/index.md",
}

// platformURLs maps platform IDs to their online documentation URLs
var platformURLs = map[string]string{
	"batocera":  "https://zaparoo.org/docs/platforms/batocera/",
	"bazzite":   "https://zaparoo.org/docs/platforms/bazzite/",
	"chimeraos": "https://zaparoo.org/docs/platforms/chimeraos/",
	"libreelec": "https://zaparoo.org/docs/platforms/libreelec/",
	"linux":     "https://zaparoo.org/docs/platforms/linux/",
	"mac":       "https://zaparoo.org/docs/platforms/mac/",
	"mister":    "https://zaparoo.org/docs/platforms/mister/",
	"mistex":    "https://zaparoo.org/docs/platforms/mistex/",
	"recalbox":  "https://zaparoo.org/docs/platforms/recalbox/",
	"steamos":   "https://zaparoo.org/docs/platforms/steamos/",
	"windows":   "https://zaparoo.org/docs/platforms/windows/",
}

var extraItems = map[string][]string{
	"batocera": {"cmd/batocera/scripts"},
}

func stripFrontmatter(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[0] == "---" {
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				return strings.Join(lines[i+1:], "\n")
			}
		}
	}
	return content
}

// expandRelativeLinks converts relative markdown links to absolute zaparoo.org URLs
func expandRelativeLinks(content, _ string) string {
	baseDocsURL := "https://zaparoo.org/docs/"

	// Pattern for markdown links: [text](path.md) or [text](path.mdx)
	// Captures the full relative path including any ../ prefixes
	linkPattern := regexp.MustCompile(`\]\(([^)]+\.mdx?)(#[^)]+)?\)`)

	return linkPattern.ReplaceAllStringFunc(content, func(match string) string {
		submatches := linkPattern.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		fullPath := submatches[1]
		anchor := ""
		if len(submatches) > 2 {
			anchor = submatches[2]
		}

		// Skip external links and absolute paths
		if strings.HasPrefix(fullPath, "http") || strings.HasPrefix(fullPath, "/") {
			return match
		}

		// Count and strip leading ../ sequences
		upLevels := 0
		path := fullPath
		for strings.HasPrefix(path, "../") {
			upLevels++
			path = strings.TrimPrefix(path, "../")
		}
		// Also handle ./ prefix (same directory)
		path = strings.TrimPrefix(path, "./")

		// Remove .md or .mdx extension
		path = strings.TrimSuffix(path, ".mdx")
		path = strings.TrimSuffix(path, ".md")

		// Remove trailing /index since zaparoo.org doesn't need it in URLs
		path = strings.TrimSuffix(path, "/index")
		path = strings.TrimSuffix(path, "index")

		// Build the absolute URL based on how many levels up we go
		// Source docs are at docs/platforms/{platform}/, so:
		// - 0 levels (./): stays in platforms/ directory
		// - 1 level (../): goes to platforms/ parent (but we treat as docs/)
		// - 2+ levels (../../): goes to docs/
		var absURL string
		if upLevels == 0 {
			// Same directory or subdirectory - relative to platforms
			absURL = baseDocsURL + "platforms/" + path
		} else {
			// Going up from platforms directory - resolve to docs base
			absURL = baseDocsURL + path
		}

		// Ensure URL ends with / and clean up any double slashes
		if !strings.HasSuffix(absURL, "/") {
			absURL += "/"
		}
		absURL = strings.ReplaceAll(absURL, "docs//", "docs/")

		return "](" + absURL + anchor + ")"
	})
}

// addDocFooter appends a footer with link to full documentation
func addDocFooter(content, platformID string) string {
	docURL, ok := platformURLs[platformID]
	if !ok {
		docURL = "https://zaparoo.org/docs/"
	}

	footer := fmt.Sprintf("\n\n---\n\nFull documentation: %s\n", docURL)
	return content + footer
}

func downloadDoc(platformID, toDir string) error {
	fileName, ok := platformDocs[platformID]
	if !ok {
		return fmt.Errorf("platform '%s' not found in the platforms list", platformID)
	}

	url := baseURL + fileName

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	if resp == nil {
		return errors.New("received nil response")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_, _ = fmt.Printf("error closing response body: %v\n", closeErr)
		}
	}()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	processedContent := string(content)

	// Strip frontmatter from MDX files
	if strings.HasSuffix(strings.ToLower(fileName), ".mdx") {
		processedContent = stripFrontmatter(processedContent)
	}

	// Expand relative links to absolute URLs
	processedContent = expandRelativeLinks(processedContent, platformID)

	// Add footer with link to full documentation
	processedContent = addDocFooter(strings.TrimSpace(processedContent), platformID)

	readmePath := filepath.Join(toDir, "README.txt")
	readmeContent := []byte(processedContent + "\n")
	if err := os.WriteFile(readmePath, readmeContent, 0o600); err != nil {
		return fmt.Errorf("failed to write README.txt: %w", err)
	}
	return nil
}

func main() {
	if len(os.Args) < 5 {
		_, _ = fmt.Println("Usage: go run makezip.go <platform> <build_dir> <app_bin> <archive_name>")
		os.Exit(1)
	}

	platform := os.Args[1]
	buildDir := os.Args[2]
	appBin := os.Args[3]
	archiveName := os.Args[4]

	if strings.HasPrefix(platform, "test") {
		os.Exit(0)
	}

	if _, err := os.Stat(buildDir); os.IsNotExist(err) {
		_, _ = fmt.Printf("The specified directory '%s' does not exist\n", buildDir)
		os.Exit(1)
	}

	licensePath := filepath.Join(buildDir, "LICENSE.txt")
	if _, err := os.Stat(licensePath); os.IsNotExist(err) {
		input, err := os.ReadFile("LICENSE")
		if err != nil {
			_, _ = fmt.Printf("Error reading LICENSE file: %v\n", err)
			os.Exit(1)
		}
		err = os.WriteFile(licensePath, input, 0o600)
		if err != nil {
			_, _ = fmt.Printf("Error copying LICENSE file: %v\n", err)
			os.Exit(1)
		}
	}

	appPath := filepath.Join(buildDir, appBin)
	if _, err := os.Stat(appPath); os.IsNotExist(err) {
		_, _ = fmt.Printf("The specified binary file '%s' does not exist\n", appPath)
		os.Exit(1)
	}

	archivePath := filepath.Join(buildDir, archiveName)
	_ = os.Remove(archivePath)

	readmePath := filepath.Join(buildDir, "README.txt")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		if err := downloadDoc(platform, buildDir); err != nil {
			_, _ = fmt.Printf("Error downloading documentation: %v\n", err)
			os.Exit(1)
		}
	}

	// Determine format based on file extension
	var err error
	if strings.HasSuffix(archiveName, ".tar.gz") {
		err = createTarGzFile(archivePath, appPath, licensePath, readmePath, platform, buildDir)
	} else {
		err = createZipFile(archivePath, appPath, licensePath, readmePath, platform, buildDir)
	}

	if err != nil {
		_, _ = fmt.Printf("Error creating archive: %v\n", err)
		os.Exit(1)
	}
}

func createZipFile(zipPath, appPath, licensePath, readmePath, platform, buildDir string) error {
	//nolint:gosec // Safe: creates zip files in build script with controlled paths
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("error creating zip file: %w", err)
	}
	defer func(zipFile *os.File) {
		_ = zipFile.Close()
	}(zipFile)

	zipWriter := zip.NewWriter(zipFile)
	defer func(zipWriter *zip.Writer) {
		if err := zipWriter.Close(); err != nil {
			_, _ = fmt.Printf("warning: failed to close zip writer: %v\n", err)
		}
	}(zipWriter)

	filesToAdd := []struct {
		path    string
		arcname string
	}{
		{appPath, filepath.Base(appPath)},
		{licensePath, filepath.Base(licensePath)},
		{readmePath, filepath.Base(readmePath)},
	}

	for _, file := range filesToAdd {
		err := addFileToZip(zipWriter, file.path, file.arcname)
		if err != nil {
			return fmt.Errorf("error adding file to zip: %w", err)
		}
	}

	if items, ok := extraItems[platform]; ok {
		for _, item := range items {
			if info, err := os.Stat(item); err == nil {
				if info.IsDir() {
					err = addDirToZip(zipWriter, item, buildDir)
				} else {
					destPath := filepath.Join(buildDir, filepath.Base(item))
					if copyErr := copyFile(item, destPath); copyErr != nil {
						return fmt.Errorf("error copying extra file: %w", copyErr)
					}
					err = addFileToZip(zipWriter, destPath, filepath.Base(item))
				}
				if err != nil {
					return fmt.Errorf("error adding extra item to zip: %w", err)
				}
			}
		}
	}

	return nil
}

func addFileToZip(zipWriter *zip.Writer, filePath, arcname string) error {
	//nolint:gosec // Safe: opens files in build script with controlled paths
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("operation failed: %w", err)
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("operation failed: %w", err)
	}
	header.Name = arcname
	header.Method = zip.Deflate

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("operation failed: %w", err)
	}

	_, err = io.Copy(writer, file)
	if err != nil {
		return fmt.Errorf("failed to copy file content to zip: %w", err)
	}
	return nil
}

func addDirToZip(zipWriter *zip.Writer, dirPath, buildDir string) error {
	if err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relPath, err := filepath.Rel(dirPath, path)
			if err != nil {
				return fmt.Errorf("failed to get relative path: %w", err)
			}

			destPath := filepath.Join(buildDir, filepath.Base(dirPath), relPath)
			if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			if err := copyFile(path, destPath); err != nil {
				return err
			}

			return addFileToZip(zipWriter, destPath, filepath.Join(filepath.Base(dirPath), relPath))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to walk directory %s: %w", dirPath, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	//nolint:gosec // Safe: reads files in build script with controlled paths
	input, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("operation failed: %w", err)
	}
	if err := os.WriteFile(dst, input, 0o600); err != nil {
		return fmt.Errorf("failed to write file %s: %w", dst, err)
	}
	return nil
}

func createTarGzFile(tarGzPath, appPath, licensePath, readmePath, platform, buildDir string) error {
	//nolint:gosec // Safe: creates tar.gz files in build script with controlled paths
	tarGzFile, err := os.Create(tarGzPath)
	if err != nil {
		return fmt.Errorf("error creating tar.gz file: %w", err)
	}
	defer func(tarGzFile *os.File) {
		_ = tarGzFile.Close()
	}(tarGzFile)

	gzipWriter := gzip.NewWriter(tarGzFile)
	defer func(gzipWriter *gzip.Writer) {
		if err := gzipWriter.Close(); err != nil {
			_, _ = fmt.Printf("warning: failed to close gzip writer: %v\n", err)
		}
	}(gzipWriter)

	tarWriter := tar.NewWriter(gzipWriter)
	defer func(tarWriter *tar.Writer) {
		if err := tarWriter.Close(); err != nil {
			_, _ = fmt.Printf("warning: failed to close tar writer: %v\n", err)
		}
	}(tarWriter)

	filesToAdd := []struct {
		path    string
		arcname string
	}{
		{appPath, filepath.Base(appPath)},
		{licensePath, filepath.Base(licensePath)},
		{readmePath, filepath.Base(readmePath)},
	}

	for _, file := range filesToAdd {
		err := addFileToTar(tarWriter, file.path, file.arcname)
		if err != nil {
			return fmt.Errorf("error adding file to tar: %w", err)
		}
	}

	if items, ok := extraItems[platform]; ok {
		for _, item := range items {
			if info, err := os.Stat(item); err == nil {
				if info.IsDir() {
					err = addDirToTar(tarWriter, item, buildDir)
				} else {
					destPath := filepath.Join(buildDir, filepath.Base(item))
					if copyErr := copyFile(item, destPath); copyErr != nil {
						return fmt.Errorf("error copying extra file: %w", copyErr)
					}
					err = addFileToTar(tarWriter, destPath, filepath.Base(item))
				}
				if err != nil {
					return fmt.Errorf("error adding extra item to tar: %w", err)
				}
			}
		}
	}

	return nil
}

func addFileToTar(tarWriter *tar.Writer, filePath, arcname string) error {
	//nolint:gosec // Safe: opens files in build script with controlled paths
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("operation failed: %w", err)
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("operation failed: %w", err)
	}
	header.Name = arcname

	err = tarWriter.WriteHeader(header)
	if err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	_, err = io.Copy(tarWriter, file)
	if err != nil {
		return fmt.Errorf("failed to copy file content to tar: %w", err)
	}
	return nil
}

func addDirToTar(tarWriter *tar.Writer, dirPath, buildDir string) error {
	if err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relPath, err := filepath.Rel(dirPath, path)
			if err != nil {
				return fmt.Errorf("failed to get relative path: %w", err)
			}

			destPath := filepath.Join(buildDir, filepath.Base(dirPath), relPath)
			if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			if err := copyFile(path, destPath); err != nil {
				return err
			}

			return addFileToTar(tarWriter, destPath, filepath.Join(filepath.Base(dirPath), relPath))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to walk directory %s: %w", dirPath, err)
	}
	return nil
}
