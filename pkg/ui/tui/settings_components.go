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

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func setupInputFieldFocus(field *tview.InputField) *tview.InputField {
	field.SetFieldBackgroundColor(CurrentTheme().FieldUnfocusedBg)
	field.SetFocusFunc(func() {
		field.SetFieldBackgroundColor(CurrentTheme().FieldFocusedBg)
	})
	field.SetBlurFunc(func() {
		field.SetFieldBackgroundColor(CurrentTheme().FieldUnfocusedBg)
	})
	return field
}

// ExitDelayOption pairs a display label with its numeric value.
type ExitDelayOption struct {
	Label string
	Value float32
}

// ExitDelayOptions provides structured exit delay choices.
var ExitDelayOptions = []ExitDelayOption{
	{Label: "0 seconds", Value: 0},
	{Label: "1 second", Value: 1},
	{Label: "2 seconds", Value: 2},
	{Label: "3 seconds", Value: 3},
	{Label: "5 seconds", Value: 5},
	{Label: "10 seconds", Value: 10},
	{Label: "15 seconds", Value: 15},
	{Label: "20 seconds", Value: 20},
	{Label: "30 seconds", Value: 30},
}

// formatToggle renders a toggle value. When selected, label is highlighted.
func formatToggle(value bool, label string, selected bool) string {
	t := CurrentTheme()
	checkbox := "[ ]"
	if value {
		checkbox = "[*]"
	}
	if selected {
		return fmt.Sprintf("[%s:%s]- %s [%s:%s]%s[-:%s]",
			t.AccentColorName, t.BgColorName, checkbox,
			t.HighlightFgName, t.HighlightBgName, label, t.BgColorName)
	}
	return fmt.Sprintf("[%s:%s]- %s [%s:%s]%s[-:-]",
		t.AccentColorName, t.BgColorName, checkbox,
		t.TextColorName, t.BgColorName, label)
}

// formatCycle renders a cycle value. When selected, label and value are highlighted.
func formatCycle(label, currentValue string, selected bool) string {
	t := CurrentTheme()
	if selected {
		return fmt.Sprintf("[%s:%s]- [%s:%s]%s: < %s >[-:%s]",
			t.AccentColorName, t.BgColorName,
			t.HighlightFgName, t.HighlightBgName, label, currentValue, t.BgColorName)
	}
	return fmt.Sprintf("[%s:%s]- [%s:%s]%s: < %s >[-:-]",
		t.AccentColorName, t.BgColorName,
		t.TextColorName, t.BgColorName, label, currentValue)
}

// formatAction renders an action item. When selected, label is highlighted.
func formatAction(label string, selected bool) string {
	t := CurrentTheme()
	if selected {
		return fmt.Sprintf("[%s:%s]- [%s:%s]%s[-:%s]",
			t.AccentColorName, t.BgColorName,
			t.HighlightFgName, t.HighlightBgName, label, t.BgColorName)
	}
	return fmt.Sprintf("[%s:%s]- [%s:%s]%s[-:-]",
		t.AccentColorName, t.BgColorName,
		t.TextColorName, t.BgColorName, label)
}

// formatNavAction renders a navigation action with arrow indicator.
func formatNavAction(label string, selected bool) string {
	t := CurrentTheme()
	if selected {
		return fmt.Sprintf("[%s:%s]→ [%s:%s]%s[-:%s]",
			t.AccentColorName, t.BgColorName,
			t.HighlightFgName, t.HighlightBgName, label, t.BgColorName)
	}
	return fmt.Sprintf("[%s:%s]→ [%s:%s]%s[-:-]",
		t.AccentColorName, t.BgColorName,
		t.TextColorName, t.BgColorName, label)
}

// formatDesc renders a description with 2-space indent.
func formatDesc(desc string) string {
	return "  " + desc
}

// settingsItem stores data for a single list item.
type settingsItem struct {
	toggleValue  *bool
	cycleIndex   *int
	itemType     string
	label        string
	description  string
	cycleOptions []string
}

// SettingsList wraps a tview.List with consistent navigation and manual highlight management.
type SettingsList struct {
	*tview.List
	pages           *tview.Pages
	rebuildPrevious func()
	helpCallback    func(string)
	previousPage    string
	items           []settingsItem
	dynamicHelpMode bool
	hasFocus        bool
}

