//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package shared

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/keyboardmap"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

type inputMacroAction uint8

const (
	inputMacroNone inputMacroAction = iota
	inputMacroDelay
	inputMacroPress
	inputMacroRelease
	inputMacroHold
)

type parsedInputMacro struct {
	name     string
	duration string
	action   inputMacroAction
}

type heldInputState struct {
	keyboard map[int]struct{}
	gamepad  map[int]struct{}
}

type linuxInputSession struct {
	input  *LinuxInput
	closed bool
}

// NewInputSession creates an isolated owner for input held across API requests.
func (l *LinuxInput) NewInputSession() platforms.InputSession {
	return &linuxInputSession{input: l}
}

func (s *linuxInputSession) KeyboardPressSequence(
	ctx context.Context,
	args []string,
	interKeyDelay time.Duration,
) error {
	s.input.sequenceMu.Lock()
	defer s.input.sequenceMu.Unlock()

	if s.inputSessionClosed() {
		return errors.New("input session is closed")
	}

	err := s.input.keyboardPressSequenceLocked(ctx, args, interKeyDelay, s)
	if err == nil {
		return nil
	}
	return errors.Join(err, s.input.releaseInputSession(s))
}

func (s *linuxInputSession) GamepadPressSequence(
	ctx context.Context,
	args []string,
	interKeyDelay time.Duration,
) error {
	s.input.sequenceMu.Lock()
	defer s.input.sequenceMu.Unlock()

	if s.inputSessionClosed() {
		return errors.New("input session is closed")
	}

	err := s.input.gamepadPressSequenceLocked(ctx, args, interKeyDelay, s)
	if err == nil {
		return nil
	}
	return errors.Join(err, s.input.releaseInputSession(s))
}

func (s *linuxInputSession) inputSessionClosed() bool {
	s.input.inputMu.Lock()
	defer s.input.inputMu.Unlock()
	return s.closed
}

func (s *linuxInputSession) ReleaseAll() error {
	s.input.inputMu.Lock()
	defer s.input.inputMu.Unlock()

	s.closed = true
	return s.input.releaseInputSessionLocked(s)
}

func parseInputMacroToken(token string) (parsedInputMacro, bool) {
	if len(token) <= 2 || token[0] != '{' || token[len(token)-1] != '}' {
		return parsedInputMacro{}, false
	}

	inner := token[1 : len(token)-1]
	switch {
	case strings.HasPrefix(inner, "delay:"):
		return parsedInputMacro{action: inputMacroDelay, duration: inner[len("delay:"):]}, true
	case strings.HasPrefix(inner, "press:"):
		return parsedInputMacro{action: inputMacroPress, name: inner[len("press:"):]}, true
	case strings.HasPrefix(inner, "release:"):
		return parsedInputMacro{action: inputMacroRelease, name: inner[len("release:"):]}, true
	case strings.HasPrefix(inner, "hold:"):
		name, duration := splitInputHold(inner[len("hold:"):])
		return parsedInputMacro{action: inputMacroHold, name: name, duration: duration}, true
	case len(inner) > 1 && inner[0] == '_':
		return parsedInputMacro{action: inputMacroPress, name: inner[1:]}, true
	case len(inner) > 1 && inner[0] == '^':
		return parsedInputMacro{action: inputMacroRelease, name: inner[1:]}, true
	case len(inner) > 1 && inner[0] == '~':
		name, duration := splitInputHold(inner[1:])
		return parsedInputMacro{action: inputMacroHold, name: name, duration: duration}, true
	default:
		return parsedInputMacro{}, false
	}
}

func splitInputHold(value string) (name, duration string) {
	if idx := strings.LastIndex(value, ":"); idx != -1 {
		return value[:idx], value[idx+1:]
	}
	return value, ""
}

func sleepInputContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (l *LinuxInput) ensureInputStateLocked() {
	if l.inputSessions == nil {
		l.inputSessions = make(map[*linuxInputSession]*heldInputState)
	}
	if l.keyboardRefs == nil {
		l.keyboardRefs = make(map[int]int)
	}
	if l.gamepadRefs == nil {
		l.gamepadRefs = make(map[int]int)
	}
}

