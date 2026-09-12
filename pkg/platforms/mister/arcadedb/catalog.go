//go:build linux

package arcadedb

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gocarina/gocsv"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

func parseCatalog(reader io.Reader) ([]ArcadeDbEntry, error) {
	var entries []ArcadeDbEntry
	if err := gocsv.Unmarshal(reader, &entries); err != nil {
		return nil, fmt.Errorf("failed to unmarshal arcadedb CSV: %w", err)
	}
	entries = filterValidEntries(entries)
	if len(entries) == 0 {
		return nil, errors.New("arcadedb contains no usable entries")
	}
	return entries, nil
}

// replaceCatalog writes beside the destination so rename can replace the cache
// atomically. Never truncate the last-good catalog on a failed write.
func (c *Client) replaceCatalog(path string, body []byte) error {
	file, err := afero.TempFile(c.fs, filepath.Dir(path), ".arcadedb-*")
	if err != nil {
		return fmt.Errorf("create arcade catalog temporary file: %w", err)
	}
	tempPath := file.Name()
	published := false
	defer func() {
		_ = file.Close()
		if published {
			return
		}
		if removeErr := c.fs.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Warn().Err(removeErr).Msg("failed to remove arcade catalog temporary file")
		}
	}()
	n, err := file.Write(body)
	if err != nil {
		return fmt.Errorf("write arcade catalog temporary file: %w", err)
	}
	if n != len(body) {
		return fmt.Errorf("write arcade catalog temporary file: %w", io.ErrShortWrite)
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("sync arcade catalog temporary file: %w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close arcade catalog temporary file: %w", err)
	}
	if err = c.fs.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace arcade catalog: %w", err)
	}
	published = true
	return nil
}
