//go:build linux || darwin

package mister

import (
	"encoding/json"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gocarina/gocsv"
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

func UpdateArcadeDb(pl platforms.Platform) (bool, error) {
	arcadeDBPath := filepath.Join(
		utils.DataDir(pl),
		platforms.AssetsDir,
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

	dbAge := time.Time{}
	if dbFile, err := os.Stat(arcadeDBPath); err == nil {
		dbAge = dbFile.ModTime()
	}

	// skip if current file is less than a day old
	if time.Since(dbAge) < 24*time.Hour {
		return false, nil
	}

	latestFile := contents[len(contents)-1]

	latestFileDate, err := time.Parse("ArcadeDatabase060102.csv", latestFile.Name)
	if err != nil {
		return false, err
	}

	if latestFileDate.Before(dbAge) {
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
		platforms.AssetsDir,
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
