//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package linuxbase

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/gamescope"
	"github.com/rs/zerolog/log"
)

const sessionEnvTimeout = 2 * time.Second

// DesktopSessionEnvOverrides returns GUI and user-directory variables for the
// active desktop session. This prevents service-specific XDG paths from leaking
// into launched applications. A detected gamescope display takes precedence.
func DesktopSessionEnvOverrides(gameMode *gamescope.Manager) []string {
	ctx, cancel := context.WithTimeout(context.Background(), sessionEnvTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "systemctl", "--user", "show-environment").Output()
	if err != nil {
		log.Debug().Err(err).Msg("failed to read current desktop session environment")
	}
	home, _ := os.UserHomeDir()
	env := helpers.MergeEnviron(desktopSessionEnvDefaults(home), ParseDesktopSessionEnv(string(output)))
	if gameMode != nil {
		if display := gameMode.GamescopeDisplay(); display != "" {
			env = helpers.MergeEnviron(env, []string{"DISPLAY=" + display})
		}
	}
	return env
}

// ParseDesktopSessionEnv filters systemd user-manager output to variables safe
// and useful for launching GUI applications in the active desktop session.
func desktopSessionEnvDefaults(home string) []string {
	if home == "" {
		return nil
	}
	return []string{
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
	}
}

func ParseDesktopSessionEnv(output string) []string {
	result := make([]string, 0, 8)
	for line := range strings.SplitSeq(output, "\n") {
		key, _, found := strings.Cut(line, "=")
		if found && IsDesktopSessionEnvKey(key) {
			result = append(result, line)
		}
	}
	return result
}

// IsDesktopSessionEnvKey reports whether key belongs to the active GUI session.
func IsDesktopSessionEnvKey(key string) bool {
	switch key {
	case "DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "XDG_SESSION_TYPE",
		"XDG_CURRENT_DESKTOP", "DESKTOP_SESSION", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS",
		"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME":
		return true
	default:
		return false
	}
}