func (l *LinuxInput) sessionStateLocked(session *linuxInputSession) *heldInputState {
	l.ensureInputStateLocked()
	state, ok := l.inputSessions[session]
	if !ok {
		state = &heldInputState{
			keyboard: make(map[int]struct{}),
			gamepad:  make(map[int]struct{}),
		}
		l.inputSessions[session] = state
	}
	return state
}

func (l *LinuxInput) keyboardDownLocked(code int) error {
	l.ensureInputStateLocked()
	if l.keyboardRefs[code] > 0 {
		l.keyboardRefs[code]++
		return nil
	}
	if l.kbd.Device == nil {
		return errors.New("virtual keyboard is disabled")
	}
	if err := l.kbd.Device.KeyDown(code); err != nil {
		return fmt.Errorf("key down %d: %w", code, err)
	}
	l.keyboardRefs[code] = 1
	return nil
}

func (l *LinuxInput) keyboardUpLocked(code int) error {
	count := l.keyboardRefs[code]
	if count == 0 {
		return nil
	}
	if count > 1 {
		l.keyboardRefs[code] = count - 1
		return nil
	}
	if l.kbd.Device == nil {
		return errors.New("virtual keyboard is disabled")
	}
	if err := l.kbd.Device.KeyUp(code); err != nil {
		return fmt.Errorf("key up %d: %w", code, err)
	}
	delete(l.keyboardRefs, code)
	return nil
}

func (l *LinuxInput) gamepadDownLocked(code int) error {
	l.ensureInputStateLocked()
	if l.gamepadRefs[code] > 0 {
		l.gamepadRefs[code]++
		return nil
	}
	if l.gpd.Device == nil {
		return errors.New("virtual gamepad is disabled")
	}
	if err := l.gpd.Device.ButtonDown(code); err != nil {
		return fmt.Errorf("button down %d: %w", code, err)
	}
	l.gamepadRefs[code] = 1
	return nil
}

func (l *LinuxInput) gamepadUpLocked(code int) error {
	count := l.gamepadRefs[code]
	if count == 0 {
		return nil
	}
	if count > 1 {
		l.gamepadRefs[code] = count - 1
		return nil
	}
	if l.gpd.Device == nil {
		return errors.New("virtual gamepad is disabled")
	}
	if err := l.gpd.Device.ButtonUp(code); err != nil {
		return fmt.Errorf("button up %d: %w", code, err)
	}
	delete(l.gamepadRefs, code)
	return nil
}

func (l *LinuxInput) sessionKeyboardDownLocked(session *linuxInputSession, code int) error {
	if session.closed {
		return errors.New("input session is closed")
	}
	state := l.sessionStateLocked(session)
	if _, ok := state.keyboard[code]; ok {
		return nil
	}
	if err := l.keyboardDownLocked(code); err != nil {
		return err
	}
	state.keyboard[code] = struct{}{}
	return nil
}

func (l *LinuxInput) sessionKeyboardUpLocked(session *linuxInputSession, code int) error {
	if session.closed {
		return errors.New("input session is closed")
	}
	state, ok := l.inputSessions[session]
	if !ok {
		return nil
	}
	if _, ok := state.keyboard[code]; !ok {
		return nil
	}
	if err := l.keyboardUpLocked(code); err != nil {
		return err
	}
	delete(state.keyboard, code)
	l.removeEmptySessionStateLocked(session, state)
	return nil
}

func (l *LinuxInput) sessionGamepadDownLocked(session *linuxInputSession, code int) error {
	if session.closed {
		return errors.New("input session is closed")
	}
	state := l.sessionStateLocked(session)
	if _, ok := state.gamepad[code]; ok {
		return nil
	}
	if err := l.gamepadDownLocked(code); err != nil {
		return err
	}
	state.gamepad[code] = struct{}{}
	return nil
}

func (l *LinuxInput) sessionGamepadUpLocked(session *linuxInputSession, code int) error {
	if session.closed {
		return errors.New("input session is closed")
	}
	state, ok := l.inputSessions[session]
	if !ok {
		return nil
	}
	if _, ok := state.gamepad[code]; !ok {
		return nil
	}
	if err := l.gamepadUpLocked(code); err != nil {
		return err
	}
	delete(state.gamepad, code)
	l.removeEmptySessionStateLocked(session, state)
	return nil
}

func (l *LinuxInput) removeEmptySessionStateLocked(session *linuxInputSession, state *heldInputState) {
	if len(state.keyboard) == 0 && len(state.gamepad) == 0 {
		delete(l.inputSessions, session)
	}
}

