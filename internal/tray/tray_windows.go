// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

// Package tray provides system tray icon support for EnergyStarGo.
package tray

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
	"github.com/KyleSanderson/EnergyStarGo/internal/winapi"
)

// Menu item IDs
const (
	idStatus          = 1001
	idStats           = 1002
	idSeparator       = 1003
	idPause           = 1004
	idResume          = 1005
	idRestore         = 1006
	idExit            = 1007
	idSvcSeparator    = 1008
	idInstallSvc      = 1009
	idUninstallSvc    = 1010
	idStartSvc        = 1011
	idStopSvc         = 1012
	idAutoStartSep    = 1013
	idToggleAutoStart = 1014
	idElevateSep      = 1015
	idRestartElevated = 1016
	idPresetsSep      = 1017
	idPresetBattery   = 1018
	idPresetBalanced  = 1019
	idPresetPerf      = 1020
)

// Win32 constants for tray
const (
	WM_USER      = 0x0400
	WM_TRAYICON  = WM_USER + 1
	WM_COMMAND   = 0x0111
	WM_CLOSE     = 0x0010
	WM_DESTROY   = 0x0002
	WM_LBUTTONUP = 0x0202
	WM_RBUTTONUP = 0x0205
	WM_CREATE    = 0x0001

	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004
	NIF_INFO    = 0x00000010

	NIS_HIDDEN = 0x00000001

	NIIF_INFO    = 0x00000001
	NIIF_WARNING = 0x00000002

	TPM_BOTTOMALIGN = 0x0020
	TPM_LEFTALIGN   = 0x0000
	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100

	WS_OVERLAPPED = 0x00000000
	CW_USEDEFAULT = 0x80000000

	IDI_APPLICATION = 32512

	MF_STRING    = 0x00000000
	MF_SEPARATOR = 0x00000800
	MF_GRAYED    = 0x00000001
	MF_CHECKED   = 0x00000008
)

var (
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	user32               = windows.NewLazySystemDLL("user32.dll")
	gdi32                = windows.NewLazySystemDLL("gdi32.dll")
	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procCreateIconIndirect  = user32.NewProc("CreateIconIndirect")
	procDestroyIcon         = user32.NewProc("DestroyIcon")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procSelectObject       = gdi32.NewProc("SelectObject")
)

// NOTIFYICONDATAW is the Win32 NOTIFYICONDATAW struct.
type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

// WNDCLASSEXW
type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  uintptr
	LpszClassName uintptr
	HIconSm       uintptr
}

// POINT struct for GetCursorPos
type POINT struct {
	X, Y int32
}

// MSG struct
type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

// ICONINFO is the Win32 ICONINFO struct for CreateIconIndirect.
type ICONINFO struct {
	FIcon    uint32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

// BITMAPINFOHEADER is the Win32 BITMAPINFOHEADER struct.
type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// isLightTheme reads the Windows personalization registry key to detect whether
// the system is using a light taskbar theme.
func isLightTheme() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	val, _, err := key.GetIntegerValue("SystemUsesLightTheme")
	if err != nil {
		return false
	}
	return val == 1
}

