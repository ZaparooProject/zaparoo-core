//go:build linux && !android

package mister

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	scummvmBaseDir = "/media/fat/ScummVM"
	scummvmIniPath = "/media/fat/ScummVM/.config/scummvm/scummvm.ini"
)

// ScummVMGame represents a game configured in scummvm.ini
type ScummVMGame struct {
	TargetID    string // Section name, used to launch the game
	Description string // Human-readable game title
	Path        string // Configured game directory, used for metadata discovery
	GameID      string // Engine game ID, shared by every variant of one game
}

// findScummVMBinary searches for ScummVM executable in the ScummVM directory
func findScummVMBinary() (string, error) {
	return findScummVMBinaryIn(scummvmBaseDir)
}

// findScummVMBinaryIn returns the ScummVM executable in dir: the first file
// named scummvm* with no extension. exFAT reports every file as executable, so
// the extension is what tells scummvm.ini, which sorts ahead of scummvm270,
// from the binary.
func findScummVMBinaryIn(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "scummvm*"))
	if err != nil {
		return "", fmt.Errorf("failed to search for ScummVM binary: %w", err)
	}

	for _, candidate := range matches {
		if filepath.Ext(candidate) != "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil {
			log.Debug().Err(err).Str("path", candidate).Msg("failed to stat ScummVM binary candidate")
			continue
		}
		if info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return candidate, nil
	}

	return "", fmt.Errorf("no ScummVM binary found in %s", dir)
}

// parseScummVMIni parses the scummvm.ini file and extracts game configurations
func parseScummVMIni(iniPath string) ([]ScummVMGame, error) {
	return parseScummVMIniFS(context.Background(), afero.NewOsFs(), iniPath)
}

func parseScummVMIniFS(ctx context.Context, fs afero.Fs, iniPath string) ([]ScummVMGame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := fs.Open(iniPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open scummvm.ini: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close scummvm.ini")
		}
	}()

	var games []ScummVMGame
	var currentGame *ScummVMGame

	const maxIniSize = 16 << 20
	limited := &io.LimitedReader{R: file, N: maxIniSize + 1}
	scanner := bufio.NewScanner(limited)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}

		// Check for section header [sectionname]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			// Save previous game if exists
			if currentGame != nil && currentGame.TargetID != "" {
				games = append(games, *currentGame)
			}

			// Extract section name
			sectionName := strings.TrimSpace(line[1 : len(line)-1])

			// Skip global sections
			if sectionName == "scummvm" || sectionName == "keymapper" {
				currentGame = nil
				continue
			}

			// Start a new game
			currentGame = &ScummVMGame{
				TargetID:    sectionName,
				Description: sectionName, // Default to target ID if no description found
			}
			continue
		}

		// Parse key=value pairs
		if currentGame != nil && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])

				switch key {
				case "description":
					currentGame.Description = value
				case "path":
					currentGame.Path = value
				case "gameid":
					currentGame.GameID = value
				}
			}
		}
	}

	// Save the last game
	if currentGame != nil && currentGame.TargetID != "" {
		games = append(games, *currentGame)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading scummvm.ini: %w", err)
	}

	if limited.N == 0 {
		return nil, fmt.Errorf("scummvm.ini exceeds %d byte limit", maxIniSize)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return games, nil
}

// scanScummVMGames implements the Scanner function for ScummVM launcher
func scanScummVMGames(
	_ context.Context,
	_ *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	// Check if ScummVM binary exists
	_, err := findScummVMBinary()
	if err != nil {
		log.Debug().Err(err).Msg("ScummVM binary not found, skipping scan")
		return results, nil // Not an error, just not installed
	}

	// Check if scummvm.ini exists
	if _, statErr := os.Stat(scummvmIniPath); os.IsNotExist(statErr) {
		log.Debug().Str("path", scummvmIniPath).Msg("scummvm.ini not found, skipping scan")
		return results, nil // No games configured
	}

	// Parse scummvm.ini
	games, err := parseScummVMIni(scummvmIniPath)
	if err != nil {
		return results, fmt.Errorf("failed to parse scummvm.ini: %w", err)
	}

	// Build virtual paths for each game
	sources := scummVMMetadataSources(games)
	for i, game := range games {
		virtualPath := virtualpath.CreateVirtualPath(shared.SchemeScummVM, game.TargetID, game.Description)
		results = append(results, platforms.ScanResult{
			Path:   virtualPath,
			Name:   game.Description,
			Source: sources[i],
			NoExt:  true, // Virtual paths have no extension
		})
	}

	log.Info().Int("count", len(games)).Msg("indexed ScummVM games")
	return results, nil
}
