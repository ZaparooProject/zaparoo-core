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

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
	"github.com/spf13/afero"
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
	"replayos":  "replayos.md",
	"steamos":   "steamos.md",
	"windows":   "windows/index.md",
	// Interim packaging fallback until dedicated ZapOS docs are published.
	"zapos": "linux/index.md",
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
	"replayos":  "https://zaparoo.org/docs/platforms/replayos/",
	"steamos":   "https://zaparoo.org/docs/platforms/steamos/",
	"windows":   "https://zaparoo.org/docs/platforms/windows/",
	// Interim packaging fallback until dedicated ZapOS docs are published.
	"zapos": "https://zaparoo.org/docs/platforms/linux/",
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
		linkPath := fullPath
		for strings.HasPrefix(linkPath, "../") {
			upLevels++
			linkPath = strings.TrimPrefix(linkPath, "../")
		}
		// Also handle ./ prefix (same directory)
		linkPath = strings.TrimPrefix(linkPath, "./")

		// Remove .md or .mdx extension
		linkPath = strings.TrimSuffix(linkPath, ".mdx")
		linkPath = strings.TrimSuffix(linkPath, ".md")

		// Remove a path-final index segment since zaparoo.org doesn't need
		// it in URLs. A filename such as platform-index is not an index page.
		if linkPath == "index" {
			linkPath = ""
		} else {
			linkPath = strings.TrimSuffix(linkPath, "/index")
		}

		// Build the absolute URL based on how many levels up we go
		// Source docs are at docs/platforms/{platform}/, so:
		// - 0 levels (./): stays in platforms/ directory
		// - 1 level (../): goes to platforms/ parent (but we treat as docs/)
		// - 2+ levels (../../): goes to docs/
		var absURL string
		if upLevels == 0 {
			// Same directory or subdirectory - relative to platforms
			absURL = baseDocsURL + "platforms/" + linkPath
		} else {
			// Going up from platforms directory - resolve to docs base
			absURL = baseDocsURL + linkPath
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

func fetchWithRetries(url string, maxRetries int) ([]byte, error) {
	var lastErr error
	for attempt := range maxRetries {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create HTTP request: %w", err)
		}

		resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: hardcoded doc download URL
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("attempt %d: %w", attempt+1, err)
			_, _ = fmt.Printf("Retry %d/%d: request failed: %v\n", attempt+1, maxRetries, err)
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			cancel()
			lastErr = fmt.Errorf(
				"attempt %d: HTTP %d: %s", attempt+1, resp.StatusCode, http.StatusText(resp.StatusCode),
			)
			_, _ = fmt.Printf("Retry %d/%d: HTTP %d\n", attempt+1, maxRetries, resp.StatusCode)
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}

		content, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("attempt %d: failed to read body: %w", attempt+1, err)
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}

		return content, nil
	}
	return nil, fmt.Errorf("all %d attempts failed, last error: %w", maxRetries, lastErr)
}

func downloadDoc(platformID, toDir string) error {
	fileName, ok := platformDocs[platformID]
	if !ok {
		return fmt.Errorf("platform '%s' not found in the platforms list", platformID)
	}

	url := baseURL + fileName

	content, err := fetchWithRetries(url, 5)
	if err != nil {
		return fmt.Errorf("downloading %s docs: %w", platformID, err)
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
	//nolint:gosec // G703: build script, not user-facing
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

	if _, err := os.Stat(buildDir); os.IsNotExist(err) { //nolint:gosec // G703: build script
		_, _ = fmt.Printf("The specified directory '%s' does not exist\n", buildDir)
		os.Exit(1)
	}

	licensePath := filepath.Join(buildDir, "LICENSE.txt")
	if _, err := os.Stat(licensePath); os.IsNotExist(err) { //nolint:gosec // G703: build script
		input, err := os.ReadFile("LICENSE")
		if err != nil {
			_, _ = fmt.Printf("Error reading LICENSE file: %v\n", err)
			os.Exit(1)
		}
		err = os.WriteFile(licensePath, input, 0o600) //nolint:gosec // G703: build script
		if err != nil {
			_, _ = fmt.Printf("Error copying LICENSE file: %v\n", err)
			os.Exit(1)
		}
	}

	appPath := filepath.Join(buildDir, appBin)
	if _, err := os.Stat(appPath); os.IsNotExist(err) { //nolint:gosec // G703: build script
		_, _ = fmt.Printf("The specified binary file '%s' does not exist\n", appPath)
		os.Exit(1)
	}

	archivePath := filepath.Join(buildDir, archiveName)
	_ = os.Remove(archivePath) //nolint:gosec // G703: build script

	readmePath := filepath.Join(buildDir, "README.txt")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) { //nolint:gosec // G703: build script
		if err := downloadDoc(platform, buildDir); err != nil {
			_, _ = fmt.Printf("Error downloading documentation: %v\n", err)
			os.Exit(1)
		}
	}

	// Determine format based on file extension
	fs := afero.NewOsFs()
	var err error
	if strings.HasSuffix(archiveName, ".tar.gz") {
		err = createTarGzFile(fs, archivePath, appPath, licensePath, readmePath, platform, buildDir)
	} else {
		err = createZipFile(fs, archivePath, appPath, licensePath, readmePath, platform, buildDir)
	}

	if err != nil {
		_, _ = fmt.Printf("Error creating archive: %v\n", err)
		os.Exit(1)
	}
}

