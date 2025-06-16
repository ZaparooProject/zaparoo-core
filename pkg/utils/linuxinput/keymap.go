//go:build linux || darwin

package linuxinput

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/utils/linuxinput/keyboardmap"
	"github.com/bendahl/uinput"
)

// ToKeyboardCode converts a single ZapScript key symbol to a uinput code.
func ToKeyboardCode(name string) (int, bool) {
	if v, ok := keyboardmap.KeyboardMap[name]; ok {
		return v, ok
	} else {
		return 0, false
	}
}

var GamepadMap = map[string]int{
	"^":        uinput.ButtonDpadUp,
	"{up}":     uinput.ButtonDpadUp,
	"v":        uinput.ButtonDpadUp,
	"V":        uinput.ButtonDpadDown,
	"{down}":   uinput.ButtonDpadDown,
	"<":        uinput.ButtonDpadLeft,
	"{left}":   uinput.ButtonDpadLeft,
	">":        uinput.ButtonDpadRight,
	"{right}":  uinput.ButtonDpadRight,
	"A":        uinput.ButtonEast,
	"a":        uinput.ButtonEast,
	"{east}":   uinput.ButtonEast,
	"B":        uinput.ButtonSouth,
	"b":        uinput.ButtonSouth,
	"{south}":  uinput.ButtonSouth,
	"X":        uinput.ButtonNorth,
	"x":        uinput.ButtonNorth,
	"{north}":  uinput.ButtonNorth,
	"Y":        uinput.ButtonWest,
	"y":        uinput.ButtonWest,
	"{west}":   uinput.ButtonWest,
	"{start}":  uinput.ButtonStart,
	"{select}": uinput.ButtonSelect,
	"{menu}":   uinput.ButtonMode,
	"L":        uinput.ButtonBumperLeft,
	"l":        uinput.ButtonBumperLeft,
	"{l1}":     uinput.ButtonBumperLeft,
	"R":        uinput.ButtonBumperRight,
	"r":        uinput.ButtonBumperRight,
	"{r1}":     uinput.ButtonBumperRight,
	"{l2}":     uinput.ButtonTriggerLeft,
	"{r2}":     uinput.ButtonTriggerRight,
}

// ToGamepadCode converts a single ZapScript button symbol to a uinput code.
func ToGamepadCode(name string) (int, bool) {
	if v, ok := GamepadMap[name]; ok {
		return v, ok
	} else {
		return 0, false
	}
}
