//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package lakka

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testUnit = "WorkingDirectory=@ZAPAROO_DIR@\n@ZAPAROO_PORT_LOCK@\nExecStart=@ZAPAROO_BIN@ -daemon\n"

func newExec(t *testing.T) *mocks.MockCommandExecutor {
	t.Helper()
	exec := &mocks.MockCommandExecutor{}
	t.Cleanup(func() { exec.AssertExpectations(t) })
	return exec
}

func TestRenderUnit(t *testing.T) {
	t.Parallel()
	unit, err := renderUnit(testUnit, "/storage/zaparoo/zaparoo", false)
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
		_, renderErr := renderUnit(testUnit, bad, false)
		require.Error(t, renderErr, bad)
	}
	_, err = renderUnit("ExecStart=/bin/true\n", "/storage/zaparoo/zaparoo", false)
	require.Error(t, err)
}

func TestRenderUnitPortLock(t *testing.T) {
	t.Parallel()
	const exe = "/storage/zaparoo/zaparoo"
	rule := "INPUT -p udp --dport 55355 ! -i lo -j DROP"

	open, err := renderUnit(testUnit, exe, false)
	require.NoError(t, err)
	assert.Equal(t, "WorkingDirectory=/storage/zaparoo\nExecStart=/storage/zaparoo/zaparoo -daemon\n", open)

	locked, err := renderUnit(testUnit, exe, true)
	require.NoError(t, err)
	assert.NotContains(t, locked, "@ZAPAROO_")
	assert.Contains(t, locked, "ExecStartPre=-/bin/sh -c 'iptables -w -C "+rule+
		" 2>/dev/null || iptables -w -I "+rule+"'\nExecStart=/storage/zaparoo/zaparoo -daemon\n")

	_, err = renderUnit("ExecStart=@ZAPAROO_BIN@\n", exe, true)
	require.ErrorContains(t, err, "port lock placeholder")
}

func expectIptables(exec *mocks.MockCommandExecutor, err error, action string) *mock.Call {
	args := append([]string{"-w", action}, strings.Fields(portLockRule)...)
	return exec.On("Run", mock.Anything, "iptables", args).Return(err).Once()
}

const (
	commandsOff = "video_driver = \"gl\"\nnetwork_cmd_enable = \"false\"\n"
	commandsOn  = "video_driver = \"gl\"\nnetwork_cmd_enable = \"true\"\n"
)

func expectInstall(exec *mocks.MockCommandExecutor, editsConfig bool) {
	expectSystemctl(exec, nil, "daemon-reload")
	expectSystemctl(exec, nil, "enable", serviceName)
	if editsConfig {
		expectSystemctl(exec, nil, "stop", retroArchService)
		expectSystemctl(exec, nil, "start", retroArchService)
	}
	expectSystemctl(exec, nil, "restart", serviceName)
}

func readFile(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return string(data)
}

// Core is the one opening the port when network commands were off, so it
// keeps them to this device, and takes both back out when it is uninstalled.
func TestInstallLocksThePortItOpens(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte(commandsOff), 0o644))
	exec := newExec(t)
	expectIptables(exec, errors.New("no such rule"), "-C")
	expectIptables(exec, nil, "-I")
	expectInstall(exec, true)
	inst := &Installer{FS: fs, Exec: exec, Unit: testUnit, Executable: "/storage/zaparoo/zaparoo"}

	require.NoError(t, inst.Install(context.Background()))
	assert.Contains(t, readFile(t, fs, UnitPath), "iptables -w -I "+portLockRule)
	assert.True(t, NetworkCommandsEnabled(readFile(t, fs, RetroArchConfigPath)))

	// Network commands are on by now, and only the unit says who did that.
	// The rule is already there, so it is not added a second time.
	expectIptables(exec, nil, "-C")
	expectInstall(exec, false)
	require.NoError(t, inst.Install(context.Background()))
	assert.Contains(t, readFile(t, fs, UnitPath), "iptables -w -I "+portLockRule, "a reinstall keeps the lock")

	expectSystemctl(exec, nil, "stop", retroArchService)
	expectSystemctl(exec, nil, "start", retroArchService)
	expectSystemctl(exec, nil, "stop", serviceName)
	expectSystemctl(exec, nil, "disable", serviceName)
	expectIptables(exec, nil, "-D")
	expectSystemctl(exec, nil, "daemon-reload")
	require.NoError(t, inst.Uninstall(context.Background()))
	assert.Equal(t, commandsOff+"network_cmd_port = \"55355\"\n", readFile(t, fs, RetroArchConfigPath))
	exists, err := afero.Exists(fs, UnitPath)
	require.NoError(t, err)
	assert.False(t, exists)
}