// NewSettingsList creates a new settings list with arrow key navigation.
func NewSettingsList(pages *tview.Pages, previousPage string) *SettingsList {
	list := tview.NewList()
	list.SetSecondaryTextColor(CurrentTheme().SecondaryTextColor)
	list.ShowSecondaryText(true)
	list.SetHighlightFullLine(false)
	list.SetSelectedStyle(tcell.StyleDefault)
	list.SetMainTextStyle(tcell.StyleDefault)

	sl := &SettingsList{
		List:         list,
		pages:        pages,
		previousPage: previousPage,
		items:        make([]settingsItem, 0),
		hasFocus:     true,
	}

	list.SetChangedFunc(func(index int, _, _ string, _ rune) {
		sl.refreshAllItems(index)
	})

	list.SetFocusFunc(func() {
		sl.hasFocus = true
		sl.refreshAllItems(sl.GetCurrentItem())
	})

	list.SetBlurFunc(func() {
		sl.hasFocus = false
		sl.refreshAllItems(-1)
	})

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			sl.goBack()
			return nil
		}
		return event
	})

	return sl
}

// SetRebuildPrevious sets a callback to rebuild the previous page on Back navigation.
// When set, going back will call this function instead of just switching to the cached page.
func (sl *SettingsList) SetRebuildPrevious(fn func()) *SettingsList {
	sl.rebuildPrevious = fn
	return sl
}

// SetHelpCallback sets a callback that fires when selection changes.
// The callback receives the description of the currently selected item.
// Use this with PageFrame's SetHelpText for dynamic help.
func (sl *SettingsList) SetHelpCallback(fn func(string)) *SettingsList {
	sl.helpCallback = fn
	return sl
}

// SetDynamicHelpMode enables or disables dynamic help mode.
// When enabled, inline descriptions are hidden and the help callback is used instead.
func (sl *SettingsList) SetDynamicHelpMode(enabled bool) *SettingsList {
	sl.dynamicHelpMode = enabled
	sl.ShowSecondaryText(!enabled)
	return sl
}

// TriggerInitialHelp calls the help callback with the first item's description.
// Call this after adding all items to set the initial help text.
func (sl *SettingsList) TriggerInitialHelp() *SettingsList {
	if sl.helpCallback != nil && len(sl.items) > 0 {
		sl.helpCallback(sl.items[0].description)
	}
	return sl
}

// GetCurrentDescription returns the description of the currently selected item.
func (sl *SettingsList) GetCurrentDescription() string {
	idx := sl.GetCurrentItem()
	if idx >= 0 && idx < len(sl.items) {
		return sl.items[idx].description
	}
	return ""
}

// goBack navigates to the previous page, rebuilding it if a rebuild callback is set.
func (sl *SettingsList) goBack() {
	if sl.rebuildPrevious != nil {
		sl.rebuildPrevious()
	} else {
		sl.pages.SwitchToPage(sl.previousPage)
	}
}

// refreshAllItems updates all items to reflect current selection state.
func (sl *SettingsList) refreshAllItems(selectedIndex int) {
	for i, item := range sl.items {
		// Only show highlight when the list has focus
		selected := sl.hasFocus && i == selectedIndex
		desc := formatDesc(item.description)

		var mainText string
		switch item.itemType {
		case "toggle":
			mainText = formatToggle(*item.toggleValue, item.label, selected)
		case "cycle":
			mainText = formatCycle(item.label, item.cycleOptions[*item.cycleIndex], selected)
		case "action":
			mainText = formatAction(item.label, selected)
		case "nav":
			mainText = formatNavAction(item.label, selected)
		}

		sl.SetItemText(i, mainText, desc)
	}

	// Call help callback with selected item's description
	if sl.helpCallback != nil && selectedIndex >= 0 && selectedIndex < len(sl.items) {
		sl.helpCallback(sl.items[selectedIndex].description)
	}
}

