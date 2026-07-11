//go:build linux

package mister

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/spf13/afero"
)

const consoleLeaseVersion = "1"

var consoleLeaseStatePath = filepath.Join(string(filepath.Separator), "tmp", "zaparoo_console_state")

type consoleLeaseController interface {
	Available() bool
	Acquire(ctx context.Context, vt string) (string, error)
	Release(ctx context.Context, nonce string) error
}

type mainConsoleLeaseController struct {
	fs             afero.Fs
	statePath      string
	commandPath    string
	pollInterval   time.Duration
	cleanupTimeout time.Duration
}

type consoleLeaseState struct {
	version string
	state   string
	nonce   string
	pid     int
}

func newMainConsoleLeaseController(fs afero.Fs) *mainConsoleLeaseController {
	return &mainConsoleLeaseController{
		fs:             fs,
		statePath:      consoleLeaseStatePath,
		commandPath:    misterconfig.CmdInterface,
		pollInterval:   20 * time.Millisecond,
		cleanupTimeout: 3 * time.Second,
	}
}

func (c *mainConsoleLeaseController) Available() bool {
	state, err := c.readState()
	if err != nil || state.version != consoleLeaseVersion || state.pid <= 0 {
		return false
	}
	_, err = c.fs.Stat(filepath.Join(string(filepath.Separator), "proc", strconv.Itoa(state.pid)))
	return err == nil
}

func (c *mainConsoleLeaseController) Acquire(ctx context.Context, vt string) (string, error) {
	nonce, err := newConsoleLeaseNonce()
	if err != nil {
		return "", err
	}
	if err := c.writeCommand(fmt.Sprintf("zaparoo_console acquire %s %s\n", nonce, vt)); err != nil {
		return "", err
	}
	if err := c.waitForState(ctx, "acquired", nonce); err != nil {
		acquireErr := fmt.Errorf("acquire Main console lease: %w", err)
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), c.cleanupTimeout)
		releaseErr := c.Release(releaseCtx, nonce)
		releaseCancel()
		if releaseErr != nil {
			return "", errors.Join(acquireErr, fmt.Errorf("clean up uncertain Main console lease: %w", releaseErr))
		}
		return "", acquireErr
	}
	return nonce, nil
}

func (c *mainConsoleLeaseController) Release(ctx context.Context, nonce string) error {
	if err := c.writeCommand(fmt.Sprintf("zaparoo_console release %s\n", nonce)); err != nil {
		return err
	}
	if err := c.waitForState(ctx, "released", nonce); err != nil {
		return fmt.Errorf("release Main console lease: %w", err)
	}
	return nil
}

func (c *mainConsoleLeaseController) writeCommand(command string) error {
	cmd, err := c.fs.OpenFile(c.commandPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open Main command interface: %w", err)
	}
	defer func() { _ = cmd.Close() }()

	if _, err := cmd.WriteString(command); err != nil {
		return fmt.Errorf("write Main command: %w", err)
	}
	return nil
}

func (c *mainConsoleLeaseController) waitForState(ctx context.Context, expected, nonce string) error {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		state, err := c.readState()
		if err == nil && state.nonce == nonce {
			switch state.state {
			case expected:
				return nil
			case "busy", "failed":
				return fmt.Errorf("main reported console lease state %q", state.state)
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *mainConsoleLeaseController) readState() (consoleLeaseState, error) {
	contents, err := afero.ReadFile(c.fs, c.statePath)
	if err != nil {
		return consoleLeaseState{}, fmt.Errorf("read Main console state: %w", err)
	}
	fields := strings.Fields(string(contents))
	if len(fields) != 4 {
		return consoleLeaseState{}, errors.New("invalid Main console state")
	}
	pid, err := strconv.Atoi(fields[1])
	if err != nil {
		return consoleLeaseState{}, fmt.Errorf("invalid Main console PID: %w", err)
	}
	return consoleLeaseState{
		version: fields[0],
		pid:     pid,
		state:   fields[2],
		nonce:   fields[3],
	}, nil
}

func newConsoleLeaseNonce() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate console lease nonce: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
