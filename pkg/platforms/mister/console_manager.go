//go:build linux

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/rs/zerolog/log"
)

// TTYReader provides an interface for reading the active TTY.
type TTYReader interface {
	GetActiveTTY() (string, error)
}

// FramebufferChecker provides an interface for checking framebuffer readiness.
type FramebufferChecker interface {
	IsReady() bool
}

// CoreNameGetter provides an interface for getting the active core name.
type CoreNameGetter interface {
	GetCoreName() string
}

// realCoreNameGetter gets the core name from MiSTer_Main's temp file.
type realCoreNameGetter struct{}

func (realCoreNameGetter) GetCoreName() string {
	return mistermain.GetActiveCoreName()
}

// realTTYReader reads the active TTY from the sysfs.
type realTTYReader struct{}

func (realTTYReader) GetActiveTTY() (string, error) {
	sys := "/sys/devices/virtual/tty/tty0/active"
	if _, err := os.Stat(sys); err != nil {
		return "", fmt.Errorf("failed to stat tty active file: %w", err)
	}

	tty, err := os.ReadFile(sys)
	if err != nil {
		return "", fmt.Errorf("failed to read tty active file: %w", err)
	}

	return string(tty[:len(tty)-1]), nil
}

// realFramebufferChecker checks if the framebuffer is accessible.
type realFramebufferChecker struct{}

func (realFramebufferChecker) IsReady() bool {
	if _, err := os.Stat("/dev/fb0"); err != nil {
		return false
	}
	if _, err := os.Stat("/sys/class/graphics/fbcon/cursor_blink"); err != nil {
		return false
	}
	return true
}

// MiSTerConsoleManager manages console/TTY switching for MiSTer platform.
type MiSTerConsoleManager struct {
	ttyReader      TTYReader
	fbChecker      FramebufferChecker
	coreNameGetter CoreNameGetter
	executor       command.Executor
	platform       *Platform
	mu             syncutil.RWMutex
	active         bool
}

func newConsoleManager(p *Platform) *MiSTerConsoleManager {
	return &MiSTerConsoleManager{
		platform:       p,
		ttyReader:      realTTYReader{},
		fbChecker:      realFramebufferChecker{},
		coreNameGetter: realCoreNameGetter{},
		executor:       &command.RealExecutor{},
	}
}

