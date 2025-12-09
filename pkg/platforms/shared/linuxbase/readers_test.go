//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package linuxbase

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupportedReaders(t *testing.T) {
	t.Parallel()

	// Setup temporary directory for config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	// Create mock platform for tty2oled reader initialization
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	readers := SupportedReaders(cfg, mockPlatform)

	// Verify we get some readers back (default-enabled readers should be returned)
	// The exact number depends on which readers are enabled by default
	assert.NotNil(t, readers)

	// Verify all returned readers have valid metadata
	for _, reader := range readers {
		metadata := reader.Metadata()
		assert.NotEmpty(t, metadata.ID, "reader should have an ID")
	}
}

func TestSupportedReadersAllHaveMetadata(t *testing.T) {
	t.Parallel()

	// Setup temporary directory for config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	readers := SupportedReaders(cfg, mockPlatform)

	// Each reader should have metadata with an ID
	for _, reader := range readers {
		metadata := reader.Metadata()

		assert.NotEmpty(t, metadata.ID,
			"reader %T should have a non-empty ID", reader)
	}
}

func TestSupportedReadersReturnsUniqueReaders(t *testing.T) {
	t.Parallel()

	// Setup temporary directory for config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	readers := SupportedReaders(cfg, mockPlatform)

	// Verify no duplicate reader IDs
	seenIDs := make(map[string]bool)
	for _, reader := range readers {
		id := reader.Metadata().ID
		assert.False(t, seenIDs[id],
			"reader ID %q should not appear more than once", id)
		seenIDs[id] = true
	}
}
