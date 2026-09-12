//go:build linux

package mister

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/spf13/afero"
)

// ScrapeSources discovers metadata beside configured ScummVM games, including
// games stored outside the install directory. The binary need not be running or
// available: the INI owns target identity, while indexed rows own write targets.
func (*Platform) ScrapeSources(
	ctx context.Context, _ *config.Instance, filesystem afero.Fs, systemID string,
) (scraper.Sources, error) {
	var sources scraper.Sources
	if systemID != systemdefs.SystemScummVM {
		return sources, nil
	}
	if filesystem == nil {
		filesystem = afero.NewOsFs()
	}
	games, err := parseScummVMIniFS(ctx, filesystem, scummvmIniPath)
	if errors.Is(err, fs.ErrNotExist) {
		return sources, nil
	}
	if err != nil {
		return sources, err
	}
	for _, game := range games {
		if game.Path == "" || strings.Contains(game.Path, "://") || virtualpath.ContainsControlChar(game.Path) {
			continue
		}
		directory := filepath.Clean(game.Path)
		if !filepath.IsAbs(directory) {
			// ScummVM is launched with its install directory as the working directory.
			directory = filepath.Join(scummvmBaseDir, directory)
		}
		root := filepath.Dir(directory)
		if root == directory {
			continue
		}
		if !slices.Contains(sources.Roots, root) {
			sources.Roots = append(sources.Roots, root)
		}
		sources.Media = append(sources.Media, scraper.MediaSource{
			MediaPath: virtualpath.CreateVirtualPath(shared.SchemeScummVM, game.TargetID, game.Description),
			Directory: directory,
		})
	}
	return sources, nil
}
