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
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
	"github.com/spf13/afero"
)

func assertBatoceraPayloadFiles(t *testing.T) {
	t.Helper()

	want := []updatepayload.File{
		{
			SourcePath: filepath.Join("cmd", "batocera", "scripts", "configs", "emulationstation", "scripts",
				"game-selected", "zaparoo_game_select.sh"),
			ArchivePath: "scripts/configs/emulationstation/scripts/game-selected/zaparoo_game_select.sh",
			InstallPath: "configs/emulationstation/scripts/game-selected/zaparoo_game_select.sh",
			Mode:        0o755,
		},
		{
			SourcePath:  filepath.Join("cmd", "batocera", "scripts", "configs", "multimedia_keys.conf"),
			ArchivePath: "scripts/configs/multimedia_keys.conf",
			InstallPath: "configs/multimedia_keys.conf",
			Mode:        0o644,
		},
		{
			SourcePath:  filepath.Join("cmd", "batocera", "scripts", "content_downloader.png"),
			ArchivePath: "scripts/content_downloader.png",
			InstallPath: "content_downloader.png",
			Mode:        0o644,
		},
		{
			SourcePath:  filepath.Join("cmd", "batocera", "scripts", "ports", "Zaparoo.sh"),
			ArchivePath: "scripts/ports/Zaparoo.sh",
			InstallPath: "../roms/ports/Zaparoo.sh",
			Mode:        0o755,
		},
		{
			SourcePath:  filepath.Join("cmd", "batocera", "scripts", "services", "zaparoo_service"),
			ArchivePath: "scripts/services/zaparoo_service",
			InstallPath: "services/zaparoo_service",
			Mode:        0o755,
		},
		{
			SourcePath:  filepath.Join("cmd", "batocera", "scripts", "zaparoo_wrapper.sh"),
			ArchivePath: "scripts/zaparoo_wrapper.sh",
			InstallPath: "zaparoo_wrapper.sh",
			Mode:        0o755,
		},
		{
			SourcePath:  filepath.Join("cmd", "batocera", "scripts", "zaparoo_write_game.sh"),
			ArchivePath: "scripts/zaparoo_write_game.sh",
			InstallPath: "zaparoo_write_game.sh",
			Mode:        0o755,
		},
	}
	if got := payloadFiles("batocera"); !slices.Equal(got, want) {
		t.Fatalf("batocera payload files = %#v, want %#v", got, want)
	}
}

func TestStripFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no frontmatter",
			input:    "# Title\nContent here",
			expected: "# Title\nContent here",
		},
		{
			name:     "with frontmatter",
			input:    "---\ntitle: Test\nauthor: Someone\n---\n# Title\nContent",
			expected: "# Title\nContent",
		},
		{
			name:     "empty frontmatter",
			input:    "---\n---\nContent after",
			expected: "Content after",
		},
		{
			name:     "unclosed frontmatter preserves content",
			input:    "---\ntitle: Test\nNo closing delimiter",
			expected: "---\ntitle: Test\nNo closing delimiter",
		},
		{
			name:     "frontmatter only",
			input:    "---\ntitle: Test\n---",
			expected: "",
		},
		{
			name:     "empty content",
			input:    "",
			expected: "",
		},
		{
			name:     "no frontmatter delimiter at start",
			input:    "Some text\n---\nMore text\n---\nEnd",
			expected: "Some text\n---\nMore text\n---\nEnd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := stripFrontmatter(tt.input)
			if result != tt.expected {
				t.Errorf("stripFrontmatter(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExpandRelativeLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    string
		platformID string
		expected   string
	}{
		{
			name:       "parent directory link with ../",
			content:    "[Getting Started](../getting-started.md)",
			platformID: "linux",
			expected:   "[Getting Started](https://zaparoo.org/docs/getting-started/)",
		},
		{
			name:       "same directory link with ./",
			content:    "[Setup](./setup.md)",
			platformID: "linux",
			expected:   "[Setup](https://zaparoo.org/docs/platforms/setup/)",
		},
		{
			name:       "link without prefix",
			content:    "[Install](install.md)",
			platformID: "linux",
			expected:   "[Install](https://zaparoo.org/docs/platforms/install/)",
		},
		{
			name:       "link with anchor",
			content:    "[FAQ Section](../faq.md#question-one)",
			platformID: "mister",
			expected:   "[FAQ Section](https://zaparoo.org/docs/faq/#question-one)",
		},
		{
			name:       "mdx extension",
			content:    "[Guide](../guide.mdx)",
			platformID: "windows",
			expected:   "[Guide](https://zaparoo.org/docs/guide/)",
		},
		{
			name:       "external link unchanged",
			content:    "[External](https://example.com/page)",
			platformID: "linux",
			expected:   "[External](https://example.com/page)",
		},
		{
			name:       "absolute path unchanged",
			content:    "[Absolute](/docs/something.md)",
			platformID: "linux",
			expected:   "[Absolute](/docs/something.md)",
		},
		{
			name:       "multiple links in content",
			content:    "See [one](../one.md) and [two](./two.md) for info.",
			platformID: "batocera",
			expected: "See [one](https://zaparoo.org/docs/one/) " +
				"and [two](https://zaparoo.org/docs/platforms/two/) for info.",
		},
		{
			name:       "no markdown links",
			content:    "This is plain text without any links.",
			platformID: "linux",
			expected:   "This is plain text without any links.",
		},
		{
			name:       "nested path with ../",
			content:    "[Nested](../guides/advanced.md)",
			platformID: "linux",
			expected:   "[Nested](https://zaparoo.org/docs/guides/advanced/)",
		},
		{
			name:       "double parent directory ../../",
			content:    "[Readers](../../readers/index.md)",
			platformID: "linux",
			expected:   "[Readers](https://zaparoo.org/docs/readers/)",
		},
		{
			name:       "triple parent directory",
			content:    "[Root](../../../something.md)",
			platformID: "linux",
			expected:   "[Root](https://zaparoo.org/docs/something/)",
		},
		{
			name:       "index.md stripped from path",
			content:    "[Index](../readers/index.md)",
			platformID: "linux",
			expected:   "[Index](https://zaparoo.org/docs/readers/)",
		},
		{
			name:       "standalone index file",
			content:    "[Home](index.md)",
			platformID: "linux",
			expected:   "[Home](https://zaparoo.org/docs/platforms/)",
		},
		{
			name:       "filename ending in index",
			content:    "[Platforms](platform-index.md)",
			platformID: "linux",
			expected:   "[Platforms](https://zaparoo.org/docs/platforms/platform-index/)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := expandRelativeLinks(tt.content, tt.platformID)
			if result != tt.expected {
				t.Errorf("expandRelativeLinks(%q, %q) = %q, want %q",
					tt.content, tt.platformID, result, tt.expected)
			}
		})
	}
}

