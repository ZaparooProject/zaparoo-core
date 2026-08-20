//go:build !linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"

func payloadFiles(string) []updatepayload.File {
	return nil
}
