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

package hoststatus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

// NewDefaults returns the generic readers for Windows.
func NewDefaults(opts DefaultOptions) Readers {
	controllers := &xinputControllers{exclude: opts.VirtualGamepadSlots}
	return Readers{
		Power:        power.ReadDetail,
		Network:      readWindowsNetwork,
		Bluetooth:    readWindowsBluetooth,
		Storage:      NewStorageReader().Read,
		Controllers:  controllers.Read,
		System:       readWindowsSystem,
		PowerControl: windowsPowerControl{},
	}
}

type windowsAdapter struct {
	ifType     uint32
	metric     uint32
	up         bool
	hasGateway bool
	virtual    bool
}

// isVirtualAdapter reports whether an adapter's description marks it as one
// Windows made up. Wi-Fi Direct and Bluetooth networking both appear as
// ordinary Wi-Fi and Ethernet adapters that are never anyone's connection.
func isVirtualAdapter(description string) bool {
	description = strings.ToLower(description)
	return strings.Contains(description, "virtual") || strings.Contains(description, "bluetooth")
}

// windowsAdapters reads what the IP helper knows about each adapter, keyed by
// the friendly name the net package also uses.
func windowsAdapters() (map[string]windowsAdapter, error) {
	size := uint32(16 * 1024)
	for range 3 {
		buf := make([]byte, size)
		//nolint:gosec // G103: the buffer is sized for the structures the call writes
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_GATEWAYS, 0, first, &size)
		if errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("GetAdaptersAddresses: %w", err)
		}
		adapters := map[string]windowsAdapter{}
		for adapter := first; adapter != nil; adapter = adapter.Next {
			adapters[windows.UTF16PtrToString(adapter.FriendlyName)] = windowsAdapter{
				ifType:     adapter.IfType,
				metric:     adapter.Ipv4Metric,
				up:         adapter.OperStatus == windows.IfOperStatusUp,
				hasGateway: adapter.FirstGatewayAddress != nil,
				virtual:    isVirtualAdapter(windows.UTF16PtrToString(adapter.Description)),
			}
		}
		return adapters, nil
	}
	return nil, errors.New("GetAdaptersAddresses: adapter list kept growing")
}

func readWindowsNetwork() (Network, error) {
	raw, err := ListRawInterfaces()
	if err != nil {
		return Network{}, err
	}
	adapters, err := windowsAdapters()
	if err != nil {
		return Network{}, err
	}

	// The adapter Windows routes through is the connected one with a gateway
	// and the lowest metric.
	defaultName := ""
	var defaultMetric uint32
	for name, adapter := range adapters {
		if !adapter.up || !adapter.hasGateway {
			continue
		}
		if defaultName == "" || adapter.metric < defaultMetric ||
			(adapter.metric == defaultMetric && name < defaultName) {
			defaultName, defaultMetric = name, adapter.metric
		}
	}

	classify := func(name string) LinkType {
		if adapters[name].virtual {
			return LinkOther
		}
		switch adapters[name].ifType {
		case windows.IF_TYPE_IEEE80211:
			return LinkWifi
		case windows.IF_TYPE_ETHERNET_CSMACD:
			return LinkWired
		default:
			return LinkOther
		}
	}
	isUp := func(iface *RawInterface) bool { return adapters[iface.Name].up }

	network := buildNetwork(raw, defaultName, classify, isUp)
	if network.Type == LinkNone {
		network.Internet = InternetNone
		network.InternetAuthoritative = true
		return network, nil
	}
	// Windows runs its own reachability check. Its "yes" is trusted; its "no"
	// is not, because it also says no behind a proxy or when its own test
	// host is blocked.
	if connected, ok := networkListConnected(); ok && connected {
		network.Internet = InternetFull
		network.InternetAuthoritative = true
	}
	return network, nil
}

const (
	clsidNetworkListManager = "{DCB00C01-570F-4A9B-8D69-199FDBA5723B}"

	// comAlreadyInitialized is S_FALSE: COM was already set up on this
	// thread, and the call still has to be balanced.
	comAlreadyInitialized = 0x1
	// comChangedMode is RPC_E_CHANGED_MODE: the thread is set up in another
	// threading mode, which works for this call and must not be balanced.
	comChangedMode = 0x80010106
)