func createZipFile(fs afero.Fs, zipPath, appPath, licensePath, readmePath, platform, _ string) error {
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
		err := addFileToZip(fs, zipWriter, file.path, file.arcname)
		if err != nil {
			return fmt.Errorf("error adding file to zip: %w", err)
		}
	}

	return addPayloadToZip(fs, zipWriter, payloadFiles(platform))
}

func validatePayloadFiles(files []updatepayload.File) error {
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if _, duplicate := seen[file.ArchivePath]; duplicate {
			return fmt.Errorf("duplicate payload archive path %q", file.ArchivePath)
		}
		seen[file.ArchivePath] = struct{}{}
		if _, ok := updatepayload.MatchArchiveFile([]updatepayload.File{file}, file.ArchivePath); !ok {
			return fmt.Errorf("payload archive path %q is invalid", file.ArchivePath)
		}
	}
	return nil
}

func payloadSourceInfo(fs afero.Fs, path string) (os.FileInfo, error) {
	if lstater, ok := fs.(afero.Lstater); ok {
		info, _, err := lstater.LstatIfPossible(path)
		if err != nil {
			return nil, fmt.Errorf("reading payload source info: %w", err)
		}
		return info, nil
	}
	info, err := fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading payload source info: %w", err)
	}
	return info, nil
}

func addPayloadToZip(fs afero.Fs, zipWriter *zip.Writer, files []updatepayload.File) error {
	if err := validatePayloadFiles(files); err != nil {
		return err
	}
	for _, file := range files {
		info, err := payloadSourceInfo(fs, file.SourcePath)
		if err != nil {
			return fmt.Errorf("reading payload source %q: %w", file.SourcePath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("payload source %q is not a regular file", file.SourcePath)
		}
		if err := addFileToZipMode(fs, zipWriter, file.SourcePath, file.ArchivePath, file.Mode); err != nil {
			return fmt.Errorf("adding payload source %q to zip: %w", file.SourcePath, err)
		}
	}
	return nil
}

func addFileToZip(fs afero.Fs, zipWriter *zip.Writer, filePath, arcname string) error {
	return addFileToZipMode(fs, zipWriter, filePath, arcname, 0)
}

func addFileToZipMode(fs afero.Fs, zipWriter *zip.Writer, filePath, arcname string, mode os.FileMode) error {
	file, err := fs.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func(file afero.File) {
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
	if mode != 0 {
		header.SetMode(mode.Perm())
	}

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

func copyFile(src, dst string) error {
	//nolint:gosec // Safe: reads files in build script with controlled paths
	input, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("operation failed: %w", err)
	}
	if err := os.WriteFile(dst, input, 0o600); err != nil { //nolint:gosec // G703: build script
		return fmt.Errorf("failed to write file %s: %w", dst, err)
	}
	return nil
}

func createTarGzFile(fs afero.Fs, tarGzPath, appPath, licensePath, readmePath, platform, _ string) error {
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
		err := addFileToTar(fs, tarWriter, file.path, file.arcname)
		if err != nil {
			return fmt.Errorf("error adding file to tar: %w", err)
		}
	}

	return addPayloadToTar(fs, tarWriter, payloadFiles(platform))
}

func addPayloadToTar(fs afero.Fs, tarWriter *tar.Writer, files []updatepayload.File) error {
	if err := validatePayloadFiles(files); err != nil {
		return err
	}
	for _, file := range files {
		info, err := payloadSourceInfo(fs, file.SourcePath)
		if err != nil {
			return fmt.Errorf("reading payload source %q: %w", file.SourcePath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("payload source %q is not a regular file", file.SourcePath)
		}
		if err := addFileToTarMode(fs, tarWriter, file.SourcePath, file.ArchivePath, file.Mode); err != nil {
			return fmt.Errorf("adding payload source %q to tar: %w", file.SourcePath, err)
		}
	}
	return nil
}

func addFileToTar(fs afero.Fs, tarWriter *tar.Writer, filePath, arcname string) error {
	return addFileToTarMode(fs, tarWriter, filePath, arcname, 0)
}

func addFileToTarMode(fs afero.Fs, tarWriter *tar.Writer, filePath, arcname string, mode os.FileMode) error {
	file, err := fs.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func(file afero.File) {
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
	if mode != 0 {
		header.Mode = int64(mode.Perm())
	}

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
