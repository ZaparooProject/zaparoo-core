//go:build linux

package mister

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScummVMScrapeSources(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	externalRoot := filepath.Join(t.TempDir(), "NAS")
	writeScummVMScrapeFile(t, fs, scummvmIniPath, fmt.Sprintf(
		"[scummvm]\npath=/ignored\n[one]\ndescription=First\npath=GAMES/monkey\n"+
			"[two]\npath=%s\n[three]\npath=GAMES/queen\n[no-path]\ndescription=No directory\n"+
			"[invalid]\npath=scummvm://invalid/title\n", filepath.Join(externalRoot, "loom")))
	sources, err := (&Platform{}).ScrapeSources(context.Background(), nil, fs, systemdefs.SystemScummVM)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(scummvmBaseDir, "GAMES"), externalRoot}, sources.Roots)
	require.Len(t, sources.Media, 3)
	assert.Equal(t, filepath.Join(scummvmBaseDir, "GAMES", "monkey"), sources.Media[0].Directory)
	assert.Equal(t, virtualpath.CreateVirtualPath("scummvm", "one", "First"), sources.Media[0].MediaPath)
	assert.Equal(t, filepath.Join(externalRoot, "loom"), sources.Media[1].Directory)

	unrelated, err := (&Platform{}).ScrapeSources(context.Background(), nil, fs, systemdefs.SystemSNES)
	require.NoError(t, err)
	assert.Empty(t, unrelated.Roots)
	assert.Empty(t, unrelated.Media)
}

func TestScummVMScrapeSourceFailures(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	pl := &Platform{}
	sources, err := pl.ScrapeSources(context.Background(), nil, fs, systemdefs.SystemScummVM)
	require.NoError(t, err, "missing installation is optional")
	assert.Empty(t, sources.Media)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = pl.ScrapeSources(ctx, nil, fs, systemdefs.SystemScummVM)
	require.ErrorIs(t, err, context.Canceled)

	writeScummVMScrapeFile(t, fs, scummvmIniPath, "[valid]\npath=GAMES/one\n"+strings.Repeat("x", 65536))
	sources, err = pl.ScrapeSources(context.Background(), nil, fs, systemdefs.SystemScummVM)
	require.Error(t, err, "malformed config must not return partial source data")
	assert.Empty(t, sources.Media)

	writeScummVMScrapeFile(t, fs, scummvmIniPath, strings.Repeat(";comment\n", 1<<21))
	sources, err = pl.ScrapeSources(context.Background(), nil, fs, systemdefs.SystemScummVM)
	require.ErrorContains(t, err, "byte limit")
	assert.Empty(t, sources.Media)
}

func FuzzParseScummVMIni(f *testing.F) {
	f.Add("[scummvm]\n[monkey]\ndescription=Monkey Island\npath=GAMES/monkey\n")
	f.Add("[one]\npath=/games/one\n[one]\npath=/games/two\n")
	f.Add("[incomplete\npath=../games\n")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 1<<20 {
			t.Skip()
		}
		fs := afero.NewMemMapFs()
		path := filepath.Join("config", "scummvm.ini")
		require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, afero.WriteFile(fs, path, []byte(input), 0o600))
		games, err := parseScummVMIniFS(context.Background(), fs, path)
		if err != nil {
			require.Empty(t, games, "parse failures must not expose partial source data")
			return
		}
		for _, game := range games {
			require.NotEmpty(t, game.TargetID)
			assert.NotContains(t, []string{"scummvm", "keymapper"}, game.TargetID)
		}
	})
}
