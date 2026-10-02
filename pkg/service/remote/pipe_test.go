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

package remote

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
)

// interactivityPlatform is a platforms.InteractivityReader test double, the
// same embedding shape as other narrow platform-capability fakes in this
// codebase: only Interactive is ever consulted by pipeWanted.
type interactivityPlatform struct {
	*mocks.MockPlatform
	interactive bool
}

func (p *interactivityPlatform) Interactive() bool { return p.interactive }

// Neither feature wanting the pipe held is unaffected by whether the
// platform is interactive: there is nothing to pace either way.
func TestPipeWantedIsFalseWhenNoFeatureWantsIt(t *testing.T) {
	t.Parallel()
	cfg := &config.Instance{}
	m := &manager{deps: Deps{Config: cfg, Platform: &interactivityPlatform{
		MockPlatform: mocks.NewMockPlatform(), interactive: true,
	}}}
	assert.False(t, m.pipeWanted())
}

// A platform that cannot answer whether it is interactive (every platform
// but Android today) must not change pipeWanted's existing behavior.
func TestPipeWantedIgnoresInteractivityOnAPlatformThatCannotAnswer(t *testing.T) {
	t.Parallel()
	cfg := &config.Instance{}
	cfg.SetRemoteControl(true)
	m := &manager{deps: Deps{Config: cfg, Platform: mocks.NewMockPlatform()}}
	assert.True(t, m.pipeWanted())
}

// On a platform that can answer, the pipe is only held while it is
// interactive: a remote operation cannot dispatch, and a library change hint
// only ever speeds up a recheck that still happens on its own schedule,
// while asleep.
func TestPipeWantedFollowsInteractivityWhenAFeatureWantsIt(t *testing.T) {
	t.Parallel()
	for _, feature := range []func(*config.Instance){
		func(cfg *config.Instance) { cfg.SetRemoteControl(true) },
		func(cfg *config.Instance) { cfg.SetLibrarySync(true) },
	} {
		cfg := &config.Instance{}
		feature(cfg)
		asleep := &manager{deps: Deps{Config: cfg, Platform: &interactivityPlatform{
			MockPlatform: mocks.NewMockPlatform(), interactive: false,
		}}}
		assert.False(t, asleep.pipeWanted(), "asleep must not hold the pipe")

		awake := &manager{deps: Deps{Config: cfg, Platform: &interactivityPlatform{
			MockPlatform: mocks.NewMockPlatform(), interactive: true,
		}}}
		assert.True(t, awake.pipeWanted(), "awake with a feature on must hold the pipe")
	}
}
