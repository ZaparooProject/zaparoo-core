/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

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

package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// serviceCondition is what this screen can truthfully say about the service.
//
// A PID file only answers whether a process exists, which is why this screen
// used to claim RUNNING through a multi-minute database migration with nothing
// behind it working, and NOT RUNNING with a fixed "may not have started" when
// the service had stopped for a specific, reportable reason.
type serviceCondition int

const (
	// serviceStopped means nothing is listening and no process was found.
	serviceStopped serviceCondition = iota
	// serviceStarting means the process is alive and still working through
	// startup. Nothing behind it answers yet.
	serviceStarting
	// serviceFailed means startup stopped on something a person has to
	// resolve. The process is alive only to report it.
	serviceFailed
	// serviceRunning means the service is fully up.
	serviceRunning
)

// logErrorLinesShown bounds how much of the log is scanned for the reason a
// start failed. Startup failures log close to the end of the file.
const logErrorLinesShown = 200

// resolveServiceCondition asks the health route what the service is doing and
// falls back to the process check when nothing answers.
func resolveServiceCondition(cfg *config.Instance, isRunning func() bool) serviceCondition {
	state, answered := client.ServiceState(cfg)
	if answered {
		switch state {
		case client.ServiceStateReady:
			return serviceRunning
		case client.ServiceStateStarting:
			return serviceStarting
		case client.ServiceStateFailed:
			return serviceFailed
		}
	}

	// Nothing answered. A process without a listener is one that either has
	// not bound yet or died after doing so.
	if isRunning() {
		return serviceStarting
	}
	return serviceStopped
}

// lastLoggedError returns the reason the service is not running, as the log
// records it.
//
// The log file is what support asks for and what the bug report template asks
// for, so the reason a start failed is already written there. Surfacing it here
// saves the user fetching the file to answer the first question they have.
//
// A failed start writes a record carrying the headline written for a person to
// read, and that is preferred over the most recent error. Core keeps logging
// after it enters the failed state — this TUI's own client dialling the
// websocket that is not serving, for one — and those later lines are symptoms
// that mask the cause. On a MiSTer held in the failed state by a schema-ahead
// user database, the block said "error disabling runZapScript" and nothing at
// all about the database.
func lastLoggedError(pl platforms.Platform) string {
	content, err := readLastLines(helpers.LogPath(pl), logErrorLinesShown)
	if err != nil {
		return ""
	}

	lines := strings.Split(content, "\n")
	mostRecent := ""
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		var entry struct {
			Level    string `json:"level"`
			Message  string `json:"message"`
			Error    string `json:"error"`
			Headline string `json:"headline"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Level != "error" && entry.Level != "fatal" {
			continue
		}

		// Only the failed-state record carries this field, so its presence is
		// what identifies the record rather than matching on the message.
		if entry.Headline != "" {
			return entry.Headline
		}
		if mostRecent == "" {
			mostRecent = entry.Message
			if entry.Error != "" {
				mostRecent = entry.Message + ": " + entry.Error
			}
		}
	}

	return mostRecent
}

// webUIAddress is the address a user can open to reach this device.
func webUIAddress(cfg *config.Instance) string {
	ip := helpers.GetLocalIP()
	if ip == "" {
		ip = "localhost"
	}
	return fmt.Sprintf("http://%s:%d/app/", ip, cfg.APIPort())
}

// serviceStatusText renders the status block for the main page.
func serviceStatusText(cfg *config.Instance, pl platforms.Platform, condition serviceCondition) string {
	t := CurrentTheme()

	switch condition {
	case serviceRunning:
		return "[" + t.SuccessColorName + "]* RUNNING[-]"
	case serviceStarting:
		return "[" + t.WarningColorName + "]~ STARTING[-]" +
			"\nZaparoo is still starting.\nThis can take a few minutes on a large library."
	case serviceFailed:
		text := "[" + t.ErrorColorName + "]x NOT WORKING[-]" +
			"\nZaparoo started but stopped on a problem\nthat needs to be fixed."
		if reason := lastLoggedError(pl); reason != "" {
			text += "\n\n" + reason
		}
		text += "\n\nOpen " + webUIAddress(cfg) + "\nor press Logs to send a report."
		return text
	case serviceStopped:
		text := "[" + t.ErrorColorName + "]x NOT RUNNING[-]" +
			"\nService may not have started."
		if reason := lastLoggedError(pl); reason != "" {
			text += "\n\n" + reason
		}
		// Logs is the one button that still works here, and it is where the
		// answer is, so name it rather than leaving the user to go looking.
		text += "\n\nPress Logs to send a report."
		return text
	}

	return "[" + t.ErrorColorName + "]x NOT RUNNING[-]"
}