// AddToggle adds a boolean toggle item to the list.
func (sl *SettingsList) AddToggle(
	label string,
	description string,
	value *bool,
	onChange func(bool),
) *SettingsList {
	index := sl.GetItemCount()
	selected := index == 0 // First item is selected by default

	sl.items = append(sl.items, settingsItem{
		itemType:    "toggle",
		label:       label,
		description: description,
		toggleValue: value,
	})

	sl.AddItem(formatToggle(*value, label, selected), formatDesc(description), 0, func() {
		*value = !*value
		onChange(*value)
		sl.refreshAllItems(sl.GetCurrentItem())
	})

	return sl
}

// AddCycle adds an inline cycle selector to the list.
func (sl *SettingsList) AddCycle(
	label string,
	description string,
	options []string,
	currentIndex *int,
	onChange func(string, int),
) *SettingsList {
	index := sl.GetItemCount()
	selected := index == 0

	sl.items = append(sl.items, settingsItem{
		itemType:     "cycle",
		label:        label,
		description:  description,
		cycleOptions: options,
		cycleIndex:   currentIndex,
	})

	sl.AddItem(formatCycle(label, options[*currentIndex], selected), formatDesc(description), 0, func() {
		*currentIndex = (*currentIndex + 1) % len(options)
		onChange(options[*currentIndex], *currentIndex)
		sl.refreshAllItems(sl.GetCurrentItem())
	})

	return sl
}

// AddAction adds a simple action item (like a submenu link or button).
func (sl *SettingsList) AddAction(
	label string,
	description string,
	action func(),
) *SettingsList {
	index := sl.GetItemCount()
	selected := index == 0

	sl.items = append(sl.items, settingsItem{
		itemType:    "action",
		label:       label,
		description: description,
	})

	sl.AddItem(formatAction(label, selected), formatDesc(description), 0, action)
	return sl
}

// AddNavAction adds a navigation action that opens a submenu or new page.
// Displays with a ">" prefix to indicate navigation.
func (sl *SettingsList) AddNavAction(
	label string,
	description string,
	action func(),
) *SettingsList {
	index := sl.GetItemCount()
	selected := index == 0

	sl.items = append(sl.items, settingsItem{
		itemType:    "nav",
		label:       label,
		description: description,
	})

	sl.AddItem(formatNavAction(label, selected), formatDesc(description), 0, action)
	return sl
}

// AddBack adds a "Back" action item with default description.
func (sl *SettingsList) AddBack() *SettingsList {
	return sl.AddBackWithDesc("Return to previous menu")
}

// AddBackWithDesc adds a "Back" action item with custom description.
func (sl *SettingsList) AddBackWithDesc(description string) *SettingsList {
	index := sl.GetItemCount()
	selected := index == 0

	sl.items = append(sl.items, settingsItem{
		itemType:    "action",
		label:       "Back",
		description: description,
	})

	sl.AddItem(formatAction("Back", selected), formatDesc(description), 0, func() {
		sl.goBack()
	})
	return sl
}

// SetupCycleKeys adds Left/Right key handling for cycle items.
func (sl *SettingsList) SetupCycleKeys(
	cycleIndices map[int]func(delta int),
) *SettingsList {
	originalCapture := sl.GetInputCapture()

	sl.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		currentItem := sl.GetCurrentItem()

		switch event.Key() {
		case tcell.KeyLeft:
			if cycleFn, ok := cycleIndices[currentItem]; ok {
				cycleFn(-1)
				return nil
			}
		case tcell.KeyRight:
			if cycleFn, ok := cycleIndices[currentItem]; ok {
				cycleFn(1)
				return nil
			}
		default:
			// Let other keys pass through
		}

		if originalCapture != nil {
			return originalCapture(event)
		}
		return event
	})

	return sl
}

// ButtonBar creates a horizontal bar of buttons with arrow key navigation.
type ButtonBar struct {
	*tview.Box
	app          *tview.Application
	onEscape     func()
	onUp         func()
	onDown       func()
	onWrap       func()
	onLeft       func()
	onRight      func()
	helpCallback func(string)
	buttons      []*tview.Button
	helpTexts    []string
	focusedIndex int
}

// NewButtonBar creates a new button bar.
func NewButtonBar(app *tview.Application) *ButtonBar {
	return &ButtonBar{
		Box:     tview.NewBox(),
		buttons: make([]*tview.Button, 0),
		app:     app,
	}
}

