//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

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

package linuxbase

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLauncherManager implements platforms.LauncherContextManager for testing.
type mockLauncherManager struct {
	newContextCalled bool
}

func (m *mockLauncherManager) NewContext() context.Context {
	m.newContextCalled = true
	return context.Background()
}

func (*mockLauncherManager) GetContext() context.Context {
	return context.Background()
}

func TestNewBase(t *testing.T) {
	t.Parallel()

	base := NewBase("test-platform")

	assert.Equal(t, "test-platform", base.ID())
	assert.Nil(t, base.trackedProcess)
	assert.Nil(t, base.launcherManager)
}

func TestSetTrackedProcess(t *testing.T) {
	t.Parallel()

	t.Run("sets_process_when_none_exists", func(t *testing.T) {
		t.Parallel()

		base := NewBase("test")

		// Start a simple process
		cmd := exec.CommandContext(context.Background(), "sleep", "10")
		require.NoError(t, cmd.Start())
		defer func() { _ = cmd.Process.Kill() }()

		base.SetTrackedProcess(cmd.Process)

		assert.NotNil(t, base.trackedProcess)
		assert.Equal(t, cmd.Process.Pid, base.trackedProcess.Pid)

		// Cleanup
		_ = cmd.Process.Kill()
	})

	t.Run("kills_previous_process_when_setting_new", func(t *testing.T) {
		t.Parallel()

		base := NewBase("test")

		// Start first process
		cmd1 := exec.CommandContext(context.Background(), "sleep", "10")
		require.NoError(t, cmd1.Start())

		base.SetTrackedProcess(cmd1.Process)
		assert.Equal(t, cmd1.Process.Pid, base.trackedProcess.Pid)

		// Start second process
		cmd2 := exec.CommandContext(context.Background(), "sleep", "10")
		require.NoError(t, cmd2.Start())
		defer func() { _ = cmd2.Process.Kill() }()

		// Set second process - should kill first
		base.SetTrackedProcess(cmd2.Process)

		assert.Equal(t, cmd2.Process.Pid, base.trackedProcess.Pid)

		// Verify first process was killed (wait should return quickly)
		_, err := cmd1.Process.Wait()
		require.NoError(t, err) // Process should be dead

		// Cleanup
		_ = cmd2.Process.Kill()
	})

	t.Run("handles_nil_process", func(t *testing.T) {
		t.Parallel()

		base := NewBase("test")

		// Should not panic
		base.SetTrackedProcess(nil)

		assert.Nil(t, base.trackedProcess)
	})
}

