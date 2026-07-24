//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package windowfocus

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	focusPollInterval = 50 * time.Millisecond
	focusTimeout      = 5 * time.Second
	getWindowOwner    = 4
	showRestore       = 9
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindow                = user32.NewProc("GetWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsGUIThread              = user32.NewProc("IsGUIThread")
	procIsIconic                 = user32.NewProc("IsIconic")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procSetActiveWindow          = user32.NewProc("SetActiveWindow")
	procSetFocus                 = user32.NewProc("SetFocus")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
)

type windowsAPI struct{}

// New creates a Windows process-window focus manager.
func New() *Manager {
	return newManager(windowsAPI{}, focusPollInterval, focusTimeout)
}

func (windowsAPI) allowProcessForeground(pid uint32) {
	_, _, _ = procAllowSetForegroundWindow.Call(uintptr(pid))
}

func (windowsAPI) findProcessWindow(pid uint32) (uintptr, bool) {
	if hwnd, found := findWindowForPIDs(map[uint32]struct{}{pid: {}}); found {
		return hwnd, true
	}
	return findWindowForPIDs(processTreePIDs(pid))
}

func findWindowForPIDs(pids map[uint32]struct{}) (uintptr, bool) {
	var found uintptr
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if owner, _, _ := procGetWindow.Call(hwnd, getWindowOwner); owner != 0 {
			return 1
		}

		var windowPID uint32
		_, _, _ = procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&windowPID)))
		if _, ok := pids[windowPID]; !ok {
			return 1
		}

		found = hwnd
		return 0
	})
	_, _, _ = procEnumWindows.Call(callback, 0)
	return found, found != 0
}

func processTreePIDs(rootPID uint32) map[uint32]struct{} {
	pids := map[uint32]struct{}{rootPID: {}}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return pids
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err = windows.Process32First(snapshot, &entry); err != nil {
		return pids
	}

	relations := make([]processRelation, 0, 64)
	for {
		relations = append(relations, processRelation{pid: entry.ProcessID, parentPID: entry.ParentProcessID})
		if err = windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}

	return processTree(rootPID, relations)
}

func (windowsAPI) activateWindow(hwnd uintptr) bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	foreground, _, _ := procGetForegroundWindow.Call()
	if foreground == hwnd {
		return true
	}

	_, _, _ = procIsGUIThread.Call(1)
	currentThread := windows.GetCurrentThreadId()
	foregroundThread := windowThreadID(foreground)
	targetThread := windowThreadID(hwnd)

	foregroundAttached := attachThreadInput(currentThread, foregroundThread)
	if foregroundAttached {
		defer detachThreadInput(currentThread, foregroundThread)
	}
	targetAttached := false
	if targetThread != foregroundThread {
		targetAttached = attachThreadInput(currentThread, targetThread)
		if targetAttached {
			defer detachThreadInput(currentThread, targetThread)
		}
	}

	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		_, _, _ = procShowWindow.Call(hwnd, showRestore)
	}
	_, _, _ = procBringWindowToTop.Call(hwnd)
	activated, _, _ := procSetForegroundWindow.Call(hwnd)
	_, _, _ = procSetActiveWindow.Call(hwnd)
	_, _, _ = procSetFocus.Call(hwnd)
	if activated == 0 {
		return false
	}
	foreground, _, _ = procGetForegroundWindow.Call()
	return foreground == hwnd
}

func windowThreadID(hwnd uintptr) uint32 {
	if hwnd == 0 {
		return 0
	}
	threadID, _, _ := procGetWindowThreadProcessID.Call(hwnd, 0)
	return uint32(threadID) //nolint:gosec // Win32 thread IDs are 32-bit values
}

func attachThreadInput(current, other uint32) bool {
	if current == 0 || other == 0 || current == other {
		return false
	}
	attached, _, _ := procAttachThreadInput.Call(uintptr(current), uintptr(other), 1)
	return attached != 0
}

func detachThreadInput(current, other uint32) {
	_, _, _ = procAttachThreadInput.Call(uintptr(current), uintptr(other), 0)
}