// createAppIcon creates a simple 16x16 ARGB icon with a green lightning bolt.
// The colors adapt to the current Windows theme (light or dark taskbar).
func createAppIcon() uintptr {
	const size = 16

	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return 0
	}
	defer procDeleteDC.Call(hdc)

	// Create color bitmap (32-bit ARGB)
	bmi := BITMAPINFOHEADER{
		BiSize:     uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
		BiWidth:    size,
		BiHeight:   -size, // top-down
		BiPlanes:   1,
		BiBitCount: 32,
	}

	var bits unsafe.Pointer
	hbmColor, _, _ := procCreateDIBSection.Call(
		hdc,
		uintptr(unsafe.Pointer(&bmi)),
		0, // DIB_RGB_COLORS
		uintptr(unsafe.Pointer(&bits)),
		0, 0,
	)
	if hbmColor == 0 || bits == nil {
		return 0
	}

	// Create mask bitmap
	bmiMask := bmi
	bmiMask.BiBitCount = 32
	var maskBits unsafe.Pointer
	hbmMask, _, _ := procCreateDIBSection.Call(
		hdc,
		uintptr(unsafe.Pointer(&bmiMask)),
		0,
		uintptr(unsafe.Pointer(&maskBits)),
		0, 0,
	)
	if hbmMask == 0 {
		procDeleteObject.Call(hbmColor)
		return 0
	}

	// Draw a lightning bolt icon
	// Pixel layout: BGRA format
	pixels := unsafe.Slice((*[4]byte)(bits), size*size)
	maskPixels := unsafe.Slice((*[4]byte)(maskBits), size*size)

	// Lightning bolt shape (16x16 grid, 1 = bolt, 0 = background)
	bolt := [16][16]byte{
		{0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0},
		{0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	}

	// Pick colours based on system theme
	var boltColor, bgColor [4]byte
	if isLightTheme() {
		boltColor = [4]byte{0x20, 0x80, 0x00, 0xFF} // dark green BGRA
		bgColor = [4]byte{0xF0, 0xF0, 0xF0, 0xFF}   // light background BGRA
	} else {
		boltColor = [4]byte{0x00, 0xCC, 0x44, 0xFF} // bright green BGRA
		bgColor = [4]byte{0x30, 0x30, 0x30, 0xFF}   // dark background BGRA
	}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			idx := y*size + x
			if bolt[y][x] == 1 {
				pixels[idx] = boltColor
			} else {
				pixels[idx] = bgColor
			}
			// Mask: all zeros = opaque
			maskPixels[idx] = [4]byte{0x00, 0x00, 0x00, 0x00}
		}
	}

	iconInfo := ICONINFO{
		FIcon:    1, // TRUE = icon
		HbmMask:  hbmMask,
		HbmColor: hbmColor,
	}

	hIcon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&iconInfo)))

	// Bitmaps can be deleted after icon creation
	procDeleteObject.Call(hbmColor)
	procDeleteObject.Call(hbmMask)

	return hIcon
}

// TrayCallbacks holds callbacks for tray menu actions.
type TrayCallbacks struct {
	OnPause   func()
	OnResume  func()
	OnRestore func()
	OnExit    func()
	GetStats  func() string
	IsPaused  func() bool
	// Service management callbacks — each should auto-elevate if not admin
	OnInstallService   func()
	OnUninstallService func()
	OnStartService     func()
	OnStopService      func()
	// Returns current service state string: "Running", "Stopped", "Not installed", etc.
	GetServiceStatus func() string
	// Auto-start at Windows login toggle
	OnToggleAutoStart  func()
	IsAutoStartEnabled func() bool
	// UAC elevation
	IsElevated        func() bool
	OnRestartElevated func()
	// Display state change — called when screen turns on/off
	OnDisplayStateChange func(displayOn bool)
	// Session state change — called for WTS_SESSION_* events
	OnSessionChange func(eventType uint32)
	// Power source change — called when AC/battery changes
	OnPowerSourceChange func(onAC bool)
	// Power plan change — called when the active power scheme changes
	OnPowerPlanChange func(planGUID winapi.GUID)
	// Profile preset selection from tray menu
	OnSetProfile func(profile string)
	// Returns the current profile name (e.g. "balanced", "aggressive")
	GetProfile func() string
}

// Tray manages the system tray icon.
type Tray struct {
	log                     *logger.Logger
	callbacks               TrayCallbacks
	hwnd                    uintptr
	nid                     NOTIFYICONDATAW
	mu                      sync.Mutex
	running                 bool
	displayStateNotifHandle uintptr
	powerSourceNotifHandle  uintptr
	powerPlanNotifHandle    uintptr
	wtsSessionNotifHandle   uintptr
}

// New creates a new Tray.
func New(log *logger.Logger, callbacks TrayCallbacks) *Tray {
	return &Tray{
		log:       log,
		callbacks: callbacks,
	}
}

var activeTray *Tray

