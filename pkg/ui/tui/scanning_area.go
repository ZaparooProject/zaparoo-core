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

package tui

import (
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ScanState represents the current state of the scanning area.
type ScanState int

const (
	ScanStateNoReader ScanState = iota
	ScanStateWaiting
	ScanStateScanned
)

// TokenInfo holds the details of a scanned token.
type TokenInfo struct {
	Time  string
	UID   string
	Value string
}

// ReaderInfo holds the details of connected readers.
type ReaderInfo struct {
	Driver string
	Count  int
}

// ScanningArea is a custom widget that displays NFC scanning status with animation.
type ScanningArea struct {
	*tview.Box
	app        *tview.Application
	animTicker *time.Ticker
	animStop   chan struct{}
	tokenInfo  *TokenInfo
	readerInfo ReaderInfo
	state      ScanState
	animFrame  int
	mu         syncutil.Mutex
}

// NewScanningArea creates a new scanning area widget.
func NewScanningArea(app *tview.Application) *ScanningArea {
	return &ScanningArea{
		Box:   tview.NewBox(),
		app:   app,
		state: ScanStateNoReader,
	}
}

// SetState changes the current state and manages animation.
func (sa *ScanningArea) SetState(state ScanState) *ScanningArea {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	oldState := sa.state
	sa.state = state

	// Start animation when entering waiting state
	if state == ScanStateWaiting && oldState != ScanStateWaiting {
		sa.startAnimation()
	}

	// Stop animation when leaving waiting state
	if state != ScanStateWaiting && oldState == ScanStateWaiting {
		sa.stopAnimation()
	}

	return sa
}

// SetReaderInfo updates the reader info and adjusts state accordingly.
func (sa *ScanningArea) SetReaderInfo(count int, driver string) *ScanningArea {
	sa.mu.Lock()
	sa.readerInfo = ReaderInfo{Count: count, Driver: driver}
	currentState := sa.state
	sa.mu.Unlock()

	if count == 0 {
		sa.SetState(ScanStateNoReader)
	} else if currentState == ScanStateNoReader {
		sa.SetState(ScanStateWaiting)
	}

	return sa
}

// SetTokenInfo sets the scanned token details and changes to scanned state.
func (sa *ScanningArea) SetTokenInfo(scanTime, uid, value string) *ScanningArea {
	sa.mu.Lock()
	sa.tokenInfo = &TokenInfo{
		Time:  scanTime,
		UID:   uid,
		Value: value,
	}
	sa.mu.Unlock()

	sa.SetState(ScanStateScanned)
	return sa
}

// ClearToken clears token info and returns to waiting state if reader connected.
func (sa *ScanningArea) ClearToken() *ScanningArea {
	sa.mu.Lock()
	sa.tokenInfo = nil
	readerCount := sa.readerInfo.Count
	sa.mu.Unlock()

	if readerCount > 0 {
		sa.SetState(ScanStateWaiting)
	} else {
		sa.SetState(ScanStateNoReader)
	}
	return sa
}

// Stop stops the animation ticker (call when leaving the page).
func (sa *ScanningArea) Stop() {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.stopAnimation()
}

// startAnimation starts the animation ticker goroutine.
// Must be called with mutex held.
func (sa *ScanningArea) startAnimation() {
	if sa.animTicker != nil {
		return // Already running
	}

	sa.animTicker = time.NewTicker(200 * time.Millisecond)
	sa.animStop = make(chan struct{})

	// Capture local references to avoid race with stopAnimation setting these to nil
	ticker := sa.animTicker
	stop := sa.animStop

	go func() {
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				sa.mu.Lock()
				sa.animFrame = (sa.animFrame + 1) % 6
				sa.mu.Unlock()
				sa.app.QueueUpdateDraw(func() {})
			}
		}
	}()
}

// stopAnimation stops the animation ticker.
// Must be called with mutex held.
func (sa *ScanningArea) stopAnimation() {
	if sa.animTicker != nil {
		sa.animTicker.Stop()
		close(sa.animStop)
		sa.animTicker = nil
		sa.animStop = nil
	}
}

