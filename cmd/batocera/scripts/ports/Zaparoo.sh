#!/bin/bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Zaparoo TUI Launcher for Batocera
# Launches Zaparoo TUI in xterm on top of EmulationStation

# Set environment for TUI compatibility
export DISPLAY=:0.0
export TERM=xterm-256color

# Launch xterm with Zaparoo TUI
# Using Batocera's standard xterm flags
# -en UTF-8: Enable UTF-8 encoding (fixes box-drawing characters)
# +sb: Disable scrollbar for fullscreen
# Window manager auto-fullscreens XTerm windows
xterm -fs 14 -fg white -bg black -fa 'Monospace' -en UTF-8 +sb -e /userdata/system/zaparoo
