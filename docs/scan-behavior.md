# Scan Behavior

This is the expected behavior of scanning tokens.

---

## Default Behavior: `readers.scan.mode='tap'`

When `readers.scan.mode='tap'` (default setting) and a game is launched using a card:

- [ ] Removing the card from the reader won't close the game.
- [ ] Leaving the card on the reader will have no effect.
- [ ] Tapping another card to launch a game will start the new game without returning to the core menu.
- [ ] Tapping the same card will reload the game from the beginning.
- [ ] Tapping a command like `input.coin` will execute the command without interrupting the game.
- [ ] Exiting the game manually through the internal menu will reset the state, allowing you to tap any card to launch a different game.
- [ ] Exiting the game manually while the card remains on the reader will not cause the game to relaunch once in the menu.

---

## Behavior: `readers.scan.mode='hold'` and `readers.scan.exit_delay=0.0`

When `readers.scan.mode='hold'` with `readers.scan.exit_delay=0.0` and a game is launched using a card:

- [ ] Removing the card from the reader will close the game immediately after `on_remove` completes, including when removal happens before launch initialization finishes. If `on_remove` contains a delay, that hook delay completes before the zero-delay hold exit starts.
- [ ] The system's `before_exit` script, if configured, runs after `on_remove` and immediately before the game closes.
- [ ] Scanning and removing a command card will execute the command without closing the game; only the launch card owns Hold mode exit.
- [ ] Exiting the game manually while the card is still on the reader will not cause the game to relaunch when returning to the menu.
- [ ] Exiting the game manually via the internal menu and then removing the card won't trigger a core menu reload.

---

## Behavior: `readers.scan.mode='hold'` and `readers.scan.exit_delay=N`

When `readers.scan.mode='hold'` with `readers.scan.exit_delay=N` and a game is launched using a card:

- [ ] Removing the card from the reader runs `on_remove` first, then starts the **N-second** hold-exit countdown after the hook completes. Any hook delay therefore precedes `exit_delay` rather than overlapping it.
- [ ] The full order on a hold-mode exit is `on_remove`, then `exit_delay`, then the system's `before_exit` script, then the game closes.
- [ ] Removing the card and reinserting it before the N-second countdown ends will not interrupt the ongoing game.
- [ ] When `on_remove` contains a `delay` command, reinserting the removed card during that delay cancels the remaining hook commands without relaunching the game.
- [ ] Removing the card and tapping a different game card will immediately launch the other game.
- [ ] Removing the launch card and tapping a command card will execute the command without resetting or transferring the countdown. Removing the command card will not trigger another exit.
- [ ] Exiting the game manually while the card is still on the reader will not cause the game to relaunch when returning to the menu.
- [ ] Exiting the game manually via the internal menu and then removing the card won't trigger a core menu reload.
- [ ] Exiting the game manually during the N-second countdown cancels the countdown and returns to the menu.

---

## Behavior: the system `before_exit` hook

A `[[systems.default]]` entry can carry a `before_exit` script that runs just
before media for that system stops or is replaced:

```toml
[[systems.default]]
system = "SNES"
before_exit = "**input.keyboard:{f2}"
```

The script is looked up against the outgoing media's system, so it applies
regardless of which launcher started it.

- [ ] Tapping a second card runs `before_exit` before the new game starts.
- [ ] `**stop`, `**playlist.stop` and the `media.stop` API method each run it
      before returning to the menu.
- [ ] A playtime limit runs it before it stops the game.
- [ ] Removing the card in hold mode runs it after `on_remove` and `exit_delay`.
- [ ] Quitting the game from the emulator or frontend does **not** run it; by
      then the media is already gone.
- [ ] Background-slot audio does not run it, and a primary launch does not run
      the hook for background media.
- [ ] A script that fails still lets the game exit, and the failure is logged.
- [ ] A script that stalls stops the exit for at most 30 seconds.
- [ ] Only one `before_exit` script runs at a time, so a script that itself
      stops or launches media does not trigger another.

---

## Per-reader mode: `[readers.drivers]` and `[[readers.connect]]`

A reader can override the global mode, so a cartridge slot can hold on a device
whose NFC antenna taps. `scan_mode` on a `[[readers.connect]]` entry applies to
that one device; on `[readers.drivers.<id>]` it applies to every reader using
that driver. An empty or unrecognised value falls through to the next level.

```toml
[readers.scan]
mode = "tap"

[readers.drivers.opticaldrive]
scan_mode = "hold"

[[readers.connect]]
driver = "pn532"
path = "/dev/ttyUSB1"
scan_mode = "hold"
```

- [ ] A reader with no `scan_mode` behaves exactly as `readers.scan.mode` says.
- [ ] A `[[readers.connect]]` entry's `scan_mode` wins over its driver's.
- [ ] A driver's `scan_mode` wins over `readers.scan.mode`.
- [ ] With one hold reader and one tap reader, removing from the tap reader
      leaves media running and removing from the hold reader exits it.
- [ ] Setting `scan_mode` never changes `readers.scan.mode`, and the app's Scan
      mode setting still reads and writes only the global value.
- [ ] The `readers` API method reports each reader's effective mode.

---

## Per-token override: the `#tap` and `#hold` traits

One token can override the mode it would otherwise inherit, without changing any
setting. Both traits are boolean, so the shorthand form is the normal one:

```
#tap||/media/games/SNES/Mario.sfc
#hold||**launch:/media/games/SNES/Mario.sfc
```

The value form works as well, since `#hold=false` means the same as `#tap`.
Trait keys are case-insensitive.

- [ ] With `readers.scan.mode='hold'`, removing a `#tap` token leaves the game
      running, and its `on_remove` hook does not run.
- [ ] With `readers.scan.mode='tap'`, removing a `#hold` token closes the game
      after `readers.scan.exit_delay`.
- [ ] A token's trait wins over its reader's `scan_mode`.
- [ ] A `#tap` token that launches media takes over hold ownership, so removing
      the card that launched the *previous* game does not close the new one.
- [ ] A `#tap` token that runs only a command leaves the current hold owner
      alone; removing the owner's card still exits.
- [ ] A token carrying both `#tap` and `#hold` is treated as carrying neither,
      and inherits the normal mode.
- [ ] A playlist started by a `#tap` token keeps tap behavior across every
      track, and a playlist item's own traits do not change it.

A misspelled trait is silent: `#taap||...` is not an error, it is simply not an
override. This follows ZapScript's existing rule that an unrecognised trait key
mixed with other content falls back to being launch content.

Traits are settled once, when the token enters the system, and describe the
token rather than a step in running it:

- [ ] A hook script or an injected command that declares `#tap` or `#hold` does
      not change the mode of the token that triggered it.
- [ ] Every track of a playlist runs under the mode the card that started the
      playlist declared.