func TestAddDocFooter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    string
		platformID string
		wantURL    string
	}{
		{
			name:       "linux platform",
			content:    "Content here",
			platformID: "linux",
			wantURL:    "https://zaparoo.org/docs/platforms/linux/",
		},
		{
			name:       "windows platform",
			content:    "Windows docs",
			platformID: "windows",
			wantURL:    "https://zaparoo.org/docs/platforms/windows/",
		},
		{
			name:       "mister platform",
			content:    "MiSTer docs",
			platformID: "mister",
			wantURL:    "https://zaparoo.org/docs/platforms/mister/",
		},
		{
			name:       "batocera platform",
			content:    "Batocera docs",
			platformID: "batocera",
			wantURL:    "https://zaparoo.org/docs/platforms/batocera/",
		},
		{
			name:       "zapos interim documentation",
			content:    "ZapOS docs",
			platformID: "zapos",
			wantURL:    "https://zaparoo.org/docs/platforms/linux/",
		},
		{
			name:       "unknown platform uses default",
			content:    "Unknown",
			platformID: "unknown-platform",
			wantURL:    "https://zaparoo.org/docs/",
		},
		{
			name:       "empty platform uses default",
			content:    "Empty",
			platformID: "",
			wantURL:    "https://zaparoo.org/docs/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := addDocFooter(tt.content, tt.platformID)

			// Check that footer separator is present
			if !strings.Contains(result, "\n\n---\n\n") {
				t.Error("expected footer separator '\\n\\n---\\n\\n' not found")
			}

			// Check that the URL is present
			if !strings.Contains(result, tt.wantURL) {
				t.Errorf("expected URL %q not found in result: %q", tt.wantURL, result)
			}

			// Check that original content is preserved
			if !strings.HasPrefix(result, tt.content) {
				t.Error("original content not preserved at start of result")
			}

			// Check that "Full documentation:" label is present
			if !strings.Contains(result, "Full documentation:") {
				t.Error("expected 'Full documentation:' label not found")
			}
		})
	}
}

