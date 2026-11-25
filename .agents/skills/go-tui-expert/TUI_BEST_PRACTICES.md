# TUI Best Practices

Design patterns and guidelines for building effective terminal user interfaces.

---

## Layout Design

### Visual Hierarchy

1. **Use borders strategically** - Not every element needs a border
2. **Group related content** - Use Flex/Grid to create logical sections
3. **Consistent spacing** - Use padding to create breathing room
4. **Clear headers** - Use titles to identify sections

```go
// Good: Clear hierarchy
mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
    AddItem(header, 3, 0, false).    // Prominent header
    AddItem(tview.NewFlex().         // Content area
        AddItem(sidebar, 25, 0, false).
        AddItem(main, 0, 1, true), 0, 1, true).
    AddItem(statusBar, 1, 0, false)  // Subtle status bar
```

### Responsive Design

Consider terminal size variations:

```go
// Use Grid with fallback layouts for narrow terminals
grid.AddItem(sidebar, 1, 0, 1, 1, 0, 80, false)  // minWidth=80
// Sidebar hidden on narrow screens

// Or check size dynamically
app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
    width, _ := screen.Size()
    if width < 80 {
        pages.SwitchToPage("compact")
    } else {
        pages.SwitchToPage("full")
    }
    return false
})
```

### Common Layouts

**Dashboard Layout:**
```go
grid.SetRows(3, 0, 1).SetColumns(0, 0, 0)
grid.AddItem(header, 0, 0, 1, 3, 0, 0, false)
grid.AddItem(panel1, 1, 0, 1, 1, 0, 0, false)
grid.AddItem(panel2, 1, 1, 1, 1, 0, 0, false)
grid.AddItem(panel3, 1, 2, 1, 1, 0, 0, false)
grid.AddItem(status, 2, 0, 1, 3, 0, 0, false)
```

**Master-Detail Layout:**
```go
flex := tview.NewFlex().
    AddItem(list, 30, 0, true).      // Master (fixed width)
    AddItem(detail, 0, 1, false)     // Detail (flexible)
```

**Wizard/Multi-Step Layout:**
```go
pages := tview.NewPages()
pages.AddPage("step1", step1Form, true, true)
pages.AddPage("step2", step2Form, true, false)
pages.AddPage("step3", step3Form, true, false)
// Navigate with pages.SwitchToPage("step2")
```

---

## Focus Management

### Focus Principles

1. **One focused element at a time** - Make it visually clear
2. **Logical focus order** - Follow reading order (left→right, top→bottom)
3. **Trap focus in modals** - Don't allow focus to escape dialogs
4. **Restore focus after modal close** - Return to previous focus

### Implementing Focus Chains

```go
// Store focusable elements in order
focusables := []tview.Primitive{field1, field2, field3, button}
focusIndex := 0

app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
    switch event.Key() {
    case tcell.KeyTab:
        focusIndex = (focusIndex + 1) % len(focusables)
        app.SetFocus(focusables[focusIndex])
        return nil
    case tcell.KeyBacktab:
        focusIndex = (focusIndex - 1 + len(focusables)) % len(focusables)
        app.SetFocus(focusables[focusIndex])
        return nil
    }
    return event
})
```

### Modal Focus Trapping

```go
var previousFocus tview.Primitive

func showModal(content tview.Primitive) {
    previousFocus = app.GetFocus()

    modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
        if event.Key() == tcell.KeyEscape {
            hideModal()
            return nil
        }
        // Keep focus within modal
        return event
    })

    pages.ShowPage("modal")
    app.SetFocus(modal)
}

func hideModal() {
    pages.HidePage("modal")
    if previousFocus != nil {
        app.SetFocus(previousFocus)
    }
}
```

---

## Keyboard Navigation

### Standard Keys

Follow conventions users expect:

| Key | Action |
|-----|--------|
| Tab | Next field |
| Shift+Tab | Previous field |
| Enter | Submit/Select/Activate |
| Escape | Cancel/Back/Close |
| Arrow keys | Navigate within component |
| Ctrl+C | Exit application |
| ? or F1 | Show help |

### Key Hints

Show available keys to users:

```go
// Status bar with key hints
statusBar := tview.NewTextView().
    SetDynamicColors(true).
    SetText("[yellow]Tab[white]:Next  [yellow]Enter[white]:Select  [yellow]Esc[white]:Back  [yellow]?[white]:Help")
```

### Context-Sensitive Keys

```go
list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
    switch event.Key() {
    case tcell.KeyDelete:
        deleteSelected()
        return nil
    case tcell.KeyRune:
        switch event.Rune() {
        case 'n':
            newItem()
            return nil
        case 'e':
            editSelected()
            return nil
        }
    }
    return event
})
```

---

## Error Handling

### Displaying Errors

Don't crash - show errors gracefully:

```go
func showError(message string) {
    modal := tview.NewModal().
        SetText(message).
        SetTextColor(tcell.ColorRed).
        AddButtons([]string{"OK"}).
        SetDoneFunc(func(buttonIndex int, buttonLabel string) {
            pages.HidePage("error")
        })
    pages.AddPage("error", modal, false, true)
}
```

### Non-Blocking Errors

For transient errors, use a status bar:

```go
func showStatus(message string, isError bool) {
    color := "[green]"
    if isError {
        color = "[red]"
    }
    statusBar.SetText(color + message + "[-]")

    // Clear after delay
    go func() {
        time.Sleep(3 * time.Second)
        app.QueueUpdateDraw(func() {
            statusBar.SetText("")
        })
    }()
}
```

### Graceful Degradation

```go
func loadData() {
    go func() {
        data, err := fetchData()
        app.QueueUpdateDraw(func() {
            if err != nil {
                textView.SetText("[red]Failed to load: " + err.Error())
                return
            }
            textView.SetText(data)
        })
    }()

    // Show loading state immediately
    textView.SetText("[gray]Loading...")
}
```

---

## Performance

### Minimize Redraws

```go
// Bad: Multiple redraws
for _, item := range items {
    table.SetCell(row, col, tview.NewTableCell(item))
    app.Draw()  // Wasteful!
}

// Good: Single redraw
for _, item := range items {
    table.SetCell(row, col, tview.NewTableCell(item))
}
app.Draw()

// Best: Let tview handle it
// Just set cells; redraw happens automatically in event loop
```

### Batch Updates

```go
app.QueueUpdateDraw(func() {
    // All these updates = single redraw
    label1.SetText("...")
    label2.SetText("...")
    table.Clear()
    for _, row := range data {
        table.SetCell(...)
    }
})
```

### Virtual Tables

For large datasets, don't load everything:

```go
// Use SetContent callback for virtual table
table.SetContent(&MyTableContent{
    data: largeDataset,
})

// MyTableContent implements tview.TableContent:
// - GetCell(row, column int) *TableCell
// - GetRowCount() int
// - GetColumnCount() int
```

### Lazy Loading

```go
treeNode.SetSelectedFunc(func() {
    if !treeNode.IsExpanded() {
        // Load children only when expanded
        children := loadChildren(treeNode)
        for _, child := range children {
            treeNode.AddChild(child)
        }
    }
})
```

---

## Testing

### Separate Logic from Rendering

```go
// Bad: Logic mixed with UI
func handleSubmit() {
    name := nameField.GetText()
    if name == "" {
        showError("Name required")
        return
    }
    // ... more validation and processing
}

// Good: Logic in testable functions
func validateName(name string) error {
    if name == "" {
        return errors.New("name required")
    }
    return nil
}

func handleSubmit() {
    if err := validateName(nameField.GetText()); err != nil {
        showError(err.Error())
        return
    }
    // ...
}

// Now validateName can be unit tested
```

### Mock the Application

```go
// Interface for testability
type UIUpdater interface {
    ShowError(message string)
    ShowSuccess(message string)
    SetStatus(message string)
}

// Real implementation uses tview
type TviewUpdater struct {
    statusBar *tview.TextView
    // ...
}

// Test implementation records calls
type MockUpdater struct {
    Errors []string
    // ...
}
```

### Test Key Handlers

```go
func TestKeyHandler(t *testing.T) {
    var saved bool
    handler := createKeyHandler(func() { saved = true })

    // Simulate Ctrl+S
    event := tcell.NewEventKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
    result := handler(event)

    assert.Nil(t, result)  // Event consumed
    assert.True(t, saved)
}
```

---

## Accessibility

### Color Considerations

1. **Don't rely on color alone** - Use symbols/text too
2. **High contrast** - Ensure text is readable
3. **Support default colors** - Respect terminal themes

```go
// Bad: Color-only status
if isError {
    cell.SetTextColor(tcell.ColorRed)
} else {
    cell.SetTextColor(tcell.ColorGreen)
}

// Good: Color + symbol
if isError {
    cell.SetText("✗ Error").SetTextColor(tcell.ColorRed)
} else {
    cell.SetText("✓ OK").SetTextColor(tcell.ColorGreen)
}
```

### Keyboard-Only Navigation

Every action must be keyboard-accessible:

```go
// If you have mouse click handlers, also have keyboard equivalents
table.SetMouseCapture(func(...) { selectRow() })
table.SetSelectedFunc(func(...) { selectRow() })  // Enter key

// Provide shortcuts for common actions
list.ShowSecondaryText(true)  // Show keyboard shortcuts
list.AddItem("New", "n", 'n', newItem)
list.AddItem("Delete", "d", 'd', deleteItem)
```

### Screen Reader Hints

```go
// Use meaningful labels
button.SetLabel("Submit Form")  // Not just "OK"

// Provide context in titles
form.SetTitle(" Edit User: John Doe ")

// Include counts
list.SetTitle(fmt.Sprintf(" Items (%d) ", count))
```

---

## Common Pitfalls

### Deadlocks

```go
// Bad: Blocking in event handler
primitive.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
    result := <-slowChannel  // Blocks UI!
    return event
})

// Good: Handle in goroutine
primitive.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
    go func() {
        result := <-slowChannel
        app.QueueUpdateDraw(func() {
            // Update UI
        })
    }()
    return event
})
```

### Focus Lost

```go
// Problem: Removed focused primitive
pages.RemovePage("current")  // Focus goes nowhere!

// Solution: Set focus explicitly
pages.RemovePage("current")
app.SetFocus(fallbackPrimitive)
```

### Terminal State Corruption

```go
// Always clean up on exit
defer func() {
    if r := recover(); r != nil {
        app.Stop()  // Restore terminal
        panic(r)    // Re-panic
    }
}()
```
