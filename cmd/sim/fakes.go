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

package main

import (
	"context"
	"io"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/libretro"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/logupload"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/online"
	rommfake "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/romm"
)

type fakeCLI func(ctx context.Context, args []string, call fakectl.Caller, stdout io.Writer) error

func fakeCLIs() map[string]fakeCLI {
	return map[string]fakeCLI{
		"libretro": libretro.CLI,
		"romm":     rommfake.CLI,
		"logs":     logupload.CLI,
		"online":   online.CLI,
	}
}
