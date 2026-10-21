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

package fatimg

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

const (
	maxDirEntries   = 65536
	maxDirSlots     = 1 << 21
	maxWalkSlots    = 1 << 23
	defaultDepth    = 16
	defaultEntries  = 100000
	attrDirectory   = 0x10
	attrVolumeLabel = 0x08
	attrLFN         = 0x0F
	lfnLast         = 0x40
	maxLFNEntries   = 20
)

// Entry is a file or directory in a FAT directory.
type Entry struct {
	// Name is the long name if the entry has a valid one, else the short name.
	Name string
	// ShortName is the 8.3 name, as NAME.EXT.
	ShortName string
	Size      uint32
	cluster   uint32
	IsDir     bool
	root      bool
}

// Path is an entry's location, '/' separated and rooted at the volume.
type Path struct {
	Long  string
	Short string
}

// WalkOptions bounds a recursive walk. Zero values select the defaults.
// Entries in a directory at depth MaxDepth are not listed, where the root is
// depth 0.
type WalkOptions struct {
	MaxDepth   int
	MaxEntries int
}

func (v *volume) rootEntry() Entry {
	return Entry{IsDir: true, root: true, cluster: v.rootClus}
}

// oemString maps an OEM code page 437/850 style byte string to UTF-8 using
// Latin-1 code points for the high half.
func oemString(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

func shortName(raw []byte, ntFlags byte) string {
	name := make([]byte, 8)
	copy(name, raw[0:8])
	if name[0] == 0x05 {
		name[0] = 0xE5
	}
	ext := raw[8:11]
	base := strings.TrimRight(oemString(name), " ")
	e := strings.TrimRight(oemString(ext), " ")
	if ntFlags&0x08 != 0 {
		base = strings.ToLower(base)
	}
	if ntFlags&0x10 != 0 {
		e = strings.ToLower(e)
	}
	if e == "" {
		return base
	}
	return base + "." + e
}

func lfnChecksum(raw []byte) byte {
	var sum byte
	for _, b := range raw[:11] {
		sum = (sum>>1 | sum<<7) + b
	}
	return sum
}

type lfnState struct {
	expected int
	total    int
	parts    [maxLFNEntries + 1][13]uint16
	checksum byte
}

func (l *lfnState) reset() { l.expected, l.total = 0, 0 }

func (l *lfnState) add(slot []byte) {
	ord := int(slot[0])
	n := ord &^ lfnLast
	if n < 1 || n > maxLFNEntries {
		l.reset()
		return
	}
	if ord&lfnLast != 0 {
		l.total = n
		l.expected = n
		l.checksum = slot[13]
	} else if l.expected == 0 || n != l.expected-1 || slot[13] != l.checksum {
		l.reset()
		return
	}
	l.expected = n
	p := &l.parts[n]
	for i := range 5 {
		p[i] = binary.LittleEndian.Uint16(slot[1+i*2:])
	}
	for i := range 6 {
		p[5+i] = binary.LittleEndian.Uint16(slot[14+i*2:])
	}
	for i := range 2 {
		p[11+i] = binary.LittleEndian.Uint16(slot[28+i*2:])
	}
}

// name returns the decoded long name if the collected entries are complete
// and match the short entry's checksum.
func (l *lfnState) name(short []byte) (string, bool) {
	defer l.reset()
	if l.total == 0 || l.expected != 1 || l.checksum != lfnChecksum(short) {
		return "", false
	}
	units := make([]uint16, 0, l.total*13)
	for n := 1; n <= l.total; n++ {
		units = append(units, l.parts[n][:]...)
	}
	for i, u := range units {
		if u == 0 {
			units = units[:i]
			break
		}
	}
	for len(units) > 0 && units[len(units)-1] == 0xFFFF {
		units = units[:len(units)-1]
	}
	if len(units) == 0 {
		return "", false
	}
	return string(utf16.Decode(units)), true
}

func isDotEntry(raw []byte) bool {
	if raw[0] != '.' {
		return false
	}
	for _, c := range raw[2:11] {
		if c != ' ' {
			return false
		}
	}
	return raw[1] == ' ' || raw[1] == '.'
}

// eachSlot calls fn for every 32-byte slot of a directory until fn stops it,
// the end marker is reached, or the budget runs out.
func (v *volume) eachSlot(dir Entry, budget *int, fn func(slot []byte) (bool, error)) error {
	scan := func(buf []byte) (bool, error) {
		for off := 0; off+32 <= len(buf); off += 32 {
			if *budget <= 0 {
				return true, ErrTooManyEntries
			}
			*budget--
			if buf[off] == 0 {
				return true, nil
			}
			if stop, err := fn(buf[off : off+32]); err != nil || stop {
				return true, err
			}
		}
		return false, nil
	}

	if dir.root && v.typ != FAT32 {
		buf := make([]byte, v.bps)
		for off := int64(0); off < v.rootBytes; off += v.bps {
			n := min(v.bps, v.rootBytes-off)
			if err := readFull(v.r, buf[:n], v.rootStart+off); err != nil {
				return err
			}
			if stop, err := scan(buf[:n]); stop {
				return err
			}
		}
		return nil
	}
	if !v.validCluster(dir.cluster) {
		return nil
	}
	buf := make([]byte, v.clBytes)
	return v.walkChain(dir.cluster, func(c uint32) (bool, error) {
		if err := readFull(v.r, buf, v.clusterOffset(c)); err != nil {
			return false, err
		}
		return scan(buf)
	})
}

// readDirRaw lists a directory and also returns its volume label, if any.
func (v *volume) readDirRaw(dir Entry, budget *int) ([]Entry, string, error) {
	var out []Entry
	var label string
	var lfn lfnState
	err := v.eachSlot(dir, budget, func(slot []byte) (bool, error) {
		if slot[0] == 0xE5 {
			lfn.reset()
			return false, nil
		}
		attr := slot[11]
		if attr&0x3F == attrLFN {
			lfn.add(slot)
			return false, nil
		}
		if attr&attrVolumeLabel != 0 {
			lfn.reset()
			if attr&attrDirectory == 0 && label == "" {
				label = strings.TrimRight(oemString(slot[:11]), " ")
			}
			return false, nil
		}
		long, hasLong := lfn.name(slot)
		if isDotEntry(slot) {
			return false, nil
		}
		if len(out) >= maxDirEntries {
			return true, ErrTooManyEntries
		}
		e := Entry{
			ShortName: shortName(slot, slot[12]),
			IsDir:     attr&attrDirectory != 0,
			Size:      binary.LittleEndian.Uint32(slot[28:32]),
			cluster:   uint32(binary.LittleEndian.Uint16(slot[26:28])),
		}
		if v.typ == FAT32 {
			e.cluster |= uint32(binary.LittleEndian.Uint16(slot[20:22])) << 16
		}
		e.Name = e.ShortName
		if hasLong {
			e.Name = long
		}
		out = append(out, e)
		return false, nil
	})
	return out, label, err
}

func splitPath(p string) ([]string, error) {
	var parts []string
	for _, c := range strings.Split(p, "/") {
		switch c {
		case "", ".":
		case "..":
			return nil, ErrInvalidPath
		default:
			if strings.ContainsRune(c, 0) {
				return nil, ErrInvalidPath
			}
			parts = append(parts, c)
		}
	}
	return parts, nil
}

func (v *volume) resolve(p string, budget *int) (Entry, Path, error) {
	parts, err := splitPath(p)
	if err != nil {
		return Entry{}, Path{}, err
	}
	cur := v.rootEntry()
	var path Path
	for _, part := range parts {
		if !cur.IsDir {
			return Entry{}, Path{}, ErrNotDir
		}
		entries, _, err := v.readDirRaw(cur, budget)
		if err != nil {
			return Entry{}, Path{}, err
		}
		found := false
		for _, e := range entries {
			if strings.EqualFold(e.Name, part) || strings.EqualFold(e.ShortName, part) {
				cur, found = e, true
				path = Path{Long: path.Long + "/" + e.Name, Short: path.Short + "/" + e.ShortName}
				break
			}
		}
		if !found {
			return Entry{}, Path{}, fmt.Errorf("%w: %s", ErrNotFound, p)
		}
	}
	return cur, path, nil
}

// ReadDir lists a directory. dir is '/' separated; "" and "/" are the root.
// Names match case-insensitively against long or short names. If the
// directory holds too many entries, the first ones are returned along with
// ErrTooManyEntries.
func (v *Volume) ReadDir(dir string) ([]Entry, error) {
	budget := maxDirSlots
	e, _, err := v.v.resolve(dir, &budget)
	if err != nil {
		return nil, err
	}
	if !e.IsDir {
		return nil, ErrNotDir
	}
	entries, _, err := v.v.readDirRaw(e, &budget)
	return entries, err
}

// ReadFile reads a whole file, failing with ErrTooLarge if it exceeds max
// bytes.
func (v *Volume) ReadFile(path string, maxSize int64) ([]byte, error) {
	if maxSize < 0 {
		return nil, ErrInvalidPath
	}
	vol := v.v
	budget := maxDirSlots
	e, _, err := vol.resolve(path, &budget)
	if err != nil {
		return nil, err
	}
	if e.IsDir {
		return nil, ErrIsDir
	}
	size := int64(e.Size)
	if size > maxSize {
		return nil, ErrTooLarge
	}
	if size == 0 {
		return []byte{}, nil
	}
	if size > int64(vol.clusters)*vol.clBytes {
		return nil, fmt.Errorf("%w: size exceeds volume", ErrCorrupt)
	}
	out := make([]byte, size)
	var done int64
	err = vol.walkChain(e.cluster, func(c uint32) (bool, error) {
		n := min(vol.clBytes, size-done)
		if rerr := readFull(vol.r, out[done:done+n], vol.clusterOffset(c)); rerr != nil {
			return false, rerr
		}
		done += n
		return done >= size, nil
	})
	if err != nil {
		return nil, err
	}
	if done < size {
		return nil, fmt.Errorf("%w: cluster chain shorter than file", ErrCorrupt)
	}
	return out, nil
}

// Walk visits every entry under root depth-first in directory order,
// calling fn with the entry's path. Directories are visited before their
// contents. It returns ErrTooManyEntries once MaxEntries entries have been
// visited and more remain.
func (v *Volume) Walk(root string, opts WalkOptions, fn func(p Path, e Entry) error) error {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultDepth
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = defaultEntries
	}
	budget := maxWalkSlots
	start, startPath, err := v.v.resolve(root, &budget)
	if err != nil {
		return err
	}
	if !start.IsDir {
		return ErrNotDir
	}
	w := walker{v: v.v, opts: opts, fn: fn, budget: &budget}
	return w.dir(start, startPath, 0)
}

type walker struct {
	v       *volume
	fn      func(p Path, e Entry) error
	budget  *int
	active  map[uint32]bool
	opts    WalkOptions
	visited int
}

func (w *walker) dir(d Entry, dp Path, depth int) error {
	entries, _, err := w.v.readDirRaw(d, w.budget)
	if err != nil {
		return err
	}
	if w.active == nil {
		w.active = make(map[uint32]bool)
	}
	w.active[d.cluster] = true
	defer delete(w.active, d.cluster)

	for _, e := range entries {
		if w.visited >= w.opts.MaxEntries {
			return ErrTooManyEntries
		}
		w.visited++
		p := Path{Long: dp.Long + "/" + e.Name, Short: dp.Short + "/" + e.ShortName}
		if err := w.fn(p, e); err != nil {
			return err
		}
		if e.IsDir && depth+1 < w.opts.MaxDepth && w.v.validCluster(e.cluster) && !w.active[e.cluster] {
			if err := w.dir(e, p, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