func (l *LinuxInput) releaseInputSessionLocked(session *linuxInputSession) error {
	state, ok := l.inputSessions[session]
	if !ok {
		return nil
	}

	var cleanupErr error
	for code := range state.keyboard {
		if err := l.keyboardUpLocked(code); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
			continue
		}
		delete(state.keyboard, code)
	}
	for code := range state.gamepad {
		if err := l.gamepadUpLocked(code); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
			continue
		}
		delete(state.gamepad, code)
	}
	l.removeEmptySessionStateLocked(session, state)
	return cleanupErr
}

func (l *LinuxInput) releaseInputSession(session *linuxInputSession) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.releaseInputSessionLocked(session)
}

func (l *LinuxInput) keyboardLocalDown(held map[int]int, code int) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.keyboardLocalDownLocked(held, code)
}

func (l *LinuxInput) keyboardLocalUp(held map[int]int, code int) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.keyboardLocalUpLocked(held, code)
}

func (l *LinuxInput) gamepadLocalDown(held map[int]int, code int) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.gamepadLocalDownLocked(held, code)
}

func (l *LinuxInput) gamepadLocalUp(held map[int]int, code int) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.gamepadLocalUpLocked(held, code)
}

func (l *LinuxInput) releaseKeyboardLocals(held map[int]int) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.releaseKeyboardLocalsLocked(held)
}

func (l *LinuxInput) releaseGamepadLocals(held map[int]int) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.releaseGamepadLocalsLocked(held)
}

func (l *LinuxInput) sessionKeyboardDown(session *linuxInputSession, code int) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.sessionKeyboardDownLocked(session, code)
}

func (l *LinuxInput) sessionKeyboardUp(session *linuxInputSession, code int) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.sessionKeyboardUpLocked(session, code)
}

func (l *LinuxInput) sessionGamepadDown(session *linuxInputSession, code int) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.sessionGamepadDownLocked(session, code)
}

func (l *LinuxInput) sessionGamepadUp(session *linuxInputSession, code int) error {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.sessionGamepadUpLocked(session, code)
}

func (l *LinuxInput) keyboardDeviceEnabled() bool {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.kbd.Device != nil
}

func (l *LinuxInput) gamepadDeviceEnabled() bool {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()
	return l.gpd.Device != nil
}

func (l *LinuxInput) keyboardLocalDownLocked(held map[int]int, code int) error {
	if err := l.keyboardDownLocked(code); err != nil {
		return err
	}
	held[code]++
	return nil
}

func (l *LinuxInput) keyboardLocalUpLocked(held map[int]int, code int) error {
	if held[code] == 0 {
		return nil
	}
	if err := l.keyboardUpLocked(code); err != nil {
		return err
	}
	if held[code] == 1 {
		delete(held, code)
	} else {
		held[code]--
	}
	return nil
}

func (l *LinuxInput) gamepadLocalDownLocked(held map[int]int, code int) error {
	if err := l.gamepadDownLocked(code); err != nil {
		return err
	}
	held[code]++
	return nil
}

func (l *LinuxInput) gamepadLocalUpLocked(held map[int]int, code int) error {
	if held[code] == 0 {
		return nil
	}
	if err := l.gamepadUpLocked(code); err != nil {
		return err
	}
	if held[code] == 1 {
		delete(held, code)
	} else {
		held[code]--
	}
	return nil
}

func (l *LinuxInput) releaseKeyboardLocalsLocked(held map[int]int) error {
	var cleanupErr error
	for code, count := range held {
		for range count {
			if err := l.keyboardUpLocked(code); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
				break
			}
		}
	}
	return cleanupErr
}

func (l *LinuxInput) releaseGamepadLocalsLocked(held map[int]int) error {
	var cleanupErr error
	for code, count := range held {
		for range count {
			if err := l.gamepadUpLocked(code); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
				break
			}
		}
	}
	return cleanupErr
}