// AddButton adds a button to the bar.
func (bb *ButtonBar) AddButton(label string, action func()) *ButtonBar {
	btn := tview.NewButton(label).SetSelectedFunc(action)
	bb.buttons = append(bb.buttons, btn)
	bb.helpTexts = append(bb.helpTexts, "")
	return bb
}

// AddButtonWithHelp adds a button with associated help text.
func (bb *ButtonBar) AddButtonWithHelp(label, helpText string, action func()) *ButtonBar {
	btn := tview.NewButton(label).SetSelectedFunc(action)
	bb.buttons = append(bb.buttons, btn)
	bb.helpTexts = append(bb.helpTexts, helpText)
	return bb
}

// SetHelpCallback sets the callback for when button focus changes.
func (bb *ButtonBar) SetHelpCallback(fn func(string)) *ButtonBar {
	bb.helpCallback = fn
	return bb
}

// triggerHelp calls the help callback with the current button's help text.
func (bb *ButtonBar) triggerHelp() {
	if bb.helpCallback != nil && bb.focusedIndex < len(bb.helpTexts) {
		bb.helpCallback(bb.helpTexts[bb.focusedIndex])
	}
}

// SetupNavigation sets up the escape callback.
func (bb *ButtonBar) SetupNavigation(onEscape func()) *ButtonBar {
	bb.onEscape = onEscape
	return bb
}

// SetOnUp sets the callback for when Up is pressed (to navigate back to content).
func (bb *ButtonBar) SetOnUp(fn func()) *ButtonBar {
	bb.onUp = fn
	return bb
}

// SetOnDown sets the callback for when Down is pressed (to wrap to top of content).
func (bb *ButtonBar) SetOnDown(fn func()) *ButtonBar {
	bb.onDown = fn
	return bb
}

// SetOnWrap sets the callback for when Tab is pressed on the last button (to wrap to top).
func (bb *ButtonBar) SetOnWrap(fn func()) *ButtonBar {
	bb.onWrap = fn
	return bb
}

// SetOnLeft sets the callback for when Left is pressed on the first button.
func (bb *ButtonBar) SetOnLeft(fn func()) *ButtonBar {
	bb.onLeft = fn
	return bb
}

// SetOnRight sets the callback for when Right is pressed on the last button.
func (bb *ButtonBar) SetOnRight(fn func()) *ButtonBar {
	bb.onRight = fn
	return bb
}

// Draw renders the button bar.
func (bb *ButtonBar) Draw(screen tcell.Screen) {
	bb.DrawForSubclass(screen, bb)

	x, y, width, _ := bb.GetInnerRect()
	if len(bb.buttons) == 0 || width <= 0 {
		return
	}

	// Calculate button widths - distribute evenly with spacing
	totalButtons := len(bb.buttons)
	spacing := 2
	totalSpacing := spacing * (totalButtons - 1)
	buttonWidth := (width - totalSpacing) / totalButtons
	if buttonWidth < 6 {
		buttonWidth = 6
	}

	hasFocus := bb.HasFocus()

	currentX := x
	for i, btn := range bb.buttons {
		btnWidth := buttonWidth
		if currentX+btnWidth > x+width {
			btnWidth = x + width - currentX
		}
		if btnWidth <= 0 {
			break
		}

		btn.SetRect(currentX, y, btnWidth, 1)

		// Show focus state on the currently selected button
		if hasFocus && i == bb.focusedIndex {
			btn.Focus(func(_ tview.Primitive) {})
		} else {
			btn.Blur()
		}

		btn.Draw(screen)
		currentX += btnWidth + spacing
	}
}

