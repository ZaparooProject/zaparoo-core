//go:build linux

/*
Zaparoo Core
Copyright (C) 2023-2025 Callan Barrett

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

package helpers

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// MaxURLLength is the maximum allowed URL length for browser opening.
// This prevents resource exhaustion from malicious tokens with extremely long URLs.
const MaxURLLength = 8192

// ValidateBrowserURL checks if the URL has a valid scheme for browser opening.
// Only http:// and https:// URLs are accepted for security.
func ValidateBrowserURL(url string) error {
	if len(url) > MaxURLLength {
		return fmt.Errorf("URL too long: %d bytes (max %d)", len(url), MaxURLLength)
	}
	lower := strings.ToLower(url)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return errors.New("invalid URL scheme: must be http:// or https://")
	}
	return nil
}

// OpenBrowser opens the given URL in the default web browser using xdg-open.
// This is a fire-and-forget operation - the browser process is started but
// not waited on. Only http:// and https:// URLs are accepted for security.
func OpenBrowser(url string) error {
	if err := ValidateBrowserURL(url); err != nil {
		return err
	}
	cmd := exec.CommandContext(context.Background(), "xdg-open", url)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to open browser: %w", err)
	}
	return nil
}
