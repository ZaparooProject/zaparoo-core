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

// Command bletest exercises Core's Bluetooth LE support against a real
// radio. It has two modes:
//
//	bletest client   connects to Core's GATT service the way the app will:
//	                 scan, read Info, pair, open an encrypted session, call
//	                 methods, receive notifications. Flags inject the faults
//	                 Core must survive.
//	bletest reader   pretends to be a Nordic UART simple serial reader for
//	                 the simpleserial_ble driver to connect to.
//
// It needs BlueZ and an adapter on the machine it runs on, which is not
// the machine running the Core under test. It imports no cgo package, so
// CGO_ENABLED=0 GOARCH=arm64 go build ./scripts/bletest cross-compiles it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/bluez"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05.000"})
	zerolog.TimeFieldFormat = time.RFC3339Nano
	if err := run(); err != nil {
		log.Fatal().Err(err).Msg("failed")
	}
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: bletest client|reader [flags]")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "client":
		return runClient(ctx, os.Args[2:])
	case "reader":
		return runReader(ctx, os.Args[2:])
	default:
		return fmt.Errorf("unknown mode %q; want client or reader", os.Args[1])
	}
}

// openAdapter opens the local adapter, powering it on: whoever runs this
// tool wants the radio used.
func openAdapter(ctx context.Context) (bluez.Adapter, error) {
	adapter, err := bluez.Open(ctx, bluez.WithPowerOn())
	if err != nil {
		return nil, err //nolint:wrapcheck // already names bluez
	}
	log.Info().Str("adapter", adapter.Address()).Msg("adapter open")
	return adapter, nil
}

// stringList is a flag that may be given more than once.
type stringList []string

func (*stringList) String() string { return "" }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

var _ flag.Value = (*stringList)(nil)
