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

- [ ] Removing the card from the reader will immediately close the game.
- [ ] Exiting the game manually while the card is still on the reader will not cause the game to relaunch when returning to the menu.
- [ ] Exiting the game manually via the internal menu and then removing the card won't trigger a core menu reload.

---

## Behavior: `readers.scan.mode='hold'` and `readers.scan.exit_delay=N`

When `readers.scan.mode='hold'` with `readers.scan.exit_delay=N` and a game is launched using a card:

- [ ] Removing the card from the reader will close the game after **N seconds**.
- [ ] Removing the card and reinserting it before the N-second countdown ends will not interrupt the ongoing game.
- [ ] Removing the card and tapping a different game card will immediately launch the other game.
- [ ] Removing the card and tapping a command card will execute the command and reset the countdown timer. You can repeatedly tap various command cards without changing the game, resetting the timer each time, and then reinsert the original game card to continue the session.
- [ ] Exiting the game manually while the card is still on the reader will not cause the game to relaunch when returning to the menu.
- [ ] Exiting the game manually via the internal menu and then removing the card won't trigger a core menu reload.
- [ ] Exiting the game manually during the N-second countdown cancels the countdown and returns to the menu.
