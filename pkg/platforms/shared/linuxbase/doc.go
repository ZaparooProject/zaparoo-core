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

// Package linuxbase provides a shared base implementation for Linux-family
// platforms (Linux, SteamOS, Bazzite, ChimeraOS). It uses Go's struct
// embedding to allow platforms to share common functionality while
// customizing only what differs (primarily the Launchers method).
//
// Usage:
//
//	type Platform struct {
//	    *linuxbase.Base
//	}
//
//	func NewPlatform() *Platform {
//	    return &Platform{
//	        Base: linuxbase.NewBase(platforms.PlatformIDBazzite),
//	    }
//	}
//
//	func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
//	    // Platform-specific launchers
//	}
package linuxbase
