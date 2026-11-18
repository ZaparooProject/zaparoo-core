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

package romm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deviceServer answers RomM's device-flow endpoints with fixed replies and
// records what the client sent.
func deviceServer(
	t *testing.T, initStatus int, initBody any, tokenStatus int, tokenBody any,
) (*Client, *deviceInitPayload) {
	t.Helper()
	sent := &deviceInitPayload{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/device/init", func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"), "linking sends no credentials")
		assert.NoError(t, json.NewDecoder(r.Body).Decode(sent))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(initStatus)
		_ = json.NewEncoder(w).Encode(initBody)
	})
	mux.HandleFunc("POST /api/auth/device/token", func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"), "linking sends no credentials")
		var req deviceTokenPayload
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "dev-code", req.DeviceCode)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(tokenStatus)
		_ = json.NewEncoder(w).Encode(tokenBody)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// A stored token for the server must not ride along on link requests.
	t.Cleanup(config.AddAuthEntryForTesting(srv.URL, config.CredentialEntry{Bearer: "stale"}))
	c, err := NewClient(srv.URL + "/")
	require.NoError(t, err)
	return c, sent
}

func validInit() deviceInitResponse {
	return deviceInitResponse{
		DeviceCode:               "dev-code",
		UserCode:                 "ABCD-1234",
		VerificationPath:         "/pair/device",
		VerificationPathComplete: "/pair/device?user_code=ABCD-1234",
		ExpiresIn:                600,
		Interval:                 5,
	}
}

func TestStartDeviceLink(t *testing.T) {
	t.Parallel()

	c, sent := deviceServer(t, http.StatusCreated, validInit(), http.StatusOK, nil)
	link, err := c.StartDeviceLink(context.Background(), DeviceLinkRequest{
		Identifier: "device-1", Name: "Living room", Platform: "mister", Version: "2.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, "dev-code", link.DeviceCode)
	assert.Equal(t, "ABCD-1234", link.UserCode)
	assert.Equal(t, c.BaseURL()+"/pair/device", link.VerificationURL)
	assert.Equal(t, c.BaseURL()+"/pair/device?user_code=ABCD-1234", link.VerificationURLComplete)
	assert.Equal(t, 600, link.ExpiresIn)
	assert.Equal(t, 5, link.Interval)

	assert.Equal(t, "device-1", sent.Identifier)
	assert.Equal(t, "Living room", sent.Name)
	assert.Equal(t, "Zaparoo Core", sent.Client)
	assert.Equal(t, "mister", sent.Platform)
	assert.Equal(t, "2.0.0", sent.Version)
	assert.Equal(t, DeviceScopes, sent.RequestedScopes)
}

func TestStartDeviceLinkRefusesForeignVerificationURL(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"https://evil.example/pair",
		"//evil.example/pair",
		"pair/device",
		`/\evil.example`,
		"",
	} {
		body := validInit()
		body.VerificationPath = path
		body.VerificationPathComplete = path
		c, _ := deviceServer(t, http.StatusCreated, body, http.StatusOK, nil)
		_, err := c.StartDeviceLink(context.Background(), DeviceLinkRequest{Identifier: "d"})
		require.Error(t, err, path)
	}

	// A bad complete URL alone falls back to the plain one.
	body := validInit()
	body.VerificationPathComplete = "https://evil.example/pair"
	c, _ := deviceServer(t, http.StatusCreated, body, http.StatusOK, nil)
	link, err := c.StartDeviceLink(context.Background(), DeviceLinkRequest{Identifier: "d"})
	require.NoError(t, err)
	assert.Equal(t, link.VerificationURL, link.VerificationURLComplete)
}

func TestStartDeviceLinkErrors(t *testing.T) {
	t.Parallel()

	c, _ := deviceServer(t, http.StatusNotFound, map[string]string{"detail": "Not Found"}, http.StatusOK, nil)
	_, err := c.StartDeviceLink(context.Background(), DeviceLinkRequest{Identifier: "d"})
	require.ErrorContains(t, err, "5.0 or later")

	body := validInit()
	body.DeviceCode = ""
	c, _ = deviceServer(t, http.StatusCreated, body, http.StatusOK, nil)
	_, err = c.StartDeviceLink(context.Background(), DeviceLinkRequest{Identifier: "d"})
	require.Error(t, err)

	c, _ = deviceServer(t, http.StatusBadGateway, nil, http.StatusOK, nil)
	_, err = c.StartDeviceLink(context.Background(), DeviceLinkRequest{Identifier: "d"})
	require.Error(t, err)
}

func TestPollDeviceToken(t *testing.T) {
	t.Parallel()

	c, _ := deviceServer(t, http.StatusCreated, validInit(), http.StatusOK,
		map[string]any{"access_token": "rmm_token", "token_type": "bearer"})
	token, err := c.PollDeviceToken(context.Background(), "dev-code")
	require.NoError(t, err)
	assert.Equal(t, "rmm_token", token)

	for detail, want := range map[string]error{
		"authorization_pending": ErrLinkPending,
		"slow_down":             ErrLinkSlowDown,
		"access_denied":         ErrLinkDenied,
		"expired_token":         ErrLinkExpired,
	} {
		pc, _ := deviceServer(t, http.StatusCreated, validInit(), http.StatusBadRequest,
			map[string]string{"detail": detail})
		_, pollErr := pc.PollDeviceToken(context.Background(), "dev-code")
		require.ErrorIs(t, pollErr, want, detail)
	}

	c, _ = deviceServer(t, http.StatusCreated, validInit(), http.StatusBadRequest,
		map[string]string{"detail": "invalid_grant"})
	_, err = c.PollDeviceToken(context.Background(), "dev-code")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrLinkPending)

	c, _ = deviceServer(t, http.StatusCreated, validInit(), http.StatusTooManyRequests, nil)
	_, err = c.PollDeviceToken(context.Background(), "dev-code")
	require.ErrorIs(t, err, ErrLinkSlowDown)

	c, _ = deviceServer(t, http.StatusCreated, validInit(), http.StatusServiceUnavailable, nil)
	_, err = c.PollDeviceToken(context.Background(), "dev-code")
	assert.True(t, IsUnreachable(err))

	c, _ = deviceServer(t, http.StatusCreated, validInit(), http.StatusOK, map[string]any{"access_token": " "})
	_, err = c.PollDeviceToken(context.Background(), "dev-code")
	require.Error(t, err)
}