func TestDownloadDoc(t *testing.T) {
	t.Parallel()

	t.Run("successful download", func(t *testing.T) {
		t.Parallel()

		// Create a test server that returns mock documentation
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("---\ntitle: Test\n---\n# Test Documentation\n\nThis is test content."))
		}))
		defer server.Close()

		// We can't easily test downloadDoc directly since it uses a hardcoded URL,
		// but we can verify the HTTP client behavior works correctly by testing
		// with a custom client approach. For now, test the processing functions.
	})

	t.Run("HTTP 404 error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Not Found"))
		}))
		defer server.Close()

		// The downloadDoc function uses hardcoded URLs, so we verify the status check
		// works by testing with a direct HTTP request pattern
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, http.NoBody)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: test hitting local test server
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status %d, got %d", http.StatusNotFound, resp.StatusCode)
		}
	})

	t.Run("unknown platform returns error", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		err := downloadDoc("nonexistent-platform", tmpDir)
		if err == nil {
			t.Error("expected error for unknown platform, got nil")
		}
		if !strings.Contains(err.Error(), "not found in the platforms list") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestCreateZipFile(t *testing.T) {
	t.Parallel()

	t.Run("creates zip with files", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		// Create test files
		appContent := []byte("binary content")
		licenseContent := []byte("LICENSE content")
		readmeContent := []byte("README content")

		appPath := filepath.Join(tmpDir, "app")
		licensePath := filepath.Join(tmpDir, "LICENSE.txt")
		readmePath := filepath.Join(tmpDir, "README.txt")
		zipPath := filepath.Join(tmpDir, "test.zip")

		if err := os.WriteFile(appPath, appContent, 0o600); err != nil {
			t.Fatalf("failed to write app file: %v", err)
		}
		if err := os.WriteFile(licensePath, licenseContent, 0o600); err != nil {
			t.Fatalf("failed to write license file: %v", err)
		}
		if err := os.WriteFile(readmePath, readmeContent, 0o600); err != nil {
			t.Fatalf("failed to write readme file: %v", err)
		}

		err := createZipFile(afero.NewOsFs(), zipPath, appPath, licensePath, readmePath, "testplatform", tmpDir)
		if err != nil {
			t.Fatalf("createZipFile failed: %v", err)
		}

		// Verify zip file exists
		if _, err := os.Stat(zipPath); os.IsNotExist(err) {
			t.Error("zip file was not created")
		}
	})
}

func TestCreateTarGzFile(t *testing.T) {
	t.Parallel()

	t.Run("creates tar.gz with files", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		// Create test files
		appContent := []byte("binary content")
		licenseContent := []byte("LICENSE content")
		readmeContent := []byte("README content")

		appPath := filepath.Join(tmpDir, "app")
		licensePath := filepath.Join(tmpDir, "LICENSE.txt")
		readmePath := filepath.Join(tmpDir, "README.txt")
		tarGzPath := filepath.Join(tmpDir, "test.tar.gz")

		if err := os.WriteFile(appPath, appContent, 0o600); err != nil {
			t.Fatalf("failed to write app file: %v", err)
		}
		if err := os.WriteFile(licensePath, licenseContent, 0o600); err != nil {
			t.Fatalf("failed to write license file: %v", err)
		}
		if err := os.WriteFile(readmePath, readmeContent, 0o600); err != nil {
			t.Fatalf("failed to write readme file: %v", err)
		}

		err := createTarGzFile(
			afero.NewOsFs(), tarGzPath, appPath, licensePath, readmePath, "testplatform", tmpDir,
		)
		if err != nil {
			t.Fatalf("createTarGzFile failed: %v", err)
		}

		// Verify tar.gz file exists
		if _, err := os.Stat(tarGzPath); os.IsNotExist(err) {
			t.Error("tar.gz file was not created")
		}
	})
}