// Run starts the tray icon. Must be called from a thread-locked goroutine.
func (t *Tray) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	activeTray = t

	// Register window class
	className, _ := syscall.UTF16PtrFromString("EnergyStarGoTray")
	hInstance := uintptr(0)

	wndProc := syscall.NewCallback(trayWndProc)

	var wc WNDCLASSEXW
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = wndProc
	wc.HInstance = hInstance
	wc.LpszClassName = uintptr(unsafe.Pointer(className))

	ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if ret == 0 {
		return fmt.Errorf("RegisterClassExW failed: %w", err)
	}

	// Load default application icon
	hIcon := createAppIcon()
	if hIcon == 0 {
		// Fallback to system application icon
		hIcon, _, _ = procLoadIconW.Call(0, uintptr(IDI_APPLICATION))
	}

	// Create hidden window for message handling
	windowName, _ := syscall.UTF16PtrFromString("EnergyStarGo")
	t.hwnd, _, err = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		WS_OVERLAPPED,
		uintptr(CW_USEDEFAULT), uintptr(CW_USEDEFAULT),
		uintptr(CW_USEDEFAULT), uintptr(CW_USEDEFAULT),
		0, 0, hInstance, 0,
	)
	if t.hwnd == 0 {
		return fmt.Errorf("CreateWindowExW failed: %w", err)
	}

	// Set up notify icon data
	t.nid.CbSize = uint32(unsafe.Sizeof(t.nid))
	t.nid.HWnd = t.hwnd
	t.nid.UID = 1
	t.nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	t.nid.UCallbackMessage = WM_TRAYICON
	t.nid.HIcon = hIcon
	copy(t.nid.SzTip[:], utf16From("EnergyStarGo - Running"))

	// Add tray icon
	ret, _, err = procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&t.nid)))
	if ret == 0 {
		return fmt.Errorf("Shell_NotifyIconW ADD failed: %w", err)
	}

	t.mu.Lock()
	t.running = true
	t.mu.Unlock()

	// Register for display power state notifications if a callback is set.
	if t.callbacks.OnDisplayStateChange != nil {
		guid := winapi.GUID_CONSOLE_DISPLAY_STATE
		if h, err := winapi.RegisterPowerSettingNotification(t.hwnd, &guid, winapi.DEVICE_NOTIFY_WINDOW_HANDLE); err != nil {
			t.log.Warn("failed to register display state notification", "error", err)
		} else {
			t.displayStateNotifHandle = h
			t.log.Info("display state notification registered", "handle", h)
		}
	}

	// Register for AC/battery source changes.
	if t.callbacks.OnPowerSourceChange != nil {
		guid := winapi.GUID_ACDC_POWER_SOURCE
		if h, err := winapi.RegisterPowerSettingNotification(t.hwnd, &guid, winapi.DEVICE_NOTIFY_WINDOW_HANDLE); err != nil {
			t.log.Warn("failed to register power source notification", "error", err)
		} else {
			t.powerSourceNotifHandle = h
			t.log.Info("power source notification registered", "handle", h)
		}
	}

	// Register for power plan (scheme) changes.
	if t.callbacks.OnPowerPlanChange != nil {
		guid := winapi.GUID_POWER_SCHEME_PERSONALITY
		if h, err := winapi.RegisterPowerSettingNotification(t.hwnd, &guid, winapi.DEVICE_NOTIFY_WINDOW_HANDLE); err != nil {
			t.log.Warn("failed to register power plan notification", "error", err)
		} else {
			t.powerPlanNotifHandle = h
			t.log.Info("power plan notification registered", "handle", h)
		}
	}

	// Register for session change notifications if a callback is set.
	if t.callbacks.OnSessionChange != nil {
		if err := winapi.WTSRegisterSessionNotification(t.hwnd, winapi.NOTIFY_FOR_THIS_SESSION); err != nil {
			t.log.Warn("failed to register session notification", "error", err)
		} else {
			t.wtsSessionNotifHandle = t.hwnd
			t.log.Info("session notification registered")
		}
	}

	t.log.Info("system tray icon initialized")

	// Message loop
	var msg MSG
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	// Clean up.
	if t.displayStateNotifHandle != 0 {
		_ = winapi.UnregisterPowerSettingNotification(t.displayStateNotifHandle)
	}
	if t.powerSourceNotifHandle != 0 {
		_ = winapi.UnregisterPowerSettingNotification(t.powerSourceNotifHandle)
	}
	if t.powerPlanNotifHandle != 0 {
		_ = winapi.UnregisterPowerSettingNotification(t.powerPlanNotifHandle)
	}
	if t.wtsSessionNotifHandle != 0 {
		_ = winapi.WTSUnRegisterSessionNotification(t.wtsSessionNotifHandle)
	}
	procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&t.nid)))
	t.mu.Lock()
	t.running = false
	t.mu.Unlock()

	return nil
}