func TestStopActiveLauncher(t *testing.T) {
	t.Parallel()

	t.Run("no_tracked_process", func(t *testing.T) {
		t.Parallel()

		activeMedia := &models.ActiveMedia{Name: "test"}
		base := NewBase("test")
		base.setActiveMedia = func(m *models.ActiveMedia) {
			activeMedia = m
		}

		err := base.StopActiveLauncher(platforms.StopForPreemption)

		require.NoError(t, err)
		assert.Nil(t, activeMedia)
	})

	t.Run("invalidates_launcher_context", func(t *testing.T) {
		t.Parallel()

		mgr := &mockLauncherManager{}
		base := NewBase("test")
		base.launcherManager = mgr
		base.setActiveMedia = func(_ *models.ActiveMedia) {}

		err := base.StopActiveLauncher(platforms.StopForPreemption)

		require.NoError(t, err)
		assert.True(t, mgr.newContextCalled)
	})

	t.Run("graceful_sigterm_exit", func(t *testing.T) {
		t.Parallel()

		// Start a process that exits quickly on SIGTERM
		cmd := exec.CommandContext(context.Background(), "sleep", "10")
		require.NoError(t, cmd.Start())

		activeMedia := &models.ActiveMedia{Name: "test"}
		base := NewBase("test")
		base.trackedProcess = cmd.Process
		base.setActiveMedia = func(m *models.ActiveMedia) {
			activeMedia = m
		}

		start := time.Now()
		err := base.StopActiveLauncher(platforms.StopForPreemption)
		elapsed := time.Since(start)

		require.NoError(t, err)
		assert.Nil(t, base.trackedProcess)
		assert.Nil(t, activeMedia)
		// Should complete quickly (SIGTERM kills sleep immediately)
		assert.Less(t, elapsed, 2*time.Second)
	})

	t.Run("sigkill_after_timeout", func(t *testing.T) {
		t.Parallel()

		// Start a process that ignores SIGTERM
		cmd := exec.CommandContext(context.Background(), "bash", "-c", "trap '' TERM; while true; do sleep 1; done")
		require.NoError(t, cmd.Start())

		// Give the process time to set up the signal trap
		time.Sleep(100 * time.Millisecond)

		activeMedia := &models.ActiveMedia{Name: "test"}
		fakeClock := clockwork.NewFakeClock()
		base := NewBase("test")
		base.SetClock(fakeClock)
		base.trackedProcess = cmd.Process
		base.setActiveMedia = func(m *models.ActiveMedia) {
			activeMedia = m
		}

		// Run StopActiveLauncher in a goroutine since it will block on the fake clock
		ctx := context.Background()
		var wg sync.WaitGroup
		var err error
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = base.StopActiveLauncher(platforms.StopForPreemption)
		}()

		// Wait for the goroutine to block on the SIGTERM timeout
		require.NoError(t, fakeClock.BlockUntilContext(ctx, 1))
		// Advance past SIGTERM timeout to trigger SIGKILL
		fakeClock.Advance(SIGTERMTimeout)

		// Wait for the goroutine to block on the SIGKILL cleanup timeout
		require.NoError(t, fakeClock.BlockUntilContext(ctx, 1))
		// Advance past SIGKILL cleanup timeout
		fakeClock.Advance(SIGKILLTimeout)

		wg.Wait()

		require.NoError(t, err)
		assert.Nil(t, base.trackedProcess)
		assert.Nil(t, activeMedia)
	})

	t.Run("already_dead_process", func(t *testing.T) {
		t.Parallel()

		// Start and immediately kill a process
		cmd := exec.CommandContext(context.Background(), "sleep", "10")
		require.NoError(t, cmd.Start())
		require.NoError(t, cmd.Process.Kill())
		_, _ = cmd.Process.Wait()

		activeMedia := &models.ActiveMedia{Name: "test"}
		base := NewBase("test")
		base.trackedProcess = cmd.Process
		base.setActiveMedia = func(m *models.ActiveMedia) {
			activeMedia = m
		}

		// Should handle gracefully (SIGTERM will fail on dead process)
		err := base.StopActiveLauncher(platforms.StopForPreemption)

		require.NoError(t, err)
		assert.Nil(t, base.trackedProcess)
		assert.Nil(t, activeMedia)
	})
}

func TestStartPost(t *testing.T) {
	t.Parallel()

	base := NewBase("test")
	mgr := &mockLauncherManager{}
	activeMedia := &models.ActiveMedia{Name: "test"}
	var setMedia *models.ActiveMedia

	err := base.StartPost(
		nil,
		mgr,
		func() *models.ActiveMedia { return activeMedia },
		func(m *models.ActiveMedia) { setMedia = m },
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, mgr, base.launcherManager)
	assert.NotNil(t, base.activeMedia)
	assert.NotNil(t, base.setActiveMedia)

	// Test that callbacks work
	assert.Equal(t, activeMedia, base.activeMedia())
	base.setActiveMedia(&models.ActiveMedia{Name: "new"})
	assert.Equal(t, "new", setMedia.Name)
}

