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

package database

import (
	"context"
	"errors"
	"fmt"
	"slices"

	mediatags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// A multi-disc folder is one game whose discs are separate files. Its user
// preferences stay on those files, the way Library sync already sees them,
// and the folder reads as holding a flag when any of its discs does. A
// change to the set follows the same rules sync applies to the copies of a
// game: a flag turned on goes to one disc unless a disc already holds it,
// and a flag turned off, or cleared by its opposite, is cleared on all.

// DiscSetMember is one disc of a multi-disc folder. MediaDBID is 0 for a
// disc that is not indexed, which then gets no MediaDB projection.
type DiscSetMember struct {
	Path      string
	MediaDBID int64
}

// discSetSyncedFlags are the preferences a disc set shares. Hidden is not
// one of them: a disc is hidden on its own, and a folder by its path.
var discSetSyncedFlags = []MediaUserFlag{
	MediaUserFlagFavorite,
	MediaUserFlagLiked,
	MediaUserFlagDisliked,
	MediaUserFlagPlayLater,
}

// mediaUserFlagOpposites are the flags a flag turned on clears, as UserDB
// clears them on one row.
var mediaUserFlagOpposites = map[MediaUserFlag][]MediaUserFlag{
	MediaUserFlagFavorite: {MediaUserFlagDisliked},
	MediaUserFlagLiked:    {MediaUserFlagDisliked},
	MediaUserFlagDisliked: {MediaUserFlagLiked, MediaUserFlagFavorite},
}

// IsDiscSetFlag reports whether flag is shared by the discs of a set.
func IsDiscSetFlag(flag MediaUserFlag) bool {
	return slices.Contains(discSetSyncedFlags, flag)
}

// ApplyMediaUserFlagsToSet records flag changes for a multi-disc folder.
// launchPath names the disc that receives a flag no disc holds yet, and the
// only disc a hidden change applies to. Every UserDB write goes in one
// transaction, then each changed disc's MediaDB projection follows, under
// the same marker ApplyMediaUserFlags keeps. It returns the flags whose
// MediaDB tag changed on any disc, with their new value.
func ApplyMediaUserFlagsToSet(
	ctx context.Context,
	db *Database,
	systemID string,
	discs []DiscSetMember,
	launchPath string,
	changes map[MediaUserFlag]bool,
) (map[MediaUserFlag]bool, error) {
	requested := MediaUserData{}
	for flag, value := range changes {
		if !requested.setFlag(flag) {
			return nil, fmt.Errorf("unknown media user flag %q", flag)
		}
		if !value {
			clearFlag(&requested, flag)
		}
	}
	if err := requested.ValidateFlags(); err != nil {
		return nil, err
	}
	launch := slices.IndexFunc(discs, func(d DiscSetMember) bool { return d.Path == launchPath })
	if launch < 0 {
		return nil, errors.New("launch disc is not one of the set's discs")
	}

	mediaUserFlagsMu.Lock()
	defer mediaUserFlagsMu.Unlock()

	projects := slices.ContainsFunc(discs, func(d DiscSetMember) bool { return d.MediaDBID > 0 })
	if projects {
		if err := EnsureMediaWritable(db.MediaDB); err != nil {
			return nil, err
		}
	}
	reports := make([]func(), 0, len(discs))
	for i := range discs {
		reports = append(reports, db.watchMediaUserFlags(systemID, discs[i].Path))
	}
	defer func() {
		for _, report := range reports {
			report()
		}
	}()

	refs := make([]MediaUserRef, 0, len(discs))
	for i := range discs {
		refs = append(refs, MediaUserRef{SystemID: systemID, Path: discs[i].Path})
	}
	current, err := db.UserDB.GetMediaUserDataForPaths(refs)
	if err != nil {
		return nil, fmt.Errorf("failed to read media user data: %w", err)
	}
	writes := PlanDiscSetFlagWrites(systemID, discs, launch, current, changes)
	if len(writes) == 0 {
		return make(map[MediaUserFlag]bool), nil
	}

	markerHeld := false
	if projects {
		_, found, markerErr := db.UserDB.GetDeviceState(DeviceStateKeyMediaUserFlagWrite)
		if markerErr != nil {
			return nil, fmt.Errorf("failed to read media user flag write marker: %w", markerErr)
		}
		markerHeld = found
		if !markerHeld {
			if setErr := db.UserDB.SetDeviceState(DeviceStateKeyMediaUserFlagWrite, "1"); setErr != nil {
				return nil, fmt.Errorf("failed to mark media user flag write: %w", setErr)
			}
		}
	}

	if err := db.UserDB.SetMediaUserFlags(writes); err != nil {
		return nil, fmt.Errorf("failed to set media user flags: %w", err)
	}

	written := make(map[MediaUserFlag]bool)
	for i := range discs {
		if discs[i].MediaDBID <= 0 ||
			!slices.ContainsFunc(writes, func(w MediaUserFlagWrite) bool { return w.Path == discs[i].Path }) {
			continue
		}
		discWritten, projectErr := projectMediaUserFlags(ctx, db, systemID, discs[i].Path, discs[i].MediaDBID)
		if projectErr != nil {
			return nil, projectErr
		}
		for flag, value := range discWritten {
			// A flag turned on anywhere is the change the set shows.
			written[flag] = written[flag] || value
		}
	}
	if projects && !markerHeld {
		// A marker that cannot be cleared only costs the next start a reconcile.
		if delErr := db.UserDB.DeleteDeviceState(DeviceStateKeyMediaUserFlagWrite); delErr != nil {
			log.Warn().Err(delErr).Msg("failed to clear the media user flag write marker")
		}
	}
	return written, nil
}

// PlanDiscSetFlagWrites turns a change to a disc set into the UserDB writes
// that carry it out, given what each disc holds now (keyed by system and
// canonical path). Clears come before the set they make room for.
func PlanDiscSetFlagWrites(
	systemID string,
	discs []DiscSetMember,
	launch int,
	current map[MediaUserRef]MediaUserData,
	changes map[MediaUserFlag]bool,
) []MediaUserFlagWrite {
	holds := func(i int, flag MediaUserFlag) bool {
		row, ok := current[MediaUserRef{SystemID: systemID, Path: discs[i].Path}]
		return ok && row.Flag(flag)
	}
	anyHolds := func(flag MediaUserFlag) bool {
		for i := range discs {
			if holds(i, flag) {
				return true
			}
		}
		return false
	}
	var clears, sets []MediaUserFlagWrite
	clearEverywhere := func(flag MediaUserFlag) {
		for i := range discs {
			if !holds(i, flag) {
				continue
			}
			write := MediaUserFlagWrite{SystemID: systemID, Path: discs[i].Path, Flag: flag}
			if !slices.Contains(clears, write) {
				clears = append(clears, write)
			}
		}
	}
	for _, flag := range MediaUserFlags {
		value, ok := changes[flag]
		if !ok {
			continue
		}
		if !IsDiscSetFlag(flag) {
			if holds(launch, flag) != value {
				sets = append(sets, MediaUserFlagWrite{
					SystemID: systemID, Path: discs[launch].Path, Flag: flag, Value: value,
				})
			}
			continue
		}
		if !value {
			clearEverywhere(flag)
			continue
		}
		for _, opposite := range mediaUserFlagOpposites[flag] {
			clearEverywhere(opposite)
		}
		if !anyHolds(flag) {
			sets = append(sets, MediaUserFlagWrite{
				SystemID: systemID, Path: discs[launch].Path, Flag: flag, Value: true,
			})
		}
	}
	return append(clears, sets...)
}

// MergeDiscSetUserTags is the tag list a disc set's entry shows: the launch
// disc's tags with the shared user flags of every disc in place of its own.
// A dislike shows only when no disc of the set is liked or a favorite, as
// Library sync rolls the set up into one game.
func MergeDiscSetUserTags(launchTags []TagInfo, discTags ...[]TagInfo) []TagInfo {
	held := make(map[MediaUserFlag]bool, len(discSetSyncedFlags))
	collect := func(list []TagInfo) {
		for _, tag := range list {
			if tag.Type == string(mediatags.TagTypeUser) && IsDiscSetFlag(MediaUserFlag(tag.Tag)) {
				held[MediaUserFlag(tag.Tag)] = true
			}
		}
	}
	collect(launchTags)
	for _, list := range discTags {
		collect(list)
	}
	if held[MediaUserFlagFavorite] || held[MediaUserFlagLiked] {
		delete(held, MediaUserFlagDisliked)
	}

	merged := make([]TagInfo, 0, len(launchTags)+len(held))
	for _, tag := range launchTags {
		if tag.Type == string(mediatags.TagTypeUser) && IsDiscSetFlag(MediaUserFlag(tag.Tag)) {
			continue
		}
		merged = append(merged, tag)
	}
	for _, flag := range discSetSyncedFlags {
		if held[flag] {
			merged = append(merged, TagInfo{Type: string(mediatags.TagTypeUser), Tag: string(flag)})
		}
	}
	return merged
}

func clearFlag(data *MediaUserData, flag MediaUserFlag) {
	switch flag {
	case MediaUserFlagFavorite:
		data.IsFavorite = false
	case MediaUserFlagHidden:
		data.IsHidden = false
	case MediaUserFlagLiked:
		data.IsLiked = false
	case MediaUserFlagDisliked:
		data.IsDisliked = false
	case MediaUserFlagPlayLater:
		data.IsPlayLater = false
	}
}