// Stop removes the tray icon and closes the tray window.
// Safe to call from any goroutine.
func (t *Tray) Stop() {
	t.mu.Lock()
	running := t.running
	t.mu.Unlock()
	if running {
		// PostMessage WM_CLOSE is safe to call cross-thread and triggers
		// DestroyWindow in the wndproc, which then sends WM_DESTROY.
		procPostMessageW.Call(t.hwnd, WM_CLOSE, 0, 0)
	}
}

// ShowNotification displays a balloon notification from the tray icon.
func (t *Tray) ShowNotification(title, message string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return
	}

	t.nid.UFlags = NIF_INFO
	copy(t.nid.SzInfoTitle[:], utf16From(title))
	copy(t.nid.SzInfo[:], utf16From(message))
	t.nid.DwInfoFlags = NIIF_INFO

	procShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&t.nid)))

	// Reset flags
	t.nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
}

// UpdateTooltip changes the tray icon tooltip text.
func (t *Tray) UpdateTooltip(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return
	}

	copy(t.nid.SzTip[:], utf16From(text))
	t.nid.UFlags = NIF_TIP
	procShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&t.nid)))
	t.nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
}

func trayWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	t := activeTray
	if t == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}

	switch msg {
	case WM_TRAYICON:
		switch lParam {
		case WM_RBUTTONUP:
			showContextMenu(t, hwnd)
		case WM_LBUTTONUP:
			showContextMenu(t, hwnd)
		}
		return 0

	case WM_COMMAND:
		handleMenuCommand(t, uint32(wParam))
		return 0

	case WM_CLOSE:
		// WM_CLOSE is posted by Stop(). Call DestroyWindow which
		// triggers WM_DESTROY on the same thread.
		procDestroyWindow.Call(hwnd)
		return 0

	case winapi.WM_POWERBROADCAST:
		if wParam == winapi.PBT_POWERSETTINGCHANGE {
			var pbs winapi.POWERBROADCAST_SETTING
			winapi.CopyFromUintptr(unsafe.Pointer(&pbs), lParam, unsafe.Sizeof(pbs))
			settingGUID := guidFromPowerSetting(pbs.PowerSetting)

			if settingGUID == winapi.GUID_CONSOLE_DISPLAY_STATE && t.callbacks.OnDisplayStateChange != nil {
				if pbs.DataLength >= 1 {
					t.callbacks.OnDisplayStateChange(pbs.Data[0] != winapi.DISPLAY_OFF)
				}
			}

			if settingGUID == winapi.GUID_ACDC_POWER_SOURCE && t.callbacks.OnPowerSourceChange != nil {
				if pbs.DataLength >= 1 {
					// 0 = AC, 1 = battery
					isAC := pbs.Data[0] == 0
					t.callbacks.OnPowerSourceChange(isAC)
				}
			}

			if settingGUID == winapi.GUID_POWER_SCHEME_PERSONALITY && t.callbacks.OnPowerPlanChange != nil {
				if pbs.DataLength >= 16 {
					planGUID := guidFromPowerSetting(*(*[16]byte)(unsafe.Pointer(&pbs.Data[0])))
					t.callbacks.OnPowerPlanChange(planGUID)
				}
			}
		}
		return 0

	case winapi.WM_WTSSESSION_CHANGE:
		if t.callbacks.OnSessionChange != nil {
			t.callbacks.OnSessionChange(uint32(wParam))
		}
		return 0

	case WM_DESTROY:
		procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&t.nid)))
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func guidFromPowerSetting(raw [16]byte) winapi.GUID {
	return winapi.GUID{
		Data1: uint32(raw[0]) | uint32(raw[1])<<8 | uint32(raw[2])<<16 | uint32(raw[3])<<24,
		Data2: uint16(raw[4]) | uint16(raw[5])<<8,
		Data3: uint16(raw[6]) | uint16(raw[7])<<8,
		Data4: [8]byte{raw[8], raw[9], raw[10], raw[11], raw[12], raw[13], raw[14], raw[15]},
	}
}

