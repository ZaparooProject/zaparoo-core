//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package gamescope makes externally launched windows visible and focused in
// gamescope Gaming Mode sessions. It does not register launches with Steam, so
// it cannot provide Steam/QAM menus, overlay injection, or Steam Input ownership.
package gamescope

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	gamescopeAtom       = "GAMESCOPE_XWAYLAND_SERVER_ID"
	x11SocketDir        = "/tmp/.X11-unix"
	detectTimeout       = 2 * time.Second
	gamingModeCacheTTL  = time.Second
	windowFindTimeout   = 5 * time.Second
	windowFallbackDelay = 2 * time.Second
	windowPollInterval  = 200 * time.Millisecond
	windowMissingLimit  = 3
	steamGameAtom       = "STEAM_GAME"
	baselayerAtom       = "GAMESCOPECTRL_BASELAYER_APPID"
	externalFocusAppID  = "1"
	minGameWindowWidth  = 100
	minGameWindowHeight = 100
	windowPIDAtom       = "_NET_WM_PID"
)

// SessionOptions controls gamescope integration. Zero value disables it.
type SessionOptions struct {
	Enabled bool
}

// Manager owns one platform session's gamescope detection and focus state.
type Manager struct {
	gamingModeCachedAt time.Time
	executor           command.Executor
	attemptCancel      context.CancelFunc
	activeFocusManager *FocusManager
	activeFocusCancel  context.CancelFunc
	gamescopeDisplay   string
	attemptID          uint64
	cacheMu            syncutil.Mutex
	focusMu            syncutil.Mutex
	attemptMu          syncutil.Mutex
	cachedGamingMode   bool
	enabled            bool
}

// FocusManager tracks focus properties for restoration.
type FocusManager struct {
	executor      command.Executor
	display       string
	originalLayer string
	mu            syncutil.Mutex
	reverted      bool
}

type windowCandidate struct {
	ID string
}

var steamWindowPatterns = []string{
	"steam", "Steam", "SteamOverlay", "steamwebhelper",
	"Steam Big Picture Mode", "mangoapp overlay window",
}

var (
	windowLineRegex = regexp.MustCompile(`^\s*(0x[0-9a-fA-F]+)\s+"[^"]*":\s+\(.*?\)\s+(\d+)x(\d+)`)
	windowPIDRegex  = regexp.MustCompile(`_NET_WM_PID.*?=\s*(\d+)`)
)

// NewManager creates an independent gamescope session manager.
func NewManager(opts SessionOptions) *Manager {
	return newManagerWithExecutor(opts, &command.RealExecutor{})
}

func newManagerWithExecutor(opts SessionOptions, executor command.Executor) *Manager {
	return &Manager{enabled: opts.Enabled, executor: executor}
}

// Enabled reports whether platform opted into gamescope integration.
func (m *Manager) Enabled() bool { return m != nil && m.enabled }

// WrapLauncher adds automatic gamescope focus handling to a non-Steam launcher.
// Launchers without a returned process remain fire-and-forget.
func (m *Manager) WrapLauncher(launcher *platforms.Launcher) {
	if !m.Enabled() || launcher == nil || launcher.ID == "Steam" || launcher.Launch == nil {
		return
	}

	launch := launcher.Launch
	kill := launcher.Kill
	launcher.Launch = func(
		cfg *config.Instance,
		path string,
		opts *platforms.LaunchOptions,
	) (*os.Process, error) {
		proc, err := launch(cfg, path, opts)
		if err == nil && proc != nil {
			go m.ManageFocus(proc)
		}
		return proc, err
	}
	launcher.Kill = func(cfg *config.Instance) error {
		m.RevertFocus()
		if kill == nil {
			return nil
		}
		return kill(cfg)
	}
}

// WrapLaunchers adds automatic focus handling to every non-Steam launcher.
func (m *Manager) WrapLaunchers(launchers []platforms.Launcher) {
	for i := range launchers {
		m.WrapLauncher(&launchers[i])
	}
}

// IsGamingMode detects a gamescope Xwayland session. Positive results are briefly cached.
func (m *Manager) IsGamingMode() bool {
	if !m.Enabled() {
		return false
	}

	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if m.cachedGamingMode && time.Since(m.gamingModeCachedAt) < gamingModeCacheTTL {
		return true
	}
	m.cachedGamingMode, m.gamescopeDisplay = m.detectGamingMode()
	if m.cachedGamingMode {
		m.gamingModeCachedAt = time.Now()
		log.Info().Str("display", m.gamescopeDisplay).Msg("gamescope Gaming Mode detected")
	} else {
		m.gamingModeCachedAt = time.Time{}
		m.gamescopeDisplay = ""
	}
	return m.cachedGamingMode
}

