//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package esbase

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSystemdUnitInstaller_InstallWritesAndEnables(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	var calls [][]string
	cmd.On("Run", mock.Anything, "systemctl", mock.Anything).Run(func(args mock.Arguments) {
		argv, ok := args.Get(2).([]string)
		require.True(t, ok)
		calls = append(calls, argv)
	}).Return(nil)

	s := SystemdUnitInstaller("/storage/.config/system.d/zaparoo.service", "zaparoo.service", "[Unit]\n")
	s.FS = afero.NewMemMapFs()
	s.Cmd = cmd

	require.NoError(t, s.Install(context.Background()))
	data, err := afero.ReadFile(s.FS, "/storage/.config/system.d/zaparoo.service")
	require.NoError(t, err)
	assert.Equal(t, "[Unit]\n", string(data))
	assert.Equal(t, [][]string{
		{"daemon-reload"},
		{"enable", "zaparoo.service"},
		{"restart", "zaparoo.service"},
	}, calls)
}

func TestSystemdUnitInstaller_InstallStopsOnFailure(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Run", mock.Anything, "systemctl", []string{"daemon-reload"}).Return(nil)
	cmd.On("Run", mock.Anything, "systemctl", []string{"enable", "zaparoo.service"}).
		Return(errors.New("no such unit"))
	cmd.On("Run", mock.Anything, "systemctl", []string{"stop", "zaparoo.service"}).Return(nil)
	cmd.On("Run", mock.Anything, "systemctl", []string{"disable", "zaparoo.service"}).Return(nil)

	s := SystemdUnitInstaller("/storage/.config/system.d/zaparoo.service", "zaparoo.service", "x")
	s.FS = afero.NewMemMapFs()
	s.Cmd = cmd

	err := s.Install(context.Background())
	require.ErrorContains(t, err, "systemctl enable zaparoo.service")
	cmd.AssertNotCalled(t, "Run", mock.Anything, "systemctl", []string{"restart", "zaparoo.service"})
	exists, err := afero.Exists(s.FS, s.Path)
	require.NoError(t, err)
	assert.False(t, exists, "a failed install removes the unit")
}

// A service enabled but not started would still run at the next boot, so a
// failed start disables it again.
func TestServiceInstaller_FailedStartDisablesTheService(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	var calls [][]string
	record := func(args mock.Arguments) {
		argv, ok := args.Get(2).([]string)
		require.True(t, ok)
		calls = append(calls, append([]string{args.String(1)}, argv...))
	}
	cmd.On("Run", mock.Anything, "svc", []string{"start", "zaparoo"}).
		Run(record).Return(errors.New("exit 1"))
	cmd.On("Run", mock.Anything, "svc", mock.Anything).Run(record).Return(nil)

	s := &ServiceInstaller{
		FS:      afero.NewMemMapFs(),
		Cmd:     cmd,
		Path:    "/userdata/system/services/zaparoo",
		Content: "#!/bin/sh\n",
		Mode:    0o755,
		Enable: [][]string{
			{"svc", "enable", "zaparoo"},
			{"svc", "start", "zaparoo"},
		},
		Disable: [][]string{
			{"svc", "stop", "zaparoo"},
			{"svc", "disable", "zaparoo"},
		},
	}

	require.ErrorContains(t, s.Install(context.Background()), "svc start zaparoo")
	assert.Equal(t, [][]string{
		{"svc", "enable", "zaparoo"},
		{"svc", "start", "zaparoo"},
		{"svc", "stop", "zaparoo"},
		{"svc", "disable", "zaparoo"},
	}, calls)
	exists, err := afero.Exists(s.FS, s.Path)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSystemdUnitInstaller_UninstallIgnoresStopFailures(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Run", mock.Anything, "systemctl", []string{"stop", "zaparoo.service"}).Return(errors.New("not loaded"))
	cmd.On("Run", mock.Anything, "systemctl", []string{"disable", "zaparoo.service"}).Return(errors.New("not loaded"))
	cmd.On("Run", mock.Anything, "systemctl", []string{"daemon-reload"}).Return(nil)

	s := SystemdUnitInstaller("/storage/.config/system.d/zaparoo.service", "zaparoo.service", "x")
	s.FS = afero.NewMemMapFs()
	s.Cmd = cmd
	require.NoError(t, afero.WriteFile(s.FS, s.Path, []byte("x"), 0o644))

	require.NoError(t, s.Uninstall(context.Background()))
	exists, err := afero.Exists(s.FS, s.Path)
	require.NoError(t, err)
	assert.False(t, exists)
	cmd.AssertCalled(t, "Run", mock.Anything, "systemctl", []string{"daemon-reload"})

	require.NoError(t, s.Uninstall(context.Background()), "uninstalling twice is fine")
}

func TestServiceInstaller_StopRunsBeforeEnableAndFailureIsIgnored(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	var calls [][]string
	record := func(args mock.Arguments) {
		argv, ok := args.Get(2).([]string)
		require.True(t, ok)
		calls = append(calls, append([]string{args.String(1)}, argv...))
	}
	cmd.On("Run", mock.Anything, "svc", []string{"stop", "zaparoo"}).
		Run(record).Return(errors.New("not running"))
	cmd.On("Run", mock.Anything, "svc", mock.Anything).Run(record).Return(nil)

	s := &ServiceInstaller{
		FS:      afero.NewMemMapFs(),
		Cmd:     cmd,
		Path:    "/userdata/system/services/zaparoo",
		Content: "#!/bin/sh\n",
		Mode:    0o755,
		Stop:    [][]string{{"svc", "stop", "zaparoo"}},
		Enable: [][]string{
			{"svc", "enable", "zaparoo"},
			{"svc", "start", "zaparoo"},
		},
	}

	require.NoError(t, s.Install(context.Background()))
	assert.Equal(t, [][]string{
		{"svc", "stop", "zaparoo"},
		{"svc", "enable", "zaparoo"},
		{"svc", "start", "zaparoo"},
	}, calls)
}

func TestCheckInstallPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bin := filepath.Join(dir, "zaparoo")
	require.NoError(t, os.WriteFile(bin, []byte("bin"), 0o600))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(bin, link))
	other := filepath.Join(dir, "other")
	require.NoError(t, os.WriteFile(other, []byte("bin"), 0o600))

	require.NoError(t, CheckInstallPath(bin, bin))
	require.NoError(t, CheckInstallPath(link, bin), "a symlink to the installed binary is the same binary")
	require.NoError(t, CheckInstallPath(bin, link))

	err := CheckInstallPath(other, bin)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "move it there first")
}