func showContextMenu(t *Tray, hwnd uintptr) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	// Status line (grayed) — IsPaused is cheap (mutex read)
	isPaused := false
	if t.callbacks.IsPaused != nil {
		isPaused = t.callbacks.IsPaused()
	}
	statusText := "EnergyStarGo: Running"
	if isPaused {
		statusText = "EnergyStarGo: Paused"
	}
	appendMenu(hMenu, MF_STRING|MF_GRAYED, idStatus, statusText)

	// Stats line (grayed)
	if t.callbacks.GetStats != nil {
		statsText := t.callbacks.GetStats()
		appendMenu(hMenu, MF_STRING|MF_GRAYED, idStats, statsText)
	}

	appendMenu(hMenu, MF_SEPARATOR, idSeparator, "")

	if isPaused {
		appendMenu(hMenu, MF_STRING, idResume, "Resume")
	} else {
		appendMenu(hMenu, MF_STRING, idPause, "Pause")
	}

	appendMenu(hMenu, MF_STRING, idRestore, "Restore All Processes")

	// Service management section
	appendMenu(hMenu, MF_SEPARATOR, idSvcSeparator, "")

	// Service status is queried once, inline — the SCM query is fast for a
	// single named service.
	svcStatus := ""
	if t.callbacks.GetServiceStatus != nil {
		svcStatus = t.callbacks.GetServiceStatus()
	}

	installFlags := uint32(MF_STRING)
	if svcStatus == "Running" || svcStatus == "Stopped" {
		installFlags |= MF_GRAYED
	}
	appendMenu(hMenu, installFlags, idInstallSvc, "Install Service")

	startFlags := uint32(MF_STRING)
	if svcStatus == "Running" || svcStatus == "Not installed" {
		startFlags |= MF_GRAYED
	}
	appendMenu(hMenu, startFlags, idStartSvc, "Start Service")

	stopFlags := uint32(MF_STRING)
	if svcStatus == "Stopped" || svcStatus == "Not installed" {
		stopFlags |= MF_GRAYED
	}
	appendMenu(hMenu, stopFlags, idStopSvc, "Stop Service")

	uninstallFlags := uint32(MF_STRING)
	if svcStatus == "Not installed" || svcStatus == "" {
		uninstallFlags |= MF_GRAYED
	}
	appendMenu(hMenu, uninstallFlags, idUninstallSvc, "Uninstall Service")

	appendMenu(hMenu, MF_SEPARATOR, idAutoStartSep, "")

	autoStartText := "Run at startup"
	if t.callbacks.IsAutoStartEnabled != nil && t.callbacks.IsAutoStartEnabled() {
		autoStartText = "\u2713 Run at startup"
	}
	appendMenu(hMenu, MF_STRING, idToggleAutoStart, autoStartText)

	appendMenu(hMenu, MF_SEPARATOR, idPresetsSep, "")

	currentProfile := ""
	if t.callbacks.GetProfile != nil {
		currentProfile = t.callbacks.GetProfile()
	}

	batteryLabel := "Profile: Battery Saver"
	balancedLabel := "Profile: Balanced"
	perfLabel := "Profile: Performance"

	if currentProfile == "aggressive" {
		batteryLabel = "\u2713 " + batteryLabel
	} else if currentProfile == "balanced" {
		balancedLabel = "\u2713 " + balancedLabel
	}
	if isPaused {
		perfLabel = "\u2713 " + perfLabel
	}

	appendMenu(hMenu, MF_STRING, idPresetBattery, batteryLabel)
	appendMenu(hMenu, MF_STRING, idPresetBalanced, balancedLabel)
	appendMenu(hMenu, MF_STRING, idPresetPerf, perfLabel)

	// "Restart Elevated" option — only shown when not already admin
	isElev := false
	if t.callbacks.IsElevated != nil {
		isElev = t.callbacks.IsElevated()
	}
	if !isElev {
		appendMenu(hMenu, MF_SEPARATOR, idElevateSep, "")
		appendMenu(hMenu, MF_STRING, idRestartElevated, "Restart Elevated (Admin)")
	}

	appendMenu(hMenu, MF_SEPARATOR, idSeparator, "")
	appendMenu(hMenu, MF_STRING, idExit, "Exit")

	// Show menu at cursor position
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(
		hMenu,
		TPM_BOTTOMALIGN|TPM_LEFTALIGN|TPM_RIGHTBUTTON|TPM_RETURNCMD,
		uintptr(pt.X), uintptr(pt.Y),
		0, hwnd, 0,
	)
	// TPM_RETURNCMD makes TrackPopupMenu return the selected command ID
	// directly instead of posting WM_COMMAND. Dispatch it ourselves.
	if cmd != 0 {
		handleMenuCommand(t, uint32(cmd))
	}
}

