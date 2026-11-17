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

package models

const (
	MethodKiosk                 = "kiosk"
	MethodKioskEnable           = "kiosk.enable"
	MethodKioskDisable          = "kiosk.disable"
	MethodKioskUnlock           = "kiosk.unlock"
	MethodKioskLock             = "kiosk.lock"
	MethodKioskUpdate           = "kiosk.update"
	MethodKioskAllow            = "kiosk.allow"
	MethodProfilesContent       = "profiles.content"
	MethodProfilesContentUpdate = "profiles.content.update"

	NotificationKioskState         = "kiosk.state"
	NotificationKioskLaunchRefused = "kiosk.launch.refused"
)

// KioskSettings are the settings of the kiosk lock. Durations are whole
// seconds.
type KioskSettings struct {
	Preset             string `json:"preset"`
	DefaultContentMode string `json:"defaultContentMode"`
	Downloads          string `json:"downloads"`
	IdleTimeout        int    `json:"idleTimeout"`
	UnlockDuration     int    `json:"unlockDuration"`
	DeviceUnlock       bool   `json:"deviceUnlock"`
	ProfileSwitching   bool   `json:"profileSwitching"`
	ExtensionCard      bool   `json:"extensionCard"`
	IdleReset          bool   `json:"idleReset"`
}

// KioskSettingsPatch changes some kiosk settings and leaves the rest.
type KioskSettingsPatch struct {
	DefaultContentMode *string `json:"defaultContentMode" validate:"omitempty,oneof=all allowed"`
	Downloads          *string `json:"downloads" validate:"omitempty,oneof=none"`
	IdleTimeout        *int    `json:"idleTimeout" validate:"omitempty,gte=30,lte=86400"`
	UnlockDuration     *int    `json:"unlockDuration" validate:"omitempty,gte=30,lte=3600"`
	DeviceUnlock       *bool   `json:"deviceUnlock"`
	ProfileSwitching   *bool   `json:"profileSwitching"`
	ExtensionCard      *bool   `json:"extensionCard"`
	IdleReset          *bool   `json:"idleReset"`
}

// KioskSeal says whether the device's own interface is closed. Sealed is
// true only when Unsealed is empty; each entry of Unsealed names one way the
// host can still be reached around the lock.
type KioskSeal struct {
	Unsealed []string `json:"unsealed"`
	Sealed   bool     `json:"sealed"`
}

// KioskResponse is the kiosk lock as clients see it. It is also the payload
// of the kiosk.state notification.
type KioskResponse struct {
	// UnlockUntil is when a temporary unlock ends, in RFC 3339. Present only
	// while State is "unlocked".
	UnlockUntil *string `json:"unlockUntil,omitempty"`
	// Settings is present whenever kiosk mode is on.
	Settings *KioskSettings `json:"settings,omitempty"`
	// State is "off", "locked" or "unlocked".
	State string    `json:"state"`
	Seal  KioskSeal `json:"seal"`
}

// KioskEnableParams turns kiosk mode on from a preset, with any settings
// that should differ from it.
type KioskEnableParams struct {
	Settings *KioskSettingsPatch `json:"settings"`
	Preset   string              `json:"preset" validate:"required,oneof=family public"`
	// ConfirmEmpty accepts a lock under which some profile can launch
	// nothing.
	ConfirmEmpty bool `json:"confirmEmpty"`
}

// KioskUpdateParams changes the settings of an enabled lock.
type KioskUpdateParams struct {
	Settings KioskSettingsPatch `json:"settings"`
}

// KioskUnlockParams lifts the lock for a while. A paired admin client sends
// no credential. A caller on the device sends an admin profile and its PIN,
// or the recovery token.
type KioskUnlockParams struct {
	ProfileID     *string `json:"profileId"`
	PIN           *string `json:"pin"`
	RecoveryToken *string `json:"recoveryToken"`
	// Duration is how long to unlock for, in seconds. Omitted, the
	// configured unlock duration is used.
	Duration *int `json:"duration" validate:"omitempty,gte=30,lte=3600"`
}

// KioskAllowParams approves one launch the content rule would refuse.
type KioskAllowParams struct {
	SystemID string `json:"systemId" validate:"required,max=255"`
	Path     string `json:"path" validate:"required,max=4096"`
}

// KioskLaunchRefusedParams is the payload of kiosk.launch.refused.
type KioskLaunchRefusedParams struct {
	SystemID  string `json:"systemId"`
	Path      string `json:"path"`
	ProfileID string `json:"profileId"`
	// Reason is "hidden", "denied", "notAllowed" or "download".
	Reason string `json:"reason"`
}

// ContentRuleEntry is one allow or deny entry of a profile's content rule.
type ContentRuleEntry struct {
	Effect   string `json:"effect" validate:"required,oneof=allow deny"`
	Kind     string `json:"kind" validate:"required,oneof=system deck media tags"`
	SystemID string `json:"systemId,omitempty" validate:"max=255"`
	Path     string `json:"path,omitempty" validate:"max=4096"`
	Value    string `json:"value,omitempty" validate:"max=4096"`
}

// ProfilesContentParams names the profile whose content rule is wanted. An
// empty or omitted profileId is the shared profile.
type ProfilesContentParams struct {
	ProfileID string `json:"profileId" validate:"max=255"`
}

// ProfilesContentUpdateParams replaces a profile's whole content rule. An
// empty mode leaves the profile on the device's default mode.
type ProfilesContentUpdateParams struct {
	ProfileID string             `json:"profileId" validate:"max=255"`
	Mode      string             `json:"mode" validate:"omitempty,oneof=all allowed"`
	Entries   []ContentRuleEntry `json:"entries" validate:"max=5000,dive"`
}

// ProfilesContentResponse is a profile's content rule. Mode is empty when
// the profile has none of its own; EffectiveMode is the mode that applies.
type ProfilesContentResponse struct {
	ProfileID     string             `json:"profileId"`
	Mode          string             `json:"mode"`
	EffectiveMode string             `json:"effectiveMode"`
	Entries       []ContentRuleEntry `json:"entries"`
}
