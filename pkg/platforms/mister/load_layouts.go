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

// The layouts below map the characters the plans type onto each core's
// keyboard. The cores take the PC keyboard by position, so what a character
// needs is found on the machine's own keycaps.

// commodoreLayout is the C64 family: the quote is shift-2, the star its own
// key at the PC's right bracket, and the colon the PC's semicolon.
func commodoreLayout() keyLayout {
	return keyLayout{symbols: map[rune]string{'"': "{shift+2}", '*': "]", ':': ";", ';': "'"}}
}

// acornLayout is the Electron and the BBC Micro: the star is shift on the key
// the PC labels with a quote, which is the colon key of the Acorn machines. The
// equals is shift-minus and the ampersand shift-6.
func acornLayout() keyLayout {
	return keyLayout{symbols: map[rune]string{
		'*': "{shift+'}",
		'"': "{shift+2}",
		':': "'",
		'=': "{shift+-}",
		'&': "{shift+6}",
	}}
}

// spectrumLayout is the Spectrum core, whose keymap puts the double quote on the
// unshifted quote key; the other symbols are symbol shift (control) and the key
// they sit on.
func spectrumLayout() keyLayout {
	return keyLayout{symbols: map[rune]string{
		'"': "'",
		'.': chord("ctrl", "m"),
		'~': chord("ctrl", "a"),
		'_': chord("ctrl", "0"),
		'-': chord("ctrl", "j"),
	}}
}

// enterpriseLayout is the Enterprise core, whose positional keyboard puts the
// colon on the unshifted quote key.
func enterpriseLayout() keyLayout {
	return keyLayout{symbols: map[rune]string{':': "'"}}
}

// atomLayout is the Acorn Atom core: the quote is shift-2.
func atomLayout() keyLayout {
	return keyLayout{symbols: map[rune]string{'"': "{shift+2}"}}
}

// zx81Layout is the ZX81 core: the quote is shift-P on the machine's keycaps.
func zx81Layout() keyLayout {
	return keyLayout{symbols: map[rune]string{'"': "{shift+p}"}}
}

// amstradLayout is the CPC core: the quote is shift-2, the bar shift on the key
// the PC labels with a bracket, and the colon and star sit on the semicolon key.
// The core can miss a quick shift, so shifted symbols are held chords and the
// plans type their commands in lowercase, which BASIC and AMSDOS accept.
func amstradLayout() keyLayout {
	return keyLayout{symbols: map[rune]string{
		'"': chord("shift", "2"),
		'|': chord("shift", "["),
		':': ";",
		'*': chord("shift", ";"),
		';': "'",
	}}
}

// nextLayout is the ZX Spectrum Next core: the quote is the unshifted quote key,
// and the other symbols are symbol shift (control) with the key they sit on.
func nextLayout() keyLayout {
	return keyLayout{symbols: map[rune]string{
		'"': "'",
		':': chord("ctrl", "z"),
		'$': chord("ctrl", "4"),
		'=': chord("ctrl", "l"),
	}}
}
