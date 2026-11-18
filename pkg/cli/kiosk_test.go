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

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:gosec // G101: a file path used by the tests, not a credential
const recoveryTokenPath = "/data/kiosk-recovery-token"

// kioskCalls answers the kiosk status call with state and records an unlock.
func kioskCalls(state string, unlockErr error, unlocked *models.KioskUnlockParams) profileAPICaller {
	return func(_ context.Context, _ *config.Instance, method, params string) (string, error) {
		switch method {
		case models.MethodKiosk:
			return `{"state":"` + state + `","seal":{"sealed":false,"unsealed":[]}}`, nil
		case models.MethodKioskUnlock:
			if unlockErr != nil {
				return "", unlockErr
			}
			if err := json.Unmarshal([]byte(params), unlocked); err != nil {
				return "", fmt.Errorf("decode unlock params: %w", err)
			}
			return `{"state":"unlocked","seal":{"sealed":false,"unsealed":[]}}`, nil
		default:
			return "", errors.New("unexpected method " + method)
		}
	}
}

func TestUnlockKioskForRecovery_LeavesAnUnlockedDeviceAlone(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"off", "unlocked"} {
		var unlocked models.KioskUnlockParams
		// No token file exists: it must not even be looked for.
		err := unlockKioskForRecovery(context.Background(), nil, afero.NewMemMapFs(),
			recoveryTokenPath, kioskCalls(state, nil, &unlocked))
		require.NoError(t, err, state)
		assert.Nil(t, unlocked.RecoveryToken, state)
	}
}

func TestUnlockKioskForRecovery_UnlocksWithTheTokenOnDisk(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, recoveryTokenPath, []byte("abc123\n"), 0o600))
	var unlocked models.KioskUnlockParams

	err := unlockKioskForRecovery(context.Background(), nil, fs,
		recoveryTokenPath, kioskCalls("locked", nil, &unlocked))
	require.NoError(t, err)
	require.NotNil(t, unlocked.RecoveryToken)
	assert.Equal(t, "abc123", *unlocked.RecoveryToken)
	assert.Nil(t, unlocked.PIN, "recovery never sends a PIN")
}

func TestUnlockKioskForRecovery_FailsWithoutTheToken(t *testing.T) {
	t.Parallel()
	var unlocked models.KioskUnlockParams
	err := unlockKioskForRecovery(context.Background(), nil, afero.NewMemMapFs(),
		recoveryTokenPath, kioskCalls("locked", nil, &unlocked))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recovery token")
	assert.Nil(t, unlocked.RecoveryToken)
}

func TestUnlockKioskForRecovery_ReportsARefusedToken(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, recoveryTokenPath, []byte("stale\n"), 0o600))
	var unlocked models.KioskUnlockParams

	err := unlockKioskForRecovery(context.Background(), nil, fs,
		recoveryTokenPath, kioskCalls("locked", errors.New("client role does not permit this method"), &unlocked))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unlock the device for recovery")
}