// InputHandler handles keyboard input for the button bar.
func (bb *ButtonBar) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return bb.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if len(bb.buttons) == 0 {
			return
		}

		switch event.Key() {
		case tcell.KeyLeft:
			if bb.focusedIndex == 0 && bb.onLeft != nil {
				bb.onLeft()
			} else {
				bb.focusedIndex = (bb.focusedIndex - 1 + len(bb.buttons)) % len(bb.buttons)
				bb.triggerHelp()
			}
		case tcell.KeyBacktab:
			if bb.focusedIndex == 0 && bb.onWrap != nil {
				bb.onWrap()
			} else {
				bb.focusedIndex = (bb.focusedIndex - 1 + len(bb.buttons)) % len(bb.buttons)
				bb.triggerHelp()
			}
		case tcell.KeyRight:
			if bb.focusedIndex == len(bb.buttons)-1 && bb.onRight != nil {
				bb.onRight()
			} else {
				bb.focusedIndex = (bb.focusedIndex + 1) % len(bb.buttons)
				bb.triggerHelp()
			}
		case tcell.KeyTab:
			if bb.focusedIndex == len(bb.buttons)-1 && bb.onWrap != nil {
				bb.onWrap()
			} else {
				bb.focusedIndex = (bb.focusedIndex + 1) % len(bb.buttons)
				bb.triggerHelp()
			}
		case tcell.KeyUp:
			if bb.onUp != nil {
				bb.onUp()
			}
		case tcell.KeyDown:
			if bb.onDown != nil {
				bb.onDown()
			} else if bb.onUp != nil {
				bb.onUp()
			}
		case tcell.KeyEnter:
			if bb.focusedIndex < len(bb.buttons) {
				btn := bb.buttons[bb.focusedIndex]
				if handler := btn.InputHandler(); handler != nil {
					handler(event, setFocus)
				}
			}
		case tcell.KeyEscape:
			if bb.onEscape != nil {
				bb.onEscape()
			}
		default:
			// Ignore other keys
		}
	})
}

// MouseHandler handles mouse input for the button bar.
func (bb *ButtonBar) MouseHandler() func(
	action tview.MouseAction,
	event *tcell.EventMouse,
	setFocus func(p tview.Primitive),
) (consumed bool, capture tview.Primitive) {
	return bb.WrapMouseHandler(func(
		action tview.MouseAction,
		event *tcell.EventMouse,
		setFocus func(p tview.Primitive),
	) (consumed bool, capture tview.Primitive) {
		if action != tview.MouseLeftClick {
			return false, nil
		}
		for i, btn := range bb.buttons {
			if !btn.InRect(event.Position()) {
				continue
			}
			bb.focusedIndex = i
			bb.triggerHelp()
			setFocus(bb)
			if handler := btn.MouseHandler(); handler != nil {
				return handler(action, event, setFocus)
			}
			return true, nil
		}
		return false, nil
	})
}

// Focus is called when the button bar receives focus.
func (bb *ButtonBar) Focus(delegate func(p tview.Primitive)) {
	if len(bb.buttons) > 0 {
		bb.Box.Focus(delegate)
		bb.triggerHelp()
	}
}

// HasFocus returns whether the button bar has focus.
func (bb *ButtonBar) HasFocus() bool {
	return bb.Box.HasFocus()
}

// GetFirstButton returns the first button for focus purposes.
func (bb *ButtonBar) GetFirstButton() *tview.Button {
	if len(bb.buttons) > 0 {
		return bb.buttons[0]
	}
	return nil
}

// UpdateButtonLabel updates the label of a button at the given index.
func (bb *ButtonBar) UpdateButtonLabel(index int, label string) {
	if index >= 0 && index < len(bb.buttons) {
		bb.buttons[index].SetLabel(label)
	}
}

// VerticalDivider draws a vertical line for separating columns.
type VerticalDivider struct {
	*tview.Box
}

// NewVerticalDivider creates a new vertical divider.
func NewVerticalDivider() *VerticalDivider {
	return &VerticalDivider{
		Box: tview.NewBox(),
	}
}

// Draw renders the vertical divider.
func (vd *VerticalDivider) Draw(screen tcell.Screen) {
	vd.DrawForSubclass(screen, vd)

	x, y, _, height := vd.GetInnerRect()
	if height <= 0 {
		return
	}

	style := tcell.StyleDefault.Foreground(CurrentTheme().BorderColor)
	for row := range height {
		screen.SetContent(x, y+row, tcell.RuneVLine, nil, style)
	}
}

// ScrollIndicatorList wraps a tview.List and draws scroll indicators.
type ScrollIndicatorList struct {
	*tview.Box
	list   *tview.List
	offset int
}

// NewScrollIndicatorList creates a new list with scroll indicators.
func NewScrollIndicatorList() *ScrollIndicatorList {
	sil := &ScrollIndicatorList{
		Box:  tview.NewBox(),
		list: tview.NewList(),
	}
	sil.list.SetWrapAround(false)
	sil.list.SetSelectedFocusOnly(true)
	sil.list.ShowSecondaryText(false)
	return sil
}