func TestAddPayloadToArchives(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	source := "payload"
	if err := fs.MkdirAll(source, 0o750); err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(source, "zaparoo_service")
	payloadContent := []byte("service")
	if err := afero.WriteFile(fs, payloadPath, payloadContent, 0o600); err != nil {
		t.Fatal(err)
	}
	files := []updatepayload.File{{
		SourcePath: payloadPath, ArchivePath: "scripts/services/zaparoo_service",
		InstallPath: "services/zaparoo_service", Mode: 0o755,
	}}

	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	if err := addPayloadToZip(fs, zipWriter, files); err != nil {
		t.Fatalf("addPayloadToZip failed: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	zipReader, zipErr := zip.NewReader(bytes.NewReader(zipBuf.Bytes()), int64(zipBuf.Len()))
	if zipErr != nil {
		t.Fatal(zipErr)
	}
	zipNames := make([]string, 0, len(zipReader.File))
	for _, file := range zipReader.File {
		zipNames = append(zipNames, file.Name)
		if file.Name != "scripts/services/zaparoo_service" {
			continue
		}
		if file.Mode().Perm() != 0o755 {
			t.Fatalf("zip payload mode = %o, want 755", file.Mode().Perm())
		}
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		content, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(content, payloadContent) {
			t.Fatalf("zip payload content = %q, want %q", content, payloadContent)
		}
	}

	var tarBuf bytes.Buffer
	tarWriter := tar.NewWriter(&tarBuf)
	if err := addPayloadToTar(fs, tarWriter, files); err != nil {
		t.Fatalf("addPayloadToTar failed: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	var tarNames []string
	tarReader := tar.NewReader(bytes.NewReader(tarBuf.Bytes()))
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		tarNames = append(tarNames, header.Name)
		if header.Name != "scripts/services/zaparoo_service" {
			continue
		}
		if header.FileInfo().Mode().Perm() != 0o755 {
			t.Fatalf("tar payload mode = %o, want 755", header.Mode)
		}
		content, readErr := io.ReadAll(tarReader)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(content, payloadContent) {
			t.Fatalf("tar payload content = %q, want %q", content, payloadContent)
		}
	}

	want := []string{"scripts/services/zaparoo_service"}
	if !slices.Equal(zipNames, want) {
		t.Fatalf("zip payload names = %v, want %v", zipNames, want)
	}
	if !slices.Equal(tarNames, want) {
		t.Fatalf("tar payload names = %v, want %v", tarNames, want)
	}
}

func TestAddPayloadToArchives_MissingSourceFails(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	files := []updatepayload.File{{
		SourcePath:  "missing",
		ArchivePath: "scripts/missing",
		InstallPath: "missing",
		Mode:        0o644,
	}}
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	if err := addPayloadToZip(fs, zipWriter, files); err == nil {
		t.Fatal("addPayloadToZip unexpectedly accepted a missing source")
	}
	var tarBuf bytes.Buffer
	if err := addPayloadToTar(fs, tar.NewWriter(&tarBuf), files); err == nil {
		t.Fatal("addPayloadToTar unexpectedly accepted a missing source")
	}
}

func TestAddPayloadToArchives_DirectorySourceFails(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	if err := fs.MkdirAll("payload-dir", 0o750); err != nil {
		t.Fatal(err)
	}
	files := []updatepayload.File{{
		SourcePath: "payload-dir", ArchivePath: "scripts/payload-dir",
		InstallPath: "payload-dir", Mode: 0o755,
	}}
	var zipBuf bytes.Buffer
	err := addPayloadToZip(fs, zip.NewWriter(&zipBuf), files)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("addPayloadToZip directory source error = %v", err)
	}
	var tarBuf bytes.Buffer
	err = addPayloadToTar(fs, tar.NewWriter(&tarBuf), files)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("addPayloadToTar directory source error = %v", err)
	}
}

func TestAddPayloadToArchives_DuplicateArchivePathFails(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	files := []updatepayload.File{
		{ArchivePath: "scripts/helper.sh", InstallPath: "helper.sh", Mode: 0o755},
		{ArchivePath: "scripts/helper.sh", InstallPath: "other-helper.sh", Mode: 0o755},
	}
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	if err := addPayloadToZip(fs, zipWriter, files); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("addPayloadToZip duplicate error = %v", err)
	}
	var tarBuf bytes.Buffer
	if err := addPayloadToTar(fs, tar.NewWriter(&tarBuf), files); err == nil ||
		!strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("addPayloadToTar duplicate error = %v", err)
	}
}

func TestAddPayloadToArchives_InvalidDefinitionFails(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	files := []updatepayload.File{{
		ArchivePath: "scripts/helper.sh", InstallPath: "/outside/helper.sh", Mode: 0o755,
	}}
	var zipBuf bytes.Buffer
	if err := addPayloadToZip(fs, zip.NewWriter(&zipBuf), files); err == nil ||
		!strings.Contains(err.Error(), "invalid") {
		t.Fatalf("addPayloadToZip invalid definition error = %v", err)
	}
	var tarBuf bytes.Buffer
	if err := addPayloadToTar(fs, tar.NewWriter(&tarBuf), files); err == nil ||
		!strings.Contains(err.Error(), "invalid") {
		t.Fatalf("addPayloadToTar invalid definition error = %v", err)
	}
}

func TestCopyFile(t *testing.T) {
	t.Parallel()

	t.Run("copies file successfully", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "source.txt")
		dstPath := filepath.Join(tmpDir, "dest.txt")

		content := []byte("test content to copy")
		if err := os.WriteFile(srcPath, content, 0o600); err != nil {
			t.Fatalf("failed to write source file: %v", err)
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			t.Fatalf("copyFile failed: %v", err)
		}

		// Verify destination file exists and has same content
		//nolint:gosec // Safe: test code with controlled paths from t.TempDir()
		readContent, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("failed to read destination file: %v", err)
		}

		if !bytes.Equal(readContent, content) {
			t.Errorf("content mismatch: got %q, want %q", readContent, content)
		}
	})

	t.Run("returns error for nonexistent source", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "nonexistent.txt")
		dstPath := filepath.Join(tmpDir, "dest.txt")

		err := copyFile(srcPath, dstPath)
		if err == nil {
			t.Error("expected error for nonexistent source, got nil")
		}
	})
}
