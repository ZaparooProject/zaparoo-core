# tview Reference Guide

## Component Overview

### Layout Components

| Component | Use When |
|-----------|----------|
| **Box** | Base primitive for all widgets; use for simple bordered containers |
| **Flex** | Stacking items horizontally or vertically with proportional sizing |
| **Grid** | Complex layouts with fixed rows/columns and responsive breakpoints |
| **Pages** | Switching between multiple full-screen views |
| **Frame** | Adding header/footer text around another primitive |

### Input Components

| Component | Use When |
|-----------|----------|
| **Form** | Collecting multiple inputs with labels and buttons |
| **InputField** | Single-line text input with optional masking |
| **TextArea** | Multi-line text editing |
| **DropDown** | Selection from a list of options |
| **Checkbox** | Boolean toggle |
| **Button** | Clickable action trigger |

### Display Components

| Component | Use When |
|-----------|----------|
| **TextView** | Displaying formatted/scrollable text (supports colors, regions) |
| **Table** | Tabular data with selectable rows/cells |
| **List** | Selectable list of items with shortcuts |
| **TreeView** | Hierarchical data navigation |
| **Image** | Displaying images using Unicode block characters |
| **Modal** | Centered message dialogs with buttons |

---

## Application Lifecycle

```go
// Create application
app := tview.NewApplication()

// Set root primitive (what fills the screen)
app.SetRoot(primitive, true)  // true = fullscreen

// Set initial focus
app.SetFocus(inputField)

// Run the application (blocks until Stop is called)
if err := app.Run(); err != nil {
    panic(err)
}

// Stop the application (usually called from event handler)
app.Stop()
```

---

## Layout Patterns

### Flex Layout

```go
// Vertical stack (default)
flex := tview.NewFlex().SetDirection(tview.FlexRow).
    AddItem(header, 3, 0, false).      // Fixed 3 rows
    AddItem(content, 0, 1, true).      // Flexible, takes remaining space
    AddItem(footer, 1, 0, false)       // Fixed 1 row

// Horizontal stack
flex := tview.NewFlex().SetDirection(tview.FlexColumn).
    AddItem(sidebar, 30, 0, false).    // Fixed 30 columns
    AddItem(main, 0, 1, true)          // Flexible

// AddItem(primitive, fixedSize, proportion, focus)
// - fixedSize > 0: use exact size
// - fixedSize = 0: use proportion for remaining space
// - focus: whether this item gets initial focus
```

### Grid Layout

```go
grid := tview.NewGrid().
    SetRows(3, 0, 3).           // 3 rows: header(3), content(flex), footer(3)
    SetColumns(30, 0, 30).      // 3 cols: left(30), center(flex), right(30)
    SetBorders(true)

// AddItem(primitive, row, col, rowSpan, colSpan, minHeight, minWidth, focus)
grid.AddItem(header, 0, 0, 1, 3, 0, 0, false)   // Header spans all columns
grid.AddItem(sidebar, 1, 0, 1, 1, 0, 0, false)  // Left column
grid.AddItem(main, 1, 1, 1, 1, 0, 0, true)      // Center
grid.AddItem(footer, 2, 0, 1, 3, 0, 0, false)   // Footer spans all columns
```

### Responsive Grid

```go
// Different layouts based on screen width
grid.SetRows(3, 0, 3).SetColumns(0).SetBorders(true)

// Wide screen layout
grid.AddItem(menu, 0, 0, 1, 3, 0, 0, false).
    AddItem(sidebar, 1, 0, 1, 1, 0, 100, false).  // minWidth=100
    AddItem(main, 1, 1, 1, 1, 0, 100, false).
    AddItem(aside, 1, 2, 1, 1, 0, 100, false).
    AddItem(footer, 2, 0, 1, 3, 0, 0, false)

// Narrow screen layout (added second, used when minWidth not met)
grid.AddItem(menu, 0, 0, 1, 1, 0, 0, false).
    AddItem(main, 1, 0, 1, 1, 0, 0, false).
    AddItem(footer, 2, 0, 1, 1, 0, 0, false)
```

### Pages Navigation

```go
pages := tview.NewPages()

// Add pages
pages.AddPage("main", mainView, true, true)     // resize=true, visible=true
pages.AddPage("settings", settingsView, true, false)
pages.AddPage("modal", modal, false, false)     // resize=false for modals

// Switch pages
pages.SwitchToPage("settings")

// Show modal (overlays current page)
pages.ShowPage("modal")

// Hide modal
pages.HidePage("modal")

// Check if page exists/is visible
if pages.HasPage("settings") { ... }
```

---

## Event Handling

### Input Capture (Custom Key Handling)

```go
// At application level
app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
    if event.Key() == tcell.KeyEsc {
        app.Stop()
        return nil  // Event consumed
    }
    return event  // Pass to focused primitive
})

// At primitive level
primitive.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
    switch event.Key() {
    case tcell.KeyCtrlS:
        save()
        return nil
    }
    return event
})
```

### Common Callbacks

