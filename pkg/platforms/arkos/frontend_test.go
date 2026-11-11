//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	esStop  = "systemctl stop " + esService
	esStart = "systemctl start " + esService
)

func TestFrontend_GameExitRestoresES(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cmd := newMockCmd()
	fe := newFrontend(cmd, fs)

	fe.beginLaunch()
	require.NoError(t, fe.stopES())
	require.NoError(t, fe.stopES(), "a second stop is a no-op")
	fe.gameStarted(100)
	fe.endLaunch()

	exists, err := afero.Exists(fs, esStoppedMarker)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, []string{esStop}, cmdCalls(cmd), "ES stays stopped while the game runs")

	fe.gameExited(100)
	assert.Equal(t, []string{esStop, esStart}, cmdCalls(cmd))
	exists, err = afero.Exists(fs, esStoppedMarker)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestFrontend_LaunchFailureRestoresES(t *testing.T) {
	t.Parallel()

	cmd := newMockCmd()
	fe := newFrontend(cmd, afero.NewMemMapFs())

	fe.beginLaunch()
	require.NoError(t, fe.stopES())
	fe.launchFailed()
	assert.Equal(t, []string{esStop}, cmdCalls(cmd), "launch still in progress")
	fe.endLaunch()
	assert.Equal(t, []string{esStop, esStart}, cmdCalls(cmd))
}

func TestFrontend_PreemptionKeepsESStopped(t *testing.T) {
	t.Parallel()

	cmd := newMockCmd()
	fe := newFrontend(cmd, afero.NewMemMapFs())

	fe.beginLaunch()
	require.NoError(t, fe.stopES())
	fe.gameStarted(1)
	fe.endLaunch()

	// A second launch replaces the first game.
	fe.beginLaunch()
	fe.gameExited(1)
	require.NoError(t, fe.stopES())
	fe.gameStarted(2)
	fe.endLaunch()
	assert.Equal(t, []string{esStop}, cmdCalls(cmd))

	fe.gameExited(1)
	assert.Equal(t, []string{esStop}, cmdCalls(cmd), "a stale exit does not end the new game")
	fe.gameExited(2)
	assert.Equal(t, []string{esStop, esStart}, cmdCalls(cmd))
}

func TestFrontend_LaunchWithoutStopLeavesESAlone(t *testing.T) {
	t.Parallel()

	cmd := newMockCmd()
	fe := newFrontend(cmd, afero.NewMemMapFs())
	fe.beginLaunch()
	fe.endLaunch()
	fe.restore()
	assert.Empty(t, cmdCalls(cmd))
}

func TestFrontend_StopFailure(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Run", mock.Anything, "systemctl", []string{"stop", esService}).Return(errors.New("boom"))
	fe := newFrontend(cmd, fs)

	require.Error(t, fe.stopES())
	exists, err := afero.Exists(fs, esStoppedMarker)
	require.NoError(t, err)
	assert.False(t, exists)
	fe.launchFailed()
	cmd.AssertNumberOfCalls(t, "Run", 1)
}

func TestFrontend_RestoreOnCoreStop(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, perfnormPath, "")
	cmd := newMockCmd()
	fe := newFrontend(cmd, fs)

	fe.beginLaunch()
	require.NoError(t, fe.stopES())
	fe.gameStarted(5)
	fe.restore()
	assert.Equal(t, []string{esStop, perfnormPath, esStart}, cmdCalls(cmd))
	assert.Zero(t, fe.runningGame())
}

func TestFrontend_RestoreRetriesAfterStartFailure(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Run", mock.Anything, "systemctl", []string{"stop", esService}).Return(nil)
	cmd.On("Run", mock.Anything, "systemctl", []string{"start", esService}).Return(errors.New("boom")).Once()
	cmd.On("Run", mock.Anything, "systemctl", []string{"start", esService}).Return(nil).Once()
	fe := newFrontend(cmd, fs)

	require.NoError(t, fe.stopES())
	fe.launchFailed()
	exists, err := afero.Exists(fs, esStoppedMarker)
	require.NoError(t, err)
	assert.True(t, exists, "marker kept so ES is started later")

	fe.restore()
	exists, err = afero.Exists(fs, esStoppedMarker)
	require.NoError(t, err)
	assert.False(t, exists)
	cmd.AssertExpectations(t)
}

func TestFrontend_RecoverStale(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cmd := newMockCmd()
	fe := newFrontend(cmd, fs)
	fe.recoverStale()
	assert.Empty(t, cmdCalls(cmd))

	writeFile(t, fs, esStoppedMarker, "")
	fe.recoverStale()
	assert.Equal(t, []string{esStart}, cmdCalls(cmd))
	exists, err := afero.Exists(fs, esStoppedMarker)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestFrontend_MarkerFailuresDoNotBlockES(t *testing.T) {
	t.Parallel()

	ro := afero.NewReadOnlyFs(afero.NewMemMapFs())
	cmd := newMockCmd()
	fe := newFrontend(cmd, ro)

	require.NoError(t, fe.stopES(), "ES stops even when the marker cannot be written")
	fe.launchFailed()
	assert.Equal(t, []string{esStop, esStart}, cmdCalls(cmd))
	assert.False(t, fe.stopped)
}

func TestFrontend_PerfnormFailureStillStartsES(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, perfnormPath, "")
	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Run", mock.Anything, "systemctl", []string{"stop", esService}).Return(nil)
	cmd.On("Run", mock.Anything, perfnormPath, []string(nil)).Return(errors.New("boom"))
	cmd.On("Run", mock.Anything, "systemctl", []string{"start", esService}).Return(nil)
	fe := newFrontend(cmd, fs)

	require.NoError(t, fe.stopES())
	fe.restore()
	cmd.AssertExpectations(t)
	assert.False(t, fe.stopped)
}
