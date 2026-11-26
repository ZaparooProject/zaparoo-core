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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotifyChangedRunsEachKindOnce(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	var got []string
	cfg.OnChange(ChangeDiscovery, func() { got = append(got, "discovery-1") })
	cfg.OnChange(ChangeDiscovery, func() { got = append(got, "discovery-2") })
	cfg.OnChange(ChangePublishers, func() { got = append(got, "publishers") })

	cfg.NotifyChanged(ChangePublishers, ChangeDiscovery, ChangePublishers, ChangeBLEName)
	assert.Equal(t, []string{"publishers", "discovery-1", "discovery-2"}, got)
}

func TestNotifyChangedListenerMayReadConfig(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	var name string
	cfg.OnChange(ChangeDiscovery, func() { name = cfg.DiscoveryInstanceName() })
	cfg.SetDiscoveryInstanceName("Den")
	cfg.NotifyChanged(ChangeDiscovery)
	assert.Equal(t, "Den", name)
}

func TestRestartRequiredTracksStartValues(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	assert.Empty(t, cfg.RestartRequired(), "nothing recorded, nothing waiting")

	cfg.RecordGamepadAtStart(true, true)
	cfg.RecordErrorReportingAtStart(false)
	assert.Empty(t, cfg.RestartRequired())

	cfg.SetVirtualGamepadEnabled(false)
	cfg.SetErrorReporting(true)
	assert.Equal(t, []string{RestartErrorReporting, RestartGamepadEnabled}, cfg.RestartRequired())

	cfg.SetVirtualGamepadEnabled(true)
	assert.Equal(t, []string{RestartErrorReporting}, cfg.RestartRequired(),
		"a value set back to what Core started with drops off")
}

func TestRestartRequiredSurvivesLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg, err := NewConfig(dir, BaseDefaults)
	require.NoError(t, err)
	cfg.RecordErrorReportingAtStart(false)

	require.NoError(t, os.WriteFile(filepath.Join(dir, CfgFile),
		[]byte("error_reporting = true\n"), 0o600))
	require.NoError(t, cfg.Load())
	assert.Equal(t, []string{RestartErrorReporting}, cfg.RestartRequired(),
		"a reload from disk keeps what the running Core started with")
}

func TestDataDirRelativePaths(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	abs := filepath.Join(t.TempDir(), "elsewhere")

	cfg := &Instance{}
	assert.Empty(t, cfg.BackupLocalDirIn(dataDir), "unset stays unset")
	assert.Empty(t, cfg.ScraperGamelistXMLCustomDir(dataDir))

	cfg.SetBackupLocalDir("backups")
	cfg.vals.Scraper.GamelistXML.CustomPath = filepath.Join("gamelists", "es")
	assert.Equal(t, filepath.Join(dataDir, "backups"), cfg.BackupLocalDirIn(dataDir),
		"a relative folder is under the data folder, not the working directory")
	assert.Equal(t, filepath.Join(dataDir, "gamelists", "es"), cfg.ScraperGamelistXMLCustomDir(dataDir))

	cfg.SetBackupLocalDir(abs)
	cfg.vals.Scraper.GamelistXML.CustomPath = abs
	assert.Equal(t, abs, cfg.BackupLocalDirIn(dataDir))
	assert.Equal(t, abs, cfg.ScraperGamelistXMLCustomDir(dataDir))
}

func TestRedactBrokerDropsCredentialsAndPaths(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{ //nolint:gosec // G101: fake credentials the test checks are redacted
		"":                                   "",
		"tcp://user:secret@broker:1883/path": "tcp://broker:1883",
		"user:secret@broker:1883":            "broker:1883",
		"broker:1883":                        "broker:1883",
	} {
		assert.Equal(t, want, RedactBroker(in), in)
	}
}
