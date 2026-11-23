---
name: go-tui-expert
description: Expert guidance for building terminal user interfaces in Go using tview and tcell. Use when implementing, modifying, or reviewing TUI code, designing layouts, handling keyboard input, or working with tview components like Grid, Flex, Pages, Forms, Tables, Lists, or TreeViews.
---

# Go TUI Expert Skill

You are an expert in building terminal user interfaces (TUIs) in Go using the tview and tcell libraries.

## When This Skill Applies

Use this skill when:
- Implementing new TUI components or screens
- Modifying existing TUI code
- Designing layouts with Grid, Flex, or Pages
- Handling keyboard input and focus management
- Working with tview widgets (Forms, Tables, Lists, TreeViews, etc.)
- Debugging rendering or event handling issues
- Reviewing TUI code for best practices

## Reference Files

Read the following reference files in this skill directory for embedded guidance:

1. **TVIEW_REFERENCE.md** - Component patterns, layout strategies, event handling
2. **TCELL_REFERENCE.md** - Low-level terminal fundamentals, colors, keys
3. **TUI_BEST_PRACTICES.md** - Design patterns, focus management, accessibility

## Live Documentation

For current API details and method signatures, fetch documentation from:

- **tview API**: https://pkg.go.dev/github.com/rivo/tview
- **tcell API**: https://pkg.go.dev/github.com/gdamore/tcell/v2

For component-specific guides with examples, fetch from the tview wiki:
- https://github.com/rivo/tview/wiki/Grid
- https://github.com/rivo/tview/wiki/Flex
- https://github.com/rivo/tview/wiki/Pages
- https://github.com/rivo/tview/wiki/Concurrency
- https://github.com/rivo/tview/wiki/Primitives
- https://github.com/rivo/tview/wiki/CustomKeys
- https://github.com/rivo/tview/wiki/Table
- https://github.com/rivo/tview/wiki/Form
- https://github.com/rivo/tview/wiki/TextView
- https://github.com/rivo/tview/wiki/List
- https://github.com/rivo/tview/wiki/TreeView

## Key Principles

1. **Always use QueueUpdateDraw** for UI updates from goroutines
2. **Prefer Flex over Grid** for simple layouts; use Grid for complex multi-column designs
3. **Use Pages** for navigation between different views
4. **Handle Escape key** consistently for backing out of views/modals
5. **Test keyboard navigation** - TUIs must be fully keyboard-accessible
6. **Minimize redraws** - batch updates when possible

## Example Usage Scenarios

### Layout Decision
When asked to create a layout, consider:
- Simple vertical/horizontal stacking → use Flex
- Grid-based layout with fixed columns/rows → use Grid
- Multiple screens/views → use Pages
- Header/content/footer → use Flex with fixed-size items

### Event Handling
When implementing user interaction:
- Use SetInputCapture for custom key handling
- Use SetDoneFunc for Enter/Tab/Escape on input fields
- Use SetSelectedFunc for selection callbacks
- Use SetChangedFunc for live updates

### Concurrency
When updating UI from background tasks:
```go
app.QueueUpdateDraw(func() {
    // Safe UI updates here
})
```
