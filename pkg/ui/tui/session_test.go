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

package tui

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewSession_Defaults(t *testing.T) {
	t.Parallel()

	sess := NewSession()

	assert.NotNil(t, sess)
	assert.Empty(t, sess.GetWriteTagZapScript())
	assert.Empty(t, sess.GetSearchMediaName())
	assert.Empty(t, sess.GetSearchMediaSystem())
	assert.Equal(t, "All", sess.GetSearchMediaSystemName())

	row, col := sess.GetMainMenuFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 0, col)
}

func TestSession_WriteTagZapScript_GetSet(t *testing.T) {
	t.Parallel()

	sess := NewSession()

	sess.SetWriteTagZapScript("**launch.system:nes")
	assert.Equal(t, "**launch.system:nes", sess.GetWriteTagZapScript())

	sess.SetWriteTagZapScript("")
	assert.Empty(t, sess.GetWriteTagZapScript())
}

func TestSession_MainMenuFocus(t *testing.T) {
	t.Parallel()

	sess := NewSession()

	sess.SetMainMenuFocus(2, 3)
	row, col := sess.GetMainMenuFocus()
	assert.Equal(t, 2, row)
	assert.Equal(t, 3, col)

	sess.SetMainMenuFocus(0, 0)
	row, col = sess.GetMainMenuFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 0, col)
}

func TestSession_SearchMediaName(t *testing.T) {
	t.Parallel()

	sess := NewSession()

	sess.SetSearchMediaName("Super Mario")
	assert.Equal(t, "Super Mario", sess.GetSearchMediaName())

	sess.SetSearchMediaName("")
	assert.Empty(t, sess.GetSearchMediaName())
}

func TestSession_SearchMediaSystem(t *testing.T) {
	t.Parallel()

	sess := NewSession()

	sess.SetSearchMediaSystem("nes")
	assert.Equal(t, "nes", sess.GetSearchMediaSystem())

	sess.SetSearchMediaSystem("")
	assert.Empty(t, sess.GetSearchMediaSystem())
}

func TestSession_SearchMediaSystemName(t *testing.T) {
	t.Parallel()

	sess := NewSession()

	// Default is "All"
	assert.Equal(t, "All", sess.GetSearchMediaSystemName())

	sess.SetSearchMediaSystemName("Nintendo Entertainment System")
	assert.Equal(t, "Nintendo Entertainment System", sess.GetSearchMediaSystemName())
}

func TestSession_ClearSearchMedia(t *testing.T) {
	t.Parallel()

	sess := NewSession()

	// Set some values
	sess.SetSearchMediaName("Zelda")
	sess.SetSearchMediaSystem("snes")
	sess.SetSearchMediaSystemName("Super Nintendo")

	// Clear
	sess.ClearSearchMedia()

	// Verify defaults are restored
	assert.Empty(t, sess.GetSearchMediaName())
	assert.Empty(t, sess.GetSearchMediaSystem())
	assert.Equal(t, "All", sess.GetSearchMediaSystemName())
}

func TestDefaultSession_ReturnsSameInstance(t *testing.T) {
	t.Parallel()

	sess1 := DefaultSession()
	sess2 := DefaultSession()

	assert.Same(t, sess1, sess2)
}

func TestSession_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	sess := NewSession()
	const goroutines = 10
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Writers
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				sess.SetSearchMediaName("game")
				sess.SetSearchMediaSystem("system")
				sess.SetSearchMediaSystemName("System Name")
				sess.SetWriteTagZapScript("script")
				sess.SetMainMenuFocus(id, j)
			}
		}(i)
	}

	// Readers
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				_ = sess.GetSearchMediaName()
				_ = sess.GetSearchMediaSystem()
				_ = sess.GetSearchMediaSystemName()
				_ = sess.GetWriteTagZapScript()
				_, _ = sess.GetMainMenuFocus()
			}
		}()
	}

	wg.Wait()
}

func TestSession_ConcurrentClear(t *testing.T) {
	t.Parallel()

	sess := NewSession()
	const goroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Writers that set and clear
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				sess.SetSearchMediaName("game")
				sess.SetSearchMediaSystem("system")
				sess.ClearSearchMedia()
			}
		}()
	}

	// Readers
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				name := sess.GetSearchMediaName()
				// After clear, name should be "" or a set value, never garbage
				assert.True(t, name == "" || name == "game")
			}
		}()
	}

	wg.Wait()
}
