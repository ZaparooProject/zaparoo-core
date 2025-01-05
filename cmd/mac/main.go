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
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/rs/zerolog"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mac"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
)

func main() {
	versionOpt := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionOpt {
		fmt.Println("Zaparoo Core v" + config.AppVersion + " (mac)")
		os.Exit(0)
	}

	pl := &mac.Platform{}

	cfg := cli.Setup(
		pl,
		config.BaseDefaults,
		[]io.Writer{zerolog.ConsoleWriter{Out: os.Stderr}},
	)

	fmt.Println("Zaparoo v" + config.AppVersion)

	stopSvc, err := service.Start(pl, cfg)
	if err != nil {
		log.Error().Msgf("error starting service: %s", err)
		fmt.Println("Error starting service:", err)
		os.Exit(1)
	}

	ip, err := utils.GetLocalIp()
	if err != nil {
		fmt.Println("Device address: Unknown")
	} else {
		fmt.Println("Device address:", ip.String())
	}

	fmt.Println("Press Enter to exit")
	fmt.Scanln()

	err = stopSvc()
	if err != nil {
		log.Error().Msgf("error stopping service: %s", err)
		fmt.Println("Error stopping service:", err)
		os.Exit(1)
	}

	os.Exit(0)
}
