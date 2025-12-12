// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package advargs

import (
	"testing"

	advargtypes "github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/advargs/types"
	"github.com/stretchr/testify/assert"
)

func TestIsActionDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action string
		want   bool
	}{
		{name: "exact match lowercase", action: "details", want: true},
		{name: "exact match uppercase", action: "DETAILS", want: true},
		{name: "exact match mixed case", action: "Details", want: true},
		{name: "run action", action: "run", want: false},
		{name: "empty string", action: "", want: false},
		{name: "similar but wrong", action: "detail", want: false},
		{name: "whitespace", action: " details ", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsActionDetails(tt.action)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsActionRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action string
		want   bool
	}{
		{name: "exact match lowercase", action: "run", want: true},
		{name: "exact match uppercase", action: "RUN", want: true},
		{name: "exact match mixed case", action: "Run", want: true},
		{name: "empty string is run", action: "", want: true},
		{name: "details action", action: "details", want: false},
		{name: "similar but wrong", action: "running", want: false},
		{name: "whitespace", action: " run ", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsActionRun(tt.action)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsModeShuffle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode string
		want bool
	}{
		{name: "exact match lowercase", mode: "shuffle", want: true},
		{name: "exact match uppercase", mode: "SHUFFLE", want: true},
		{name: "exact match mixed case", mode: "Shuffle", want: true},
		{name: "empty string", mode: "", want: false},
		{name: "similar but wrong", mode: "shuffled", want: false},
		{name: "whitespace", mode: " shuffle ", want: false},
		{name: "sequential mode", mode: "sequential", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsModeShuffle(tt.mode)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestShouldRun_EmptyWhen verifies that empty When condition means command should run.
func TestShouldRun_EmptyWhen(t *testing.T) {
	t.Parallel()

	args := advargtypes.GlobalArgs{When: ""}
	assert.True(t, ShouldRun(args))
}
