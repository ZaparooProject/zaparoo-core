// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package mocks

import (
	"fmt"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/stretchr/testify/mock"
)

// MockPlatform is a mock implementation of the Platform interface using testify/mock
type MockPlatform struct {
	mock.Mock
	launchedMedia   []string // Track launched media for verification
	launchedSystems []string // Track launched systems for verification
	keyboardPresses []string // Track keyboard presses for verification
	gamepadPresses  []string // Track gamepad presses for verification
}

// ID returns the unique ID of this platform
func (m *MockPlatform) ID() string {
	args := m.Called()
	return args.String(0)
}

// StartPre runs any necessary platform setup BEFORE the main service has started running
func (m *MockPlatform) StartPre(cfg *config.Instance) error {
	args := m.Called(cfg)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock platform start pre failed: %w", err)
	}
	return nil
}

// StartPost runs any necessary platform setup AFTER the main service has started running
func (m *MockPlatform) StartPost(cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	getActiveMedia func() *models.ActiveMedia, setActiveMedia func(*models.ActiveMedia),
) error {
	args := m.Called(cfg, launcherManager, getActiveMedia, setActiveMedia)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock platform start post failed: %w", err)
	}
	return nil
}

// Stop runs any necessary cleanup tasks before the rest of the service starts shutting down
func (m *MockPlatform) Stop() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock platform stop failed: %w", err)
	}
	return nil
}

// Settings returns all simple platform-specific settings such as paths
func (m *MockPlatform) Settings() platforms.Settings {
	args := m.Called()
	if settings, ok := args.Get(0).(platforms.Settings); ok {
		return settings
	}
	return platforms.Settings{}
}

// ScanHook is run immediately AFTER a successful scan, but BEFORE it is processed for launching
func (m *MockPlatform) ScanHook(token *tokens.Token) error {
	args := m.Called(token)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock platform scan hook failed: %w", err)
	}
	return nil
}

// SupportedReaders returns a list of supported reader modules for platform
func (m *MockPlatform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	args := m.Called(cfg)
	if readerList, ok := args.Get(0).([]readers.Reader); ok {
		return readerList
	}
	return []readers.Reader{}
}

// RootDirs returns a list of root folders to scan for media files
func (m *MockPlatform) RootDirs(cfg *config.Instance) []string {
	args := m.Called(cfg)
	if dirs, ok := args.Get(0).([]string); ok {
		return dirs
	}
	return []string{}
}

// NormalizePath converts a path to a normalized form for the platform
func (m *MockPlatform) NormalizePath(cfg *config.Instance, path string) string {
	args := m.Called(cfg, path)
	return args.String(0)
}

// StopActiveLauncher kills/exits the currently running launcher process
func (m *MockPlatform) StopActiveLauncher(intent platforms.StopIntent) error {
	args := m.Called(intent)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock platform stop active launcher failed: %w", err)
	}
	return nil
}

// ReturnToMenu returns the platform to its main UI/launcher/frontend
func (m *MockPlatform) ReturnToMenu() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock platform return to menu failed: %w", err)
	}
	return nil
}

// PlayAudio plays an audio file at the given path
func (m *MockPlatform) PlayAudio(path string) error {
	args := m.Called(path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock platform play audio failed: %w", err)
	}
	return nil
}