// GamescopeDisplay returns detected gamescope X display.
func (m *Manager) GamescopeDisplay() string {
	if !m.IsGamingMode() {
		return ""
	}
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	return m.gamescopeDisplay
}

func (m *Manager) detectGamingMode() (found bool, display string) {
	sockets, err := filepath.Glob(filepath.Join(x11SocketDir, "X*"))
	if err != nil {
		return false, ""
	}
	for _, socket := range sockets {
		display := ":" + strings.TrimPrefix(filepath.Base(socket), "X")
		if m.hasGamescopeAtom(display) {
			return true, display
		}
	}
	return false, ""
}

func (m *Manager) hasGamescopeAtom(display string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()
	output, err := m.executor.Output(ctx, "xprop", "-display", display, "-root", gamescopeAtom)
	return err == nil && strings.Contains(string(output), "CARDINAL")
}

// ManageFocus makes proc's external window visible and focused by gamescope.
// This is compositor focus only; it does not make Steam own the launch.
func (m *Manager) ManageFocus(proc *os.Process) {
	if !m.Enabled() || proc == nil {
		return
	}
	ctx, cancel, id := m.beginFocusAttempt()
	defer func() { cancel(); m.endFocusAttempt(id) }()
	if !m.IsGamingMode() {
		return
	}
	display := m.GamescopeDisplay()
	if display == "" {
		return
	}
	windowID, err := m.findGameWindow(ctx, display, proc.Pid)
	if err != nil {
		log.Warn().Err(err).Int("pid", proc.Pid).Msg("failed to find game window for focus")
		return
	}
	// Serialize baselayer transitions so overlapping launches cannot capture or
	// restore another focus manager's temporary external app layer.
	m.focusMu.Lock()
	previous := m.activeFocusManager
	previousCancel := m.activeFocusCancel
	m.activeFocusManager = nil
	m.activeFocusCancel = nil
	if previousCancel != nil {
		previousCancel()
	}
	if previous != nil {
		previous.Revert()
	}

	original, err := m.getBaselayerValue(display)
	if err != nil {
		original = ""
	}
	if err := m.setSteamGameProperty(display, windowID); err != nil {
		m.focusMu.Unlock()
		return
	}
	if err := m.setBaselayerValue(display, externalFocusAppID, original); err != nil {
		m.focusMu.Unlock()
		return
	}
	fm := &FocusManager{executor: m.executor, display: display, originalLayer: original}
	//nolint:gosec // Context is canceled on window close, focus replacement, or explicit revert.
	watchCtx, watchCancel := context.WithCancel(context.Background())
	m.activeFocusManager = fm
	m.activeFocusCancel = watchCancel
	m.focusMu.Unlock()
	log.Debug().Int("pid", proc.Pid).Str("windowID", windowID).Str("display", display).
		Msg("gamescope external window focus set")
	go m.revertFocusWhenWindowCloses(watchCtx, display, windowID, fm)
}

func (m *Manager) beginFocusAttempt() (context.Context, context.CancelFunc, uint64) {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // Canceled on completion or explicit revert.
	m.attemptMu.Lock()
	previous := m.attemptCancel
	m.attemptID++
	id := m.attemptID
	m.attemptCancel = cancel
	m.attemptMu.Unlock()
	if previous != nil {
		previous()
	}
	return ctx, cancel, id
}

func (m *Manager) endFocusAttempt(id uint64) {
	m.attemptMu.Lock()
	defer m.attemptMu.Unlock()
	if m.attemptID == id {
		m.attemptCancel = nil
	}
}

func (m *Manager) revertFocusWhenWindowCloses(
	ctx context.Context, display, windowID string, fm *FocusManager,
) {
	ticker := time.NewTicker(windowPollInterval)
	defer ticker.Stop()
	missing := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		detectCtx, cancel := context.WithTimeout(ctx, detectTimeout)
		_, err := m.executor.Output(detectCtx, "xprop", "-display", display, "-id", windowID, steamGameAtom)
		cancel()
		if err == nil {
			missing = 0
			continue
		}
		if ctx.Err() != nil {
			return
		}
		missing++
		if missing < windowMissingLimit {
			continue
		}

		m.focusMu.Lock()
		if m.activeFocusManager != fm {
			m.focusMu.Unlock()
			return
		}
		m.activeFocusManager = nil
		m.activeFocusCancel = nil
		m.focusMu.Unlock()
		fm.Revert()
		return
	}
}

// RevertFocus cancels this manager's pending focus and restores its active baselayer state.
func (m *Manager) RevertFocus() {
	if m == nil {
		return
	}
	m.attemptMu.Lock()
	cancel := m.attemptCancel
	m.attemptCancel = nil
	m.attemptMu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.focusMu.Lock()
	fm := m.activeFocusManager
	focusCancel := m.activeFocusCancel
	m.activeFocusManager = nil
	m.activeFocusCancel = nil
	m.focusMu.Unlock()
	if focusCancel != nil {
		focusCancel()
	}
	if fm != nil {
		fm.Revert()
	}
}

