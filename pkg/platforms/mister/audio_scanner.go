//go:build linux

package mister

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

const misterAudioScannerLauncherID = "mister-audio-scanner"

var misterAudioExtensions = map[string]struct{}{
	".wav":  {},
	".mp3":  {},
	".ogg":  {},
	".flac": {},
}

func createAudioScannerLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:                 misterAudioScannerLauncherID,
		SystemID:           systemdefs.SystemAudio,
		SkipFilesystemScan: true,
		Scanner: func(ctx context.Context, cfg *config.Instance, _ string, _ []platforms.ScanResult) (
			[]platforms.ScanResult,
			error,
		) {
			return scanMiSTerAudioPaths(ctx, misterAudioScanRoots(cfg))
		},
	}
}

func misterAudioScanRoots(cfg *config.Instance) []string {
	platformRoots := misterconfig.RootDirs(cfg)
	roots := make([]string, 0, len(platformRoots)+1)
	roots = append(roots, filepath.Join(misterconfig.SDRootDir, "music"))
	for _, root := range platformRoots {
		roots = append(roots, filepath.Join(root, "Audio"))
	}
	return roots
}

func scanMiSTerAudioPaths(ctx context.Context, roots []string) ([]platforms.ScanResult, error) {
	seenRoots := make(map[string]struct{}, len(roots))
	seenFiles := make(map[string]struct{})
	files := make([]string, 0)

	for _, root := range roots {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		path, err := mediascanner.FindPath(ctx, root)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			log.Trace().Err(err).Str("path", root).Msg("skipping MiSTer audio folder")
			continue
		}

		cleanPath := filepath.Clean(path)
		if _, ok := seenRoots[cleanPath]; ok {
			continue
		}
		seenRoots[cleanPath] = struct{}{}

		walkErr := filepath.WalkDir(cleanPath, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				log.Trace().Err(walkErr).Str("path", path).Msg("skipping MiSTer audio path")
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if entry.IsDir() {
				return nil
			}
			if _, ok := misterAudioExtensions[strings.ToLower(filepath.Ext(path))]; !ok {
				return nil
			}
			if _, ok := seenFiles[path]; ok {
				return nil
			}
			seenFiles[path] = struct{}{}
			files = append(files, path)
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("walk MiSTer audio folder %s: %w", cleanPath, walkErr)
		}
	}

	sort.Strings(files)
	results := make([]platforms.ScanResult, 0, len(files))
	for _, path := range files {
		results = append(results, platforms.ScanResult{Path: path})
	}
	return results, nil
}
