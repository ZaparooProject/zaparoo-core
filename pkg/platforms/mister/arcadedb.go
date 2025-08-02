//go:build linux

package mister

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/gocarina/gocsv"
	"github.com/rs/zerolog/log"
)

type GithubContentsItem struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Sha         string `json:"sha"`
	Size        int    `json:"size"`
	Url         string `json:"url"`
	HtmlUrl     string `json:"html_url"`
	GitUrl      string `json:"git_url"`
	DownloadUrl string `json:"download_url"`
	Type        string `json:"type"`
	Links       struct {
		Self string `json:"self"`
		Git  string `json:"git"`
		Html string `json:"html"`
	} `json:"_links"`
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
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close file")
		}
	}(file)

	info, err := file.Stat()
	if err != nil {
		return "", err
	}

	size := info.Size()
	header := fmt.Sprintf("blob %d\x00", size)

	hasher := sha1.New()
	hasher.Write([]byte(header))
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func UpdateArcadeDb(pl platforms.Platform) (bool, error) {
	arcadeDBPath := filepath.Join(
		utils.DataDir(pl),
		config.AssetsDir,
		ArcadeDbFile,
	)

	resp, err := http.Get(ArcadeDbUrl)
	if err != nil {
		return false, err
	}
	defer func(b io.ReadCloser) {
		_ = b.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var contents []GithubContentsItem
	err = json.Unmarshal(body, &contents)
	if err != nil {
		return false, err
	} else if len(contents) == 0 {
		return false, nil
	}

	err = os.MkdirAll(filepath.Dir(arcadeDBPath), 0755)
	if err != nil {
		return false, err
	}

	latestFile := contents[len(contents)-1]
	latestSha := latestFile.Sha

	localSha, err := getGitBlobSha1(arcadeDBPath)
	if err == nil && localSha == latestSha {
		return false, nil
	}

	resp, err = http.Get(latestFile.DownloadUrl)
	if err != nil {
		return false, err
	}
	defer func(b io.ReadCloser) {
		_ = b.Close()
	}(resp.Body)

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	err = os.WriteFile(arcadeDBPath, body, 0644)
	if err != nil {
		return false, err
	}

	return true, nil
}

func ReadArcadeDb(pl platforms.Platform) ([]ArcadeDbEntry, error) {
	arcadeDBPath := filepath.Join(
		utils.DataDir(pl),
		config.AssetsDir,
		ArcadeDbFile,
	)

	if _, err := os.Stat(arcadeDBPath); os.IsNotExist(err) {
		return nil, err
	}

	dbFile, err := os.Open(arcadeDBPath)
	if err != nil {
		return nil, err
	}
	defer func(c io.Closer) {
		_ = c.Close()
	}(dbFile)

	entries := make([]ArcadeDbEntry, 0)
	err = gocsv.Unmarshal(dbFile, &entries)
	if err != nil {
		return nil, err
	}

	return entries, nil
}
