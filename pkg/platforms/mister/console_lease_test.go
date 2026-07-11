//go:build linux

package mister

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMainConsoleLeaseController_Available(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "console-state")
	require.NoError(t, os.WriteFile(statePath, []byte(fmt.Sprintf("1 %d ready -\n", os.Getpid())), 0o600))

	controller := &mainConsoleLeaseController{statePath: statePath}
	assert.True(t, controller.Available())
}

func TestMainConsoleLeaseController_AvailableRejectsStalePID(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "console-state")
	require.NoError(t, os.WriteFile(statePath, []byte("1 2147483647 ready -\n"), 0o600))

	controller := &mainConsoleLeaseController{statePath: statePath}
	assert.False(t, controller.Available())
}

func TestMainConsoleLeaseController_WaitForState(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "console-state")
	controller := &mainConsoleLeaseController{
		statePath:    statePath,
		pollInterval: time.Millisecond,
	}
	require.NoError(t, os.WriteFile(statePath, []byte(fmt.Sprintf("1 %d ready -\n", os.Getpid())), 0o600))

	go func() {
		time.Sleep(5 * time.Millisecond)
		_ = os.WriteFile(statePath, []byte(fmt.Sprintf("1 %d acquired test-nonce\n", os.Getpid())), 0o600)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, controller.waitForState(ctx, "acquired", "test-nonce"))
}

func TestMainConsoleLeaseController_WaitForFailure(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "console-state")
	require.NoError(t, os.WriteFile(statePath, []byte(fmt.Sprintf("1 %d busy test-nonce\n", os.Getpid())), 0o600))
	controller := &mainConsoleLeaseController{
		statePath:    statePath,
		pollInterval: time.Millisecond,
	}

	err := controller.waitForState(context.Background(), "acquired", "test-nonce")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "busy")
}
