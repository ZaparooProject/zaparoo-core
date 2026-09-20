// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

package tui

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	testingmocks "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMainPage_ShowsAppFooter(t *testing.T) {
	mainPageNotifyState.Cancel()
	t.Cleanup(mainPageNotifyState.Cancel)

	runner := NewTestAppRunner(t, 75, 15)
	defer runner.Stop()

	platform := testingmocks.NewMockPlatform()
	platform.On("ID").Return("test")
	// The stopped status block reads the tail of the log for the last error.
	platform.On("Settings").Return(platforms.Settings{LogDir: t.TempDir(), DataDir: t.TempDir()})
	pages := tview.NewPages()
	// Not a bare config: the main page asks the health route what the service
	// is doing, and a default config points at the port a real Core would be
	// on, which would make this test depend on the machine running it.
	BuildMainPage(
		serveHealthState(t, ""), pages, runner.App(), platform,
		func() bool { return false }, "", "", nil,
	)

	runner.Start(pages)

	const expectedFooterText = "Connect with the Zaparoo App (iOS/Android): https://zaparoo.app/"
	require.True(t, runner.WaitForText(
		expectedFooterText,
		uiSettleTimeout,
	))

	screenLines := strings.Split(runner.GetScreenText(), "\n")
	footerX := -1
	for _, line := range screenLines {
		if byteIndex := strings.Index(line, expectedFooterText); byteIndex >= 0 {
			footerX = utf8.RuneCountInString(line[:byteIndex])
			break
		}
	}
	require.GreaterOrEqual(t, footerX, 0)
	_, screenWidth, _ := runner.Screen().GetContents()
	assert.Equal(t, (screenWidth-utf8.RuneCountInString(expectedFooterText))/2, footerX)

	var introY, visitX, supportX int
	foundIntro := false
	for y, line := range screenLines {
		visitX = strings.Index(line, "Visit")
		supportX = strings.Index(line, "for guides")
		if visitX >= 0 && supportX >= 0 {
			introY = y
			foundIntro = true
			break
		}
	}
	require.True(t, foundIntro)
	visitStyle := runner.Screen().GetCellStyle(visitX, introY)
	supportStyle := runner.Screen().GetCellStyle(supportX, introY)
	visitForeground, visitBackground, visitAttributes := visitStyle.Decompose()
	supportForeground, supportBackground, supportAttributes := supportStyle.Decompose()
	assert.Equal(t, visitForeground, supportForeground)
	assert.Equal(t, visitBackground, supportBackground)
	assert.Equal(t, visitAttributes, supportAttributes)
	assert.Equal(t, visitStyle.GetUnderlineStyle(), supportStyle.GetUnderlineStyle())

	platform.AssertExpectations(t)
}

func TestMainPageState_SetCancel(t *testing.T) {
	t.Parallel()

	state := &mainPageState{}
	cancelCalled := false

	// Set a cancel function
	state.SetCancel(func() {
		cancelCalled = true
	})

	// Cancel should work
	state.Cancel()
	assert.True(t, cancelCalled)

	// Cancel again should be safe (nil check)
	state.Cancel()
}

func TestMainPageState_SetCancelReplacesOld(t *testing.T) {
	t.Parallel()

	state := &mainPageState{}
	firstCancelCalled := false
	secondCancelCalled := false

	// Set first cancel
	state.SetCancel(func() {
		firstCancelCalled = true
	})

	// Set second cancel (should call first)
	state.SetCancel(func() {
		secondCancelCalled = true
	})

	// First should have been called when replaced
	assert.True(t, firstCancelCalled)
	assert.False(t, secondCancelCalled)

	// Now cancel should call second
	state.Cancel()
	assert.True(t, secondCancelCalled)
}

func TestMainPageState_Concurrent(t *testing.T) {
	t.Parallel()

	state := &mainPageState{}
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent SetCancel and Cancel operations
	for range iterations {
		wg.Add(2)
		go func() {
			defer wg.Done()
			state.SetCancel(func() {})
		}()
		go func() {
			defer wg.Done()
			state.Cancel()
		}()
	}

	wg.Wait()
	// Just verify no race conditions - no assertion needed
}

func TestButtonGrid_NewButtonGrid(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	require.NotNil(t, grid)
	assert.NotNil(t, grid.Box)
	assert.Equal(t, 3, grid.cols)
	assert.Len(t, grid.buttons, 2) // 2 rows
}