// GetList returns the underlying tview.List for configuration.
func (sil *ScrollIndicatorList) GetList() *tview.List {
	return sil.list
}

// Draw renders the list with scroll indicators.
func (sil *ScrollIndicatorList) Draw(screen tcell.Screen) {
	sil.DrawForSubclass(screen, sil)

	x, y, width, height := sil.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	// Draw the list (reserve 1 column for indicators)
	listWidth := width - 1
	if listWidth < 1 {
		listWidth = width
	}
	sil.list.SetRect(x, y, listWidth, height)
	sil.list.Draw(screen)

	// Calculate scroll state
	itemCount := sil.list.GetItemCount()
	currentItem := sil.list.GetCurrentItem()

	if itemCount == 0 || itemCount <= height {
		return // No scrolling needed
	}

	// Estimate offset based on current item
	sil.offset = currentItem - height/2
	if sil.offset < 0 {
		sil.offset = 0
	}
	if sil.offset > itemCount-height {
		sil.offset = itemCount - height
	}

	// Draw arrow indicators in the corner
	scrollX := x + width - 1
	t := CurrentTheme()
	arrowStyle := tcell.StyleDefault.Foreground(t.ContrastBackgroundColor)

	if sil.offset > 0 {
		screen.SetContent(scrollX, y, tcell.RuneUArrow, nil, arrowStyle)
	}
	if sil.offset+height < itemCount {
		screen.SetContent(scrollX, y+height-1, tcell.RuneDArrow, nil, arrowStyle)
	}
}

// Focus delegates focus to the underlying list.
func (sil *ScrollIndicatorList) Focus(delegate func(p tview.Primitive)) {
	delegate(sil.list)
}

// HasFocus returns whether the list has focus.
func (sil *ScrollIndicatorList) HasFocus() bool {
	return sil.list.HasFocus()
}

// InputHandler returns the list's input handler.
func (sil *ScrollIndicatorList) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return sil.list.InputHandler()
}

// MouseHandler returns the list's mouse handler.
func (sil *ScrollIndicatorList) MouseHandler() func(
	tview.MouseAction, *tcell.EventMouse, func(tview.Primitive),
) (bool, tview.Primitive) {
	return sil.list.MouseHandler()
}

// CheckListItem represents an item with separate display label and value.
type CheckListItem struct {
	Label string
	Value string
}

// CheckList is a list with toggleable checkbox items for multi-select.
type CheckList struct {
	*tview.List
	selected        map[int]bool
	onChange        func(selected []string)
	onSelectionSync func(count int)
	items           []CheckListItem
}

// NewCheckList creates a new multi-select checkbox list.
func NewCheckList(items, initiallySelected []string, onChange func(selected []string)) *CheckList {
	checkItems := make([]CheckListItem, len(items))
	for i, item := range items {
		checkItems[i] = CheckListItem{Label: item, Value: item}
	}
	return NewCheckListWithValues(checkItems, initiallySelected, onChange)
}

// NewCheckListWithValues creates a checklist with separate labels and values.
func NewCheckListWithValues(
	items []CheckListItem,
	initiallySelected []string,
	onChange func(selected []string),
) *CheckList {
	list := tview.NewList()
	list.SetSecondaryTextColor(CurrentTheme().SecondaryTextColor)
	list.ShowSecondaryText(false)

	selectedMap := make(map[int]bool)
	for i, item := range items {
		for _, sel := range initiallySelected {
			if item.Value == sel {
				selectedMap[i] = true
				break
			}
		}
	}

	cl := &CheckList{
		List:     list,
		selected: selectedMap,
		items:    items,
		onChange: onChange,
	}

	cl.refresh()

	return cl
}

func (cl *CheckList) refresh() {
	cl.Clear()
	for i, item := range cl.items {
		index := i
		cl.AddItem(cl.formatItem(index, item.Label), "", 0, func() {
			cl.toggle(index)
		})
	}
}

func (cl *CheckList) formatItem(index int, label string) string {
	if cl.selected[index] {
		return "[*] " + label
	}
	return "[ ] " + label
}

