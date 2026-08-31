//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package launchers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

const (
	maxMoonlightTargetSize  = 16 << 10
	maxMoonlightFieldLength = 4096
)

type moonlightTarget struct {
	Host string `json:"host"`
	App  string `json:"app"`
}

//nolint:govet // Test hooks keep resolver configuration grouped.
type moonlightOptions struct {
	resolver  applicationResolverOptions
	launchEnv func() []string
}

// NewMoonlightLauncher creates a launcher for bounded .moonlight target files.
// Targets may be JSON ({"host":"host","app":"app"}) or two lines containing host and app.
func NewMoonlightLauncher() platforms.Launcher {
	return buildMoonlightLauncher(moonlightOptions{
		resolver: applicationResolverOptions{checkFlatpak: true},
	})
}

func buildMoonlightLauncher(options moonlightOptions) platforms.Launcher {
	resolve := newApplicationResolver("moonlight", FlatpakMoonlightID, options.resolver)
	buildCommand := func(path string) (*platforms.LaunchCommand, error) {
		target, err := readMoonlightTarget(path)
		if err != nil {
			return nil, err
		}
		installation, err := resolve()
		if err != nil {
			return nil, err
		}
		return buildApplicationCommand(
			withFlatpakDieWithParent(installation),
			[]string{"stream", target.Host, target.App}, options.launchEnv,
		), nil
	}

	return platforms.Launcher{
		ID: "Moonlight", SystemID: systemdefs.SystemPC,
		Folders: []string{"moonlight"}, Extensions: []string{".moonlight"},
		Lifecycle: platforms.LifecycleBlocking, AllowListOnly: true,
		Availability: func(*config.Instance) error {
			_, err := resolve()
			return err
		},
		BuildLaunchCommand: func(
			_ *config.Instance, path string, _ *platforms.LaunchOptions,
		) (*platforms.LaunchCommand, error) {
			return buildCommand(path)
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			command, err := buildCommand(path)
			if err != nil {
				return nil, err
			}
			process, err := startTrackedApplicationCommand(command)
			if err != nil {
				return nil, fmt.Errorf("start Moonlight stream: %w", err)
			}
			return process, nil
		},
	}
}

func readMoonlightTarget(path string) (moonlightTarget, error) {
	if !strings.EqualFold(filepath.Ext(path), ".moonlight") {
		return moonlightTarget{}, errors.New("moonlight target requires a .moonlight file")
	}
	data, err := readLimitedApplicationFile(path, maxMoonlightTargetSize)
	if err != nil {
		return moonlightTarget{}, err
	}
	return parseMoonlightTargetData(data)
}

func parseMoonlightTargetData(data []byte) (moonlightTarget, error) {
	var target moonlightTarget
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &target); err != nil {
			return moonlightTarget{}, fmt.Errorf("parse Moonlight target: %w", err)
		}
	} else {
		lines := strings.Split(string(trimmed), "\n")
		if len(lines) != 2 {
			return moonlightTarget{}, errors.New("moonlight target must contain host and app")
		}
		target.Host = lines[0]
		target.App = lines[1]
	}
	target.Host = strings.TrimSpace(target.Host)
	target.App = strings.TrimSpace(target.App)
	if !validApplicationField(target.Host, maxMoonlightFieldLength) ||
		!validApplicationField(target.App, maxMoonlightFieldLength) {
		return moonlightTarget{}, errors.New("invalid Moonlight target")
	}
	return target, nil
}