func TestButtonGrid_AddRow(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}

	// Add first row
	grid.AddRow(btn1, btn2)
	assert.Len(t, grid.buttons[0], 2)
	assert.Nil(t, grid.buttons[1])

	// Add second row
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}
	grid.AddRow(btn3)
	assert.Len(t, grid.buttons[1], 1)
}

func TestButtonGrid_SetOnHelp(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 2)

	helpReceived := ""
	grid.SetOnHelp(func(help string) {
		helpReceived = help
	})

	// Trigger help callback manually
	if grid.onHelp != nil {
		grid.onHelp("Test help text")
	}

	assert.Equal(t, "Test help text", helpReceived)
}

func TestButtonGrid_SetOnEscape(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 2)

	escapeCalled := false
	grid.SetOnEscape(func() {
		escapeCalled = true
	})

	// Trigger escape callback manually
	if grid.onEscape != nil {
		grid.onEscape()
	}

	assert.True(t, escapeCalled)
}

func TestButtonGridItem_Disabled(t *testing.T) {
	t.Parallel()

	item := &ButtonGridItem{
		Button:   tview.NewButton("Test"),
		HelpText: "Test help",
		Disabled: true,
	}

	assert.True(t, item.Disabled)
	assert.Equal(t, "Test help", item.HelpText)
}

func TestButtonGrid_FocusFirst(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	grid.FocusFirst()

	row, col := grid.GetFocus()
	assert.Equal(t, 0, row, "Should focus first row")
	assert.Equal(t, 0, col, "Should focus first column")
}

func TestButtonGrid_FocusFirst_SkipsDisabled(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	// First button is disabled
	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1", Disabled: true}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	grid.FocusFirst()

	row, col := grid.GetFocus()
	assert.Equal(t, 0, row, "Should focus first row")
	assert.Equal(t, 1, col, "Should skip disabled and focus second column")
}

func TestButtonGrid_SetFocus(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	grid.SetFocus(0, 2)

	row, col := grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 2, col)
}

func TestButtonGrid_SetFocus_FallsBackOnDisabled(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2", Disabled: true}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	// Try to set focus on disabled button
	grid.SetFocus(0, 1)

	// Should fall back to first enabled
	row, col := grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 0, col, "Should fall back to first enabled button")
}

func TestButtonGrid_isCurrentEnabled(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2", Disabled: true}

	grid.AddRow(btn1, btn2)

	grid.focusedRow = 0
	grid.focusedCol = 0
	assert.True(t, grid.isCurrentEnabled(), "First button should be enabled")

	grid.focusedCol = 1
	assert.False(t, grid.isCurrentEnabled(), "Second button should be disabled")
}

func TestButtonGrid_HelpCallback_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	var helpMu syncutil.Mutex
	var helpTexts []string
	grid.SetOnHelp(func(text string) {
		helpMu.Lock()
		helpTexts = append(helpTexts, text)
		helpMu.Unlock()
	})

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help for button 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help for button 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help for button 3"}

	grid.AddRow(btn1, btn2, btn3)

	runner.Start(grid)
	runner.SetFocus(grid)

	// Helper to safely get helpTexts
	getHelpTexts := func() []string {
		helpMu.Lock()
		defer helpMu.Unlock()
		return append([]string(nil), helpTexts...)
	}

	// Helper to check if text is in help texts
	containsHelpText := func(text string) bool {
		for _, h := range getHelpTexts() {
			if h == text {
				return true
			}
		}
		return false
	}

	// Should trigger help for first button on focus
	assert.True(t, runner.WaitForCondition(func() bool {
		return containsHelpText("Help for button 1")
	}, uiSettleTimeout), "Should receive help for first button")

	// Navigate right
	runner.SimulateArrowRight()

	assert.True(t, runner.WaitForCondition(func() bool {
		return containsHelpText("Help for button 2")
	}, uiSettleTimeout), "Should receive help for second button")
}

func TestButtonGrid_Navigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}
	btn4 := &ButtonGridItem{Button: tview.NewButton("Btn4"), HelpText: "Help 4"}
	btn5 := &ButtonGridItem{Button: tview.NewButton("Btn5"), HelpText: "Help 5"}

	grid.AddRow(btn1, btn2, btn3)
	grid.AddRow(btn4, btn5, nil)

	runner.Start(grid)
	runner.SetFocus(grid)

	// Initial focus should be (0, 0)
	row, col := grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 0, col)

	// Navigate right
	runner.SimulateArrowRight()
	row, col = grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 1, col)

	// Navigate down
	runner.SimulateArrowDown()
	row, col = grid.GetFocus()
	assert.Equal(t, 1, row)
	assert.Equal(t, 1, col)

	// Navigate left
	runner.SimulateArrowLeft()
	row, col = grid.GetFocus()
	assert.Equal(t, 1, row)
	assert.Equal(t, 0, col)

	// Navigate up
	runner.SimulateArrowUp()
	row, col = grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 0, col)
}

