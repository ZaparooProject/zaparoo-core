//go:build linux && !android

package retropie

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	_ platforms.TrackedProcessWaiter       = (*Platform)(nil)
	_ platforms.TrackedProcessMediaClearer = (*Platform)(nil)
)

type clearRecorder struct {
	mu    syncutil.Mutex
	count int
}

func (c *clearRecorder) set(m *models.ActiveMedia) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m == nil {
		c.count++
	}
}

func (c *clearRecorder) cleared() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

func startProc(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // fixed test commands
	require.NoError(t, cmd.Start())
	return cmd
}

func TestClearTrackedProcessMedia_ExitWhileTrackedClears(t *testing.T) {
	t.Parallel()
	rec := &clearRecorder{}
	p := &Platform{setActiveMedia: rec.set}
	cmd := startProc(t, "true")
	p.SetTrackedProcess(cmd.Process)

	require.NoError(t, p.WaitTrackedProcess(cmd.Process))
	assert.True(t, p.ClearTrackedProcessMedia(cmd.Process))
	assert.Equal(t, 1, rec.cleared())
	assert.False(t, p.ClearTrackedProcessMedia(cmd.Process), "clear is consumed once")
}

func TestClearTrackedProcessMedia_LateExitDoesNotClearNewerMedia(t *testing.T) {
	t.Parallel()
	rec := &clearRecorder{}
	p := &Platform{setActiveMedia: rec.set}
	old := startProc(t, "sleep", "10")
	p.SetTrackedProcess(old.Process)

	waitErr := make(chan error, 1)
	go func() { waitErr <- p.WaitTrackedProcess(old.Process) }()

	// A newer launch replaces the tracked process before the old one exits.
	newer := startProc(t, "sleep", "10")
	t.Cleanup(func() { _ = newer.Process.Kill(); _ = newer.Wait() })
	p.SetTrackedProcess(newer.Process)

	require.NoError(t, old.Process.Kill())
	<-waitErr

	assert.False(t, p.ClearTrackedProcessMedia(old.Process))
	assert.Zero(t, rec.cleared())
}

func TestClearTrackedProcessMedia_StoppedProcessDoesNotClearAgain(t *testing.T) {
	t.Parallel()
	rec := &clearRecorder{}
	p := &Platform{setActiveMedia: rec.set}
	cmd := startProc(t, "sleep", "10")
	p.SetTrackedProcess(cmd.Process)

	waitErr := make(chan error, 1)
	go func() { waitErr <- p.WaitTrackedProcess(cmd.Process) }()
	// Wait is claimed before Core stops the process.
	require.Eventually(t, func() bool {
		p.processMu.RLock()
		defer p.processMu.RUnlock()
		return p.waitClaimed
	}, 5*time.Second, time.Millisecond)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForPreemption))
	<-waitErr

	assert.False(t, p.ClearTrackedProcessMedia(cmd.Process))
	assert.Equal(t, 1, rec.cleared(), "only the stop path clears media")
}
