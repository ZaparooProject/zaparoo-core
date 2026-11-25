# tcell Reference Guide

tcell is the low-level terminal library that tview is built on. Understanding tcell helps when:
- Implementing custom primitives
- Handling advanced key combinations
- Working with colors and styles at a low level
- Debugging rendering issues

---

## Screen Model

### Cells and Coordinates

The terminal is a grid of cells. Each cell contains:
- A primary rune (character)
- Optional combining runes
- A style (foreground, background, attributes)

```go
// Get screen dimensions
width, height := screen.Size()

// Set cell content
screen.SetContent(x, y, 'A', nil, style)
// x=column, y=row (0-indexed, origin top-left)

// Set cell with combining characters
screen.SetContent(x, y, 'e', []rune{'\u0301'}, style)  // é

// Show changes
screen.Show()  // or screen.Sync() for full redraw
```

### Screen Lifecycle (Direct tcell Usage)

```go
// Create screen
screen, err := tcell.NewScreen()
if err != nil {
    log.Fatal(err)
}

// Initialize
if err := screen.Init(); err != nil {
    log.Fatal(err)
}
defer screen.Fini()  // Clean up terminal on exit

// Clear screen
screen.Clear()

// Main loop
for {
    ev := screen.PollEvent()
    // Handle events...
    screen.Show()
}
```

---

## Events

### Event Types

```go
switch ev := screen.PollEvent().(type) {
case *tcell.EventKey:
    // Keyboard input
case *tcell.EventMouse:
    // Mouse input (if enabled)
case *tcell.EventResize:
    // Terminal resized
    screen.Sync()  // Redraw everything
case *tcell.EventInterrupt:
    // Interrupt signal
case *tcell.EventError:
    // Error occurred
}
```

### EventKey Details

```go
ev := event.(*tcell.EventKey)

// Get key type
key := ev.Key()

// Get character (only valid for KeyRune)
if key == tcell.KeyRune {
    char := ev.Rune()
}

// Get modifiers
mods := ev.Modifiers()
if mods&tcell.ModCtrl != 0 { /* Ctrl held */ }
if mods&tcell.ModAlt != 0 { /* Alt held */ }
if mods&tcell.ModShift != 0 { /* Shift held */ }
```

---

## Keys

### Special Keys

```go
// Navigation
tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight
tcell.KeyHome, tcell.KeyEnd
tcell.KeyPgUp, tcell.KeyPgDn

// Editing
tcell.KeyEnter
tcell.KeyTab, tcell.KeyBacktab  // Shift+Tab
tcell.KeyBackspace, tcell.KeyBackspace2
tcell.KeyDelete
tcell.KeyInsert

// Control
tcell.KeyEscape, tcell.KeyEsc
tcell.KeyCtrlC, tcell.KeyCtrlD, tcell.KeyCtrlZ
// (All Ctrl+letter combinations: KeyCtrlA through KeyCtrlZ)

// Function keys
tcell.KeyF1 through tcell.KeyF12

// Regular character
tcell.KeyRune  // Use ev.Rune() to get the character
```

### Checking Key Combinations

```go
func handleKey(ev *tcell.EventKey) {
    // Ctrl+S
    if ev.Key() == tcell.KeyCtrlS {
        save()
        return
    }

    // Alt+X (modifier with rune)
    if ev.Key() == tcell.KeyRune && ev.Modifiers()&tcell.ModAlt != 0 {
        if ev.Rune() == 'x' {
            exit()
            return
        }
    }

    // Regular character
    if ev.Key() == tcell.KeyRune {
        insertChar(ev.Rune())
    }
}
```

---

## Colors

### Color Types

```go
// Default terminal colors (recommended for themes)
tcell.ColorDefault

// Basic 8 colors
tcell.ColorBlack, tcell.ColorMaroon, tcell.ColorGreen, tcell.ColorOlive
tcell.ColorNavy, tcell.ColorPurple, tcell.ColorTeal, tcell.ColorSilver

// Bright variants
tcell.ColorGray, tcell.ColorRed, tcell.ColorLime, tcell.ColorYellow
tcell.ColorBlue, tcell.ColorFuchsia, tcell.ColorAqua, tcell.ColorWhite

// 256-color palette (index 0-255)
tcell.PaletteColor(42)

// True color (24-bit RGB)
tcell.NewRGBColor(255, 128, 0)  // Orange

// Hex color
tcell.GetColor("#ff8000")
```

