//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package launchers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplicationResolverRetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	t.Run("native", func(t *testing.T) {
		t.Parallel()

		calls := 0
		resolve := newApplicationResolver("example", "com.example.App", applicationResolverOptions{
			lookPath: func(name string) (string, error) {
				calls++
				if calls == 1 {
					return "", errors.New("temporarily unavailable")
				}
				return "/usr/bin/" + name, nil
			},
		})

		_, err := resolve()
		require.Error(t, err)
		installation, err := resolve()
		require.NoError(t, err)
		assert.Equal(t, applicationInstallation{executable: "/usr/bin/example"}, installation)
		cached, err := resolve()
		require.NoError(t, err)
		assert.Equal(t, installation, cached)
		assert.Equal(t, 2, calls)
	})

	t.Run("flatpak", func(t *testing.T) {
		t.Parallel()

		flatpakCalls := 0
		resolve := newApplicationResolver("example", "com.example.App", applicationResolverOptions{
			checkFlatpak:       true,
			isFlatpakInstalled: func(string) bool { return true },
			lookPath: func(name string) (string, error) {
				if name != "flatpak" {
					return "", errors.New("not installed")
				}
				flatpakCalls++
				if flatpakCalls == 1 {
					return "", errors.New("temporarily unavailable")
				}
				return "/usr/bin/flatpak", nil
			},
		})

		_, err := resolve()
		require.Error(t, err)
		installation, err := resolve()
		require.NoError(t, err)
		assert.Equal(t, applicationInstallation{
			executable: "/usr/bin/flatpak",
			argsPrefix: []string{"run", "com.example.App"},
			flatpak:    true,
		}, installation)
		_, err = resolve()
		require.NoError(t, err)
		assert.Equal(t, 2, flatpakCalls)
	})
}
