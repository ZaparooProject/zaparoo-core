//go:build linux || darwin

package helpers

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

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

type ServiceEntry func() (func() error, error)

type Service struct {
	pl     platforms.Platform
	start  ServiceEntry
	stop   func() error
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
	path := filepath.Join(s.pl.Settings().TempDir, config.PidFile)
	pid := os.Getpid()
	err := os.WriteFile(path, []byte(fmt.Sprintf("%d", pid)), 0o600)
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
	path := filepath.Join(s.pl.Settings().TempDir, config.PidFile)

	if _, err := os.Stat(path); err == nil {
		//nolint:gosec // Safe: reads PID files for service management
		pidFile, err := os.ReadFile(path)
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

	stop, err := s.start()
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

	if !s.daemon {
		err := s.stopService()
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	<-make(chan struct{})
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
	tempFile, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
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
	cmd := exec.CommandContext(ctx, tempPath, "-service", "exec", "&")
	env := os.Environ()
	cmd.Env = env

	// point new binary to existing config file
	configPath := filepath.Join(ConfigDir(s.pl), config.CfgFile)

	if _, statErr := os.Stat(configPath); statErr == nil {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", config.CfgEnv, configPath))
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", config.AppEnv, binPath))

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("error starting service: %w", err)
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

	for s.Running() {
		time.Sleep(1 * time.Second)
	}

	err := s.Start()
	if err != nil {
		return err
	}

	return nil
}

func (s *Service) ServiceHandler(cmd *string) error {
	switch *cmd {
	case "exec":
		s.startService()
		return nil
	case "start":
		err := s.Start()
		if err != nil {
			log.Error().Msg(err.Error())
			return err
		}
		return nil
	case "stop":
		err := s.Stop()
		if err != nil {
			log.Error().Msg(err.Error())
			return err
		}
		return nil
	case "restart":
		err := s.Restart()
		if err != nil {
			log.Error().Msg(err.Error())
			return err
		}
		return nil
	case "status":
		if s.Running() {
			_, _ = fmt.Println("started")
			return nil
		}
		_, _ = fmt.Println("stopped")
		return errors.New("service not running")
	case "":
		// Do nothing for empty command
		return nil
	default:
		_, _ = fmt.Printf("Unknown service argument: %s", *cmd)
		return fmt.Errorf("unknown service argument: %s", *cmd)
	}
}
