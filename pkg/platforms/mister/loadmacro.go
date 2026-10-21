//go:build linux && !android

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

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/inputmacro"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput/keyboardmap"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker/activegame"
	"github.com/rs/zerolog/log"
)

const (
	// loadKeyDelay is the gap between typed keys. Slower than the 100 ms input
	// default is never needed; the 8-bit machines scan their keyboards far
	// faster than this.
	loadKeyDelay = 100 * time.Millisecond
	// loadPlanFreshness is how long a recorded plan waits for its probe before
	// it is considered abandoned.
	loadPlanFreshness = 2 * time.Minute
	// loadTimeoutPad is added to a plan's own time when sizing the ready bound,
	// to cover the core taking its media.
	loadTimeoutPad = 15 * time.Second
	// loadKeyFudge scales the typing estimate to cover shifted keys, which
	// take extra presses.
	loadKeyFudge = 2
)

var errLoadKeyboardUnavailable = errors.New("platform cannot type")

// keyLayout names the keys that type the characters a core's keyboard does not
// take at face value. The cores map the PC keyboard by position, so a symbol
// is the key and shift of the machine's own keycap, not of the PC's.
type keyLayout struct {
	symbols map[rune]string
}

// token returns the keys that type r. Letters are typed unshifted, which is the
// machine's plain letter: a shifted PC letter is a graphics character on most
// of these machines.
func (l keyLayout) token(r rune) string {
	if t, ok := l.symbols[r]; ok {
		return t
	}
	if r >= 'A' && r <= 'Z' {
		return string(r - 'A' + 'a')
	}
	return string(r)
}

// loadStep is one piece of input a load plan types once the core has its media.
type loadStep struct {
	// text is typed as keys. A {name} or {a+b} token presses a special key or
	// combination; anything else is typed one character at a time.
	text string
	// pause waits before text is typed.
	pause time.Duration
}

// loadPlan describes a launch that needs input after the core takes its media:
// a command that starts the program the media holds.
type loadPlan struct {
	// layout maps the characters of the steps onto the machine's keyboard.
	layout keyLayout
	// target is the file Main loads. For a game inside an image it is the image.
	target string
	steps  []loadStep
	// options are OSD options the core needs set for the media to be usable,
	// and a reset for a machine that must start the media from a fresh boot.
	options []mgls.Setting
	// settle waits after the core has its media, for the machine to reach the
	// state the first step expects. There is no signal for this, so it is a
	// fixed time per core.
	settle time.Duration
}

// loadPlanner resolves the plan for a launch path. ok is false when the media
// needs no input, which launches exactly as it always has.
type loadPlanner func(cfg *config.Instance, path string) (plan loadPlan, ok bool, err error)

func typeText(text string) loadStep { return loadStep{text: text} }

func typeAfter(pause time.Duration, text string) loadStep {
	return loadStep{pause: pause, text: text}
}

// keyTokens splits step text into the tokens the keyboard sequence takes and
// checks each one names a key.
func keyTokens(text string, layout keyLayout) ([]string, error) {
	tokens := make([]string, 0, len(text))
	for rest := text; rest != ""; {
		if rest[0] == '{' {
			end := strings.IndexByte(rest, '}')
			if end < 0 {
				return nil, fmt.Errorf("unterminated key token in %q", text)
			}
			tokens = append(tokens, rest[:end+1])
			rest = rest[end+1:]
			continue
		}
		r, size := utf8.DecodeRuneInString(rest)
		if r == utf8.RuneError {
			return nil, fmt.Errorf("invalid character in %q", text)
		}
		mapped := layout.token(r)
		if len(mapped) > 1 && strings.HasPrefix(mapped, "{") {
			// A layout may map a character to a sequence of keys, such as a chord.
			parts, err := keyTokens(mapped, keyLayout{})
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, parts...)
		} else {
			tokens = append(tokens, mapped)
		}
		rest = rest[size:]
	}
	for _, token := range tokens {
		if err := checkKeyToken(token); err != nil {
			return nil, err
		}
	}
	return tokens, nil
}

// maxMacroDelay bounds an inline delay in step text.
const maxMacroDelay = 10 * time.Second

