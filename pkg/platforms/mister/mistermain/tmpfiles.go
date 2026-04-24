//go:build linux

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

package mistermain

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

// ReadCoreName reads the active core name from MiSTer's temp file.
func ReadCoreName() (string, error) {
	data, err := os.ReadFile(config.CoreNameFile)
	if err != nil {
		return "", fmt.Errorf("read core name file: %w", err)
	}

	name := strings.TrimSpace(string(data))
	if name == "" {
		return "", errors.New("core name file is empty")
	}

	return name, nil
}

// GetActiveCoreName returns the active core name, or empty string on error.
func GetActiveCoreName() string {
	name, err := ReadCoreName()
	if err != nil {
		log.Error().Err(err).Msg("error trying to get the core name")
		return ""
	}

	return name
}