// A reinstall whose restart fails must not take away the service that was
// working before it: the previous file stays and nothing is disabled.
func TestServiceInstaller_FailedReinstallKeepsThePreviousService(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Run", mock.Anything, "systemctl", []string{"daemon-reload"}).Return(nil)
	cmd.On("Run", mock.Anything, "systemctl", []string{"enable", "zaparoo.service"}).Return(nil)
	cmd.On("Run", mock.Anything, "systemctl", []string{"restart", "zaparoo.service"}).
		Return(errors.New("start failed"))

	s := SystemdUnitInstaller("/storage/.config/system.d/zaparoo.service", "zaparoo.service", "new")
	s.FS = afero.NewMemMapFs()
	s.Cmd = cmd
	require.NoError(t, afero.WriteFile(s.FS, s.Path, []byte("old"), 0o644))

	err := s.Install(context.Background())
	require.ErrorContains(t, err, "systemctl restart zaparoo.service")
	data, err := afero.ReadFile(s.FS, s.Path)
	require.NoError(t, err)
	assert.Equal(t, "old", string(data))
	cmd.AssertNotCalled(t, "Run", mock.Anything, "systemctl", []string{"disable", "zaparoo.service"})
	cmd.AssertNotCalled(t, "Run", mock.Anything, "systemctl", []string{"stop", "zaparoo.service"})
}

func TestServiceInstaller_RejectsEmptyCommands(t *testing.T) {
	t.Parallel()

	for name, s := range map[string]*ServiceInstaller{
		"empty enable":  {Enable: [][]string{{}}},
		"empty program": {Stop: [][]string{{"", "x"}}},
		"empty disable": {Disable: [][]string{nil}},
		"empty reload":  {Reload: [][]string{{}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s.FS = afero.NewMemMapFs()
			s.Cmd = &mocks.MockCommandExecutor{}
			s.Path = "/etc/zaparoo.service"

			require.ErrorIs(t, s.Install(context.Background()), errEmptyServiceCommand)
			require.ErrorIs(t, s.Uninstall(context.Background()), errEmptyServiceCommand)
			exists, err := afero.Exists(s.FS, s.Path)
			require.NoError(t, err)
			assert.False(t, exists, "nothing is written before the commands are checked")
		})
	}
}

