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

package hoststatus

import (
	"context"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
)

// powerCommandTimeout bounds how long a power command may take to hand the
// request to the operating system.
const powerCommandTimeout = 10 * time.Second

// ExecPowerControl carries out power actions by running a command, for
// systems where that is the interface the operating system offers.
type ExecPowerControl struct {
	Executor command.Executor
	// Commands maps each action the device supports to the program and
	// arguments that perform it.
	Commands map[PowerAction][]string
	// Permitted reports whether this process may run the commands. Nil means
	// it always may.
	Permitted func() bool
}

func (c *ExecPowerControl) availability() Availability {
	if c.Permitted != nil && !c.Permitted() {
		return NotPermitted
	}
	return Supported
}

func (c *ExecPowerControl) PowerActions(context.Context) map[PowerAction]Availability {
	actions := make(map[PowerAction]Availability, len(c.Commands))
	for action, argv := range c.Commands {
		if len(argv) > 0 {
			actions[action] = c.availability()
		}
	}
	return actions
}

func (c *ExecPowerControl) PreparePowerAction(_ context.Context, action PowerAction) (func() error, error) {
	argv := c.Commands[action]
	if len(argv) == 0 {
		return nil, ErrUnsupported
	}
	if c.availability() == NotPermitted {
		return nil, ErrNotPermitted
	}
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), powerCommandTimeout)
		defer cancel()
		if err := c.Executor.Run(ctx, argv[0], argv[1:]...); err != nil {
			return fmt.Errorf("running %s: %w", argv[0], err)
		}
		return nil
	}, nil
}
