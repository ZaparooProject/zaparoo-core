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
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/romm"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const fakeRomMURL = "http://romm.test:8080"

// fakeRomMLink answers the device flow with a scripted list of poll results.
type fakeRomMLink struct {
	startErr error
	request  chan romm.DeviceLinkRequest
	polls    chan error
	token    string
}

func (*fakeRomMLink) BaseURL() string { return fakeRomMURL }

func (f *fakeRomMLink) StartDeviceLink(_ context.Context, req romm.DeviceLinkRequest) (*romm.DeviceLink, error) {
	if f.request != nil {
		f.request <- req
	}
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &romm.DeviceLink{
		DeviceCode:              "dev-code",
		UserCode:                "ABCD-1234",
		VerificationURL:         fakeRomMURL + "/pair/device",
		VerificationURLComplete: fakeRomMURL + "/pair/device?user_code=ABCD-1234",
		ExpiresIn:               60,
		Interval:                1,
	}, nil
}

func (f *fakeRomMLink) PollDeviceToken(ctx context.Context, deviceCode string) (string, error) {
	if deviceCode != "dev-code" {
		return "", errors.New("wrong device code")
	}
	select {
	case err := <-f.polls:
		if err != nil {
			return "", err
		}
		return f.token, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func useFakeRomMLink(t *testing.T, f *fakeRomMLink) {
	t.Helper()
	orig := newRomMLinkClient
	newRomMLinkClient = func(serverURL string) (rommLinkClient, error) {
		if _, err := romm.NormalizeServerURL(serverURL); err != nil {
			return nil, err //nolint:wrapcheck // test fake
		}
		return f, nil
	}
	t.Cleanup(func() { newRomMLinkClient = orig })
}

func rommLinkEnv(t *testing.T, serverURL string) (requests.RequestEnv, *config.Instance, <-chan models.Notification) {
	t.Helper()
	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: t.TempDir(), ConfigDir: t.TempDir(),
	}).Maybe()
	st, notificationCh := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(st.StopService)
	params, err := json.Marshal(models.SettingsAuthLinkParams{
		URL: serverURL, Provider: models.AuthLinkProviderRomM,
	})
	require.NoError(t, err)
	return requests.RequestEnv{
		Context:  context.Background(),
		Config:   cfg,
		Platform: mockPlatform,
		State:    st,
		Params:   params,
		IsLocal:  true,
	}, cfg, notificationCh
}

func TestSettingsAuthLinkRomM_RequiresLocalOrAdmin(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)
	useFakeRomMLink(t, &fakeRomMLink{polls: make(chan error)})

	env, _, _ := rommLinkEnv(t, fakeRomMURL)
	env.IsLocal = false
	env.ClientRole = string(permissions.RoleMember)
	_, err := HandleSettingsAuthLink(env, nil)
	require.Error(t, err)
	assert.Nil(t, activeAuthLink, "a refused start leaves no session")

	env.IsLocal = false
	env.ClientRole = ""
	_, err = HandleSettingsAuthLink(env, nil)
	require.Error(t, err)

	env.ClientRole = string(permissions.RoleAdmin)
	result, err := HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
	status, ok := result.(models.AuthLinkStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.AuthLinkProviderRomM, status.Provider)
}

func TestSettingsAuthLinkRomM_RequiresURL(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)
	useFakeRomMLink(t, &fakeRomMLink{polls: make(chan error)})

	for _, u := range []string{"", "  ", "ftp://romm.test", "http://user:pw@romm.test"} {
		env, _, _ := rommLinkEnv(t, u)
		_, err := HandleSettingsAuthLink(env, nil)
		var clientErr *models.ClientError
		require.ErrorAs(t, err, &clientErr, u)
	}
	assert.Nil(t, activeAuthLink)
}

func TestSettingsAuthLink_UnknownProvider(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)

	params, err := json.Marshal(models.SettingsAuthLinkParams{URL: fakeRomMURL, Provider: "other"})
	require.NoError(t, err)
	_, err = HandleSettingsAuthLink(requests.RequestEnv{IsLocal: true, Params: params}, nil)
	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
}

func TestSettingsAuthLinkRomM_StartFailureReleasesSession(t *testing.T) {
	// Not parallel: uses package-level link session state.
	resetAuthLinkState(t)
	useFakeRomMLink(t, &fakeRomMLink{startErr: errors.New("connection refused")})

	env, _, _ := rommLinkEnv(t, fakeRomMURL)
	_, err := HandleSettingsAuthLink(env, nil)
	require.Error(t, err)

	authLinkMu.Lock()
	defer authLinkMu.Unlock()
	assert.Nil(t, activeAuthLink)
	assert.False(t, authLinkStarting, "a failed start can be retried")
}