// Draw renders the scanning area based on current state.
func (sa *ScanningArea) Draw(screen tcell.Screen) {
	sa.DrawForSubclass(screen, sa)

	x, y, width, height := sa.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	sa.mu.Lock()
	state := sa.state
	frame := sa.animFrame
	tokenInfo := sa.tokenInfo
	readerInfo := sa.readerInfo
	sa.mu.Unlock()

	t := CurrentTheme()

	// Draw reader status at top
	readerY := y
	drawReaderStatus(screen, x, readerY, width, readerInfo, t)

	// Footer message at bottom
	footerMsg := "Media won't launch when TUI is open"
	if height >= 2 {
		footerY := y + height - 1
		footerStyle := tcell.StyleDefault.
			Foreground(t.SecondaryTextColor).
			Background(t.PrimitiveBackgroundColor)
		drawCenteredText(screen, x, footerY, width, footerMsg, footerStyle)
	}

	// Content area (between reader status and footer)
	contentY := y + 2           // After reader status + blank line
	contentHeight := height - 4 // Remove reader status, blank, footer, blank

	switch state {
	case ScanStateNoReader:
		drawNoReader(screen, x, contentY, width, contentHeight, t)
	case ScanStateWaiting:
		drawWaiting(screen, x, contentY, width, contentHeight, frame, t)
	case ScanStateScanned:
		drawScanned(screen, x, contentY, width, contentHeight, tokenInfo, t)
	}
}

// drawReaderStatus renders the reader status at the top with status indicator.
func drawReaderStatus(
	screen tcell.Screen,
	x, y, width int,
	info ReaderInfo,
	t *Theme,
) {
	// Status indicator dot - green when connected, dim when none
	var dotColor tcell.Color
	var status string
	switch info.Count {
	case 0:
		dotColor = t.SecondaryTextColor
		status = "No readers"
	case 1:
		dotColor = t.SuccessColor
		status = fmt.Sprintf("1 reader (%s)", info.Driver)
	default:
		dotColor = t.SuccessColor
		status = fmt.Sprintf("%d readers", info.Count)
	}

	// Truncate if too long (accounting for dot + space)
	maxStatusLen := width - 3
	if len(status) > maxStatusLen && maxStatusLen > 3 {
		status = status[:maxStatusLen-3] + "..."
	}

	dotStyle := tcell.StyleDefault.
		Foreground(dotColor).
		Background(t.PrimitiveBackgroundColor)
	textStyle := tcell.StyleDefault.
		Foreground(t.PrimaryTextColor).
		Background(t.PrimitiveBackgroundColor)

	screen.SetContent(x, y, tcell.RuneDiamond, nil, dotStyle)
	screen.SetContent(x+1, y, ' ', nil, textStyle)

	// Draw status text
	for i, r := range status {
		if x+2+i < x+width {
			screen.SetContent(x+2+i, y, r, nil, textStyle)
		}
	}
}

// drawNoReader renders the "no reader connected" state.
func drawNoReader(
	screen tcell.Screen,
	x, y, width, height int,
	t *Theme,
) {
	if height < 1 {
		return
	}

	msg := "No reader connected"
	centerY := y + height/2
	style := tcell.StyleDefault.
		Foreground(t.SecondaryTextColor).
		Background(t.PrimitiveBackgroundColor)
	drawCenteredText(screen, x, centerY, width, msg, style)
}

// drawWaiting renders the waiting state with animation.
func drawWaiting(
	screen tcell.Screen,
	x, y, width, height int,
	frame int,
	t *Theme,
) {
	if height < 2 {
		return
	}

	// Calculate vertical center for wave + text
	centerY := y + height/2 - 1

	// Draw animated wave with color gradient (inner bright, outer dim)
	drawWaveFrame(screen, x, centerY, width, frame, t)

	// Draw instruction text below (white/primary color)
	textStyle := tcell.StyleDefault.
		Foreground(t.PrimaryTextColor).
		Background(t.PrimitiveBackgroundColor)
	drawCenteredText(screen, x, centerY+2, width, "Place tag on reader", textStyle)
}

// drawWaveFrame draws a single frame of the wave animation with gradient colors.
func drawWaveFrame(screen tcell.Screen, x, y, width, frame int, t *Theme) {
	// Wave patterns for each frame
	// Frame 0: •
	// Frame 1: )•(
	// Frame 2: ))•((
	// Frame 3: )))•(((
	// Frame 4: ))•((
	// Frame 5: )•(

	// Color styles: center brightest (border/accent), outer dimmer (secondary)
	centerStyle := tcell.StyleDefault.
		Foreground(t.BorderColor).
		Background(t.PrimitiveBackgroundColor).
		Bold(true)
	innerStyle := tcell.StyleDefault.
		Foreground(t.PrimaryTextColor).
		Background(t.PrimitiveBackgroundColor)
	outerStyle := tcell.StyleDefault.
		Foreground(t.SecondaryTextColor).
		Background(t.PrimitiveBackgroundColor)

	type waveChar struct {
		style tcell.Style
		char  rune
	}

	// Build wave pattern based on frame
	dot := tcell.RuneDiamond
	var wave []waveChar
	switch frame {
	case 0:
		wave = []waveChar{{char: dot, style: centerStyle}}
	case 1, 5:
		wave = []waveChar{
			{char: ')', style: innerStyle},
			{char: dot, style: centerStyle},
			{char: '(', style: innerStyle},
		}
	case 2, 4:
		wave = []waveChar{
			{char: ')', style: outerStyle},
			{char: ')', style: innerStyle},
			{char: dot, style: centerStyle},
			{char: '(', style: innerStyle},
			{char: '(', style: outerStyle},
		}
	case 3:
		wave = []waveChar{
			{char: ')', style: outerStyle},
			{char: ')', style: outerStyle},
			{char: ')', style: innerStyle},
			{char: dot, style: centerStyle},
			{char: '(', style: innerStyle},
			{char: '(', style: outerStyle},
			{char: '(', style: outerStyle},
		}
	}

	// Center the wave
	startX := x + (width-len(wave))/2
	if startX < x {
		startX = x
	}

	for i, wc := range wave {
		if startX+i < x+width {
			screen.SetContent(startX+i, y, wc.char, nil, wc.style)
		}
	}
}

