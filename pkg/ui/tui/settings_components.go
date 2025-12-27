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

// errorModalPage is the page name for the error modal overlay.
const errorModalPage = "errorModal"

// showErrorModal displays an error message modal to the user.
func showErrorModal(pages *tview.Pages, app *tview.Application, message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			pages.HidePage(errorModalPage)
			pages.RemovePage(errorModalPage)
		})
	pages.AddPage(errorModalPage, modal, false, true)
	app.SetFocus(modal)
}

// formatToggle renders a toggle value. When selected, label is highlighted.
func formatToggle(value bool, label string, selected bool) string {
	checkbox := "[ ]"
	if value {
		checkbox = "[*]"
	}
	if selected {
		return fmt.Sprintf("[yellow:%s]- %s [black:yellow]%s[-:%s]", ThemeBgColor, checkbox, label, ThemeBgColor)
	}
	return fmt.Sprintf("[yellow:%s]- %s [white:%s]%s[-:-]", ThemeBgColor, checkbox, ThemeBgColor, label)
}

// formatCycle renders a cycle value. When selected, label and value are highlighted.
func formatCycle(label, currentValue string, selected bool) string {
	bg := ThemeBgColor
	if selected {
		return fmt.Sprintf("[yellow:%s]- [black:yellow]%s: < %s >[-:%s]", bg, label, currentValue, bg)
	}
	return fmt.Sprintf("[yellow:%s]- [white:%s]%s: < %s >[-:-]", bg, bg, label, currentValue)
}

// formatAction renders an action item. When selected, label is highlighted.
func formatAction(label string, selected bool) string {
	if selected {
		return "[yellow:" + ThemeBgColor + "]- [black:yellow]" + label + "[-:" + ThemeBgColor + "]"
	}
	return "[yellow:" + ThemeBgColor + "]- [white:" + ThemeBgColor + "]" + label + "[-:-]"
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

// SettingsList wraps a tview.List with consistent game-like navigation.
// It handles arrow keys for navigation and Escape to go back.
// It manually manages selection highlighting so only the label is highlighted.
type SettingsList struct {
	*tview.List
	pages        *tview.Pages
	previousPage string
	items        []settingsItem
}

// NewSettingsList creates a new settings list with game-like navigation.
func NewSettingsList(pages *tview.Pages, previousPage string) *SettingsList {
	list := tview.NewList()
	list.SetSecondaryTextColor(tcell.ColorGray)
	list.ShowSecondaryText(true)
	list.SetHighlightFullLine(false)

	// Disable built-in selection highlighting - we manage it manually
	list.SetSelectedStyle(tcell.StyleDefault)
	list.SetMainTextStyle(tcell.StyleDefault)

	sl := &SettingsList{
		List:         list,
		pages:        pages,
		previousPage: previousPage,
		items:        make([]settingsItem, 0),
	}

	// Update item styling when selection changes
	list.SetChangedFunc(func(index int, _, _ string, _ rune) {
		sl.refreshAllItems(index)
	})

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(previousPage)
			return nil
		}
		return event
	})

	return sl
}

// refreshAllItems updates all items to reflect current selection state.
func (sl *SettingsList) refreshAllItems(selectedIndex int) {
	for i, item := range sl.items {
		selected := i == selectedIndex
		desc := formatDesc(item.description)

		var mainText string
		switch item.itemType {
		case "toggle":
			mainText = formatToggle(*item.toggleValue, item.label, selected)
		case "cycle":
			mainText = formatCycle(item.label, item.cycleOptions[*item.cycleIndex], selected)
		case "action":
			mainText = formatAction(item.label, selected)
		}

		sl.SetItemText(i, mainText, desc)
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

// AddBack adds a "Go back" action item with default description.
func (sl *SettingsList) AddBack() *SettingsList {
	return sl.AddBackWithDesc("Return to previous menu")
}

// AddBackWithDesc adds a "Go back" action item with custom description.
func (sl *SettingsList) AddBackWithDesc(description string) *SettingsList {
	index := sl.GetItemCount()
	selected := index == 0

	sl.items = append(sl.items, settingsItem{
		itemType:    "action",
		label:       "Go back",
		description: description,
	})

	sl.AddItem(formatAction("Go back", selected), formatDesc(description), 0, func() {
		sl.pages.SwitchToPage(sl.previousPage)
	})
	return sl
}

// SetupCycleKeys adds input capture to handle Left/Right keys for cycle selectors only.
// Toggles use Enter only. This should be called after all items are added.
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
	*tview.Flex
	app     *tview.Application
	buttons []*tview.Button
}

// NewButtonBar creates a new button bar.
func NewButtonBar(app *tview.Application) *ButtonBar {
	flex := tview.NewFlex().SetDirection(tview.FlexColumn)
	return &ButtonBar{
		Flex:    flex,
		buttons: make([]*tview.Button, 0),
		app:     app,
	}
}

// AddButton adds a button to the bar.
func (bb *ButtonBar) AddButton(label string, action func()) *ButtonBar {
	btn := tview.NewButton(label).SetSelectedFunc(action)
	bb.buttons = append(bb.buttons, btn)
	bb.AddItem(btn, 0, 1, len(bb.buttons) == 1)
	bb.AddItem(tview.NewBox(), 1, 0, false) // spacer
	return bb
}

// SetupNavigation sets up Left/Right arrow navigation between buttons.
func (bb *ButtonBar) SetupNavigation(onEscape func()) *ButtonBar {
	for i, btn := range bb.buttons {
		prevIndex := (i - 1 + len(bb.buttons)) % len(bb.buttons)
		nextIndex := (i + 1) % len(bb.buttons)

		btn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			switch event.Key() {
			case tcell.KeyLeft:
				bb.app.SetFocus(bb.buttons[prevIndex])
				return nil
			case tcell.KeyRight:
				bb.app.SetFocus(bb.buttons[nextIndex])
				return nil
			case tcell.KeyEscape:
				if onEscape != nil {
					onEscape()
				}
				return nil
			default:
				return event
			}
		})
	}
	return bb
}

// GetFirstButton returns the first button for focus purposes.
func (bb *ButtonBar) GetFirstButton() *tview.Button {
	if len(bb.buttons) > 0 {
		return bb.buttons[0]
	}
	return nil
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
	list.SetSecondaryTextColor(tcell.ColorYellow)
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

// SetupNavigation adds escape key handling. Toggles use Enter only.
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