func handleMenuCommand(t *Tray, id uint32) {
	switch id {
	case idPause:
		if t.callbacks.OnPause != nil {
			go t.callbacks.OnPause()
			t.UpdateTooltip("EnergyStarGo - Paused")
		}
	case idResume:
		if t.callbacks.OnResume != nil {
			go t.callbacks.OnResume()
			t.UpdateTooltip("EnergyStarGo - Running")
		}
	case idRestore:
		if t.callbacks.OnRestore != nil {
			go t.callbacks.OnRestore()
		}
	case idInstallSvc:
		if t.callbacks.OnInstallService != nil {
			go t.callbacks.OnInstallService()
		}
	case idUninstallSvc:
		if t.callbacks.OnUninstallService != nil {
			go t.callbacks.OnUninstallService()
		}
	case idStartSvc:
		if t.callbacks.OnStartService != nil {
			go t.callbacks.OnStartService()
		}
	case idStopSvc:
		if t.callbacks.OnStopService != nil {
			go t.callbacks.OnStopService()
		}
	case idToggleAutoStart:
		if t.callbacks.OnToggleAutoStart != nil {
			go t.callbacks.OnToggleAutoStart()
		}
	case idRestartElevated:
		if t.callbacks.OnRestartElevated != nil {
			go t.callbacks.OnRestartElevated()
		}
	case idPresetBattery:
		if t.callbacks.OnSetProfile != nil {
			go t.callbacks.OnSetProfile("aggressive")
		}
		t.UpdateTooltip("EnergyStarGo - Battery Saver")
	case idPresetBalanced:
		if t.callbacks.OnSetProfile != nil {
			go t.callbacks.OnSetProfile("balanced")
		}
		t.UpdateTooltip("EnergyStarGo - Balanced")
	case idPresetPerf:
		// Performance mode: stop throttling and restore all processes.
		if t.callbacks.OnPause != nil {
			go t.callbacks.OnPause()
		}
		if t.callbacks.OnRestore != nil {
			go t.callbacks.OnRestore()
		}
		t.UpdateTooltip("EnergyStarGo - Performance (throttling disabled)")
	case idExit:
		if t.callbacks.OnExit != nil {
			go t.callbacks.OnExit()
		}
		t.Stop()
	}
}

// UpdateStatus updates the tray tooltip with current engine + battery status.
func (t *Tray) UpdateStatus(engineStatus string, batteryStatus string) {
	tooltip := "EnergyStarGo - " + engineStatus
	if batteryStatus != "" {
		tooltip += " | " + batteryStatus
	}
	t.UpdateTooltip(tooltip)
}

func appendMenu(hMenu uintptr, flags uint32, id uint32, text string) {
	textPtr, _ := syscall.UTF16PtrFromString(text)
	procAppendMenuW.Call(hMenu, uintptr(flags), uintptr(id), uintptr(unsafe.Pointer(textPtr)))
}

func utf16From(s string) []uint16 {
	u, _ := syscall.UTF16FromString(s)
	return u
}