// A player who had network commands on may be using them from another device,
// so their port is left open and their configuration alone.
func TestInstallLeavesAPortThePlayerOpened(t *testing.T) {
	t.Parallel()
	for _, cfg := range []string{commandsOn, commandsOn + "network_cmd_port = \"1234\"\n"} {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte(cfg), 0o644))
		exec := newExec(t)
		expectInstall(exec, !NetworkCommandsEnabled(cfg))
		inst := &Installer{FS: fs, Exec: exec, Unit: testUnit, Executable: "/storage/zaparoo/zaparoo"}

		require.NoError(t, inst.Install(context.Background()))
		assert.NotContains(t, readFile(t, fs, UnitPath), "iptables")

		expectSystemctl(exec, nil, "stop", serviceName)
		expectSystemctl(exec, nil, "disable", serviceName)
		expectSystemctl(exec, nil, "daemon-reload")
		require.NoError(t, inst.Uninstall(context.Background()))
		assert.Contains(t, readFile(t, fs, RetroArchConfigPath), "network_cmd_enable = \"true\"")
	}
}

// Without the lock the port would be open to the network, so nothing is
// installed and network commands stay off.
func TestInstallRefusesWhenThePortCannotBeLocked(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte(commandsOff), 0o644))
	exec := newExec(t)
	expectIptables(exec, errors.New("no such rule"), "-C")
	expectIptables(exec, errors.New("iptables: not found"), "-I")
	inst := &Installer{FS: fs, Exec: exec, Unit: testUnit, Executable: "/storage/zaparoo/zaparoo"}

	require.ErrorContains(t, inst.Install(context.Background()), "keep retroarch network commands to this device")
	assert.Equal(t, commandsOff, readFile(t, fs, RetroArchConfigPath))
	exists, err := afero.Exists(fs, UnitPath)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestUninstallKeepsTheLockWhenCommandsCannotBeTurnedOff(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte(commandsOn), 0o644))
	unit, err := renderUnit(testUnit, "/storage/zaparoo/zaparoo", true)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, UnitPath, []byte(unit), 0o644))
	exec := newExec(t)
	expectSystemctl(exec, errors.New("boom"), "stop", retroArchService)
	inst := &Installer{FS: fs, Exec: exec, Unit: testUnit, Executable: "/storage/zaparoo/zaparoo"}

	require.ErrorContains(t, inst.Uninstall(context.Background()), "stop retroarch")
	assert.Equal(t, unit, readFile(t, fs, UnitPath))
}

func TestUninstallWithoutARetroArchConfig(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	unit, err := renderUnit(testUnit, "/storage/zaparoo/zaparoo", true)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, UnitPath, []byte(unit), 0o644))
	exec := newExec(t)
	expectSystemctl(exec, nil, "stop", serviceName)
	expectSystemctl(exec, nil, "disable", serviceName)
	expectIptables(exec, errors.New("no such rule"), "-D")
	expectSystemctl(exec, nil, "daemon-reload")
	inst := &Installer{FS: fs, Exec: exec, Unit: testUnit, Executable: "/storage/zaparoo/zaparoo"}

	require.NoError(t, inst.Uninstall(context.Background()))
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
	// RetroArch keeps the first of duplicate keys.
	assert.False(t, NetworkCommandsEnabled("network_cmd_enable = \"false\"\nnetwork_cmd_enable = \"true\"\n"))
	assert.True(t, NetworkCommandsEnabled("network_cmd_enable = \"true\"\nnetwork_cmd_enable = \"false\"\n"))
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