func (l *LinuxInput) pressKeyboardToken(arg string) (retErr error) {
	codes, isCombo, err := linuxinput.ParseKeyCombo(arg)
	if err != nil {
		return fmt.Errorf("failed to parse key combo: %w", err)
	}
	if !isCombo && codes[0] < 0 {
		codes = []int{42, -codes[0]}
		isCombo = true
	}

	l.sequenceMu.Lock()
	defer l.sequenceMu.Unlock()
	if !l.keyboardDeviceEnabled() {
		return errors.New("virtual keyboard is disabled")
	}

	held := make(map[int]int)
	var pressErr error
	defer func() {
		retErr = errors.Join(retErr, l.releaseKeyboardLocals(held))
	}()
	for _, code := range codes {
		if err := l.keyboardLocalDown(held, code); err != nil {
			pressErr = err
			break
		}
	}
	if pressErr == nil {
		pressErr = sleepInputContext(context.Background(), l.kbd.Delay)
	}
	if pressErr == nil {
		for i := len(codes) - 1; i >= 0; i-- {
			if err := l.keyboardLocalUp(held, codes[i]); err != nil {
				pressErr = err
				break
			}
		}
	}
	if pressErr != nil {
		if isCombo {
			return fmt.Errorf("failed to press keyboard combo: %w", pressErr)
		}
		return fmt.Errorf("failed to press keyboard key: %w", pressErr)
	}
	return nil
}

func (l *LinuxInput) pressGamepadToken(name string) (retErr error) {
	l.sequenceMu.Lock()
	defer l.sequenceMu.Unlock()
	if !l.gamepadDeviceEnabled() {
		return errors.New("virtual gamepad is disabled")
	}
	code, ok := linuxinput.ToGamepadCode(name)
	if !ok {
		return fmt.Errorf("unknown button: %s", name)
	}

	held := make(map[int]int)
	defer func() {
		retErr = errors.Join(retErr, l.releaseGamepadLocals(held))
	}()
	if err := l.gamepadLocalDown(held, code); err != nil {
		return fmt.Errorf("failed to press gamepad button %s: %w", name, err)
	}
	if err := sleepInputContext(context.Background(), l.gpd.Delay); err != nil {
		return fmt.Errorf("failed to press gamepad button %s: %w", name, err)
	}
	if err := l.gamepadLocalUp(held, code); err != nil {
		return fmt.Errorf("failed to press gamepad button %s: %w", name, err)
	}
	return nil
}

func (l *LinuxInput) pressKeyboardSequence(args []string, interKeyDelay time.Duration) error {
	l.sequenceMu.Lock()
	defer l.sequenceMu.Unlock()
	return l.keyboardPressSequenceLocked(context.Background(), args, interKeyDelay, nil)
}

func (l *LinuxInput) pressGamepadSequence(args []string, interKeyDelay time.Duration) error {
	l.sequenceMu.Lock()
	defer l.sequenceMu.Unlock()
	return l.gamepadPressSequenceLocked(context.Background(), args, interKeyDelay, nil)
}