func TestButtonGrid_TabNavigation_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	runner.Start(grid)
	runner.SetFocus(grid)

	// Tab should navigate right
	runner.SimulateTab()
	row, col := grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 1, col)

	// Backtab should navigate left
	runner.SimulateBacktab()
	row, col = grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 0, col)
}

func TestButtonGrid_EscapeCallback_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	escapeCalled := make(chan struct{}, 1)
	grid.SetOnEscape(func() {
		select {
		case escapeCalled <- struct{}{}:
		default:
		}
	})

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	grid.AddRow(btn1)

	runner.Start(grid)
	runner.SetFocus(grid)

	runner.SimulateEscape()

	assert.True(t, runner.WaitForSignal(escapeCalled, uiSettleTimeout), "Escape callback should be called")
}

func TestButtonGrid_EnterActivatesButton_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	buttonPressed := make(chan struct{}, 1)
	btn1 := &ButtonGridItem{
		Button: tview.NewButton("Btn1").SetSelectedFunc(func() {
			select {
			case buttonPressed <- struct{}{}:
			default:
			}
		}),
		HelpText: "Help 1",
	}
	grid.AddRow(btn1)

	runner.Start(grid)
	runner.SetFocus(grid)

	runner.SimulateEnter()

	assert.True(t, runner.WaitForSignal(buttonPressed, uiSettleTimeout), "Button should be activated on Enter")
}

func TestButtonGrid_DisabledButtonsSkipped_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2", Disabled: true}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	runner.Start(grid)
	runner.SetFocus(grid)

	// Initial focus (0, 0)
	_, col := grid.GetFocus()
	assert.Equal(t, 0, col)

	// Navigate right - should skip disabled btn2 and go to btn3
	runner.SimulateArrowRight()
	row, col := grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 2, col, "Should skip disabled button and go to third")
}

func TestMainFrame_Delegates(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView().SetText("Test content")
	frame := NewMainFrame(content)

	assert.NotNil(t, frame.content)

	// InputHandler should delegate to content
	handler := frame.InputHandler()
	assert.NotNil(t, handler)
}

func TestMainFrame_HasFocus(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView()
	frame := NewMainFrame(content)

	// HasFocus should delegate to content
	hasFocus := frame.HasFocus()
	assert.False(t, hasFocus, "Content should not have focus initially")
}

func TestMainFrame_NilContent(t *testing.T) {
	t.Parallel()

	frame := NewMainFrame(nil)

	// Should handle nil content gracefully
	handler := frame.InputHandler()
	assert.Nil(t, handler)

	hasFocus := frame.HasFocus()
	assert.False(t, hasFocus)
}

func TestMainFrame_MouseHandler(t *testing.T) {
	t.Parallel()

	content := tview.NewTextView()
	frame := NewMainFrame(content)

	handler := frame.MouseHandler()
	assert.NotNil(t, handler)
}

func TestButtonGrid_WrapAround_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 80, 25)
	defer runner.Stop()

	app := runner.App()
	grid := NewButtonGrid(app, 3)

	btn1 := &ButtonGridItem{Button: tview.NewButton("Btn1"), HelpText: "Help 1"}
	btn2 := &ButtonGridItem{Button: tview.NewButton("Btn2"), HelpText: "Help 2"}
	btn3 := &ButtonGridItem{Button: tview.NewButton("Btn3"), HelpText: "Help 3"}

	grid.AddRow(btn1, btn2, btn3)

	runner.Start(grid)
	runner.SetFocus(grid)

	// Navigate to the last column
	runner.SimulateArrowRight()
	runner.SimulateArrowRight()

	_, col := grid.GetFocus()
	assert.Equal(t, 2, col, "Should be at last column")

	// Navigate right again - should wrap to first
	runner.SimulateArrowRight()
	row, col := grid.GetFocus()
	assert.Equal(t, 0, row)
	assert.Equal(t, 0, col, "Should wrap to first column")
}

