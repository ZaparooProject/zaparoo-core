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

package tokens

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionDeliversFirstResultOnly(t *testing.T) {
	t.Parallel()

	c := NewCompletion()
	first := errors.New("first")

	assert.True(t, c.Complete(first))
	assert.False(t, c.Complete(errors.New("second")))
	assert.False(t, c.Complete(nil))

	select {
	case err := <-c.Done():
		require.ErrorIs(t, err, first)
	default:
		t.Fatal("first result was not delivered")
	}

	select {
	case err := <-c.Done():
		t.Fatalf("second result was delivered: %v", err)
	default:
	}
}

func TestCompletionNilReceiverIsNoOp(t *testing.T) {
	t.Parallel()

	var c *Completion
	assert.False(t, c.Complete(errors.New("ignored")))
	assert.Nil(t, c.Done())

	// A nil Completion on a token must never block a select on it.
	select {
	case <-c.Done():
		t.Fatal("nil completion yielded a result")
	case <-time.After(10 * time.Millisecond):
	}
}

func TestCompletionNeverBlocksWithoutReader(t *testing.T) {
	t.Parallel()

	c := NewCompletion()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Complete(nil)
		c.Complete(errors.New("late"))
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Complete blocked with no reader")
	}
}

func TestCompletionExactlyOnceUnderContention(t *testing.T) {
	t.Parallel()

	c := NewCompletion()
	const workers = 32
	var wg sync.WaitGroup
	wins := make(chan int, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.Complete(errors.New("worker")) {
				wins <- i
			}
		}()
	}
	wg.Wait()
	close(wins)

	assert.Len(t, wins, 1)
	<-c.Done()
	select {
	case <-c.Done():
		t.Fatal("more than one result delivered")
	default:
	}
}
