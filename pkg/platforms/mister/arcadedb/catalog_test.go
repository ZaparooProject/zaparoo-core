//go:build linux

package arcadedb

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errCatalogWrite = errors.New("synthetic catalog write failure")

type catalogFailureFS struct {
	afero.Fs
	mode string
}

func (fs catalogFailureFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if fs.mode == "create" {
		return nil, errCatalogWrite
	}
	file, err := fs.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, fmt.Errorf("open fixture file: %w", err)
	}
	return catalogFailureFile{File: file, mode: fs.mode}, nil
}

func (fs catalogFailureFS) Rename(oldPath, newPath string) error {
	if fs.mode == "rename" {
		return errCatalogWrite
	}
	if err := fs.Fs.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename fixture: %w", err)
	}
	return nil
}

type catalogFailureFile struct {
	afero.File
	mode string
}

func (f catalogFailureFile) Write(data []byte) (int, error) {
	if f.mode == "short" {
		return 0, nil
	}
	if f.mode == "write" {
		return 0, errCatalogWrite
	}
	n, err := f.File.Write(data)
	if err != nil {
		return n, fmt.Errorf("write fixture: %w", err)
	}
	return n, nil
}

func (f catalogFailureFile) Sync() error {
	if f.mode == "sync" {
		return errCatalogWrite
	}
	if err := f.File.Sync(); err != nil {
		return fmt.Errorf("sync fixture: %w", err)
	}
	return nil
}

func (f catalogFailureFile) Close() error {
	if err := f.File.Close(); err != nil {
		return fmt.Errorf("close fixture: %w", err)
	}
	if f.mode == "close" {
		return errCatalogWrite
	}
	return nil
}

func TestReplaceCatalogPreservesLastGood(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"create", "write", "short", "sync", "close", "rename", "success"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			dir := "catalog"
			path := filepath.Join(dir, "arcade.csv")
			require.NoError(t, fs.MkdirAll(dir, 0o750))
			original := []byte("setname,name\nold,Old\n")
			replacement := []byte("setname,name\nnew,New\n")
			require.NoError(t, afero.WriteFile(fs, path, original, 0o600))
			err := NewClient(nil, catalogFailureFS{Fs: fs, mode: mode}, "", "").replaceCatalog(path, replacement)
			if mode == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			content, err := afero.ReadFile(fs, path)
			require.NoError(t, err)
			if mode == "success" {
				assert.Equal(t, replacement, content)
			} else {
				assert.Equal(t, original, content)
			}
			entries, err := afero.ReadDir(fs, dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "temporary file must be cleaned up")
			assert.Equal(t, "arcade.csv", entries[0].Name())
			assert.Equal(t, os.FileMode(0o600), entries[0].Mode().Perm())
		})
	}
}

func FuzzParseCatalog(f *testing.F) {
	f.Add([]byte("setname,name\npacman,Pac-Man\n"))
	f.Add([]byte("setname,name\nbad,too,many\n"))
	f.Add([]byte("setname,name\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		entries, err := parseCatalog(bytes.NewReader(data))
		if err != nil {
			return
		}
		require.NotEmpty(t, entries)
		for i := range entries {
			require.NotEmpty(t, entries[i].Setname)
			require.NotEmpty(t, entries[i].Name)
		}
	})
}