// networkListConnected asks the Network List Manager whether Windows believes
// it has internet access. The second result is false when it cannot be asked.
func networkListConnected() (connected, ok bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		var oleErr *ole.OleError
		if !errors.As(err, &oleErr) {
			return false, false
		}
		switch oleErr.Code() {
		case comAlreadyInitialized:
			defer ole.CoUninitialize()
		case comChangedMode:
		default:
			return false, false
		}
	} else {
		defer ole.CoUninitialize()
	}

	unknown, err := ole.CreateInstance(ole.NewGUID(clsidNetworkListManager), ole.IID_IUnknown)
	if err != nil {
		log.Debug().Err(err).Msg("could not create network list manager")
		return false, false
	}
	defer unknown.Release()

	manager, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return false, false
	}
	defer manager.Release()

	value, err := oleutil.GetProperty(manager, "IsConnectedToInternet")
	if err != nil {
		return false, false
	}
	defer func() { _ = value.Clear() }()

	connected, ok = value.Value().(bool)
	return connected, ok
}

type bluetoothFindRadioParams struct {
	size uint32
}

// readWindowsBluetooth reports whether Windows has a usable Bluetooth radio.
// It only enumerates; nothing here can turn a radio on.
func readWindowsBluetooth() (Bluetooth, error) {
	dll := windows.NewLazySystemDLL("bthprops.cpl")
	findFirst := dll.NewProc("BluetoothFindFirstRadio")
	findClose := dll.NewProc("BluetoothFindRadioClose")
	if findFirst.Find() != nil || findClose.Find() != nil {
		return Bluetooth{}, ErrUnsupported
	}

	params := bluetoothFindRadioParams{size: uint32(unsafe.Sizeof(bluetoothFindRadioParams{}))}
	var radio windows.Handle
	//nolint:gosec // G103: pointers are to locals the call fills in
	find, _, _ := findFirst.Call(uintptr(unsafe.Pointer(&params)), uintptr(unsafe.Pointer(&radio)))
	if find == 0 {
		return Bluetooth{}, nil
	}
	_, _, _ = findClose.Call(find)
	if radio != 0 {
		_ = windows.CloseHandle(radio)
	}
	return Bluetooth{Present: true}, nil
}

const (
	xinputSlots = 4

	xinputDevTypeGamepad = 0

	xinputBatteryDisconnected = 0x00
	xinputBatteryWired        = 0x01
	xinputBatteryUnknown      = 0xFF
)

type xinputState struct {
	packetNumber uint32
	buttons      uint16
	leftTrigger  byte
	rightTrigger byte
	thumbLX      int16
	thumbLY      int16
	thumbRX      int16
	thumbRY      int16
}

type xinputBattery struct {
	batteryType  byte
	batteryLevel byte
}

// xinputControllers lists controllers through XInput, which is the only
// controller API reachable without a new dependency. It knows a slot, a wired
// flag and a coarse battery level, and nothing about what the controller is.
type xinputControllers struct {
	exclude func() []int
}

func (r *xinputControllers) Read() ([]Controller, error) {
	dll := windows.NewLazySystemDLL("xinput1_4.dll")
	getState := dll.NewProc("XInputGetState")
	getBattery := dll.NewProc("XInputGetBatteryInformation")
	if getState.Find() != nil {
		return nil, ErrUnsupported
	}

	excluded := map[int]struct{}{}
	if r.exclude != nil {
		for _, slot := range r.exclude() {
			excluded[slot] = struct{}{}
		}
	}

	controllers := []Controller{}
	for slot := range xinputSlots {
		if _, skip := excluded[slot]; skip {
			continue
		}
		var state xinputState
		//nolint:gosec // G103: pointer is to a local the call fills in
		ret, _, _ := getState.Call(uintptr(slot), uintptr(unsafe.Pointer(&state)))
		if ret != 0 {
			continue
		}
		controller := Controller{ID: "xinput" + strconv.Itoa(slot), Connection: ConnectionUnknown}
		if getBattery.Find() == nil {
			var battery xinputBattery
			//nolint:gosec // G103: pointer is to a local the call fills in
			ret, _, _ = getBattery.Call(uintptr(slot), xinputDevTypeGamepad, uintptr(unsafe.Pointer(&battery)))
			if ret == 0 {
				controller.Connection, controller.Battery = xinputBatteryInfo(battery)
			}
		}
		controllers = append(controllers, controller)
	}
	return controllers, nil
}

