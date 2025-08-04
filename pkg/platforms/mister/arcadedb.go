//go:build linux

package mister

import (
	"context"
	"crypto/sha1" //nolint:gosec // Required for git blob SHA1 verification against GitHub API
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/gocarina/gocsv"
	"github.com/rs/zerolog/log"
)

type GithubLinks struct {
	Self string `json:"self"`
	Git  string `json:"git"`
	HTML string `json:"html"`
}

type GithubContentsItem struct {
	Links       GithubLinks `json:"_links"` //nolint:tagliatelle // GitHub API format
	Name        string      `json:"name"`
	Path        string      `json:"path"`
	Sha         string      `json:"sha"`
	URL         string      `json:"url"`
	HTMLURL     string      `json:"html_url"`     //nolint:tagliatelle // GitHub API format
	GitURL      string      `json:"git_url"`      //nolint:tagliatelle // GitHub API format
	DownloadURL string      `json:"download_url"` //nolint:tagliatelle // GitHub API format
	Type        string      `json:"type"`
	Size        int         `json:"size"`
}

type ArcadeDbEntry struct {
	Setname         string `csv:"setname"`
	Name            string `csv:"name"`
	Region          string `csv:"region"`
	Version         string `csv:"version"`
	Alternative     string `csv:"alternative"`
	ParentTitle     string `csv:"parent_title"`
	Platform        string `csv:"platform"`
	Series          string `csv:"series"`
	Homebrew        string `csv:"homebrew"`
	Bootleg         string `csv:"bootleg"`
	Year            string `csv:"year"`
	Manufacturer    string `csv:"manufacturer"`
	Category        string `csv:"category"`
	Linebreak1      string `csv:"linebreak1"`
	Resolution      string `csv:"resolution"`
	Flip            string `csv:"flip"`
	Linebreak2      string `csv:"linebreak2"`
	Players         string `csv:"players"`
	MoveInputs      string `csv:"move_inputs"`
	SpecialControls string `csv:"special_controls"`
	NumButtons      string `csv:"num_buttons"`
}

func getGitBlobSha1(filePath string) (string, error) {
	file, err := os.Open(filePath) //nolint:gosec // Internal path for arcade DB verification
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func(file *os.File) {
		closeErr := file.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close file")
		}
	}(file)

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	size := info.Size()
	header := fmt.Sprintf("blob %d\x00", size)

	hasher := sha1.New() //nolint:gosec // Required for git blob SHA1 verification against GitHub API
	_, _ = hasher.Write([]byte(header))
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to copy file to hasher: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func UpdateArcadeDb(pl platforms.Platform) (bool, error) {
	arcadeDBPath := filepath.Join(
		helpers.DataDir(pl),
		config.AssetsDir,
		ArcadeDbFile,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ArcadeDbURL, http.NoBody)
	if err != nil {
		return false, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	if resp == nil {
		return false, errors.New("received nil response")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close response body")
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read response body: %w", err)
	}

	var contents []GithubContentsItem
	err = json.Unmarshal(body, &contents)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal JSON: %w", err)
	} else if len(contents) == 0 {
		return false, nil
	}

	err = os.MkdirAll(filepath.Dir(arcadeDBPath), 0o750)
	if err != nil {
		return false, fmt.Errorf("failed to create directory: %w", err)
	}

	latestFile := contents[len(contents)-1]
	latestSha := latestFile.Sha

	localSha, err := getGitBlobSha1(arcadeDBPath)
	if err == nil && localSha == latestSha {
		return false, nil
	}

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, latestFile.DownloadURL, http.NoBody)
	if err != nil {
		return false, fmt.Errorf("failed to create download request: %w", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to download arcadedb: %w", err)
	}
	if resp == nil {
		return false, errors.New("received nil response")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close response body")
		}
	}()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read download body: %w", err)
	}

	err = os.WriteFile(arcadeDBPath, body, 0o600)
	if err != nil {
		return false, fmt.Errorf("failed to write arcadedb file: %w", err)
	}

	return true, nil
}

func ReadArcadeDb(pl platforms.Platform) ([]ArcadeDbEntry, error) {
	arcadeDBPath := filepath.Join(
		helpers.DataDir(pl),
		config.AssetsDir,
		ArcadeDbFile,
	)

	if _, err := os.Stat(arcadeDBPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("arcadedb file does not exist: %w", err)
	}

	dbFile, err := os.Open(arcadeDBPath) //nolint:gosec // Internal path for arcade DB reading
	if err != nil {
		return nil, fmt.Errorf("failed to open arcadedb file: %w", err)
	}
	defer func(c io.Closer) {
		_ = c.Close()
	}(dbFile)

	entries := make([]ArcadeDbEntry, 0)
	err = gocsv.Unmarshal(dbFile, &entries)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal arcadedb CSV: %w", err)
	}

	return entries, nil
}