// checkKeyToken checks a token is a key, a combination, or an inline press,
// release, hold or delay control the keyboard sequence understands.
func checkKeyToken(token string) error {
	control := inputmacro.ClassifyControl(token)
	switch control.Action {
	case inputmacro.ActionNone:
		return checkKeyName(token)
	case inputmacro.ActionDelay:
		return checkMacroDelay(control.Value)
	case inputmacro.ActionHold:
		name, duration := inputmacro.SplitHold(control.Value)
		if duration != "" {
			if err := checkMacroDelay(duration); err != nil {
				return err
			}
		}
		return checkKeyName(braceKeyName(name))
	default:
		return checkKeyName(braceKeyName(control.Value))
	}
}

func braceKeyName(name string) string {
	if utf8.RuneCountInString(name) > 1 && !strings.HasPrefix(name, "{") {
		return "{" + name + "}"
	}
	return name
}

func checkKeyName(token string) error {
	if _, _, err := keyboardmap.ParseKeyCombo(token); err != nil {
		return fmt.Errorf("key %q: %w", token, err)
	}
	return nil
}

func checkMacroDelay(value string) error {
	d, err := time.ParseDuration(value)
	if err != nil {
		ms, convErr := strconv.Atoi(value)
		if convErr != nil {
			return fmt.Errorf("delay %q: %w", value, err)
		}
		d = time.Duration(ms) * time.Millisecond
	}
	if d < 0 || d > maxMacroDelay {
		return fmt.Errorf("delay %q is out of range", value)
	}
	return nil
}

// typingTime estimates how long the plan's input takes, for sizing the ready
// bound.
func (p *loadPlan) typingTime() time.Duration {
	total := p.settle
	for _, step := range p.steps {
		total += step.pause + time.Duration(len(step.text))*loadKeyDelay*loadKeyFudge
	}
	return total
}

// readyTimeout is the bound the service gives the whole wait: the core taking
// its media, then the plan's input.
func (p *loadPlan) readyTimeout() time.Duration {
	return loadTimeoutPad + p.typingTime()
}

type pendingPlan struct {
	recorded time.Time
	plan     loadPlan
}

// loadPlans holds the plan of each launch until its readiness probe runs. The
// launch and the probe are separate calls into separately built launcher
// lists, so the hand-off goes through the shared readiness state.
type loadPlans struct {
	byPath map[string]pendingPlan
	mu     syncutil.Mutex
}

func (l *loadPlans) record(path string, plan *loadPlan, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byPath == nil {
		l.byPath = make(map[string]pendingPlan)
	}
	for key, pending := range l.byPath {
		if now.Sub(pending.recorded) > loadPlanFreshness {
			delete(l.byPath, key)
		}
	}
	l.byPath[path] = pendingPlan{plan: *plan, recorded: now}
}

func (l *loadPlans) peek(path string, now time.Time) (loadPlan, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	pending, ok := l.byPath[path]
	if !ok || now.Sub(pending.recorded) > loadPlanFreshness {
		return loadPlan{}, false
	}
	return pending.plan, true
}

func (l *loadPlans) take(path string, now time.Time) (loadPlan, bool) {
	plan, ok := l.peek(path, now)
	if ok {
		l.mu.Lock()
		delete(l.byPath, path)
		l.mu.Unlock()
	}
	return plan, ok
}

// launchPlanned launches a path that has a load plan. The core loads the plan's
// target with the plan's options; the input itself is typed by the readiness
// probe, once Main has handed the media to the core, so the launch returns at
// once and media is published as it always is.
func launchPlanned(
	pl platforms.Platform,
	coreID string,
	plan loadPlanner,
	fallback func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error),
) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		resolved, ok, err := plan(cfg, path)
		if err != nil {
			return nil, err
		}
		if !ok {
			return fallback(cfg, path, opts)
		}
		for _, step := range resolved.steps {
			if _, tokenErr := keyTokens(step.text, resolved.layout); tokenErr != nil {
				return nil, fmt.Errorf("load plan for %s: %w", path, tokenErr)
			}
		}

		launchReadiness.plans.record(path, &resolved, launchReadiness.clock.Now())
		proc, err := launchWithOptions(pl, coreID, resolved.options)(cfg, resolved.target, opts)
		if err != nil {
			launchReadiness.plans.take(path, launchReadiness.clock.Now())
			return nil, err
		}

		// The core was launched with the image or file; name the game inside it
		// as the active one, so media and history carry the game's own path.
		if path != resolved.target {
			if setErr := activegame.SetActiveGame(path); setErr != nil {
				return nil, fmt.Errorf("failed to set active game: %w", setErr)
			}
		}
		return proc, nil
	}
}