func TestSettingsAuthLinkRomM_Approved(t *testing.T) {
	// Not parallel: swaps the RomM client and uses package-level link state.
	resetAuthLinkState(t)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
	t.Cleanup(config.ClearAuthCfgForTesting)

	fake := &fakeRomMLink{
		polls:   make(chan error, 2),
		request: make(chan romm.DeviceLinkRequest, 1),
		token:   "rmm_device_token",
	}
	fake.polls <- romm.ErrLinkPending
	fake.polls <- nil
	useFakeRomMLink(t, fake)

	env, cfg, notificationCh := rommLinkEnv(t, fakeRomMURL)
	result, err := HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
	started, ok := result.(models.AuthLinkStatusResponse)
	require.True(t, ok)
	assert.Equal(t, models.AuthLinkStatusPending, started.Status)
	assert.Equal(t, models.AuthLinkProviderRomM, started.Provider)
	assert.Equal(t, "ABCD-1234", started.UserCode)
	assert.Equal(t, fakeRomMURL+"/pair/device", started.VerificationURL)
	require.NotNil(t, started.ExpiresAt)

	req := <-fake.request
	assert.Equal(t, cfg.DeviceID(), req.Identifier)
	assert.Equal(t, "test-platform", req.Platform)
	assert.NotEmpty(t, req.Name)
	assert.Equal(t, config.AppVersion, req.Version)

	pending := readAuthLinkNotification(t, notificationCh)
	assert.Equal(t, models.AuthLinkStatusPending, pending.Status)
	assertAuthLinkNotificationRedacted(t, &pending)

	approved := waitForAuthLinkStatus(t, models.AuthLinkStatusApproved, 10*time.Second)
	assert.Equal(t, models.AuthLinkProviderRomM, approved.Provider)

	entry := config.LookupAuth(config.GetAuthCfg(), fakeRomMURL+"/api/roms")
	require.NotNil(t, entry, "the approved link stores the token for the server")
	assert.Equal(t, "rmm_device_token", entry.Bearer)
	assert.Equal(t, fakeRomMURL, romm.ServerURL(cfg))
}

func TestSettingsAuthLinkRomM_Denied(t *testing.T) {
	// Not parallel: swaps the RomM client and uses package-level link state.
	resetAuthLinkState(t)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
	t.Cleanup(config.ClearAuthCfgForTesting)

	fake := &fakeRomMLink{polls: make(chan error, 1)}
	fake.polls <- romm.ErrLinkDenied
	useFakeRomMLink(t, fake)

	env, cfg, _ := rommLinkEnv(t, fakeRomMURL)
	_, err := HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)

	failed := waitForAuthLinkStatus(t, models.AuthLinkStatusFailed, 10*time.Second)
	assert.Contains(t, failed.Error, "denied")
	assert.Nil(t, config.LookupAuth(config.GetAuthCfg(), fakeRomMURL+"/api/roms"))
	assert.Empty(t, romm.ServerURL(cfg))
}

// Linking makes the RomM launcher available at once, without a launcher
// refresh or a restart.
func TestSettingsAuthLinkRomM_ApprovedMakesLauncherAvailable(t *testing.T) {
	// Not parallel: swaps the RomM client and uses package-level link state.
	resetAuthLinkState(t)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
	t.Cleanup(config.ClearAuthCfgForTesting)

	fake := &fakeRomMLink{polls: make(chan error, 1), token: "rmm_device_token"}
	fake.polls <- nil
	useFakeRomMLink(t, fake)

	env, cfg, _ := rommLinkEnv(t, fakeRomMURL)
	mockPlatform, ok := env.Platform.(*mocks.MockPlatform)
	require.True(t, ok)
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{romm.NewLauncher(mockPlatform)}).Maybe()
	cache := &helpers.LauncherCache{}
	cache.Refresh(mockPlatform, cfg)
	launcher := cache.GetLauncherByID(romm.LauncherID)
	require.NotNil(t, launcher)
	require.False(t, launcher.Available, "no server before linking")
	env.LauncherCache = cache

	_, err := HandleSettingsAuthLink(env, nil)
	require.NoError(t, err)
	waitForAuthLinkStatus(t, models.AuthLinkStatusApproved, 10*time.Second)

	launcher = cache.GetLauncherByID(romm.LauncherID)
	require.NotNil(t, launcher)
	assert.True(t, launcher.Available, launcher.AvailabilityReason)
}