// Open switches to console mode on the specified VT.
// The provided context can be used to cancel the operation if the launcher is superseded.
func (m *MiSTerConsoleManager) Open(ctx context.Context, vt string) error {
	// Check if launcher context is already cancelled
	if ctx.Err() != nil {
		log.Debug().Err(ctx.Err()).Msg("launcher context cancelled before open")
		return ctx.Err()
	}

	// Check if console is already active (for video→video transitions)
	m.mu.RLock()
	isActive := m.active
	m.mu.RUnlock()

	if isActive {
		log.Debug().Msg("console already active, skipping F9 sequence")
		return nil
	}

	// We use the F9 key to signal MiSTer_Main to release the framebuffer and
	// allow Linux console access. F9 triggers video_fb_enable() in Main_MiSTer which:
	// 1. Switches VT using VT_ACTIVATE/VT_WAITACTIVE ioctls
	// 2. Sends SPI commands to FPGA to release framebuffer control
	// 3. Stops OSD rendering when video_chvt(0) != 2
	//
	// Problem: When the menu "sleeps", keypresses can be eaten by Main and not
	// trigger the console switch. We use retry logic with exponential
	// backoff and verification to handle this reliably.
	//
	// When in menu: do chvt first to prime/wake the VT subsystem before F9.
	// When a game is running: skip initial chvt as it interferes with
	// MiSTer_Main's VT switching and causes timeouts.
	coreName := m.coreNameGetter.GetCoreName()
	if coreName == misterconfig.MenuCore {
		chvtCtx, chvtCancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := m.executor.Run(chvtCtx, "chvt", vt)
		chvtCancel()
		if err != nil {
			log.Debug().Err(err).Msg("error switching VT from menu")
			// Don't return error - continue with F9 loop
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	backoff := 50 * time.Millisecond
	maxBackoff := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		// Check if launcher context was cancelled
		if ctx.Err() != nil {
			log.Debug().Err(ctx.Err()).Msg("launcher context cancelled during F9 loop")
			return ctx.Err()
		}

		// Press F9 to signal MiSTer_Main to release framebuffer
		err := m.platform.KeyboardPress("{f9}")
		if err != nil {
			return fmt.Errorf("failed to press F9 key: %w", err)
		}

		time.Sleep(backoff)
		backoff = time.Duration(float64(backoff) * 1.5)
		if backoff > maxBackoff {
			backoff = maxBackoff
		}

		// Verify console mode active by checking VT state
		tty, err := m.getTTY()
		if err != nil {
			log.Warn().Err(err).Msg("failed to get TTY")
			return err
		}
		if tty == "tty"+f9ConsoleVT {
			log.Debug().Msg("console mode confirmed")
			// Wait for framebuffer to be ready
			if err := m.waitForFramebuffer(time.Until(deadline)); err != nil {
				return err
			}

			// Switch to target VT
			if vt != f9ConsoleVT {
				chvtCtx, chvtCancel := context.WithTimeout(context.Background(), 2*time.Second)
				err := m.executor.Run(chvtCtx, "chvt", vt)
				chvtCancel()
				if err != nil {
					log.Debug().Err(err).Msgf("failed to switch to tty%s", vt)
				}
			}

			// Set console active flag
			m.mu.Lock()
			m.active = true
			m.mu.Unlock()

			return nil
		}
	}

	// Timeout - log final state
	finalTTY, _ := m.getTTY()
	log.Error().Msgf("timeout after 5s - stuck on %s, expected tty%s", finalTTY, f9ConsoleVT)
	return errors.New("timeout waiting for console switch after 5s")
}

// Close exits console mode and returns to normal display.
func (m *MiSTerConsoleManager) Close() error {
	// Check if console is already inactive (for FPGA/MGL transitions)
	m.mu.RLock()
	isActive := m.active
	m.mu.RUnlock()

	if !isActive {
		log.Debug().Msg("console already inactive, skipping close")
		return nil
	}

	// Restore console cursor state on both TTYs
	if err := m.Restore(f9ConsoleVT); err != nil {
		log.Debug().Err(err).Msg("failed to restore tty1 state")
	}
	if err := m.Restore(launcherConsoleVT); err != nil {
		log.Debug().Err(err).Msgf("failed to restore tty%s state", launcherConsoleVT)
	}

	// Press F12 to return to FPGA framebuffer
	if err := m.platform.KeyboardPress("{f12}"); err != nil {
		return fmt.Errorf("failed to press F12 key: %w", err)
	}

	// Clear console active flag
	m.mu.Lock()
	m.active = false
	m.mu.Unlock()

	log.Debug().Msg("console closed, returned to FPGA mode")
	return nil
}

// Clean prepares a console for use (clears screen, hides cursor).
// This is public to allow launchers to clean specific TTYs.
func (*MiSTerConsoleManager) Clean(vt string) error {
	// Clear screen and reset
	err := writeTty(vt, "\033[2J\033[H")
	if err != nil {
		return err
	}

	// Disable cursor blinking
	err = echoFile("/sys/class/graphics/fbcon/cursor_blink", "0")
	if err != nil {
		return err
	}

	// Hide cursor
	return writeTty(vt, "\033[?25l")
}

// Restore restores console cursor state.
// This is public to allow launchers to restore specific TTYs.
func (*MiSTerConsoleManager) Restore(vt string) error {
	err := writeTty(vt, "\033[?25h")
	if err != nil {
		return err
	}

	return echoFile("/sys/class/graphics/fbcon/cursor_blink", "1")
}

// getTTY returns the currently active TTY.
func (m *MiSTerConsoleManager) getTTY() (string, error) {
	tty, err := m.ttyReader.GetActiveTTY()
	if err != nil {
		return "", fmt.Errorf("failed to get active TTY: %w", err)
	}
	return tty, nil
}

// waitForFramebuffer waits for the framebuffer device to become accessible.
func (m *MiSTerConsoleManager) waitForFramebuffer(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.fbChecker.IsReady() {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("framebuffer not ready")
}
