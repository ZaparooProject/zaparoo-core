//go:build linux || darwin

/*
Zaparoo Core
Copyright (C) 2023, 2024 Callan Barrett

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

package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/restart"
	"github.com/rs/zerolog/log"
)

type ServiceEntry func() (*service.StartResult, error)

type Service struct {
	pl     platforms.Platform
	cfg    *config.Instance
	start  ServiceEntry
	stop   func() error
	done   <-chan struct{}
	daemon bool
}

type ServiceArgs struct {
	Platform platforms.Platform
	Config   *config.Instance
	Entry    ServiceEntry
	NoDaemon bool
}

type processWaitFunc func(time.Duration) error

type commandWaiter struct {
	cmd  *exec.Cmd
	done chan error
	once sync.Once
}

const (
	serviceStopTimeout        = 10 * time.Second
	serviceKillTimeout        = 3 * time.Second
	serviceStopPollInterval   = 100 * time.Millisecond
	servicePortReleaseTimeout = 3 * time.Second
	daemonReadyTimeout        = 3 * time.Second
)

func NewService(args ServiceArgs) (*Service, error) {
	err := os.MkdirAll(args.Platform.Settings().TempDir, 0o750)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &Service{
		daemon: !args.NoDaemon,
		cfg:    args.Config,
		start:  args.Entry,
		pl:     args.Platform,
	}, nil
}

// Create new PID file using current process PID.
func (s *Service) createPidFile() error {
	pidPath := filepath.Join(s.pl.Settings().TempDir, config.PidFile)
	pid := os.Getpid()
	//nolint:gosec // PID path is derived from the configured temp directory and fixed filename.
	file, err := os.OpenFile(
		pidPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("failed to create PID file: %w", err)
	}
	defer func() { _ = file.Close() }()

	_, err = file.WriteString(strconv.Itoa(pid))
	if err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	return nil
}

func (s *Service) removePidFile() error {
	err := os.Remove(filepath.Join(s.pl.Settings().TempDir, config.PidFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	return nil
}

// Pid returns the process ID of the current running service daemon.
func (s *Service) Pid() (int, error) {
	pid := 0
	pidPath := filepath.Join(s.pl.Settings().TempDir, config.PidFile)

	info, err := os.Lstat(pidPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return pid, nil
		}
		return pid, fmt.Errorf("error checking pid file: %w", err)
	}
	if validateErr := validatePIDFileInfo(info); validateErr != nil {
		return pid, validateErr
	}

	//nolint:gosec // Safe: PID file path is validated before reading
	pidFile, err := os.ReadFile(pidPath)
	if err != nil {
		return pid, fmt.Errorf("error reading pid file: %w", err)
	}

	pidInt, err := strconv.Atoi(string(pidFile))
	if err != nil {
		return pid, fmt.Errorf("error parsing pid: %w", err)
	}

	pid = pidInt
	return pid, nil
}

func validatePIDFileInfo(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("pid file is a symlink")
	}
	if !info.Mode().IsRegular() {
		return errors.New("pid file is not a regular file")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("pid file is group or world writable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok && int64(stat.Uid) != int64(os.Geteuid()) {
		return fmt.Errorf("pid file is owned by uid %d, expected %d", stat.Uid, os.Geteuid())
	}
	return nil
}

func servicePIDConflictError(pid int) error {
	return fmt.Errorf(
		"service PID file points to live process %d that does not match the Zaparoo service binary",
		pid,
	)
}

// Running returns true if the service is running.
func (s *Service) Running() (bool, error) {
	pid, err := s.Pid()
	if err != nil {
		return false, fmt.Errorf("error reading service PID: %w", err)
	}

	if pid == 0 {
		return false, nil
	}

	if pidRunning(pid) {
		if s.pidMatchesService(pid) {
			return true, nil
		}
		log.Warn().
			Int("pid", pid).
			Msg("service PID file points to live process that does not match service binary")
		return false, servicePIDConflictError(pid)
	}

	if rmErr := s.removePidFile(); rmErr != nil {
		log.Debug().Err(rmErr).Int("pid", pid).Msg("failed to remove stale service PID file")
	}
	return false, nil
}

func (s *Service) stopService() error {
	log.Info().Msgf("stopping service")

	err := s.stop()
	if err != nil {
		log.Error().Err(err).Msg("error stopping service")
		return err
	}

	err = s.removePidFile()
	if err != nil {
		log.Error().Err(err).Msgf("error removing pid file")
		return err
	}

	s.cleanupServiceBinary()

	return nil
}

// prepareBinary copies the binary into DataDir so the original can be
// replaced by external updaters while the service is running.
func (s *Service) prepareBinary(binPath string) (string, error) {
	ext := filepath.Ext(binPath)
	name := strings.TrimSuffix(filepath.Base(binPath), ext)
	copyName := name + ".service" + ext
	dataDir := helpers.DataDir(s.pl)
	copyPath := filepath.Join(dataDir, copyName)

	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", fmt.Errorf("error creating data directory: %w", err)
	}

	equal, eqErr := filesEqual(binPath, copyPath)
	if eqErr != nil {
		log.Warn().Err(eqErr).Msg("error comparing binaries, proceeding with copy")
	} else if equal {
		log.Debug().Msg("skipping binary copy, service binary already up to date")
		return copyPath, nil
	}

	//nolint:gosec // G304: binPath from os.Executable()
	binFile, err := os.Open(binPath)
	if err != nil {
		return "", fmt.Errorf("error opening binary: %w", err)
	}
	defer func() { _ = binFile.Close() }()

	//nolint:gosec // creates service binary copy in data dir
	copyFile, err := os.OpenFile(
		copyPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o700,
	)
	if err != nil {
		return "", fmt.Errorf("error creating service binary: %w", err)
	}
	defer func() { _ = copyFile.Close() }()

	_, err = io.Copy(copyFile, binFile)
	if err != nil {
		return "", fmt.Errorf("error copying binary: %w", err)
	}

	return copyPath, nil
}

// cleanupServiceBinary removes the service binary copy from DataDir.
func (s *Service) cleanupServiceBinary() {
	exePath, err := os.Executable()
	if err != nil {
		log.Error().Err(err).Msg("error getting executable path")
		return
	}
	if !strings.HasPrefix(exePath, helpers.DataDir(s.pl)) {
		return
	}
	if rmErr := os.Remove(exePath); rmErr != nil {
		log.Error().Err(rmErr).Msg("error removing service binary")
	}
}

// filesEqual reports whether the files at pathA and pathB have identical
// contents. Returns false (not an error) if pathB does not exist. A size
// comparison is performed first as a fast pre-filter before streaming.
func filesEqual(pathA, pathB string) (bool, error) {
	infoA, err := os.Stat(pathA) //nolint:gosec // G703: paths from os.Executable() and internal DataDir
	if err != nil {
		return false, fmt.Errorf("error statting source: %w", err)
	}

	infoB, err := os.Stat(pathB) //nolint:gosec // G703: paths from os.Executable() and internal DataDir
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("error statting destination: %w", err)
	}

	if infoA.Size() != infoB.Size() {
		return false, nil
	}

	//nolint:gosec // G304: paths from os.Executable() and internal DataDir
	fa, err := os.Open(pathA)
	if err != nil {
		return false, fmt.Errorf("error opening source: %w", err)
	}
	defer func() { _ = fa.Close() }()

	//nolint:gosec // G304: paths from os.Executable() and internal DataDir
	fb, err := os.Open(pathB)
	if err != nil {
		return false, fmt.Errorf("error opening destination: %w", err)
	}
	defer func() { _ = fb.Close() }()

	bufA := make([]byte, 32*1024)
	bufB := make([]byte, 32*1024)
	for {
		nA, errA := fa.Read(bufA)
		nB, errB := fb.Read(bufB)

		if !bytes.Equal(bufA[:nA], bufB[:nB]) {
			return false, nil
		}

		if errA == io.EOF && errB == io.EOF {
			return true, nil
		}
		if errA != nil {
			return false, fmt.Errorf("error reading source: %w", errA)
		}
		if errB != nil {
			return false, fmt.Errorf("error reading destination: %w", errB)
		}
	}
}

// Set up signal handler to stop service on SIGINT or SIGTERM.
// Exits the application on signal.
func (s *Service) setupStopService() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs

		err := s.stopService()
		if err != nil {
			os.Exit(1)
		}

		os.Exit(0)
	}()
}

// Starts the service and blocks until the service is stopped.
func (s *Service) startService() {
	running, err := s.Running()
	if err != nil {
		log.Error().Err(err).Msg("service PID file conflict")
		os.Exit(1)
	}
	if running {
		log.Error().Msg("service already running")
		os.Exit(1)
	}

	log.Info().Msg("starting service")

	err = s.createPidFile()
	if err != nil {
		log.Error().Err(err).Msg("error creating pid file")
		os.Exit(1)
	}

	err = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 1)
	if err != nil {
		log.Error().Err(err).Msg("error setting nice level")
	}

	result, err := s.start()
	if err != nil {
		log.Error().Err(err).Msg("error starting service")

		err = s.removePidFile()
		if err != nil {
			log.Error().Err(err).Msg("error removing pid file")
		}

		os.Exit(1)
	}

	s.setupStopService()
	s.stop = result.Stop
	s.done = result.Done

	if !s.daemon {
		if stopErr := s.stopService(); stopErr != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	<-result.Done
	log.Info().Msg("service shut down internally")

	err = s.removePidFile()
	if err != nil {
		log.Error().Err(err).Msg("error removing pid file")
	}

	if result.RestartRequested != nil && result.RestartRequested() {
		log.Info().Msg("restart requested, re-executing binary")

		s.cleanupServiceBinary()

		if execErr := restart.Exec(); execErr != nil {
			log.Error().Err(execErr).Msg("failed to re-exec for restart")
			os.Exit(1)
		}
	}

	os.Exit(0)
}

// Start a new service daemon in the background.
func (s *Service) Start() error {
	running, err := s.Running()
	if err != nil {
		return err
	}
	if running {
		return errors.New("service already running")
	}

	binPath := ""
	appPath := os.Getenv(config.AppEnv)
	if appPath != "" {
		binPath = appPath
	} else {
		exePath, exeErr := os.Executable()
		if exeErr != nil {
			return fmt.Errorf("error getting absolute binary path: %w", exeErr)
		}
		binPath = exePath
	}

	serviceBin, err := s.prepareBinary(binPath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	//nolint:gosec // Safe: executes copy of current binary for service restart
	cmd := exec.CommandContext(ctx, serviceBin, "-service", "exec")
	env := os.Environ()
	cmd.Env = env

	// Detach from parent: create new session
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// point new binary to existing config file
	configPath := filepath.Join(helpers.ConfigDir(s.pl), config.CfgFile)

	if _, statErr := os.Stat(configPath); statErr == nil {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", config.CfgEnv, configPath))
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", config.AppEnv, binPath))

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("error starting service: %w", err)
	}

	err = cmd.Process.Release()
	if err != nil {
		return fmt.Errorf("error releasing service process: %w", err)
	}

	// Give process a moment to write PID file
	time.Sleep(500 * time.Millisecond)

	pid, pidErr := s.Pid()
	if pidErr != nil {
		log.Error().Err(pidErr).Msg("PID file not found after service start - process may have failed")
		return fmt.Errorf("service started but PID file not found: %w", pidErr)
	}

	log.Info().Msgf("service process started with PID %d", pid)

	running, err = s.Running()
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("service process %d started but is no longer running", pid)
	}

	return nil
}

// Stop the service daemon.
func (s *Service) Stop() error {
	pid, err := s.Pid()
	if err != nil {
		return err
	}
	if pid == 0 {
		return errors.New("service not running")
	}
	if !pidRunning(pid) {
		return s.removePidFile()
	}
	if !s.pidMatchesService(pid) {
		return fmt.Errorf("refusing to stop PID %d because it does not match the Zaparoo service binary", pid)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	if err := stopProcess(process, pid, func(timeout time.Duration) error {
		return s.waitForServiceExit(pid, timeout)
	}); err != nil {
		return err
	}

	if err := s.removePidFile(); err != nil {
		return err
	}
	return waitForAPIPortRelease(s.cfg, servicePortReleaseTimeout, serviceStopPollInterval)
}

func (s *Service) waitForServiceExit(pid int, timeout time.Duration) error {
	pidPath := filepath.Join(s.pl.Settings().TempDir, config.PidFile)
	return waitForPIDExit(pid, timeout, serviceStopPollInterval, func(pid int) bool {
		if _, err := os.Stat(pidPath); errors.Is(err, os.ErrNotExist) {
			return false
		}
		return pidRunning(pid)
	})
}

func stopProcess(process *os.Process, pid int, wait processWaitFunc) error {
	if process == nil {
		return errors.New("process is nil")
	}

	if err := signalProcess(process, pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	stopErr := wait(serviceStopTimeout)
	if stopErr == nil {
		return nil
	}
	log.Warn().Err(stopErr).Int("pid", pid).Msg("process did not stop after SIGTERM, sending SIGKILL")

	if err := signalProcess(process, pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("failed to send SIGKILL to process %d: %w", pid, err)
	}
	return wait(serviceKillTimeout)
}

func signalProcess(process *os.Process, pid int, sig syscall.Signal) error {
	if pid > 0 {
		if err := syscall.Kill(-pid, sig); err == nil {
			return nil
		} else if errors.Is(err, syscall.EPERM) {
			log.Debug().Err(err).Int("pid", pid).Msg("failed to signal process group, falling back to process signal")
		} else if !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("failed to signal process group %d: %w", pid, err)
		}
	}
	if err := process.Signal(sig); err != nil {
		return fmt.Errorf("failed to signal process %d: %w", pid, err)
	}
	return nil
}

func pidRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, syscall.Signal(0))
	if err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	return !pidIsZombie(pid)
}

func pidIsZombie(pid int) bool {
	statPath := filepath.Join(procDir(), strconv.Itoa(pid), "stat")
	data, err := os.ReadFile(statPath) //nolint:gosec // reads process status for service management
	if err != nil {
		return false
	}

	fieldsStart := strings.LastIndexByte(string(data), ')')
	if fieldsStart == -1 || fieldsStart+2 >= len(data) {
		return false
	}
	return data[fieldsStart+2] == 'Z'
}

func (s *Service) pidMatchesService(pid int) bool {
	if runtime.GOOS != "linux" {
		return true
	}

	dataDir := helpers.DataDir(s.pl)
	exePath, err := os.Readlink(filepath.Join(procDir(), strconv.Itoa(pid), "exe"))
	if err == nil && pathLooksLikeServiceBinary(exePath, dataDir) {
		return true
	}

	cmdlinePath := filepath.Join(procDir(), strconv.Itoa(pid), "cmdline")
	cmdline, err := os.ReadFile(cmdlinePath) //nolint:gosec // reads process status for service management
	if err != nil {
		return false
	}
	for _, arg := range strings.Split(string(cmdline), "\x00") {
		if pathLooksLikeServiceBinary(arg, dataDir) {
			return true
		}
	}
	return false
}

func procDir() string {
	return string(filepath.Separator) + "proc"
}

func pathLooksLikeServiceBinary(path, dataDir string) bool {
	if path == "" || dataDir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dataDir), filepath.Clean(path))
	if err != nil || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return false
	}
	return filepath.Dir(rel) == "." && strings.Contains(filepath.Base(rel), ".service")
}

func waitForPIDExit(
	pid int,
	timeout time.Duration,
	pollInterval time.Duration,
	isRunning func(int) bool,
) error {
	if pid <= 0 {
		return nil
	}

	deadline := time.Now().Add(timeout)
	for isRunning(pid) {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for process %d to stop", pid)
		}
		time.Sleep(pollInterval)
	}

	return nil
}

func waitForAPIPortRelease(cfg *config.Instance, timeout, pollInterval time.Duration) error {
	if cfg == nil || cfg.APIPort() == 0 {
		return nil
	}

	addrs := apiDialAddresses(cfg)
	deadline := time.Now().Add(timeout)
	for {
		released := true
		for _, addr := range addrs {
			ctx, cancel := context.WithTimeout(context.Background(), pollInterval)
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
			cancel()
			if err != nil {
				continue
			}
			released = false
			_ = conn.Close()
		}
		if released {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for API port %s to release", strings.Join(addrs, ", "))
		}
		time.Sleep(pollInterval)
	}
}

func newCommandWaiter(cmd *exec.Cmd) *commandWaiter {
	return &commandWaiter{
		cmd:  cmd,
		done: make(chan error, 1),
	}
}

func (w *commandWaiter) wait(timeout time.Duration) error {
	w.once.Do(func() {
		go func() { w.done <- w.cmd.Wait() }()
	})

	select {
	case err := <-w.done:
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return nil
			}
			return err
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for process %d to stop", w.cmd.Process.Pid)
	}
}

func apiDialAddresses(cfg *config.Instance) []string {
	host, port, err := net.SplitHostPort(cfg.APIListen())
	if err != nil {
		return []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.APIPort()))}
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return []string{net.JoinHostPort("127.0.0.1", port), net.JoinHostPort("::1", port)}
	}
	return []string{net.JoinHostPort(host, port)}
}

// Restart is intentionally idempotent: if Running reports no live PID, Stop and
// waitForPIDExit are skipped and Start is called directly. When a PID is still
// active, Restart follows the full Stop -> waitForPIDExit -> Start sequence so
// pidRunning has confirmed the old process is gone before the replacement starts.
func (s *Service) Restart() error {
	oldPID := 0
	running, err := s.Running()
	if err != nil {
		return err
	}
	if running {
		oldPID, err = s.Pid()
		if err != nil {
			return err
		}

		err = s.Stop()
		if err != nil {
			return err
		}
	}

	if waitErr := s.waitForServiceExit(oldPID, serviceStopTimeout); waitErr != nil {
		return waitErr
	}
	if waitErr := waitForAPIPortRelease(s.cfg, servicePortReleaseTimeout, serviceStopPollInterval); waitErr != nil {
		return waitErr
	}

	err = s.Start()
	if err != nil {
		return err
	}

	return nil
}

// WaitForAPI waits for the service API to become available with health monitoring.
// Returns nil if API became available, error if timeout or process crashed.
func (s *Service) WaitForAPI(cfg *config.Instance, maxWait, checkInterval time.Duration) error {
	if client.WaitForAPI(cfg, maxWait, checkInterval) {
		log.Info().Msg("API is now available")
		return nil
	}

	running, err := s.Running()
	if err != nil {
		log.Error().Err(err).Msg("service PID file conflict")
		return err
	}
	if !running {
		log.Error().Msg("service process is no longer running")
		return errors.New("service process crashed during startup")
	}

	log.Warn().Msg("service process is running but API is not responding")
	return errors.New("API did not become available within timeout")
}

// SpawnDaemon spawns a daemon subprocess for service isolation (e.g., TUI mode).
// It waits for the service API to become available and returns a cleanup function.
// The cleanup function sends SIGTERM and waits for graceful shutdown with timeout.
// Returns a no-op cleanup function if service was already running.
func SpawnDaemon(cfg *config.Instance) (cleanup func(), err error) {
	if client.IsServiceRunning(cfg) {
		log.Info().
			Int("port", cfg.APIPort()).
			Msg("connecting to existing service instance")
		return func() {}, nil // no-op cleanup when using existing service
	}

	log.Info().Msg("spawning daemon subprocess")

	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	//nolint:gosec // exe is from os.Executable()
	cmd := exec.CommandContext(context.Background(), exe, "-daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start daemon: %w", err)
	}
	waiter := newCommandWaiter(cmd)
	var cleanupOnce sync.Once
	log.Info().Int("pid", cmd.Process.Pid).Msg("daemon subprocess started")

	// Wait for service to be ready
	deadline := time.Now().Add(daemonReadyTimeout)
	for time.Now().Before(deadline) {
		if client.IsServiceRunning(cfg) {
			log.Info().Int("pid", cmd.Process.Pid).Msg("daemon API is ready")
			return func() {
				cleanupOnce.Do(func() {
					if cmd.Process == nil {
						return
					}

					process := cmd.Process
					pid := process.Pid
					defer func() { cmd.Process = nil }()

					log.Info().Int("pid", pid).Msg("stopping daemon subprocess")
					if err := stopProcess(process, pid, waiter.wait); err != nil {
						log.Error().Err(err).Int("pid", pid).Msg("error stopping daemon subprocess")
					}
					if err := waitForAPIPortRelease(
						cfg, servicePortReleaseTimeout, serviceStopPollInterval,
					); err != nil {
						log.Warn().Err(err).Msg("daemon subprocess stopped but API port is still in use")
					}
				})
			}, nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := stopProcess(cmd.Process, cmd.Process.Pid, waiter.wait); err != nil {
		log.Warn().
			Err(err).
			Int("pid", cmd.Process.Pid).
			Msg("failed to clean up daemon subprocess after startup timeout")
	}
	return nil, errors.New("daemon failed to start within 3 seconds")
}

func (s *Service) ServiceHandler(cmd *string) error {
	switch *cmd {
	case "exec":
		s.startService()
		return nil
	case "start":
		err := s.Start()
		if err != nil {
			log.Error().Err(err).Msg("error starting service")
			os.Exit(1)
		}
		os.Exit(0)
	case "stop":
		err := s.Stop()
		if err != nil {
			log.Error().Err(err).Msg("error stopping service")
			os.Exit(1)
		}
		os.Exit(0)
	case "restart":
		err := s.Restart()
		if err != nil {
			log.Error().Err(err).Msg("error restarting service")
			os.Exit(1)
		}
		os.Exit(0)
	case "status":
		running, err := s.Running()
		if err != nil {
			log.Error().Err(err).Msg("service PID file conflict")
			os.Exit(1)
		}
		if running {
			_, _ = fmt.Println("started")
			os.Exit(0)
		}
		_, _ = fmt.Println("stopped")
		os.Exit(1)
	case "":
		return nil // no command provided, do nothing
	default:
		return fmt.Errorf("unknown service argument: %s", *cmd)
	}
	return nil
}