### Color Tips

```go
// Use ColorDefault for terminal theme compatibility
style := tcell.StyleDefault.
    Foreground(tcell.ColorDefault).
    Background(tcell.ColorDefault)

// Check terminal capabilities
if screen.Colors() >= 256 {
    // Can use 256-color palette
}
if screen.Colors() >= 16777216 {
    // Can use true color
}
```

---

## Styles

### Creating Styles

```go
// Start from default
style := tcell.StyleDefault

// Set colors
style = style.Foreground(tcell.ColorRed)
style = style.Background(tcell.ColorBlue)

// Chained
style := tcell.StyleDefault.
    Foreground(tcell.ColorYellow).
    Background(tcell.ColorBlack)
```

### Style Attributes

```go
style := tcell.StyleDefault.
    Bold(true).
    Italic(true).
    Underline(true).
    StrikeThrough(true).
    Reverse(true).  // Swap fg/bg
    Blink(true).    // Not widely supported
    Dim(true)       // Reduced intensity

// Reset to normal
style = tcell.StyleDefault
```

### Common Style Patterns

```go
var (
    styleNormal    = tcell.StyleDefault
    styleHighlight = tcell.StyleDefault.Reverse(true)
    styleError     = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
    styleDisabled  = tcell.StyleDefault.Foreground(tcell.ColorGray)
    styleFocused   = tcell.StyleDefault.Background(tcell.ColorNavy)
)
```

---

## Integration with tview

### When to Use tcell Directly

Use tcell directly when:
1. Creating custom primitives
2. Drawing custom graphics
3. Handling raw terminal events
4. Needing precise control over rendering

### In Custom Primitives

```go
type CustomWidget struct {
    *tview.Box
}

func (c *CustomWidget) Draw(screen tcell.Screen) {
    // Let Box draw border/title
    c.Box.DrawForSubclass(screen, c)

    // Get inner area (inside border)
    x, y, width, height := c.GetInnerRect()

    // Draw custom content
    style := tcell.StyleDefault.Foreground(tcell.ColorGreen)
    for col := 0; col < width; col++ {
        screen.SetContent(x+col, y, '-', nil, style)
    }
}
```

### Accessing Screen in tview

```go
// In SetDrawFunc callback
primitive.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
    // Custom drawing here
    style := tcell.StyleDefault.Foreground(tcell.ColorRed)
    tview.Print(screen, "Custom", x, y, width, tview.AlignLeft, tcell.ColorRed)
    return x, y, width, height
})
```

---

## Mouse Support

### Enabling Mouse

```go
// In tcell
screen.EnableMouse()

// In tview
app.EnableMouse(true)
```

### Mouse Events

```go
case *tcell.EventMouse:
    x, y := ev.Position()
    buttons := ev.Buttons()

    if buttons&tcell.Button1 != 0 {
        // Left click
    }
    if buttons&tcell.Button2 != 0 {
        // Middle click
    }
    if buttons&tcell.Button3 != 0 {
        // Right click
    }
    if buttons&tcell.WheelUp != 0 {
        // Scroll up
    }
    if buttons&tcell.WheelDown != 0 {
        // Scroll down
    }
```

---

## Terminal Capabilities

### Checking Capabilities

```go
// Number of colors
colors := screen.Colors()

// Can display unicode
hasUnicode := screen.CharacterSet() == "UTF-8"

// Screen size
width, height := screen.Size()

// Has mouse support
screen.HasMouse()
```

### Setting Terminal Title

```go
// In tview (v0.42.0+)
app.SetTitle("My Application")

// Or directly with escape sequences (less portable)
fmt.Print("\033]0;My Title\007")
```