func (cl *CheckList) toggle(index int) {
	cl.selected[index] = !cl.selected[index]
	cl.SetItemText(index, cl.formatItem(index, cl.items[index].Label), "")
	if cl.onChange != nil {
		cl.onChange(cl.GetSelected())
	}
	if cl.onSelectionSync != nil {
		cl.onSelectionSync(cl.GetSelectedCount())
	}
}

// GetSelected returns the list of selected item values.
func (cl *CheckList) GetSelected() []string {
	result := make([]string, 0)
	for i, item := range cl.items {
		if cl.selected[i] {
			result = append(result, item.Value)
		}
	}
	return result
}

// GetSelectedCount returns the number of selected items.
func (cl *CheckList) GetSelectedCount() int {
	count := 0
	for _, selected := range cl.selected {
		if selected {
			count++
		}
	}
	return count
}

// SetSelectionSyncFunc sets a callback that fires when selection changes.
func (cl *CheckList) SetSelectionSyncFunc(fn func(count int)) {
	cl.onSelectionSync = fn
}

// SetupNavigation adds escape key handling.
func (cl *CheckList) SetupNavigation(pages *tview.Pages, previousPage string) *CheckList {
	cl.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(previousPage)
			return nil
		}
		return event
	})
	return cl
}

// SystemItem represents a system with ID and display name.
type SystemItem struct {
	ID   string
	Name string
}

// SystemSelectorMode defines whether the selector allows single or multiple selections.
type SystemSelectorMode int

const (
	// SystemSelectorSingle allows selecting one system (with optional "All" option).
	SystemSelectorSingle SystemSelectorMode = iota
	// SystemSelectorMulti allows selecting multiple systems (checkbox style).
	SystemSelectorMulti
)

// SystemSelector is a reusable system selection component.
// It can operate in single-select or multi-select mode.
type SystemSelector struct {
	*ScrollIndicatorList
	list        *tview.List
	selected    map[int]bool
	onMulti     func(selected []string)
	onSingle    func(systemID string)
	items       []SystemItem
	singleIndex int
	mode        SystemSelectorMode
	includeAll  bool
	autoConfirm bool
}

// SystemSelectorConfig configures a new SystemSelector.
type SystemSelectorConfig struct {
	OnMulti     func(selected []string)
	OnSingle    func(systemID string)
	Systems     []SystemItem
	Selected    []string
	Mode        SystemSelectorMode
	IncludeAll  bool
	AutoConfirm bool // Single-select only: auto-close on selection
}

// NewSystemSelector creates a new system selector with the given configuration.
func NewSystemSelector(cfg *SystemSelectorConfig) *SystemSelector {
	scrollList := NewScrollIndicatorList()
	list := scrollList.GetList()
	list.SetSecondaryTextColor(CurrentTheme().SecondaryTextColor)
	list.ShowSecondaryText(false)
	list.SetSelectedFocusOnly(true)
	// Enable wrap around in single select mode for easier navigation
	list.SetWrapAround(cfg.Mode == SystemSelectorSingle)

	selectedMap := make(map[int]bool)
	singleIndex := 0

	if cfg.Mode == SystemSelectorMulti {
		for i, item := range cfg.Systems {
			for _, sel := range cfg.Selected {
				if item.ID == sel {
					selectedMap[i] = true
					break
				}
			}
		}
	} else if len(cfg.Selected) > 0 && cfg.Selected[0] != "" {
		for i, item := range cfg.Systems {
			if item.ID == cfg.Selected[0] {
				if cfg.IncludeAll {
					singleIndex = i + 1 // +1 because "All" is at index 0
				} else {
					singleIndex = i
				}
				break
			}
		}
	}

	ss := &SystemSelector{
		ScrollIndicatorList: scrollList,
		list:                list,
		items:               cfg.Systems,
		selected:            selectedMap,
		singleIndex:         singleIndex,
		mode:                cfg.Mode,
		includeAll:          cfg.IncludeAll,
		autoConfirm:         cfg.AutoConfirm,
		onMulti:             cfg.OnMulti,
		onSingle:            cfg.OnSingle,
	}

	ss.refresh()

	return ss
}