func (l *LinuxInput) keyboardPressSequenceLocked(
	ctx context.Context,
	args []string,
	interKeyDelay time.Duration,
	session *linuxInputSession,
) (retErr error) {
	if !l.keyboardDeviceEnabled() {
		return errors.New("virtual keyboard is disabled")
	}
	if interKeyDelay == 0 {
		interKeyDelay = DefaultInterKeyDelay
	}

	const shiftCode = 42
	localHeld := make(map[int]int)
	defer func() {
		retErr = errors.Join(retErr, l.releaseKeyboardLocals(localHeld))
	}()

	localDown := func(code int) error {
		return l.keyboardLocalDown(localHeld, code)
	}
	localUp := func(code int) error {
		return l.keyboardLocalUp(localHeld, code)
	}
	persistentDown := func(code int) error {
		if session == nil {
			return localDown(code)
		}
		return l.sessionKeyboardDown(session, code)
	}
	persistentUp := func(code int) error {
		if session == nil {
			return localUp(code)
		}
		return l.sessionKeyboardUp(session, code)
	}

	for i := 0; i < len(args); {
		if err := ctx.Err(); err != nil {
			return err
		}
		token := args[i]
		if macro, ok := parseInputMacroToken(token); ok {
			switch macro.action {
			case inputMacroNone:
				return fmt.Errorf("unsupported input macro token %q", token)
			case inputMacroDelay:
				duration, err := parseMacroDuration(macro.duration)
				if err != nil {
					return fmt.Errorf("invalid delay token %q: %w", token, err)
				}
				if err := sleepInputContext(ctx, duration); err != nil {
					return fmt.Errorf("delay token %q: %w", token, err)
				}
			case inputMacroPress, inputMacroRelease, inputMacroHold:
				code, err := resolveHoldKeyCode(macro.name)
				if err != nil {
					return fmt.Errorf("token %q: %w", token, err)
				}
				switch macro.action {
				case inputMacroNone, inputMacroDelay:
					return fmt.Errorf("unsupported input macro token %q", token)
				case inputMacroPress:
					if downErr := persistentDown(code); downErr != nil {
						return fmt.Errorf("token %q: %w", token, downErr)
					}
				case inputMacroRelease:
					if upErr := persistentUp(code); upErr != nil {
						return fmt.Errorf("token %q: %w", token, upErr)
					}
				case inputMacroHold:
					holdDuration := l.kbd.Delay
					if macro.duration != "" {
						holdDuration, err = parseMacroDuration(macro.duration)
						if err != nil {
							return fmt.Errorf("invalid hold duration in %q: %w", token, err)
						}
					}
					if err := localDown(code); err != nil {
						return fmt.Errorf("token %q key down: %w", token, err)
					}
					if err := sleepInputContext(ctx, holdDuration); err != nil {
						return fmt.Errorf("token %q hold: %w", token, err)
					}
					if err := localUp(code); err != nil {
						return fmt.Errorf("token %q key up: %w", token, err)
					}
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			i++
			continue
		}

		if baseCode, ok := keyboardmap.IsShiftedKey(token); ok {
			end := i + 1
			for end < len(args) {
				if _, shifted := keyboardmap.IsShiftedKey(args[end]); !shifted {
					break
				}
				end++
			}
			if err := localDown(shiftCode); err != nil {
				return fmt.Errorf("failed to press shift down: %w", err)
			}
			for j := i; j < end; j++ {
				if j > i {
					baseCode, _ = keyboardmap.IsShiftedKey(args[j])
				}
				if err := localDown(baseCode); err != nil {
					return fmt.Errorf("failed to press shifted key %q down: %w", args[j], err)
				}
				if err := sleepInputContext(ctx, l.kbd.Delay); err != nil {
					return fmt.Errorf("press shifted key %q: %w", args[j], err)
				}
				if err := localUp(baseCode); err != nil {
					return fmt.Errorf("failed to release shifted key %q: %w", args[j], err)
				}
				if err := sleepInputContext(ctx, interKeyDelay); err != nil {
					return fmt.Errorf("inter-key delay: %w", err)
				}
			}
			if err := localUp(shiftCode); err != nil {
				return fmt.Errorf("failed to release shift: %w", err)
			}
			i = end
			continue
		}

		codes, _, err := linuxinput.ParseKeyCombo(token)
		if err != nil {
			return fmt.Errorf("failed to parse key %q: %w", token, err)
		}
		for _, code := range codes {
			if err := localDown(code); err != nil {
				return fmt.Errorf("failed to press key %q down: %w", token, err)
			}
		}
		if err := sleepInputContext(ctx, l.kbd.Delay); err != nil {
			return fmt.Errorf("press key %q: %w", token, err)
		}
		for j := len(codes) - 1; j >= 0; j-- {
			if err := localUp(codes[j]); err != nil {
				return fmt.Errorf("failed to release key %q: %w", token, err)
			}
		}
		if err := sleepInputContext(ctx, interKeyDelay); err != nil {
			return fmt.Errorf("inter-key delay: %w", err)
		}
		i++
	}
	return nil
}

func resolveGamepadHoldCode(name string) (int, error) {
	arg := name
	if len([]rune(name)) > 1 {
		arg = "{" + name + "}"
	}
	code, ok := linuxinput.ToGamepadCode(arg)
	if !ok {
		return 0, fmt.Errorf("unknown button: %s", name)
	}
	return code, nil
}

func (l *LinuxInput) gamepadPressSequenceLocked(
	ctx context.Context,
	args []string,
	interKeyDelay time.Duration,
	session *linuxInputSession,
) (retErr error) {
	if !l.gamepadDeviceEnabled() {
		return errors.New("virtual gamepad is disabled")
	}
	if interKeyDelay == 0 {
		interKeyDelay = DefaultInterKeyDelay
	}

	localHeld := make(map[int]int)
	defer func() {
		retErr = errors.Join(retErr, l.releaseGamepadLocals(localHeld))
	}()
	localDown := func(code int) error {
		return l.gamepadLocalDown(localHeld, code)
	}
	localUp := func(code int) error {
		return l.gamepadLocalUp(localHeld, code)
	}
	persistentDown := func(code int) error {
		if session == nil {
			return localDown(code)
		}
		return l.sessionGamepadDown(session, code)
	}
	persistentUp := func(code int) error {
		if session == nil {
			return localUp(code)
		}
		return l.sessionGamepadUp(session, code)
	}

	for _, token := range args {
		if err := ctx.Err(); err != nil {
			return err
		}
		if macro, ok := parseInputMacroToken(token); ok {
			switch macro.action {
			case inputMacroNone:
				return fmt.Errorf("unsupported input macro token %q", token)
			case inputMacroDelay:
				duration, err := parseMacroDuration(macro.duration)
				if err != nil {
					return fmt.Errorf("invalid delay token %q: %w", token, err)
				}
				if err := sleepInputContext(ctx, duration); err != nil {
					return fmt.Errorf("delay token %q: %w", token, err)
				}
			case inputMacroPress, inputMacroRelease, inputMacroHold:
				code, err := resolveGamepadHoldCode(macro.name)
				if err != nil {
					return fmt.Errorf("token %q: %w", token, err)
				}
				switch macro.action {
				case inputMacroNone, inputMacroDelay:
					return fmt.Errorf("unsupported input macro token %q", token)
				case inputMacroPress:
					if downErr := persistentDown(code); downErr != nil {
						return fmt.Errorf("token %q: %w", token, downErr)
					}
				case inputMacroRelease:
					if upErr := persistentUp(code); upErr != nil {
						return fmt.Errorf("token %q: %w", token, upErr)
					}
				case inputMacroHold:
					holdDuration := l.gpd.Delay
					if macro.duration != "" {
						holdDuration, err = parseMacroDuration(macro.duration)
						if err != nil {
							return fmt.Errorf("invalid hold duration in %q: %w", token, err)
						}
					}
					if err := localDown(code); err != nil {
						return fmt.Errorf("token %q button down: %w", token, err)
					}
					if err := sleepInputContext(ctx, holdDuration); err != nil {
						return fmt.Errorf("token %q hold: %w", token, err)
					}
					if err := localUp(code); err != nil {
						return fmt.Errorf("token %q button up: %w", token, err)
					}
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			continue
		}

		code, ok := linuxinput.ToGamepadCode(token)
		if !ok {
			return fmt.Errorf("unknown button: %s", token)
		}
		if err := localDown(code); err != nil {
			return fmt.Errorf("failed to press gamepad button %s: %w", token, err)
		}
		if err := sleepInputContext(ctx, l.gpd.Delay); err != nil {
			return fmt.Errorf("press gamepad button %s: %w", token, err)
		}
		if err := localUp(code); err != nil {
			return fmt.Errorf("failed to release gamepad button %s: %w", token, err)
		}
		if err := sleepInputContext(ctx, interKeyDelay); err != nil {
			return fmt.Errorf("inter-button delay: %w", err)
		}
	}
	return nil
}

func (l *LinuxInput) closeInputDevices() {
	l.inputMu.Lock()
	defer l.inputMu.Unlock()

	for session := range l.inputSessions {
		session.closed = true
		if err := l.releaseInputSessionLocked(session); err != nil {
			log.Warn().Err(err).Msg("error releasing input session during device shutdown")
		}
	}
	for code := range l.keyboardRefs {
		if l.kbd.Device != nil {
			if err := l.kbd.Device.KeyUp(code); err != nil {
				log.Warn().Err(err).Int("key_code", code).Msg("error releasing keyboard key during device shutdown")
			}
		}
		delete(l.keyboardRefs, code)
	}
	for code := range l.gamepadRefs {
		if l.gpd.Device != nil {
			if err := l.gpd.Device.ButtonUp(code); err != nil {
				log.Warn().Err(err).Int("button_code", code).
					Msg("error releasing gamepad button during device shutdown")
			}
		}
		delete(l.gamepadRefs, code)
	}
	clear(l.inputSessions)

	if l.kbd.Device != nil {
		if err := l.kbd.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing keyboard")
		}
		l.kbd.Device = nil
	}
	if l.gpd.Device != nil {
		if err := l.gpd.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing gamepad")
		}
		l.gpd.Device = nil
	}
}
