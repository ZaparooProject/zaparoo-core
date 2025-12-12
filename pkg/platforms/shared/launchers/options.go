//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package launchers

// LutrisOptions configures the Lutris launcher behavior.
type LutrisOptions struct {
	// CheckFlatpak enables checking for Flatpak Lutris installation.
	// Flatpak path: ~/.var/app/net.lutris.Lutris/
	CheckFlatpak bool
}

// HeroicOptions configures the Heroic launcher behavior.
type HeroicOptions struct {
	// CheckFlatpak enables checking for Flatpak Heroic installation.
	// Flatpak path: ~/.var/app/com.heroicgameslauncher.hgl/
	CheckFlatpak bool
}