// LaunchSystem launches a system by ID
func (m *MockPlatform) LaunchSystem(cfg *config.Instance, systemID string) error {
	args := m.Called(cfg, systemID)
	m.launchedSystems = append(m.launchedSystems, systemID)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// LaunchMedia launches some media by path and sets the active media if it was successful
func (m *MockPlatform) LaunchMedia(cfg *config.Instance, path string, launcher *platforms.Launcher) error {
	args := m.Called(cfg, path, launcher)
	m.launchedMedia = append(m.launchedMedia, path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// KeyboardPress presses and then releases a single keyboard button
func (m *MockPlatform) KeyboardPress(key string) error {
	args := m.Called(key)
	m.keyboardPresses = append(m.keyboardPresses, key)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// GamepadPress presses and then releases a single gamepad button
func (m *MockPlatform) GamepadPress(button string) error {
	args := m.Called(button)
	m.gamepadPresses = append(m.gamepadPresses, button)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// ForwardCmd processes a platform-specific ZapScript command
func (m *MockPlatform) ForwardCmd(env *platforms.CmdEnv) (platforms.CmdResult, error) {
	args := m.Called(env)
	if result, ok := args.Get(0).(platforms.CmdResult); ok {
		if err := args.Error(1); err != nil {
			return result, fmt.Errorf("mock operation failed: %w", err)
		}
		return result, nil
	}
	if err := args.Error(1); err != nil {
		return platforms.CmdResult{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return platforms.CmdResult{}, nil
}

// LookupMapping is a platform-specific method of matching a token to a mapping
func (m *MockPlatform) LookupMapping(token *tokens.Token) (string, bool) {
	args := m.Called(token)
	return args.String(0), args.Bool(1)
}

// Launchers is the complete list of all launchers available on this platform
func (m *MockPlatform) Launchers(cfg *config.Instance) []platforms.Launcher {
	args := m.Called(cfg)
	if launchers, ok := args.Get(0).([]platforms.Launcher); ok {
		return launchers
	}
	return []platforms.Launcher{}
}

// ShowNotice displays a string on-screen of the platform device
func (m *MockPlatform) ShowNotice(cfg *config.Instance, args widgetmodels.NoticeArgs,
) (func() error, time.Duration, error) {
	callArgs := m.Called(cfg, args)
	var fn func() error
	var duration time.Duration
	if f, ok := callArgs.Get(0).(func() error); ok {
		fn = f
	}
	if d, ok := callArgs.Get(1).(time.Duration); ok {
		duration = d
	}
	if err := callArgs.Error(2); err != nil {
		return fn, duration, fmt.Errorf("mock operation failed: %w", err)
	}
	return fn, duration, nil
}

// ShowLoader displays a string on-screen alongside an animation
func (m *MockPlatform) ShowLoader(cfg *config.Instance, args widgetmodels.NoticeArgs) (func() error, error) {
	callArgs := m.Called(cfg, args)
	var fn func() error
	if f, ok := callArgs.Get(0).(func() error); ok {
		fn = f
	}
	if err := callArgs.Error(1); err != nil {
		return fn, fmt.Errorf("mock operation failed: %w", err)
	}
	return fn, nil
}

// ShowPicker displays a list picker on-screen of the platform device
func (m *MockPlatform) ShowPicker(cfg *config.Instance, args widgetmodels.PickerArgs) error {
	callArgs := m.Called(cfg, args)
	if err := callArgs.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockPlatform) ConsoleManager() platforms.ConsoleManager {
	args := m.Called()
	if manager, ok := args.Get(0).(platforms.ConsoleManager); ok {
		return manager
	}
	// Return a safe default if not configured
	return platforms.NoOpConsoleManager{}
}

// Helper methods for testing

// GetLaunchedMedia returns a slice of all media paths that were launched
func (m *MockPlatform) GetLaunchedMedia() []string {
	return append([]string(nil), m.launchedMedia...) // Return a copy
}

// GetLaunchedSystems returns a slice of all system IDs that were launched
func (m *MockPlatform) GetLaunchedSystems() []string {
	return append([]string(nil), m.launchedSystems...) // Return a copy
}

// GetKeyboardPresses returns a slice of all keyboard keys that were pressed
func (m *MockPlatform) GetKeyboardPresses() []string {
	return append([]string(nil), m.keyboardPresses...) // Return a copy
}

// GetGamepadPresses returns a slice of all gamepad buttons that were pressed
func (m *MockPlatform) GetGamepadPresses() []string {
	return append([]string(nil), m.gamepadPresses...) // Return a copy
}

// ClearHistory clears all tracked interactions
func (m *MockPlatform) ClearHistory() {
	m.launchedMedia = m.launchedMedia[:0]
	m.launchedSystems = m.launchedSystems[:0]
	m.keyboardPresses = m.keyboardPresses[:0]
	m.gamepadPresses = m.gamepadPresses[:0]
}

// SetTrackedProcess stores a process handle for lifecycle management
func (m *MockPlatform) SetTrackedProcess(proc *os.Process) {
	// Mock implementation - just store for testing
	args := m.Called(proc)
	_ = args // Silence unused variable warning if no return values configured
}

// NewMockPlatform creates a new MockPlatform instance
func NewMockPlatform() *MockPlatform {
	return &MockPlatform{
		launchedMedia:   make([]string, 0),
		launchedSystems: make([]string, 0),
		keyboardPresses: make([]string, 0),
		gamepadPresses:  make([]string, 0),
	}
}

// SetupBasicMock configures the mock with typical default values for basic operations
func (m *MockPlatform) SetupBasicMock() {
	m.On("ID").Return("mock-platform")
	m.On("Settings").Return(platforms.Settings{})
	m.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{"/mock/roms"})
	m.On("SupportedReaders", mock.AnythingOfType("*config.Instance")).Return([]readers.Reader{})
	m.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{})

	// Setup common stub functions for UI methods
	noopFunc := func() error { return nil }
	m.On("ShowNotice", mock.AnythingOfType("*config.Instance"),
		mock.AnythingOfType("widgetmodels.NoticeArgs")).Return(noopFunc, time.Duration(0), nil)
	m.On("ShowLoader", mock.AnythingOfType("*config.Instance"),
		mock.AnythingOfType("widgetmodels.NoticeArgs")).Return(noopFunc, nil)
}