// Revert restores original baselayer once.
func (fm *FocusManager) Revert() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if fm.reverted {
		return
	}
	fm.reverted = true
	if fm.originalLayer == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()
	err := fm.executor.Run(ctx,
		"xprop", "-display", fm.display, "-root",
		"-format", baselayerAtom, "32co",
		"-set", baselayerAtom, fm.originalLayer,
	)
	if err != nil {
		log.Warn().Err(err).Str("display", fm.display).Msg("failed to revert baselayer property")
	}
}

func (m *Manager) findGameWindow(parent context.Context, display string, pid int) (string, error) {
	ctx, cancel := context.WithTimeout(parent, windowFindTimeout)
	defer cancel()
	ticker := time.NewTicker(windowPollInterval)
	defer ticker.Stop()
	started := time.Now()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timeout waiting for game window: %w", ctx.Err())
		case <-ticker.C:
			allowFallback := time.Since(started) >= windowFallbackDelay
			id, err := m.findNonSteamWindowForProcess(ctx, display, pid, allowFallback)
			if err == nil && id != "" {
				return id, nil
			}
		}
	}
}

func (m *Manager) findNonSteamWindowForProcess(
	ctx context.Context,
	display string,
	pid int,
	allowFallback bool,
) (string, error) {
	output, err := m.executor.Output(ctx, "xwininfo", "-display", display, "-root", "-tree")
	if err != nil {
		return "", fmt.Errorf("xwininfo failed: %w", err)
	}
	candidates := parseWindowCandidates(string(output))
	for _, candidate := range candidates {
		if pid > 0 && m.windowMatchesProcess(ctx, display, candidate.ID, pid) {
			return candidate.ID, nil
		}
	}
	// Flatpak windows report sandbox PIDs, which cannot match the host launcher PID.
	// Wait for the emulator's final game window before using the topmost candidate.
	if allowFallback && len(candidates) > 0 {
		return candidates[0].ID, nil
	}
	return "", nil
}

func parseWindowCandidates(output string) []windowCandidate {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var candidates []windowCandidate
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), "0x") || containsAny(line, steamWindowPatterns) {
			continue
		}
		matches := windowLineRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		width, widthErr := strconv.Atoi(matches[2])
		height, heightErr := strconv.Atoi(matches[3])
		if widthErr == nil && heightErr == nil && width >= minGameWindowWidth && height >= minGameWindowHeight {
			candidates = append(candidates, windowCandidate{ID: matches[1]})
		}
	}
	return candidates
}

func containsAny(value string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(value, pattern) {
			return true
		}
	}
	return false
}

func (m *Manager) windowMatchesProcess(ctx context.Context, display, windowID string, pid int) bool {
	output, err := m.executor.Output(ctx, "xprop", "-display", display, "-id", windowID, windowPIDAtom)
	if err != nil {
		return false
	}
	windowPID, ok := ParseWindowPIDOutput(string(output))
	return ok && windowPID == pid
}

// ParseWindowPIDOutput extracts owning PID from xprop output.
func ParseWindowPIDOutput(output string) (int, bool) {
	matches := windowPIDRegex.FindStringSubmatch(output)
	if len(matches) < 2 {
		return 0, false
	}
	pid, err := strconv.Atoi(matches[1])
	return pid, err == nil
}

// ParseBaselayerOutput extracts baselayer values from xprop output.
func ParseBaselayerOutput(output string) string {
	parts := strings.SplitN(output, "=", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// BuildBaselayerValue prepends appID to existing baselayer values.
func BuildBaselayerValue(appID, original string) string {
	if original != "" {
		return appID + ", " + original
	}
	return appID
}

func (m *Manager) getBaselayerValue(display string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()
	output, err := m.executor.Output(ctx, "xprop", "-display", display, "-root", baselayerAtom)
	if err != nil {
		return "", fmt.Errorf("get baselayer: %w", err)
	}
	return ParseBaselayerOutput(string(output)), nil
}

func (m *Manager) setSteamGameProperty(display, windowID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()
	err := m.executor.Run(ctx,
		"xprop", "-display", display, "-id", windowID,
		"-f", steamGameAtom, "32c", "-set", steamGameAtom, "1",
	)
	if err != nil {
		return fmt.Errorf("set STEAM_GAME: %w", err)
	}
	return nil
}

func (m *Manager) setBaselayerValue(display, appID, original string) error {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()
	err := m.executor.Run(ctx,
		"xprop", "-display", display, "-root",
		"-format", baselayerAtom, "32co",
		"-set", baselayerAtom, BuildBaselayerValue(appID, original),
	)
	if err != nil {
		return fmt.Errorf("set baselayer: %w", err)
	}
	return nil
}
