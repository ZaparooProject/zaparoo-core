//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinuxDefaults(t *testing.T) {
	t.Parallel()

	defaults := linuxDefaults()
	require.NotNil(t, defaults.Service.Encryption)
	assert.True(t, *defaults.Service.Encryption)
	assert.Nil(t, config.BaseDefaults.Service.Encryption, "must not mutate shared platform defaults")
}
