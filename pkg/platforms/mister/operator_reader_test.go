//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

package mister

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperatorReaderDetection(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	reader := newOperatorReader(&config.Instance{}, fs)

	metadata := reader.Metadata()
	assert.Equal(t, operatorReaderID, metadata.ID)
	assert.Equal(t, "Epilogue Operator cartridge bridge", metadata.Description)
	assert.True(t, metadata.DefaultEnabled)
	assert.True(t, metadata.DefaultAutoDetect)
	assert.Equal(t, []string{operatorReaderID}, reader.IDs())
	assert.Empty(t, reader.Detect(nil), "missing Operator entry point must not activate reader")

	require.NoError(t, fs.MkdirAll(filepath.Dir(operatorEntryPath), 0o755))
	require.NoError(t, afero.WriteFile(fs, operatorEntryPath, nil, 0o644))
	expectedConnection := operatorReaderID + ":" + operatorTokenPath
	assert.Equal(t, expectedConnection, reader.Detect(nil))
	assert.Empty(t, reader.Detect([]string{"file:" + operatorTokenPath}),
		"connected token path must not be detected twice")

	require.NoError(t, fs.Remove(operatorEntryPath))
	require.NoError(t, fs.Mkdir(operatorEntryPath, 0o755))
	assert.Empty(t, reader.Detect(nil), "directory at entry point path must not activate reader")
}

func TestSupportedReadersIncludesOperator(t *testing.T) {
	t.Parallel()

	platform := NewPlatform()
	supportedReaders := platform.SupportedReaders(&config.Instance{})

	var found bool
	for _, reader := range supportedReaders {
		if reader.Metadata().ID == operatorReaderID {
			found = true
		}
		require.NoError(t, reader.Close())
	}
	assert.True(t, found)
}
