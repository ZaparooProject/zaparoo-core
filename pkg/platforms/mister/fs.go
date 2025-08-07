package mister

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"

	misterconfig "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
)

func ActiveGameEnabled() bool {
	_, err := os.Stat(misterconfig.ActiveGameFile)
	return err == nil
}

func SetActiveGame(path string) error {
	file, err := os.Create(misterconfig.ActiveGameFile)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(path)
	if err != nil {
		return err
	}

	return nil
}

func GetActiveGame() (string, error) {
	data, err := os.ReadFile(misterconfig.ActiveGameFile)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Convert a launchable path to an absolute path.
func ResolvePath(path string) string {
	if path == "" {
		return path
	}

	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(misterconfig.SDRootDir)

	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	return abs
}

type RecentEntry struct {
	Directory string
	Name      string
	Label     string
}

func ReadRecent(path string) ([]RecentEntry, error) {
	var recents []RecentEntry

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	for {
		entry := make([]byte, 1024+256+256)
		n, err := file.Read(entry)
		if err == io.EOF || n == 0 {
			break
		} else if err != nil {
			return nil, err
		}

		empty := true
		for _, b := range entry {
			if b != 0 {
				empty = false
			}
		}
		if empty {
			break
		}

		recents = append(recents, RecentEntry{
			Directory: strings.Trim(string(entry[:1024]), "\x00"),
			Name:      strings.Trim(string(entry[1024:1280]), "\x00"),
			Label:     strings.Trim(string(entry[1280:1536]), "\x00"),
		})
	}

	return recents, nil
}

type MGLFile struct {
	XMLName xml.Name `xml:"file"`
	Delay   int      `xml:"delay,attr"`
	Type    string   `xml:"type,attr"`
	Index   int      `xml:"index,attr"`
	Path    string   `xml:"path,attr"`
}

type MGL struct {
	XMLName xml.Name `xml:"mistergamedescription"`
	Rbf     string   `xml:"rbf"`
	SetName string   `xml:"setname"`
	File    MGLFile  `xml:"file"`
}

func ReadMgl(path string) (MGL, error) {
	var mgl MGL

	if _, err := os.Stat(path); err != nil {
		return mgl, err
	}

	file, err := os.ReadFile(path)
	if err != nil {
		return mgl, err
	}

	decoder := xml.NewDecoder(bytes.NewReader(file))
	decoder.Strict = false

	err = decoder.Decode(&mgl)
	if err != nil {
		return mgl, err
	}

	return mgl, nil
}
