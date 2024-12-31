/*
Zaparoo Core
Copyright (C) 2023 Gareth Jones
Copyright (C) 2023, 2024 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package main

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config/migrate"
	"github.com/rs/zerolog"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/windows"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
)

func main() {
	sigs := make(chan os.Signal, 1)
	doStop := make(chan bool, 1)
	stopped := make(chan bool, 1)
	defer close(sigs)
	defer close(doStop)
	defer close(stopped)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	pl := &windows.Platform{}
	flags := cli.SetupFlags()

	flags.Pre(pl)

	defaults := config.BaseDefaults
	iniPath := filepath.Join(utils.ExeDir(), "tapto.ini")
	if migrate.Required(iniPath, filepath.Join(pl.ConfigDir(), config.CfgFile)) {
		migrated, err := migrate.IniToToml(iniPath)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error migrating config: %v\n", err)
			os.Exit(1)
		} else {
			defaults = migrated
		}
	}

	cfg := cli.Setup(
		pl,
		defaults,
		[]io.Writer{zerolog.ConsoleWriter{Out: os.Stderr}},
	)

	flags.Post(cfg)

	stopSvc, err := service.Start(pl, cfg)
	if err != nil {
		log.Error().Msgf("error starting service: %s", err)
		fmt.Println("Error starting service:", err)
		os.Exit(1)
	}

	go func() {
		// just wait for either of these
		select {
		case <-sigs:
			break
		case <-doStop:
			break
		}

		err := stopSvc()
		if err != nil {
			log.Error().Msgf("error stopping service: %s", err)
			os.Exit(1)
		}

		stopped <- true
	}()

	ip, err := utils.GetLocalIp()
	if err != nil {
		fmt.Println("Device address: Unknown")
	} else {
		fmt.Println("Device address:", ip.String())
	}

	fmt.Println("Press any key to exit")
	_, _ = fmt.Scanln()
	doStop <- true
	<-stopped

	os.Exit(0)
}
