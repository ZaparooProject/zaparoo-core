//go:build linux && !android

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
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/rs/zerolog/log"
)

const (
	logindService = "org.freedesktop.login1"
	logindPath    = "/org/freedesktop/login1"
	logindManager = "org.freedesktop.login1.Manager"
)

// logindMethods are the login manager calls behind each action. Each has a
// matching "Can" query answering yes, no, challenge or na.
var logindMethods = map[PowerAction]string{
	PowerReboot:   "Reboot",
	PowerShutdown: "PowerOff",
	PowerSuspend:  "Suspend",
}

// LinuxPowerControl asks the login manager to change the machine's power
// state, which is what lets an unprivileged service do it where the
// distribution's policy allows. Systems without a login manager fall back to
// running the commands directly.
type LinuxPowerControl struct {
	bus      busCaller
	Fallback PowerController
}

// NewLinuxPowerControl returns a controller that runs reboot and poweroff as
// the fallback. lookPath finds a program, and euid is the effective user ID.
func NewLinuxPowerControl(
	executor command.Executor,
	lookPath func(string) (string, error),
	euid func() int,
) *LinuxPowerControl {
	commands := map[PowerAction][]string{}
	for action, program := range map[PowerAction]string{PowerReboot: "reboot", PowerShutdown: "poweroff"} {
		if path, err := lookPath(program); err == nil {
			commands[action] = []string{path}
		}
	}
	return &LinuxPowerControl{
		bus: newSystemBus(),
		Fallback: &ExecPowerControl{
			Executor:  executor,
			Commands:  commands,
			Permitted: func() bool { return euid() == 0 },
		},
	}
}

// logindAnswer asks the login manager whether an action is possible. The
// second result is false when there is no login manager to ask.
func (c *LinuxPowerControl) logindAnswer(ctx context.Context, action PowerAction) (Availability, bool) {
	if c.bus == nil {
		return "", false
	}
	body, err := c.bus.call(ctx, logindService, logindPath, logindManager+".Can"+logindMethods[action])
	if err != nil {
		if !errors.Is(err, errNoSystemBus) && !errors.Is(err, errNoService) {
			log.Debug().Err(err).Str("action", string(action)).Msg("could not query login manager")
		}
		return "", false
	}
	answer, _ := firstString(body)
	switch answer {
	case "yes":
		return Supported, true
	case "no", "challenge":
		// "challenge" wants a person to authenticate, which a background
		// service cannot do.
		return NotPermitted, true
	default:
		return Unsupported, true
	}
}

func firstString(body []any) (string, bool) {
	if len(body) == 0 {
		return "", false
	}
	value, ok := body[0].(string)
	return value, ok
}

func (c *LinuxPowerControl) PowerActions(ctx context.Context) map[PowerAction]Availability {
	fallback := c.Fallback.PowerActions(ctx)
	actions := make(map[PowerAction]Availability, len(PowerActions))
	for _, action := range PowerActions {
		if answer, ok := c.logindAnswer(ctx, action); ok {
			if answer != Unsupported {
				actions[action] = answer
			}
			continue
		}
		if answer, ok := fallback[action]; ok {
			actions[action] = answer
		}
	}
	return actions
}

func (c *LinuxPowerControl) PreparePowerAction(ctx context.Context, action PowerAction) (func() error, error) {
	method, known := logindMethods[action]
	if !known {
		return nil, ErrUnsupported
	}
	answer, ok := c.logindAnswer(ctx, action)
	if !ok {
		//nolint:wrapcheck // the sentinel errors are matched by the caller
		return c.Fallback.PreparePowerAction(ctx, action)
	}
	switch answer {
	case Supported:
	case NotPermitted:
		return nil, ErrNotPermitted
	default:
		return nil, ErrUnsupported
	}
	return func() error {
		// The argument is "interactive": false means do not prompt anyone.
		_, err := c.bus.call(context.Background(), logindService, logindPath, logindManager+"."+method, false)
		if err != nil {
			return fmt.Errorf("asking login manager to %s: %w", action, err)
		}
		return nil
	}, nil
}