// serveHealthState runs a stand-in for Core's health route on a free port and
// returns a config pointing at it. resolveServiceCondition asks that route
// what the service is doing, so this is what decides which buttons the main
// page offers.
func serveHealthState(t *testing.T, state string) *config.Instance {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	if state == "" {
		// Nothing listening: the caller wants the no-answer path.
		require.NoError(t, listener.Close())
	} else {
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, `{"status":"ok","state":%q}`, state)
		})
		server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		})
	}

	cfg, err := testhelpers.NewTestConfigWithListenAndPort(nil, t.TempDir(), "127.0.0.1", addr.Port)
	require.NoError(t, err)
	return cfg
}

// The log page used to live behind Settings, which is disabled whenever the
// service is not running — so the one thing a user needs in order to report a
// problem was locked behind the thing that had broken. In that state the slot
// becomes the log page itself.
func TestBuildMainPage_OffersLogsWhenTheServiceIsNotRunning(t *testing.T) {
	mainPageNotifyState.Cancel()
	t.Cleanup(mainPageNotifyState.Cancel)

	runner := NewTestAppRunner(t, 75, 15)
	defer runner.Stop()

	platform := testingmocks.NewMockPlatform()
	platform.On("ID").Return("test")
	platform.On("Settings").Return(platforms.Settings{LogDir: t.TempDir(), DataDir: t.TempDir()})

	pages := tview.NewPages()
	BuildMainPage(
		serveHealthState(t, ""), pages, runner.App(), platform,
		func() bool { return false }, "", "", nil,
	)
	runner.Start(pages)

	require.True(t, runner.WaitForText("Logs", uiSettleTimeout),
		"the log page has to be reachable when the service is down")
	assert.NotContains(t, runner.GetScreenText(), "Settings",
		"Settings is disabled in this state, so its slot is the log page instead")
}

// When the service is up, the slot is Settings as before and the log page is
// reached through it.
func TestBuildMainPage_OffersSettingsWhenTheServiceIsRunning(t *testing.T) {
	mainPageNotifyState.Cancel()
	t.Cleanup(mainPageNotifyState.Cancel)

	runner := NewTestAppRunner(t, 75, 15)
	defer runner.Stop()

	platform := testingmocks.NewMockPlatform()
	platform.On("ID").Return("test")
	platform.On("Settings").Return(platforms.Settings{LogDir: t.TempDir(), DataDir: t.TempDir()}).Maybe()

	pages := tview.NewPages()
	BuildMainPage(
		serveHealthState(t, "ready"), pages, runner.App(), platform,
		func() bool { return true }, "", "", nil,
	)
	runner.Start(pages)

	require.True(t, runner.WaitForText("Settings", uiSettleTimeout))
}

// Naming the button is not the same as being able to use it. The whole point of
// the slot change is that a user with a dead service can get their log out, so
// this drives it the way they would: press Logs, land on the log page, press
// escape, land back on the main page.
func TestBuildMainPage_LogsButtonOpensTheLogPageAndComesBack(t *testing.T) {
	mainPageNotifyState.Cancel()
	t.Cleanup(mainPageNotifyState.Cancel)

	runner := NewTestAppRunner(t, 75, 15)
	defer runner.Stop()

	logDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(logDir, config.LogFile), []byte(`{"level":"error","message":"boom"}`+"\n"), 0o600,
	))

	platform := testingmocks.NewMockPlatform()
	platform.On("ID").Return("test")
	platform.On("Settings").Return(platforms.Settings{LogDir: logDir, DataDir: t.TempDir()})

	pages := tview.NewPages()
	BuildMainPage(
		serveHealthState(t, ""), pages, runner.App(), platform,
		func() bool { return false }, "", "", nil,
	)
	runner.Start(pages)

	// The help line under the grid names whichever button holds focus, so it is
	// how this checks the Logs slot is enabled — and it is checked before any
	// key is sent, because with the slot disabled focus falls through to Exit
	// and a blind Enter would stop the application.
	require.True(t, runner.WaitForText("View and upload log files", uiSettleTimeout),
		"the Logs slot has to be enabled and focusable, not just drawn")

	runner.SimulateEnter()
	require.True(t, runner.WaitForText("Export Logs", uiSettleTimeout),
		"pressing Logs has to open the log page")

	runner.SimulateEscape()
	require.True(t, runner.WaitForText("NOT RUNNING", uiSettleTimeout),
		"escape has to land back on the main page, not strand the user on the log page")
	name, _ := pages.GetFrontPage()
	assert.Equal(t, PageMain, name)
}
