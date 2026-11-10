//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package lakka

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testUnit = "WorkingDirectory=@ZAPAROO_DIR@\nExecStart=@ZAPAROO_BIN@ -daemon\n"

func newExec(t *testing.T) *mocks.MockCommandExecutor {
	t.Helper()
	exec := &mocks.MockCommandExecutor{}
	t.Cleanup(func() { exec.AssertExpectations(t) })
	return exec
}

func TestRenderUnit(t *testing.T) {
	t.Parallel()
	unit, err := renderUnit(testUnit, "/storage/zaparoo/zaparoo")
	require.NoError(t, err)
	assert.Equal(t, "WorkingDirectory=/storage/zaparoo\nExecStart=/storage/zaparoo/zaparoo -daemon\n", unit)

	for _, bad := range []string{
		"zaparoo",
		"/storage/my zaparoo/zaparoo",
		"/storage/zap%h/zaparoo",
		"/storage/zap$HOME/zaparoo",
		"/storage/zap\"/zaparoo",
		"/storage/zap\n/zaparoo",
	} {
		_, renderErr := renderUnit(testUnit, bad)
		require.Error(t, renderErr, bad)
	}
	_, err = renderUnit("ExecStart=/bin/true\n", "/storage/zaparoo/zaparoo")
	require.Error(t, err)
}

func TestSetConfigValues(t *testing.T) {
	t.Parallel()
	in := "video_driver = \"gl\"\nnetwork_cmd_enable = \"false\"\n# network_cmd_port = \"1\"\n" +
		"network_cmd_enable=\"false\"\nmenu_driver = \"ozone\""
	out := setConfigValues(in, map[string]string{networkCmdEnableKey: "true", networkCmdPortKey: networkCmdPort})
	assert.Equal(t, "video_driver = \"gl\"\nnetwork_cmd_enable = \"true\"\n# network_cmd_port = \"1\"\n"+
		"network_cmd_enable = \"true\"\nmenu_driver = \"ozone\"\nnetwork_cmd_port = \"55355\"\n", out)
	assert.True(t, NetworkCommandsEnabled(out))
}

func TestNetworkCommandsEnabled(t *testing.T) {
	t.Parallel()
	assert.True(t, NetworkCommandsEnabled("network_cmd_enable = \"true\"\n"))
	assert.True(t, NetworkCommandsEnabled("network_cmd_enable = \"true\"\nnetwork_cmd_port = \"55355\"\n"))
	assert.False(t, NetworkCommandsEnabled("network_cmd_enable = \"true\"\nnetwork_cmd_port = \"5000\"\n"))
	assert.False(t, NetworkCommandsEnabled("network_cmd_enable = \"true\"\nnetwork_cmd_enable = \"false\"\n"))
	assert.False(t, NetworkCommandsEnabled(""))
}

func TestEnableNetworkCommandsEditsWhileRetroArchIsStopped(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte("network_cmd_enable = \"false\"\n"), 0o644))
	exec := newExec(t)
	expectSystemctl(exec, nil, "stop", retroArchService).Run(func(mock.Arguments) {
		// RetroArch saves its configuration as it exits.
		_ = afero.WriteFile(fs, RetroArchConfigPath, []byte("network_cmd_enable = \"false\"\nsaved = \"1\"\n"), 0o644)
	})
	expectSystemctl(exec, nil, "start", retroArchService)

	require.NoError(t, EnableNetworkCommands(context.Background(), fs, exec))
	data, err := afero.ReadFile(fs, RetroArchConfigPath)
	require.NoError(t, err)
	assert.Equal(t, "network_cmd_enable = \"true\"\nsaved = \"1\"\nnetwork_cmd_port = \"55355\"\n", string(data))
}

func TestEnableNetworkCommandsAlreadyOn(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte("network_cmd_enable = \"true\"\n"), 0o644))
	require.NoError(t, EnableNetworkCommands(context.Background(), fs, newExec(t)))
}

func TestEnableNetworkCommandsStartsFromImageDefault(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, retroArchDefaultConfig,
		[]byte("network_cmd_enable = \"false\"\nnetwork_cmd_port = \"55355\"\n"), 0o644))
	exec := newExec(t)
	expectSystemctl(exec, nil, "stop", retroArchService)
	expectSystemctl(exec, nil, "start", retroArchService)

	require.NoError(t, EnableNetworkCommands(context.Background(), fs, exec))
	data, err := afero.ReadFile(fs, RetroArchConfigPath)
	require.NoError(t, err)
	assert.Equal(t, "network_cmd_enable = \"true\"\nnetwork_cmd_port = \"55355\"\n", string(data))
}

func TestEnableNetworkCommandsRestartsRetroArchAfterWriteFailure(t *testing.T) {
	t.Parallel()
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, RetroArchConfigPath, []byte("network_cmd_enable = \"false\"\n"), 0o644))
	exec := newExec(t)
	expectSystemctl(exec, nil, "stop", retroArchService)
	expectSystemctl(exec, nil, "start", retroArchService)

	require.Error(t, EnableNetworkCommands(context.Background(), afero.NewReadOnlyFs(base), exec))
}

func TestEnableNetworkCommandsStopFailureLeavesConfig(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte("network_cmd_enable = \"false\"\n"), 0o644))
	exec := newExec(t)
	expectSystemctl(exec, errors.New("boom"), "stop", retroArchService)

	require.Error(t, EnableNetworkCommands(context.Background(), fs, exec))
	data, err := afero.ReadFile(fs, RetroArchConfigPath)
	require.NoError(t, err)
	assert.Equal(t, "network_cmd_enable = \"false\"\n", string(data))
}

func TestInstallAndUninstall(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte("network_cmd_enable = \"true\"\n"), 0o644))
	exec := newExec(t)
	expectSystemctl(exec, nil, "daemon-reload")
	expectSystemctl(exec, nil, "enable", serviceName)
	expectSystemctl(exec, nil, "restart", serviceName)
	inst := &Installer{FS: fs, Exec: exec, Unit: testUnit, Executable: "/storage/zaparoo/zaparoo"}

	require.NoError(t, inst.Install(context.Background()))
	data, err := afero.ReadFile(fs, UnitPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "ExecStart=/storage/zaparoo/zaparoo -daemon")

	expectSystemctl(exec, nil, "stop", serviceName)
	expectSystemctl(exec, nil, "disable", serviceName)
	expectSystemctl(exec, nil, "daemon-reload")
	require.NoError(t, inst.Uninstall(context.Background()))
	exists, err := afero.Exists(fs, UnitPath)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestInstallRefusesUnsafeExecutable(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	inst := &Installer{FS: fs, Exec: newExec(t), Unit: testUnit, Executable: "/storage/my games/zaparoo"}
	require.Error(t, inst.Install(context.Background()))
	exists, err := afero.Exists(fs, UnitPath)
	require.NoError(t, err)
	assert.False(t, exists)
}
