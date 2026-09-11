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

package methods

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func logsRequestEnv(t *testing.T, logBody, stderrBody string) requests.RequestEnv {
	t.Helper()

	logDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(logDir, config.LogFile), []byte(logBody), 0o600))
	if stderrBody != "" {
		require.NoError(t, os.WriteFile(
			filepath.Join(logDir, config.StderrFile), []byte(stderrBody), 0o600))
	}

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{LogDir: logDir, TempDir: logDir})
	return requests.RequestEnv{Context: context.Background(), Platform: pl, IsLocal: true}
}

func decodeLogResponse(t *testing.T, result any) string {
	t.Helper()
	resp, ok := result.(models.LogDownloadResponse)
	require.True(t, ok, "unexpected response type %T", result)
	raw, err := base64.StdEncoding.DecodeString(resp.Content)
	require.NoError(t, err)
	assert.Len(t, raw, resp.Size, "Size must describe the content actually returned")
	return string(raw)
}

func TestHandleLogsDownload_IncludesCapturedCrash(t *testing.T) {
	t.Parallel()

	// This is the path a reported log comes from. A crash that kills the
	// service exists only in the stderr capture, so if it does not travel
	// here it does not reach a bug report at all.
	env := logsRequestEnv(t, "{\"msg\":\"one\"}\n", "panic: misaligned address\n")

	result, err := HandleLogsDownload(env)
	require.NoError(t, err)

	body := decodeLogResponse(t, result)
	assert.Contains(t, body, "{\"msg\":\"one\"}")
	assert.Contains(t, body, "panic: misaligned address")
}

func TestHandleLogsDownload_FitsTheUploadBudget(t *testing.T) {
	t.Parallel()

	// What this returns is what a client uploads, and the service rejects an
	// oversized request outright — taking the crash report with it.
	env := logsRequestEnv(t,
		strings.Repeat("{\"filler\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"}\n", 40000),
		"panic: still here\n")

	result, err := HandleLogsDownload(env)
	require.NoError(t, err)

	body := decodeLogResponse(t, result)
	assert.LessOrEqual(t, len(body), config.LogBundleMaxBytes)
	assert.Contains(t, body, "panic: still here", "trimming must not drop the crash")
}

func TestHandleLogsDownload_WorksWithoutACaptureFile(t *testing.T) {
	t.Parallel()

	// The capture only exists once the daemon has started the service.
	env := logsRequestEnv(t, "log line one\n", "")

	result, err := HandleLogsDownload(env)
	require.NoError(t, err)

	assert.Equal(t, "log line one\n", decodeLogResponse(t, result))
}

func TestHandleLogsDownload_ErrorsWhenTheLogIsMissing(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{LogDir: t.TempDir(), TempDir: t.TempDir()})

	_, err := HandleLogsDownload(requests.RequestEnv{
		Context: context.Background(), Platform: pl, IsLocal: true,
	})

	require.Error(t, err)
}
