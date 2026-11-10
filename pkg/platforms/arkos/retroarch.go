//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
)

const defaultRetroArchPort = 55355

// retroArchConfigPath returns the config of the RetroArch build a launch
// uses: ArkOS ships 64-bit retroarch and 32-bit retroarch32 side by side, each
// with its own config directory.
func retroArchConfigPath(launch *esLaunch) (string, bool) {
	name := launch.emulator
	if name != "retroarch" && name != "retroarch32" {
		switch {
		case strings.Contains(launch.command, "/retroarch32 "):
			name = "retroarch32"
		case strings.Contains(launch.command, "/retroarch "):
			name = "retroarch"
		default:
			return "", false
		}
	}
	return filepath.Join(frontendHome, ".config", name, "retroarch.cfg"), true
}

// retroArchControls returns network command controls for a launch whose
// RetroArch has network commands enabled. Core does not edit the user's
// RetroArch config; with commands off, no controls are offered.
func (p *Platform) retroArchControls(launch *esLaunch) map[string]platforms.Control {
	path, ok := retroArchConfigPath(launch)
	if !ok {
		return nil
	}
	data, err := readLimited(p.fs, path)
	if err != nil {
		return nil
	}
	enabled, port := parseNetworkCommandConfig(string(data))
	if !enabled {
		return nil
	}
	return sharedretroarch.Controls(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

func parseNetworkCommandConfig(cfg string) (enabled bool, port int) {
	port = defaultRetroArchPort
	for line := range strings.SplitSeq(cfg, "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "network_cmd_enable":
			enabled = value == "true"
		case "network_cmd_port":
			if n, err := strconv.Atoi(value); err == nil && n > 0 && n < 65536 {
				port = n
			}
		}
	}
	return enabled, port
}
