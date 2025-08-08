package mistermain

import (
	"io"
	"os"
	"strings"
)

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