// preflightPlan resolves the plan before the running media is stopped, so a
// game that cannot be found fails the launch instead of ending what was playing.
func preflightPlan(
	plan loadPlanner,
	prior func(*config.Instance, string, *platforms.LaunchOptions) error,
) func(*config.Instance, string, *platforms.LaunchOptions) error {
	return func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) error {
		if prior != nil {
			if err := prior(cfg, path, opts); err != nil {
				return err
			}
		}
		_, _, err := plan(cfg, path)
		return err
	}
}

// runPlan types the plan recorded for path, once the core has its media. It
// returns nil when the launch has no plan.
func (r *coreReadiness) runPlan(ctx context.Context, path string) error {
	plan, ok := r.plans.take(path, r.clock.Now())
	if !ok {
		return nil
	}

	r.keyMu.Lock()
	press := r.pressSequence
	r.keyMu.Unlock()
	if press == nil {
		return fmt.Errorf("%w: no keyboard", errLoadKeyboardUnavailable)
	}

	if err := r.sleep(ctx, plan.settle); err != nil {
		return fmt.Errorf("wait for the machine to boot: %w", err)
	}
	for _, step := range plan.steps {
		if err := r.sleep(ctx, step.pause); err != nil {
			return fmt.Errorf("wait to type: %w", err)
		}
		tokens, err := keyTokens(step.text, plan.layout)
		if err != nil {
			return err
		}
		if len(tokens) == 0 {
			continue
		}
		log.Debug().Str("path", path).Int("keys", len(tokens)).Msg("typing load command")
		if err := press(tokens, loadKeyDelay); err != nil {
			return fmt.Errorf("type load command: %w", err)
		}
	}
	return nil
}

func (r *coreReadiness) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("sleep: %w", ctx.Err())
	case <-r.clock.After(d):
		return nil
	}
}

// keyboardSequencer is implemented by platforms that take a whole key sequence.
type keyboardSequencer interface {
	KeyboardPressSequence(args []string, interKeyDelay time.Duration) error
}

// pressSequenceFor returns the platform's sequence typer, or one that presses
// keys one at a time when the platform has no sequence method.
func pressSequenceFor(pl platforms.Platform) func([]string, time.Duration) error {
	if ks, ok := pl.(keyboardSequencer); ok {
		return ks.KeyboardPressSequence
	}
	return func(tokens []string, delay time.Duration) error {
		for _, token := range tokens {
			if err := pl.KeyboardPress(token); err != nil {
				return fmt.Errorf("press %q: %w", token, err)
			}
			time.Sleep(delay)
		}
		return nil
	}
}

// withLoadPlans gives the launchers of cores that need typed input after
// loading their media a planner. A launcher keeps its own scanner and test,
// which the image planners extend.
func withLoadPlans(pl platforms.Platform, ls []platforms.Launcher) []platforms.Launcher {
	planners := loadPlannersFor(pl)
	for i := range ls {
		planner, ok := planners[ls[i].SystemID]
		if !ok || ls[i].Launch == nil || ls[i].ID != ls[i].SystemID {
			continue
		}
		ls[i].Launch = launchPlanned(pl, ls[i].SystemID, planner.plan, ls[i].Launch)
		ls[i].Preflight = preflightPlan(planner.plan, ls[i].Preflight)
		if planner.extend != nil {
			planner.extend(&ls[i])
		}
	}
	return ls
}

// loadPlannerSet pairs a planner with the launcher changes it needs, such as
// listing the games inside an image.
type loadPlannerSet struct {
	plan   loadPlanner
	extend func(*platforms.Launcher)
}
