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

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bindTaken(t *testing.T) error {
	t.Helper()

	lc := &net.ListenConfig{}
	first, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	second, err := lc.Listen(context.Background(), "tcp", first.Addr().String())
	if err == nil {
		_ = second.Close()
		t.Fatal("binding a taken port succeeded")
	}
	return fmt.Errorf("failed to bind API listener: %w", err)
}

func TestIsAddrInUse(t *testing.T) {
	t.Parallel()

	assert.True(t, IsAddrInUse(bindTaken(t)))
	assert.False(t, IsAddrInUse(errors.New("listen tcp: lookup nowhere: no such host")))
	assert.False(t, IsAddrInUse(nil))
}

// Another Core already holding the port is the user's environment, not a
// fault, so it must not reach error reporting.
func TestLogBindFailureLevel(t *testing.T) {
	logs := captureLogs(t, zerolog.WarnLevel)

	LogBindFailure(bindTaken(t))
	LogBindFailure(errors.New("listen tcp: permission denied"))

	levels := map[string]string{}
	for _, line := range strings.Split(logs.String(), "\n") {
		var entry struct {
			Level   string `json:"level"`
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil || !strings.HasPrefix(entry.Message, "failed to bind") {
			continue
		}
		levels[entry.Error] = entry.Level
	}
	require.Len(t, levels, 2)
	for errText, level := range levels {
		if strings.Contains(errText, "permission denied") {
			assert.Equal(t, "error", level)
		} else {
			assert.Equal(t, "warn", level, errText)
		}
	}
}

// A service start that fails on a taken port was reported a second time, at
// Error, by the code that started the service, after the bind itself had
// already been logged at Warn.
func TestStartupFailureLevel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, zerolog.WarnLevel,
		StartupFailureLevel(fmt.Errorf("api startup failed: %w", bindTaken(t))))
	assert.Equal(t, zerolog.WarnLevel,
		StartupFailureLevel(fmt.Errorf("opening user database: %w", database.ErrSchemaAhead)))
	assert.Equal(t, zerolog.ErrorLevel, StartupFailureLevel(errors.New("disk went away")))
}
