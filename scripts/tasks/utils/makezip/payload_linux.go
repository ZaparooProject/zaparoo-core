//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/batocera"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
)

func payloadFiles(platform string) []updatepayload.File {
	if platform != "batocera" {
		return nil
	}
	return batocera.UpdatePayload()
}
