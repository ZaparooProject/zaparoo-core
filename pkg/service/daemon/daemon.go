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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

type ServiceEntry func() (func() error, <-chan struct{}, error)

type Service struct {
	pl     platforms.Platform
	start  ServiceEntry
	stop   func() error
	done   <-chan struct{}
	daemon bool
}

type ServiceArgs struct {
	Platform platforms.Platform
	Entry    ServiceEntry
	NoDaemon bool
}

func NewService(args ServiceArgs) (*Service, error) {
	err := os.MkdirAll(args.Platform.Settings().TempDir, 0o750)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &Service{
		daemon: !args.NoDaemon,
		start:  args.Entry,
		pl:     args.Platform,
	}, nil
}

// Create new PID file using current process PID.
func (s *Service) createPidFile() error {
	pidPath := filepath.Join(s.pl.Settings().TempDir, config.PidFile)
	pid := os.Getpid()
	err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o600)
	if err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	return nil
}

func (s *Service) removePidFile() error {
	err := os.Remove(filepath.Join(s.pl.Settings().TempDir, config.PidFile))
	if err != nil {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	return nil
}

// Pid returns the process ID of the current running service daemon.
func (s *Service) Pid() (int, error) {
	pid := 0
	pidPath := filepath.Join(s.pl.Settings().TempDir, config.PidFile)

	if _, err := os.Stat(pidPath); err == nil {
		//nolint:gosec // Safe: reads PID files for service management
		pidFile, err := os.ReadFile(pidPath)
		if err != nil {
			return pid, fmt.Errorf("error reading pid file: %w", err)
		}

		pidInt, err := strconv.Atoi(string(pidFile))
		if err != nil {
			return pid, fmt.Errorf("error parsing pid: %w", err)
		}

		pid = pidInt
	}

	return pid, nil
}

// Running returns true if the service is running.
func (s *Service) Running() bool {
	pid, err := s.Pid()
	if err != nil {
		return false
	}

	if pid == 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))

	return err == nil
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

	// remove temporary binary
	tempPath, err := os.Executable()
	if err != nil {
		log.Error().Err(err).Msg("error getting executable path")
	} else if strings.HasPrefix(tempPath, s.pl.Settings().TempDir) {
		err = os.Remove(tempPath)
		if err != nil {
			log.Error().Err(err).Msg("error removing temporary binary")
		}
	}

	return nil
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
	if s.Running() {
		log.Error().Msg("service already running")
		os.Exit(1)
	}

	log.Info().Msg("starting service")

	err := s.createPidFile()
	if err != nil {
		log.Error().Err(err).Msg("error creating pid file")
		os.Exit(1)
	}

	err = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 1)
	if err != nil {
		log.Error().Err(err).Msg("error setting nice level")
	}

	stop, done, err := s.start()
	if err != nil {
		log.Error().Err(err).Msg("error starting service")

		err = s.removePidFile()
		if err != nil {
			log.Error().Err(err).Msg("error removing pid file")
		}

		os.Exit(1)
	}

	s.setupStopService()
	s.stop = stop
	s.done = done

	if !s.daemon {
		if stopErr := s.stopService(); stopErr != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	<-done
	log.Info().Msg("service shut down internally")

	err = s.removePidFile()
	if err != nil {
		log.Error().Err(err).Msg("error removing pid file")
	}

	os.Exit(0)
}

// Start a new service daemon in the background.
func (s *Service) Start() error {
	if s.Running() {
		return errors.New("service already running")
	}

	// create a copy in binary in tmp so the original can be updated
	binPath := ""
	appPath := os.Getenv(config.AppEnv)
	if appPath != "" {
		binPath = appPath
	} else {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("error getting absolute binary path: %w", err)
		}
		binPath = exePath
	}

	binFile, err := os.Open(binPath)
	if err != nil {
		return fmt.Errorf("error opening binary: %w", err)
	}

	tempPath := filepath.Join(s.pl.Settings().TempDir, filepath.Base(binPath))
	//nolint:gosec // Safe: creates temporary binary file for service restart in controlled directory
	tempFile, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o700)
	if err != nil {
		return fmt.Errorf("error creating temp binary: %w", err)
	}

	_, err = io.Copy(tempFile, binFile)
	if err != nil {
		return fmt.Errorf("error copying binary to temp: %w", err)
	}

	err = tempFile.Close()
	if err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	err = binFile.Close()
	if err != nil {
		return fmt.Errorf("failed to close binary file: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	//nolint:gosec // Safe: executes copy of current binary for service restart
	cmd := exec.CommandContext(ctx, tempPath, "-service", "exec")
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

	if !s.Running() {
		return fmt.Errorf("service process %d started but is no longer running", pid)
	}

	return nil
}

// Stop the service daemon.
func (s *Service) Stop() error {
	if !s.Running() {
		return errors.New("service not running")
	}

	pid, err := s.Pid()
	if err != nil {
		return err
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		return fmt.Errorf("failed to send SIGTERM to process: %w", err)
	}

	return nil
}

func (s *Service) Restart() error {
	if s.Running() {
		err := s.Stop()
		if err != nil {
			return err
		}
	}

	// Wait for service to stop with timeout
	deadline := time.Now().Add(10 * time.Second)
	for s.Running() {
		if time.Now().After(deadline) {
			return errors.New("timeout waiting for service to stop")
		}
		time.Sleep(500 * time.Millisecond)
	}

	err := s.Start()
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

	if !s.Running() {
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

	// Wait for service to be ready
	ready := false
	for range 30 {
		if client.IsServiceRunning(cfg) {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !ready {
		_ = cmd.Process.Kill()
		return nil, errors.New("daemon failed to start within 3 seconds")
	}

	log.Info().Msg("daemon subprocess started")

	// Return cleanup function
	return func() {
		if cmd.Process == nil {
			return
		}

		log.Info().Msg("stopping daemon subprocess")
		_ = cmd.Process.Signal(syscall.SIGTERM)

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			log.Warn().Msg("daemon shutdown timed out, killing")
			_ = cmd.Process.Kill()
		}
	}, nil
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
		if s.Running() {
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