func xinputBatteryInfo(battery xinputBattery) (string, *ControllerBattery) {
	switch battery.batteryType {
	case xinputBatteryWired:
		return ConnectionUSB, nil
	case xinputBatteryDisconnected, xinputBatteryUnknown:
		return ConnectionUnknown, nil
	}
	levels := []string{BatteryLevelEmpty, BatteryLevelLow, BatteryLevelMedium, BatteryLevelFull}
	if int(battery.batteryLevel) >= len(levels) {
		return ConnectionUnknown, nil
	}
	return ConnectionUnknown, &ControllerBattery{Level: levels[battery.batteryLevel]}
}

func readWindowsSystem() (System, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return System{}, fmt.Errorf("reading hostname: %w", err)
	}
	return System{Hostname: hostname}, nil
}

const (
	shutdownPrivilege = "SeShutdownPrivilege"
	// shutdownReasonPlanned is SHTDN_REASON_FLAG_PLANNED.
	shutdownReasonPlanned = 0x80000000
)

type windowsPowerControl struct{}

// enableShutdownPrivilege turns on the privilege every power action needs. A
// user who may not shut the machine down does not hold it at all.
func enableShutdownPrivilege() error {
	var token windows.Token
	access := uint32(windows.TOKEN_ADJUST_PRIVILEGES | windows.TOKEN_QUERY)
	if err := windows.OpenProcessToken(windows.CurrentProcess(), access, &token); err != nil {
		return fmt.Errorf("opening process token: %w", err)
	}
	defer func() { _ = token.Close() }()

	name, err := windows.UTF16PtrFromString(shutdownPrivilege)
	if err != nil {
		return fmt.Errorf("encoding privilege name: %w", err)
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, name, &luid); err != nil {
		return fmt.Errorf("looking up shutdown privilege: %w", err)
	}
	privileges := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges:     [1]windows.LUIDAndAttributes{{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}},
	}
	// Called directly because the wrapper in x/sys discards the last-error
	// value, and that value is the only sign the privilege was not granted.
	adjust := windows.NewLazySystemDLL("advapi32.dll").NewProc("AdjustTokenPrivileges")
	//nolint:gosec // G103: pointer is to a local structure the call reads
	ret, _, lastErr := adjust.Call(uintptr(token), 0, uintptr(unsafe.Pointer(&privileges)), 0, 0, 0)
	if ret == 0 {
		return fmt.Errorf("adjusting token privileges: %w", lastErr)
	}
	if errors.Is(lastErr, windows.ERROR_NOT_ALL_ASSIGNED) {
		return ErrNotPermitted
	}
	return nil
}

func suspendAllowed() bool {
	proc := windows.NewLazySystemDLL("powrprof.dll").NewProc("IsPwrSuspendAllowed")
	if proc.Find() != nil {
		return false
	}
	ret, _, _ := proc.Call()
	return ret != 0
}

func (windowsPowerControl) PowerActions(context.Context) map[PowerAction]Availability {
	availability := Supported
	if err := enableShutdownPrivilege(); err != nil {
		availability = NotPermitted
	}
	actions := map[PowerAction]Availability{
		PowerReboot:   availability,
		PowerShutdown: availability,
	}
	if suspendAllowed() {
		actions[PowerSuspend] = availability
	}
	return actions
}

func (windowsPowerControl) PreparePowerAction(_ context.Context, action PowerAction) (func() error, error) {
	if action == PowerSuspend && !suspendAllowed() {
		return nil, ErrUnsupported
	}
	if err := enableShutdownPrivilege(); err != nil {
		log.Debug().Err(err).Msg("shutdown privilege unavailable")
		return nil, ErrNotPermitted
	}
	switch action {
	case PowerReboot, PowerShutdown:
		reboot := action == PowerReboot
		return func() error {
			reason := uint32(windows.SHTDN_REASON_MAJOR_OTHER | shutdownReasonPlanned)
			if err := windows.InitiateSystemShutdownEx(nil, nil, 0, false, reboot, reason); err != nil {
				return fmt.Errorf("initiating system %s: %w", action, err)
			}
			return nil
		}, nil
	case PowerSuspend:
		return func() error {
			proc := windows.NewLazySystemDLL("powrprof.dll").NewProc("SetSuspendState")
			// Arguments: hibernate, force, disable wake events.
			if ret, _, err := proc.Call(0, 0, 0); ret == 0 {
				return fmt.Errorf("suspending system: %w", err)
			}
			return nil
		}, nil
	default:
		return nil, ErrUnsupported
	}
}
