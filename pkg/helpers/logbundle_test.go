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

package helpers_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bundlePlatform(t *testing.T, logBody, stderrBody string) platforms.Platform {
	t.Helper()

	logDir := t.TempDir()
	if logBody != "" {
		require.NoError(t, os.WriteFile(
			filepath.Join(logDir, config.LogFile), []byte(logBody), 0o600))
	}
	if stderrBody != "" {
		require.NoError(t, os.WriteFile(
			filepath.Join(logDir, config.StderrFile), []byte(stderrBody), 0o600))
	}

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{LogDir: logDir, TempDir: logDir})
	return pl
}

func TestReadLogBundle_AppendsStderr(t *testing.T) {
	t.Parallel()

	pl := bundlePlatform(t, "{\"msg\":\"one\"}\n", "panic: boom\n")

	data, err := helpers.ReadLogBundle(pl, 0)
	require.NoError(t, err)

	assert.Contains(t, string(data), "{\"msg\":\"one\"}")
	assert.Contains(t, string(data), "panic: boom",
		"the crash only exists in the stderr file, so it has to travel with the log")
	assert.Contains(t, string(data), config.StderrFile, "the appended section should be labelled")
}

func TestReadLogBundle_OmitsEmptyStderr(t *testing.T) {
	t.Parallel()

	pl := bundlePlatform(t, "log line one\n", "   \n\n")

	data, err := helpers.ReadLogBundle(pl, 0)
	require.NoError(t, err)

	assert.Equal(t, "log line one\n", string(data),
		"a stderr file holding only whitespace should add nothing")
}

func TestReadLogBundle_MissingStderrIsNotAnError(t *testing.T) {
	t.Parallel()

	pl := bundlePlatform(t, "log line one\n", "")

	data, err := helpers.ReadLogBundle(pl, 0)
	require.NoError(t, err, "the stderr file does not exist until the daemon starts the service")
	assert.Equal(t, "log line one\n", string(data))
}

func TestReadLogBundle_KeepsCrashWhenLogFillsTheBudget(t *testing.T) {
	t.Parallel()

	// The log rotates at 1 MB, which is the whole upload budget, so a full log
	// plus a crash would be rejected by the service and the report lost.
	const limit = 4096
	bigLog := strings.Repeat("{\"msg\":\"filler\"}\n", 1000)
	pl := bundlePlatform(t, bigLog, "panic: misaligned address\n")

	data, err := helpers.ReadLogBundle(pl, limit)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(data), limit, "the bundle must fit the upload limit")
	assert.Contains(t, string(data), "panic: misaligned address",
		"the crash must survive trimming; it is the reason for the bundle")
	assert.Contains(t, string(data), "trimmed", "trimming should be stated in the output")
	assert.Contains(t, string(data), "{\"msg\":\"filler\"}", "the newest log entries should be kept")
}

func TestReadLogBundle_TrimsOnWholeLines(t *testing.T) {
	t.Parallel()

	// A cut mid-line leaves a broken JSON entry that parsers trip over.
	const limit = 512
	body := strings.Repeat("{\"n\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"}\n", 200)
	pl := bundlePlatform(t, body, "")

	data, err := helpers.ReadLogBundle(pl, limit)
	require.NoError(t, err)

	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	for _, line := range lines[1:] { // first line is the trim notice
		assert.True(t, bytes.HasPrefix(line, []byte("{")) && bytes.HasSuffix(line, []byte("}")),
			"every retained line should be whole, got %q", line)
	}
}

func TestReadLogBundle_NoLimitKeepsEverything(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("x", 100000) + "\n"
	pl := bundlePlatform(t, body, "")

	data, err := helpers.ReadLogBundle(pl, 0)
	require.NoError(t, err)

	assert.Len(t, data, len(body), "an unlimited bundle should not trim, e.g. a copy to storage")
}