// drawScanned renders the scanned token details with minimal accent style.
func drawScanned(
	screen tcell.Screen,
	x, y, width, height int,
	info *TokenInfo,
	t *Theme,
) {
	if info == nil || height < 3 {
		return
	}

	// Labels use LabelColor + bold, values use accent color
	labelStyle := tcell.StyleDefault.
		Foreground(t.LabelColor).
		Background(t.PrimitiveBackgroundColor).
		Bold(true)
	valueStyle := tcell.StyleDefault.
		Foreground(t.BorderColor).
		Background(t.PrimitiveBackgroundColor)

	const labelWidth = 7 // "Value: " is longest at 7 chars
	maxValueWidth := width - labelWidth

	// Draw Time
	currentY := y
	drawTokenLine(screen, x, currentY, "Time", info.Time, labelWidth, maxValueWidth, labelStyle, valueStyle)
	currentY++

	// Draw UID
	if currentY < y+height {
		drawTokenLine(screen, x, currentY, "UID", info.UID, labelWidth, maxValueWidth, labelStyle, valueStyle)
		currentY++
	}

	// Draw Value with word wrapping
	if currentY < y+height {
		drawLabel(screen, x, currentY, "Value", labelWidth, labelStyle)
		// Wrap the value text
		wrapped := wrapText(info.Value, maxValueWidth)
		for i, line := range wrapped {
			if currentY+i >= y+height {
				break
			}
			drawValue(screen, x+labelWidth, currentY+i, line, maxValueWidth, valueStyle)
		}
	}
}

// drawTokenLine draws a single label: value line.
func drawTokenLine(
	screen tcell.Screen, x, y int,
	label, value string,
	labelWidth, maxValueWidth int,
	labelStyle, valueStyle tcell.Style,
) {
	drawLabel(screen, x, y, label, labelWidth, labelStyle)
	drawValue(screen, x+labelWidth, y, value, maxValueWidth, valueStyle)
}

// drawLabel draws a right-padded label with colon.
func drawLabel(screen tcell.Screen, x, y int, label string, labelWidth int, style tcell.Style) {
	padded := label + ":"
	for len(padded) < labelWidth {
		padded += " "
	}
	for i, r := range padded {
		screen.SetContent(x+i, y, r, nil, style)
	}
}

// drawValue draws a value, truncating if needed.
func drawValue(screen tcell.Screen, x, y int, value string, maxWidth int, style tcell.Style) {
	display := value
	if len(display) > maxWidth && maxWidth > 3 {
		display = display[:maxWidth-3] + "..."
	}
	for i, r := range display {
		if i < maxWidth {
			screen.SetContent(x+i, y, r, nil, style)
		}
	}
}

// wrapText wraps text to fit within maxWidth, breaking on spaces when possible.
func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return nil
	}
	if len(text) <= maxWidth {
		return []string{text}
	}

	// Estimate number of lines needed
	lines := make([]string, 0, (len(text)/maxWidth)+1)
	remaining := text

	for remaining != "" {
		if len(remaining) <= maxWidth {
			lines = append(lines, remaining)
			break
		}

		// Find a good break point (last space within maxWidth)
		breakAt := maxWidth
		for i := maxWidth - 1; i > 0; i-- {
			if remaining[i] == ' ' {
				breakAt = i
				break
			}
		}

		lines = append(lines, remaining[:breakAt])
		remaining = remaining[breakAt:]
		// Skip leading space on next line
		for remaining != "" && remaining[0] == ' ' {
			remaining = remaining[1:]
		}
	}

	return lines
}

// drawCenteredText draws text centered horizontally within the given bounds.
func drawCenteredText(screen tcell.Screen, x, y, width int, text string, style tcell.Style) {
	textLen := len(text)
	startX := x + (width-textLen)/2
	if startX < x {
		startX = x
	}
	for i, r := range text {
		if startX+i < x+width {
			screen.SetContent(startX+i, y, r, nil, style)
		}
	}
}
