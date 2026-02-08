//go:build linux

package arcadedb

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // Required for git blob SHA1 verification against GitHub API
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	config2 "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/gocarina/gocsv"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

type Client struct {
	httpClient *http.Client
	fs         afero.Fs
	apiURL     string
	filename   string
}

func NewClient(httpClient *http.Client, fs afero.Fs, apiURL, filename string) *Client {
	return &Client{
		httpClient: httpClient,
		fs:         fs,
		apiURL:     apiURL,
		filename:   filename,
	}
}

func defaultClient() *Client {
	return NewClient(http.DefaultClient, afero.NewOsFs(), config2.ArcadeDbURL, config2.ArcadeDbFile)
}

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

func parseGitHubContentsResponse(statusCode int, body []byte) ([]GithubContentsItem, error) {
	if statusCode != http.StatusOK {
		bodyPreview := string(body)
		if len(bodyPreview) > 200 {
			bodyPreview = bodyPreview[:200] + "..."
		}
		if statusCode == http.StatusForbidden {
			return nil, fmt.Errorf(
				"GitHub API returned %d (forbidden, probably rate limited): %s",
				statusCode, bodyPreview)
		}
		return nil, fmt.Errorf("GitHub API returned %d: %s", statusCode, bodyPreview)
	}

	var contents []GithubContentsItem
	if err := json.Unmarshal(body, &contents); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return contents, nil
}

func (c *Client) getGitBlobSha1(filePath string) (string, error) {
	file, err := c.fs.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close file")
		}
	}()

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

func (c *Client) doRequest(ctx context.Context, url string) (statusCode int, body []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	if resp == nil {
		return 0, nil, errors.New("received nil response")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close response body")
		}
	}()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return resp.StatusCode, body, nil
}

func (c *Client) Update(arcadeDBPath string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	statusCode, body, err := c.doRequest(ctx, c.apiURL)
	if err != nil {
		return false, err
	}

	contents, err := parseGitHubContentsResponse(statusCode, body)
	if err != nil {
		return false, err
	}
	if len(contents) == 0 {
		return false, nil
	}

	err = c.fs.MkdirAll(filepath.Dir(arcadeDBPath), 0o750)
	if err != nil {
		return false, fmt.Errorf("failed to create directory: %w", err)
	}

	var arcadeDbFile *GithubContentsItem
	for i := range contents {
		if contents[i].Name == c.filename && contents[i].Type == "file" {
			arcadeDbFile = &contents[i]
			break
		}
	}
	if arcadeDbFile == nil {
		return false, fmt.Errorf("file %s not found in repository", c.filename)
	}

	latestSha := arcadeDbFile.Sha

	localSha, err := c.getGitBlobSha1(arcadeDBPath)
	if err == nil && localSha == latestSha {
		return false, nil
	}

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	statusCode, body, err = c.doRequest(ctx, arcadeDbFile.DownloadURL)
	if err != nil {
		return false, fmt.Errorf("failed to download arcadedb: %w", err)
	}
	if statusCode != http.StatusOK {
		return false, fmt.Errorf("download failed with status %d", statusCode)
	}

	err = afero.WriteFile(c.fs, arcadeDBPath, body, 0o600)
	if err != nil {
		return false, fmt.Errorf("failed to write arcadedb file: %w", err)
	}

	return true, nil
}

func (c *Client) Read(arcadeDBPath string) ([]ArcadeDbEntry, error) {
	exists, err := afero.Exists(c.fs, arcadeDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check arcadedb file: %w", err)
	}

	if !exists {
		return c.readEmbedded()
	}

	file, err := c.fs.Open(arcadeDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open arcadedb file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	entries := make([]ArcadeDbEntry, 0)
	err = gocsv.Unmarshal(file, &entries)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal arcadedb CSV: %w", err)
	}

	return entries, nil
}

func (*Client) readEmbedded() ([]ArcadeDbEntry, error) {
	if len(EmbeddedArcadeDB) == 0 {
		return nil, errors.New("arcadedb file not found and no embedded database available")
	}

	log.Info().Msg("using embedded arcade database as fallback")

	entries := make([]ArcadeDbEntry, 0)
	if err := gocsv.Unmarshal(bytes.NewReader(EmbeddedArcadeDB), &entries); err != nil {
		return nil, fmt.Errorf("failed to unmarshal embedded arcadedb CSV: %w", err)
	}

	return entries, nil
}

// UpdateArcadeDb checks for and downloads arcade database updates.
// This is the public API that uses default HTTP client and OS filesystem.
func UpdateArcadeDb(pl platforms.Platform) (bool, error) {
	arcadeDBPath := filepath.Join(
		helpers.DataDir(pl),
		config.AssetsDir,
		config2.ArcadeDbFile,
	)
	return defaultClient().Update(arcadeDBPath)
}

// ReadArcadeDb reads and parses the arcade database CSV file.
// This is the public API that uses the OS filesystem.
func ReadArcadeDb(pl platforms.Platform) ([]ArcadeDbEntry, error) {
	arcadeDBPath := filepath.Join(
		helpers.DataDir(pl),
		config.AssetsDir,
		config2.ArcadeDbFile,
	)
	return defaultClient().Read(arcadeDBPath)
}