func TestNoOpMethods(t *testing.T) {
	t.Parallel()

	base := NewBase("test")

	// Test all no-op methods don't panic and return expected values
	assert.NoError(t, base.StartPre(nil))
	assert.NoError(t, base.Stop())
	assert.NoError(t, base.ScanHook(nil))
	assert.NoError(t, base.ReturnToMenu())
	assert.NoError(t, base.KeyboardPress("a"))
	assert.NoError(t, base.GamepadPress("a"))

	result, err := base.ForwardCmd(nil)
	require.NoError(t, err)
	assert.Empty(t, result)

	path, found := base.LookupMapping(nil)
	assert.Empty(t, path)
	assert.False(t, found)

	closeFunc, duration, err := base.ShowNotice(nil, widgetmodels.NoticeArgs{})
	assert.Nil(t, closeFunc)
	assert.Zero(t, duration)
	require.ErrorIs(t, err, platforms.ErrNotSupported)

	loaderClose, err := base.ShowLoader(nil, widgetmodels.NoticeArgs{})
	assert.Nil(t, loaderClose)
	require.ErrorIs(t, err, platforms.ErrNotSupported)

	require.ErrorIs(t, base.ShowPicker(nil, widgetmodels.PickerArgs{}), platforms.ErrNotSupported)

	assert.IsType(t, platforms.NoOpConsoleManager{}, base.ConsoleManager())
}

func TestLaunchSystem(t *testing.T) {
	t.Parallel()

	base := NewBase("test")

	err := base.LaunchSystem(nil, "some-system")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestConcurrentProcessAccess(t *testing.T) {
	t.Parallel()

	base := NewBase("test")
	base.setActiveMedia = func(_ *models.ActiveMedia) {}

	// Run concurrent operations to verify mutex works
	done := make(chan struct{})

	// Goroutine 1: Set processes
	go func() {
		for range 10 {
			cmd := exec.CommandContext(context.Background(), "sleep", "10")
			if err := cmd.Start(); err == nil {
				base.SetTrackedProcess(cmd.Process)
			}
		}
		done <- struct{}{}
	}()

	// Goroutine 2: Stop launcher
	go func() {
		for range 10 {
			_ = base.StopActiveLauncher(platforms.StopForPreemption)
		}
		done <- struct{}{}
	}()

	// Wait for both to complete without deadlock
	<-done
	<-done

	// Final cleanup
	_ = base.StopActiveLauncher(platforms.StopForPreemption)
}

// TestProcessSignalBehavior verifies that processes are properly terminated.
// Note: Testing the exact signal sequence (SIGTERM then SIGKILL) is difficult
// because signal handling is inherently racy. We verify the observable behavior:
// processes get killed, tracked process is cleared, active media is cleared.
func TestProcessSignalBehavior(t *testing.T) {
	t.Parallel()

	t.Run("process_terminated_correctly", func(t *testing.T) {
		t.Parallel()

		// Start a normal process
		cmd := exec.CommandContext(context.Background(), "sleep", "30")
		require.NoError(t, cmd.Start())
		pid := cmd.Process.Pid

		base := NewBase("test")
		base.trackedProcess = cmd.Process
		base.setActiveMedia = func(_ *models.ActiveMedia) {}

		err := base.StopActiveLauncher(platforms.StopForPreemption)
		require.NoError(t, err)
		assert.Nil(t, base.trackedProcess)

		// Verify process is actually dead by trying to signal it
		// Signal(0) is a no-op that checks if process exists
		proc, _ := os.FindProcess(pid)
		err = proc.Signal(syscall.Signal(0))
		assert.Error(t, err, "process should be terminated")
	})
}

// TestProcessKillRace tests that rapid kill/set doesn't cause issues.
func TestProcessKillRace(t *testing.T) {
	t.Parallel()

	base := NewBase("test")
	base.setActiveMedia = func(_ *models.ActiveMedia) {}

	for range 5 {
		cmd := exec.CommandContext(context.Background(), "sleep", "1")
		require.NoError(t, cmd.Start())

		base.SetTrackedProcess(cmd.Process)
		time.Sleep(10 * time.Millisecond)
		_ = base.StopActiveLauncher(platforms.StopForPreemption)
	}
}
