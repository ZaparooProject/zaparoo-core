// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Flags struct {
	Write      *string
	Read       *bool
	Run        *string
	Launch     *string
	API        *string
	Version    *bool
	Config     *bool
	ShowLoader *string
	ShowPicker *string
	Reload     *bool
}

// SetupFlags defines all common CLI flags between platforms.
func SetupFlags() *Flags {
	return &Flags{
		Write: flag.String(
			"write",
			"",
			"write value to next scanned token",
		),
		Read: flag.Bool(
			"read",
			false,
			"print next scanned token without running",
		),
		Run: flag.String(
			"run",
			"",
			"run value directly as ZapScript",
		),
		Launch: flag.String(
			"launch",
			"",
			"alias of run (DEPRECATED)",
		),
		API: flag.String(
			"api",
			"",
			"send method and params to API and print response",
		),
		Version: flag.Bool(
			"version",
			false,
			"print version and exit",
		),
		Config: flag.Bool(
			"config",
			false,
			"start the text ui to handle Zaparoo config",
		),
		Reload: flag.Bool(
			"reload",
			false,
			"reload config and mappings from disk",
		),
	}
}

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// Pre runs flag parsing and actions any immediate flags that don't
// require environment setup. Add any custom flags before running this.
func (f *Flags) Pre(pl platforms.Platform) {
	flag.Parse()

	if *f.Version {
		_, _ = fmt.Printf("Zaparoo v%s (%s)\n", config.AppVersion, pl.ID())
		os.Exit(0)
	}
}

func runFlag(cfg *config.Instance, value string) {
	data, err := json.Marshal(&models.RunParams{
		Text: &value,
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error encoding params: %v\n", err)
		os.Exit(1)
	}

	_, err = client.LocalClient(context.Background(), cfg, models.MethodRun, string(data))
	if err != nil {
		log.Error().Err(err).Msg("error running")
		_, _ = fmt.Fprintf(os.Stderr, "Error running: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Post actions all remaining common flags that require the environment to be
// set up. Logging is allowed.
func (f *Flags) Post(cfg *config.Instance, _ platforms.Platform) {
	switch {
	case isFlagPassed("write"):
		if *f.Write == "" {
			_, _ = fmt.Fprint(os.Stderr, "Error: write flag requires a value\n")
			os.Exit(1)
		}

		data, err := json.Marshal(&models.ReaderWriteParams{
			Text: *f.Write,
		})
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error encoding params: %v\n", err)
			os.Exit(1)
		}

		enableRun := client.DisableZapScript(cfg)

		// cleanup after ctrl-c
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs
			enableRun()
			os.Exit(1)
		}()

		_, err = client.LocalClient(context.Background(), cfg, models.MethodReadersWrite, string(data))
		if err != nil {
			log.Error().Err(err).Msg("error writing tag")
			_, _ = fmt.Fprintf(os.Stderr, "Error writing tag: %v\n", err)
			enableRun()
			os.Exit(1)
		}
		_, _ = fmt.Fprintf(os.Stderr, "Tag: %s written successfully\n", *f.Write)
		enableRun()
		os.Exit(0)
	case *f.Read:
		enableRun := client.DisableZapScript(cfg)

		// cleanup after ctrl-c
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs
			close(sigs)
			enableRun()
			os.Exit(0)
		}()

		resp, err := client.WaitNotification(
			context.Background(), 0,
			cfg, models.NotificationTokensAdded,
		)
		if err != nil {
			log.Error().Err(err).Msg("error waiting for notification")
			_, _ = fmt.Fprintf(os.Stderr, "Error waiting for notification: %v\n", err)
			close(sigs)
			enableRun()
			os.Exit(1)
		}

		close(sigs)
		enableRun()
		_, _ = fmt.Println(resp)
		os.Exit(0)
	case isFlagPassed("launch"):
		if *f.Launch == "" {
			_, _ = fmt.Fprint(os.Stderr, "Error: launch flag requires a value\n")
			os.Exit(1)
		}
		runFlag(cfg, *f.Launch)
	case isFlagPassed("run"):
		if *f.Run == "" {
			_, _ = fmt.Fprint(os.Stderr, "Error: run flag requires a value\n")
		}
		runFlag(cfg, *f.Run)
	case isFlagPassed("api"):
		if *f.API == "" {
			_, _ = fmt.Fprint(os.Stderr, "Error: api flag requires a value\n")
			os.Exit(1)
		}

		ps := strings.SplitN(*f.API, ":", 2)
		method := ps[0]
		params := ""
		if len(ps) > 1 {
			params = ps[1]
		}

		resp, err := client.LocalClient(context.Background(), cfg, method, params)
		if err != nil {
			log.Error().Err(err).Msg("error calling API")
			_, _ = fmt.Fprintf(os.Stderr, "Error calling API: %v\n", err)
			os.Exit(1)
		}

		_, _ = fmt.Println(resp)
		os.Exit(0)
	case *f.Reload:
		_, err := client.LocalClient(context.Background(), cfg, models.MethodSettingsReload, "")
		if err != nil {
			log.Error().Err(err).Msg("error reloading settings")
			_, _ = fmt.Fprintf(os.Stderr, "Error reloading: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

// Setup initializes the user config and logging. Returns a user config object.
//
//nolint:gocritic // config struct copied for immutability
func Setup(
	pl platforms.Platform,
	defaultConfig config.Values,
	writers []io.Writer,
) *config.Instance {
	// Ensure directories exist before logging initialization
	err := helpers.EnsureDirectories(pl)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error creating directories: %v\n", err)
		os.Exit(1)
	}

	err = helpers.InitLogging(pl, writers)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error initializing logging: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.NewConfig(helpers.ConfigDir(pl), defaultConfig)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if cfg.DebugLogging() {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Initialize error reporting (opt-in)
	if err := telemetry.Init(
		cfg.ErrorReporting(),
		cfg.DeviceID(),
		config.AppVersion,
		pl.ID(),
	); err != nil {
		log.Warn().Err(err).Msg("failed to initialize error reporting")
	}

	return cfg
}
