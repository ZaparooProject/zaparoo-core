//go:build linux

package mister

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	scummvmBaseDir = "/media/fat/ScummVM"
	scummvmIniPath = "/media/fat/ScummVM/.config/scummvm/scummvm.ini"
)

// ScummVMGame represents a game configured in scummvm.ini
type ScummVMGame struct {
	TargetID    string // Section name, used to launch the game
	Description string // Human-readable game title
}

// findScummVMBinary searches for ScummVM executable in the ScummVM directory
func findScummVMBinary() (string, error) {
	pattern := filepath.Join(scummvmBaseDir, "scummvm*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("failed to search for ScummVM binary: %w", err)
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no ScummVM binary found in %s", scummvmBaseDir)
	}

	// Return the first match
	binary := matches[0]

	// Verify it's executable
	info, err := os.Stat(binary)
	if err != nil {
		return "", fmt.Errorf("failed to stat ScummVM binary %s: %w", binary, err)
	}

	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("ScummVM binary %s is not executable", binary)
	}

	return binary, nil
}

// parseScummVMIni parses the scummvm.ini file and extracts game configurations
func parseScummVMIni(iniPath string) ([]ScummVMGame, error) {
	file, err := os.Open(iniPath) //nolint:gosec // Path is from const or config
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

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
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

				// Extract description for human-readable name
				if key == "description" {
					currentGame.Description = value
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
	for _, game := range games {
		virtualPath := helpers.CreateVirtualPath("scummvm", game.TargetID, game.Description)
		results = append(results, platforms.ScanResult{
			Path:  virtualPath,
			Name:  game.Description,
			NoExt: true, // Virtual paths have no extension
		})
	}

	log.Info().Int("count", len(games)).Msg("indexed ScummVM games")
	return results, nil
}