func TestServiceInstaller_DefaultsTheFileMode(t *testing.T) {
	t.Parallel()

	s := &ServiceInstaller{FS: afero.NewMemMapFs(), Cmd: &mocks.MockCommandExecutor{}, Path: "/etc/zaparoo.service"}
	require.NoError(t, s.Install(context.Background()))
	info, err := s.FS.Stat(s.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestServiceInstaller_UnreadableExistingFileStopsInstall(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/zaparoo.service", []byte("old"), 0o000))
	s := &ServiceInstaller{FS: unreadableFS{fs}, Cmd: &mocks.MockCommandExecutor{}, Path: "/etc/zaparoo.service"}
	require.ErrorContains(t, s.Install(context.Background()), "read existing service file")
}

type unreadableFS struct{ afero.Fs }

func (unreadableFS) Open(string) (afero.File, error) { return nil, os.ErrPermission }

func TestCheckRunningInstallPath(t *testing.T) {
	t.Parallel()

	exe, err := os.Executable()
	require.NoError(t, err)
	require.NoError(t, CheckRunningInstallPath(exe))
	require.Error(t, CheckRunningInstallPath(filepath.Join(t.TempDir(), "zaparoo")))
}

func runningBinary(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return exe
}

func TestServiceInstaller_InstallCommandInstallsFromTheServiceBinary(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Run", mock.Anything, "systemctl", mock.Anything).Return(nil)
	s := SystemdUnitInstaller("/storage/.config/system.d/zaparoo.service", "zaparoo.service", "[Unit]\n")
	s.FS = afero.NewMemMapFs()
	s.Cmd = cmd

	var out bytes.Buffer
	require.NoError(t, s.InstallCommand(context.Background(), runningBinary(t), &out))
	assert.Equal(t, "Zaparoo service installed and started.\n", out.String())
	exists, err := afero.Exists(s.FS, s.Path)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestServiceInstaller_InstallCommandRefusesAnotherBinary(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	s := SystemdUnitInstaller("/storage/.config/system.d/zaparoo.service", "zaparoo.service", "[Unit]\n")
	s.FS = afero.NewMemMapFs()
	s.Cmd = cmd

	var out bytes.Buffer
	err := s.InstallCommand(context.Background(), "/storage/zaparoo/zaparoo", &out)
	require.ErrorContains(t, err, "the service runs /storage/zaparoo/zaparoo")
	assert.Empty(t, out.String())
	cmd.AssertNotCalled(t, "Run", mock.Anything, mock.Anything, mock.Anything)
	exists, err := afero.Exists(s.FS, s.Path)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestServiceInstaller_InstallCommandReportsInstallFailure(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Run", mock.Anything, "systemctl", mock.Anything).Return(errors.New("no systemd"))
	s := SystemdUnitInstaller("/storage/.config/system.d/zaparoo.service", "zaparoo.service", "[Unit]\n")
	s.FS = afero.NewMemMapFs()
	s.Cmd = cmd

	var out bytes.Buffer
	err := s.InstallCommand(context.Background(), runningBinary(t), &out)
	require.ErrorContains(t, err, "systemctl daemon-reload: no systemd")
	assert.Empty(t, out.String())
}

func TestServiceInstaller_UninstallCommand(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Run", mock.Anything, "systemctl", mock.Anything).Return(nil)
	s := SystemdUnitInstaller("/storage/.config/system.d/zaparoo.service", "zaparoo.service", "[Unit]\n")
	s.FS = afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(s.FS, s.Path, []byte("[Unit]\n"), 0o644))
	s.Cmd = cmd

	var out bytes.Buffer
	require.NoError(t, s.UninstallCommand(context.Background(), &out))
	assert.Equal(t, "Zaparoo service uninstalled.\n", out.String())
	exists, err := afero.Exists(s.FS, s.Path)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestServiceInstaller_UninstallCommandReportsFailure(t *testing.T) {
	t.Parallel()

	cmd := &mocks.MockCommandExecutor{}
	cmd.On("Run", mock.Anything, "systemctl", mock.Anything).Return(errors.New("no systemd"))
	s := SystemdUnitInstaller("/storage/.config/system.d/zaparoo.service", "zaparoo.service", "[Unit]\n")
	s.FS = afero.NewMemMapFs()
	s.Cmd = cmd

	var out bytes.Buffer
	err := s.UninstallCommand(context.Background(), &out)
	require.ErrorContains(t, err, "systemctl daemon-reload: no systemd")
	assert.Empty(t, out.String())
}