```go
// InputField
inputField.SetDoneFunc(func(key tcell.Key) {
    switch key {
    case tcell.KeyEnter:
        submit(inputField.GetText())
    case tcell.KeyEscape:
        cancel()
    case tcell.KeyTab:
        app.SetFocus(nextField)
    }
})

inputField.SetChangedFunc(func(text string) {
    // Called on every keystroke
    updatePreview(text)
})

// List
list.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
    // Called when Enter pressed on item
})

list.SetChangedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
    // Called when selection changes (arrow keys)
})

// Table
table.SetSelectedFunc(func(row, column int) {
    // Called when Enter pressed on cell
})

table.SetSelectionChangedFunc(func(row, column int) {
    // Called when selection moves
})
```

---

## Concurrency

**Critical Rule**: Never modify UI from goroutines without synchronization.

### Safe UI Updates

```go
// QueueUpdateDraw: Update UI and trigger redraw
go func() {
    result := fetchData()
    app.QueueUpdateDraw(func() {
        textView.SetText(result)
    })
}()

// QueueUpdate: Update UI without immediate redraw
app.QueueUpdate(func() {
    // Multiple updates
    label1.SetText("...")
    label2.SetText("...")
})
app.Draw()  // Single redraw after all updates
```

### TextView Concurrency

TextView is special - it's safe to write directly:

```go
// Safe: TextView implements io.Writer
go func() {
    fmt.Fprintf(textView, "Log: %s\n", message)
    // But still need to trigger redraw:
    app.Draw()
}()
```

---

## Styling

### Borders and Titles

```go
box.SetBorder(true)
box.SetBorderColor(tcell.ColorBlue)
box.SetTitle(" Title ")
box.SetTitleAlign(tview.AlignCenter)  // AlignLeft, AlignCenter, AlignRight
box.SetTitleColor(tcell.ColorYellow)
```

### Colors

```go
// Named colors
tcell.ColorRed, tcell.ColorGreen, tcell.ColorBlue

// Default terminal colors
tcell.ColorDefault  // Terminal's default foreground/background

// 256-color palette
tcell.Color256  // Not commonly used directly

// True color (if terminal supports)
tcell.NewRGBColor(255, 128, 0)
```

### Text Formatting (in TextView, etc.)

```go
// Color tags
textView.SetDynamicColors(true)
fmt.Fprint(textView, "[red]Error:[white] Something went wrong")
fmt.Fprint(textView, "[#00ff00]Green text")  // Hex colors

// Style tags
fmt.Fprint(textView, "[::b]Bold[::-] Normal")  // Bold
fmt.Fprint(textView, "[::u]Underline[::-]")    // Underline
fmt.Fprint(textView, "[::r]Reverse[::-]")      // Reverse video

// Combined
fmt.Fprint(textView, "[red::b]Bold red text[-::-]")
```

---

## Common Patterns

### Modal Dialog

```go
func showModal(app *tview.Application, pages *tview.Pages, message string) {
    modal := tview.NewModal().
        SetText(message).
        AddButtons([]string{"OK", "Cancel"}).
        SetDoneFunc(func(buttonIndex int, buttonLabel string) {
            pages.HidePage("modal")
            if buttonLabel == "OK" {
                // Handle OK
            }
        })

    pages.AddPage("modal", modal, false, true)
}
```

### Form with Validation

```go
form := tview.NewForm().
    AddInputField("Name", "", 20, nil, nil).
    AddPasswordField("Password", "", 20, '*', nil).
    AddButton("Submit", func() {
        name := form.GetFormItemByLabel("Name").(*tview.InputField).GetText()
        if name == "" {
            showError("Name is required")
            return
        }
        submit(name)
    }).
    AddButton("Cancel", func() {
        app.Stop()
    })

form.SetBorder(true).SetTitle(" Login ").SetTitleAlign(tview.AlignCenter)
```

### Table with Data

```go
table := tview.NewTable().
    SetBorders(true).
    SetSelectable(true, false)  // rows selectable, not columns

// Header row
for col, header := range []string{"ID", "Name", "Status"} {
    table.SetCell(0, col,
        tview.NewTableCell(header).
            SetTextColor(tcell.ColorYellow).
            SetAlign(tview.AlignCenter).
            SetSelectable(false))
}

// Data rows
for row, item := range items {
    table.SetCell(row+1, 0, tview.NewTableCell(item.ID))
    table.SetCell(row+1, 1, tview.NewTableCell(item.Name))
    table.SetCell(row+1, 2, tview.NewTableCell(item.Status))
}

table.SetFixed(1, 0)  // Fix header row
```

### Focus Chain

```go
// Manual focus management
field1.SetDoneFunc(func(key tcell.Key) {
    if key == tcell.KeyTab {
        app.SetFocus(field2)
    }
})

field2.SetDoneFunc(func(key tcell.Key) {
    if key == tcell.KeyTab {
        app.SetFocus(field1)
    } else if key == tcell.KeyBacktab {
        app.SetFocus(field1)
    }
})
```
