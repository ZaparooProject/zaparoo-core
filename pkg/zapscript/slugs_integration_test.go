//go:build slugs_integration

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package zapscript

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testDataDir = "testdata/inputs"

type launchCapture struct {
	Path string
	Name string
}

type testStats struct {
	tested   int
	found    int
	notFound int
	failures []string
}

func TestSlugMatching_Integration(t *testing.T) {
	dbPath := filepath.Join(testDataDir, "media.db")

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skipf("Integration test skipped: %s not found (run with media.db in testdata/)", dbPath)
	}

	ctx := context.Background()
	mockPlatform := setupMockPlatform(t, dbPath)

	db, err := mediadb.OpenMediaDB(ctx, mockPlatform)
	require.NoError(t, err)
	defer db.Close()

	cfg := createTestConfig()

	testFiles, err := filepath.Glob(filepath.Join(testDataDir, "*.txt"))
	if err != nil {
		t.Fatalf("Failed to glob test files: %v", err)
	}

	if len(testFiles) == 0 {
		t.Skip("No .txt test files found in testdata/")
	}

	totalStats := &testStats{}
	allSystemStats := make(map[string]*testStats)

	for _, filePath := range testFiles {
		testFile := filepath.Base(filePath)
		systemStats := &testStats{}
		allSystemStats[testFile] = systemStats

		t.Run(testFile, func(t *testing.T) {
			file, err := os.Open(filePath)
			require.NoError(t, err)
			defer file.Close()

			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}

				systemStats.tested++
				totalStats.tested++

				_, slug, err := testSlugResolutionFlow(ctx, db, cfg, mockPlatform, line)

				if err != nil {
					systemStats.notFound++
					totalStats.notFound++
					failure := fmt.Sprintf("NO MATCH: \"%s\" -> %s", line, slug)
					systemStats.failures = append(systemStats.failures, failure)
					totalStats.failures = append(totalStats.failures, failure)
				} else {
					systemStats.found++
					totalStats.found++
				}
			}

			require.NoError(t, scanner.Err())
		})
	}

	t.Cleanup(func() {
		for _, failure := range totalStats.failures {
			fmt.Fprintln(os.Stderr, failure)
		}

		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "=== OVERALL STATISTICS ===")
		fmt.Fprintf(os.Stderr, "Total tested: %d\n", totalStats.tested)
		if totalStats.tested > 0 {
			fmt.Fprintf(os.Stderr, "Found: %d (%.1f%%)\n", totalStats.found, float64(totalStats.found)/float64(totalStats.tested)*100)
			fmt.Fprintf(os.Stderr, "Not found: %d (%.1f%%)\n", totalStats.notFound, float64(totalStats.notFound)/float64(totalStats.tested)*100)
		}
	})
}

func testSlugResolutionFlow(
	ctx context.Context,
	db *mediadb.MediaDB,
	cfg *config.Instance,
	pl platforms.Platform,
	input string,
) (*launchCapture, string, error) {
	reader := parser.NewParser(fmt.Sprintf("**launch.slug:%s", input))
	script, err := reader.ParseScript()
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse ZapScript: %w", err)
	}

	if len(script.Cmds) == 0 {
		return nil, "", fmt.Errorf("no commands parsed from input")
	}

	cmd := script.Cmds[0]

	captured := &launchCapture{}

	mockLauncherPlatform := &mockLauncherPlatform{
		Platform: pl,
		captured: captured,
	}

	env := platforms.CmdEnv{
		Cfg: cfg,
		Database: &database.Database{
			MediaDB: db,
		},
		Cmd:           cmd,
		TotalCommands: 1,
		CurrentIndex:  0,
		Unsafe:        false,
	}

	result, err := cmdSlug(mockLauncherPlatform, env)

	parts := strings.SplitN(input, "/", 2)
	slugInfo := input
	if len(parts) == 2 {
		systemID := parts[0]
		gameName := parts[1]
		slug := slugs.SlugifyString(gameName)
		slugInfo = fmt.Sprintf("%s/%s", systemID, slug)
	}

	if err != nil {
		return nil, slugInfo, err
	}

	if !result.MediaChanged {
		return nil, slugInfo, fmt.Errorf("cmdSlug succeeded but MediaChanged was false")
	}

	if captured.Path == "" {
		return nil, slugInfo, fmt.Errorf("cmdSlug succeeded but no path was captured")
	}

	captured.Name = filepath.Base(captured.Path)

	return captured, slugInfo, nil
}

func setupMockPlatform(t *testing.T, dbPath string) platforms.Platform {
	t.Helper()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: filepath.Dir(dbPath),
	})
	mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{
		{
			ID:         "test",
			SystemID:   "",
			Extensions: []string{},
			Test: func(cfg *config.Instance, path string) bool {
				return true
			},
			Launch: func(cfg *config.Instance, path string) (*os.Process, error) {
				return nil, nil
			},
		},
	})
	mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockPlatform.On("StopActiveLauncher").Return(nil)

	return mockPlatform
}

func createTestConfig() *config.Instance {
	cfg := &config.Instance{}
	return cfg
}

type mockLauncherPlatform struct {
	platforms.Platform
	captured *launchCapture
}

func (m *mockLauncherPlatform) LaunchMedia(cfg *config.Instance, path string, launcher *platforms.Launcher) error {
	if m.captured != nil {
		m.captured.Path = path
	}
	return nil
}