func (ss *SystemSelector) refresh() {
	ss.list.Clear()

	if ss.mode == SystemSelectorSingle {
		if ss.includeAll {
			ss.list.AddItem(ss.formatSingleItem(0, "All"), "", 0, func() {
				ss.selectSingle(-1)
			})
		}
		for i, item := range ss.items {
			index := i
			displayIndex := i
			if ss.includeAll {
				displayIndex = i + 1
			}
			ss.list.AddItem(ss.formatSingleItem(displayIndex, item.Name), "", 0, func() {
				ss.selectSingle(index)
			})
		}
		ss.list.SetCurrentItem(ss.singleIndex)
	} else {
		for i, item := range ss.items {
			index := i
			ss.list.AddItem(ss.formatMultiItem(index, item.Name), "", 0, func() {
				ss.toggleMulti(index)
			})
		}
	}
}

func (ss *SystemSelector) formatSingleItem(displayIndex int, label string) string {
	if displayIndex == ss.singleIndex {
		return "(*) " + label
	}
	return "( ) " + label
}

func (ss *SystemSelector) formatMultiItem(index int, label string) string {
	if ss.selected[index] {
		return "[*] " + label
	}
	return "[ ] " + label
}

func (ss *SystemSelector) selectSingle(itemIndex int) {
	ss.singleIndex = ss.list.GetCurrentItem()

	// Refresh all items to update radio button display
	if ss.includeAll {
		ss.list.SetItemText(0, ss.formatSingleItem(0, "All"), "")
	}
	for i, item := range ss.items {
		displayIndex := i
		if ss.includeAll {
			displayIndex = i + 1
		}
		ss.list.SetItemText(displayIndex, ss.formatSingleItem(displayIndex, item.Name), "")
	}

	if ss.onSingle != nil {
		if itemIndex < 0 {
			ss.onSingle("")
		} else {
			ss.onSingle(ss.items[itemIndex].ID)
		}
	}
}

func (ss *SystemSelector) toggleMulti(index int) {
	ss.selected[index] = !ss.selected[index]
	ss.list.SetItemText(index, ss.formatMultiItem(index, ss.items[index].Name), "")
	if ss.onMulti != nil {
		ss.onMulti(ss.GetSelected())
	}
}

// GetSelected returns the list of selected system IDs.
func (ss *SystemSelector) GetSelected() []string {
	if ss.mode == SystemSelectorSingle {
		if ss.includeAll && ss.singleIndex == 0 {
			return []string{}
		}
		idx := ss.singleIndex
		if ss.includeAll {
			idx--
		}
		if idx >= 0 && idx < len(ss.items) {
			return []string{ss.items[idx].ID}
		}
		return []string{}
	}

	result := make([]string, 0)
	for i, item := range ss.items {
		if ss.selected[i] {
			result = append(result, item.ID)
		}
	}
	return result
}

// GetSelectedCount returns the number of selected items.
func (ss *SystemSelector) GetSelectedCount() int {
	if ss.mode == SystemSelectorSingle {
		if ss.includeAll && ss.singleIndex == 0 {
			return 0
		}
		return 1
	}
	count := 0
	for _, selected := range ss.selected {
		if selected {
			count++
		}
	}
	return count
}

// GetCurrentItem returns the index of the currently selected list item.
func (ss *SystemSelector) GetCurrentItem() int {
	return ss.list.GetCurrentItem()
}

// SetCurrentItem sets the currently selected list item.
func (ss *SystemSelector) SetCurrentItem(index int) {
	ss.list.SetCurrentItem(index)
}

// GetItemCount returns the number of items in the list.
func (ss *SystemSelector) GetItemCount() int {
	return ss.list.GetItemCount()
}

// SetInputCapture sets the input capture function on the underlying list.
func (ss *SystemSelector) SetInputCapture(capture func(event *tcell.EventKey) *tcell.EventKey) {
	ss.list.SetInputCapture(capture)
}

// GetSelectedSingle returns the currently selected system ID (single-select mode).
// Returns empty string if "All" is selected or in multi-select mode.
func (ss *SystemSelector) GetSelectedSingle() string {
	if ss.mode != SystemSelectorSingle {
		return ""
	}
	if ss.includeAll && ss.singleIndex == 0 {
		return ""
	}
	idx := ss.singleIndex
	if ss.includeAll {
		idx--
	}
	if idx >= 0 && idx < len(ss.items) {
		return ss.items[idx].ID
	}
	return ""
}
