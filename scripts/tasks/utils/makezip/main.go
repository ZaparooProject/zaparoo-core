package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const baseURL = "https://github.com/ZaparooProject/zaparoo.org/raw/refs/heads/main/docs/platforms/"

var platformDocs = map[string]string{
	"batocera":  "batocera.md",
	"bazzite":   "bazzite.mdx",
	"chimeraos": "chimeraos.mdx",
	"libreelec": "libreelec.md",
	"linux":     "linux.mdx",
	"mac":       "mac.mdx",
	"mister":    "mister.md",
	"mistex":    "mistex.md",
	"recalbox":  "recalbox.mdx",
	"steamos":   "steamos.md",
	"windows":   "windows/index.md",
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
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_, _ = fmt.Printf("error closing response body: %v\n", closeErr)
		}
	}()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	processedContent := string(content)
	if strings.HasSuffix(strings.ToLower(fileName), ".mdx") {
		processedContent = stripFrontmatter(processedContent)
	}

	return os.WriteFile(filepath.Join(toDir, "README.txt"), []byte(strings.TrimSpace(processedContent)+"\n"), 0o644)
}

func main() {
	if len(os.Args) < 5 {
		_, _ = fmt.Println("Usage: go run makezip.go <platform> <build_dir> <app_bin> <zip_name>")
		os.Exit(1)
	}

	platform := os.Args[1]
	buildDir := os.Args[2]
	appBin := os.Args[3]
	zipName := os.Args[4]

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
		err = os.WriteFile(licensePath, input, 0o644)
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

	zipPath := filepath.Join(buildDir, zipName)
	_ = os.Remove(zipPath)

	readmePath := filepath.Join(buildDir, "README.txt")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		if err := downloadDoc(platform, buildDir); err != nil {
			_, _ = fmt.Printf("Error downloading documentation: %v\n", err)
			os.Exit(1)
		}
	}

	if err := createZipFile(zipPath, appPath, licensePath, readmePath, platform, buildDir); err != nil {
		_, _ = fmt.Printf("Error creating zip: %v\n", err)
		os.Exit(1)
	}
}

func createZipFile(zipPath, appPath, licensePath, readmePath, platform, buildDir string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("error creating zip file: %w", err)
	}
	defer func(zipFile *os.File) {
		_ = zipFile.Close()
	}(zipFile)

	zipWriter := zip.NewWriter(zipFile)
	defer func(zipWriter *zip.Writer) {
		_ = zipWriter.Close()
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
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = arcname
	header.Method = zip.Deflate

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, file)
	return err
}

func addDirToZip(zipWriter *zip.Writer, dirPath, buildDir string) error {
	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relPath, err := filepath.Rel(dirPath, path)
			if err != nil {
				return err
			}

			destPath := filepath.Join(buildDir, filepath.Base(dirPath), relPath)
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}

			if err := copyFile(path, destPath); err != nil {
				return err
			}

			return addFileToZip(zipWriter, destPath, filepath.Join(filepath.Base(dirPath), relPath))
		}
		return nil
	})
}

func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, input, 0o644)
}
