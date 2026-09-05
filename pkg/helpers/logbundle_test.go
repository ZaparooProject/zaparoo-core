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

func TestReadLogBundle_NeverExceedsBudgetWhenStderrIsLarge(t *testing.T) {
	t.Parallel()

	// Regression test: the log budget was computed as
	// maxBytes - separator - len(stderr), which goes negative once the capture
	// is large. trimFront read a non-positive budget as "no limit" and returned
	// the log untrimmed, so the bundle blew the cap in exactly the case the cap
	// exists for — a full log plus a crash.
	const limit = 300
	pl := bundlePlatform(t,
		strings.Repeat("{\"a\":1}\n", 500),
		strings.Repeat("E", 400)+"\n")

	data, err := helpers.ReadLogBundle(pl, limit)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(data), limit, "bundle must fit the limit however large the capture is")
	assert.Contains(t, string(data), "EEEE", "the capture is appended last, so trimming must keep it")
}

func TestReadLogBundle_NeverExceedsTinyBudget(t *testing.T) {
	t.Parallel()

	// The trim notice itself is ~60 bytes, and it used to be returned whole
	// even when it was longer than the entire budget.
	pl := bundlePlatform(t, strings.Repeat("{\"a\":1}\n", 500), "")

	for _, limit := range []int{1, 10, 20, 59, 80} {
		data, err := helpers.ReadLogBundle(pl, limit)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(data), limit, "limit %d", limit)
	}
}

func TestReadLogBundle_DropsAPartialFirstLine(t *testing.T) {
	t.Parallel()

	// A budget that lands inside a single long line has no whole line to keep.
	// It used to return the tail of that line, which is a fragment: the output
	// began mid-JSON and any parser reading it trips on the first entry.
	oneLongLine := "{\"k\":\"" + strings.Repeat("v", 400) + "\"}\n"
	pl := bundlePlatform(t, oneLongLine, "")

	data, err := helpers.ReadLogBundle(pl, 120)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(data), 120)
	body := strings.SplitN(string(data), "\n", 2)
	require.Len(t, body, 2)
	assert.Empty(t, strings.TrimSpace(body[1]),
		"nothing but the notice should survive when no whole line fits, got %q", body[1])
}

func TestReadLogBundle_CaptureSurvivesALogThatFillsTheBudget(t *testing.T) {
	t.Parallel()

	// The capture is budgeted before the log for this reason: the log rotates
	// at the size of the whole upload budget, so budgeting the other way round
	// lets a full log crowd out the crash — losing the one thing worth
	// reporting, in the one case worth reporting it.
	const limit = 2048
	pl := bundlePlatform(t,
		strings.Repeat("{\"filler\":\"aaaaaaaaaaaaaaaaaaaa\"}\n", 400),
		"panic: the crash that matters\n")

	data, err := helpers.ReadLogBundle(pl, limit)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(data), limit)
	assert.Contains(t, string(data), "panic: the crash that matters")
}

func TestReadLogBundle_CaptureIsKeptWhenItIsOneLongLine(t *testing.T) {
	t.Parallel()

	// A capture is free text, not JSON, so it is trimmed by bytes rather than
	// lines. Line-aligning it would discard a single-line capture whole, which
	// an earlier attempt at this did.
	const limit = 512
	pl := bundlePlatform(t,
		strings.Repeat("{\"a\":1}\n", 200),
		strings.Repeat("X", 900)+"TAIL_OF_CAPTURE")

	data, err := helpers.ReadLogBundle(pl, limit)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(data), limit)
	assert.Contains(t, string(data), "TAIL_OF_CAPTURE",
		"a single-line capture must be trimmed, not discarded")
}
