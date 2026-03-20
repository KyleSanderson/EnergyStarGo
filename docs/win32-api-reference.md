# Win32 API Reference for EcoQoS/Efficiency Mode Process Throttler in Go

## Table of Contents
1. [SetProcessInformation + ProcessPowerThrottling](#1-setprocessinformation)
2. [SetWinEventHook (EVENT_SYSTEM_FOREGROUND)](#2-setwineventhook)
3. [GetMessage/TranslateMessage/DispatchMessage](#3-message-loop)
4. [OpenProcess / CloseHandle](#4-openprocess--closehandle)
5. [GetWindowThreadProcessId](#5-getwindowthreadprocessid)
6. [QueryFullProcessImageName](#6-queryfullprocessimagename)
7. [SetPriorityClass](#7-setpriorityclass)
8. [EnumChildWindows](#8-enumchildwindows)
9. [GetForegroundWindow](#9-getforegroundwindow)
10. [golang.org/x/sys/windows Availability Matrix](#10-availability-matrix)
11. [Go Calling Patterns](#11-go-calling-patterns)

---

## 1. SetProcessInformation

**DLL:** `kernel32.dll`  
**Header:** `processthreadsapi.h`  
**Min Client:** Windows 8

### C Signature
```c
BOOL SetProcessInformation(
    HANDLE                    hProcess,              // [in]
    PROCESS_INFORMATION_CLASS ProcessInformationClass,// [in]
    LPVOID                    ProcessInformation,     // [in]
    DWORD                     ProcessInformationSize  // [in]
);
```

### PROCESS_INFORMATION_CLASS Enum
```
ProcessMemoryPriority                      = 0
ProcessMemoryExhaustionInfo                = 1
ProcessAppMemoryInfo                       = 2
ProcessInPrivateInfo                       = 3
ProcessPowerThrottling                     = 4  ← THIS ONE
ProcessReservedValue1                      = 5
ProcessTelemetryCoverageInfo               = 6
ProcessProtectionLevelInfo                 = 7
ProcessLeapSecondInfo                      = 8
ProcessMachineTypeInfo                     = 9
ProcessOverrideSubsequentPrefetchParameter = 10
ProcessMaxOverridePrefetchParameter        = 11
ProcessInformationClassMax                 = 12
```

### PROCESS_POWER_THROTTLING_STATE Struct

```c
typedef struct _PROCESS_POWER_THROTTLING_STATE {
    ULONG Version;      // offset 0, size 4 bytes
    ULONG ControlMask;  // offset 4, size 4 bytes
    ULONG StateMask;    // offset 8, size 4 bytes
} PROCESS_POWER_THROTTLING_STATE;
// Total size: 12 bytes (sizeof = 12)
```

| Field       | Type  | Offset | Size | Description |
|-------------|-------|--------|------|-------------|
| Version     | ULONG | 0      | 4    | Must be `PROCESS_POWER_THROTTLING_CURRENT_VERSION` |
| ControlMask | ULONG | 4      | 4    | Selects which mechanisms to control |
| StateMask   | ULONG | 8      | 4    | Which mechanisms should be on (bits set) or off (bits clear) |

### Constants
```
PROCESS_POWER_THROTTLING_CURRENT_VERSION     = 1  (0x1)
PROCESS_POWER_THROTTLING_EXECUTION_SPEED     = 1  (0x1)
PROCESS_POWER_THROTTLING_IGNORE_TIMER_RESOLUTION = 2  (0x2)
```

### EcoQoS Usage Pattern
- **Enable EcoQoS (throttle):** `ControlMask = 0x1, StateMask = 0x1`
- **Disable EcoQoS (boost):** `ControlMask = 0x1, StateMask = 0x0`
- **Reset to system-managed:** `ControlMask = 0x0, StateMask = 0x0`

### Go Struct
```go
type PROCESS_POWER_THROTTLING_STATE struct {
    Version     uint32
    ControlMask uint32
    StateMask   uint32
}

const (
    ProcessPowerThrottling                              = 4
    PROCESS_POWER_THROTTLING_CURRENT_VERSION     uint32 = 1
    PROCESS_POWER_THROTTLING_EXECUTION_SPEED     uint32 = 0x1
    PROCESS_POWER_THROTTLING_IGNORE_TIMER_RESOLUTION uint32 = 0x2
)
```

### Go Call (manual LazyProc — NOT in x/sys/windows)
```go
var (
    modkernel32            = windows.NewLazySystemDLL("kernel32.dll")
    procSetProcessInformation = modkernel32.NewProc("SetProcessInformation")
)

func SetProcessInformation(hProcess windows.Handle, class uint32, info unsafe.Pointer, size uint32) error {
    r1, _, err := procSetProcessInformation.Call(
        uintptr(hProcess),
        uintptr(class),
        uintptr(info),
        uintptr(size),
    )
    if r1 == 0 {
        return err
    }
    return nil
}

// Usage:
state := PROCESS_POWER_THROTTLING_STATE{
    Version:     PROCESS_POWER_THROTTLING_CURRENT_VERSION,
    ControlMask: PROCESS_POWER_THROTTLING_EXECUTION_SPEED,
    StateMask:   PROCESS_POWER_THROTTLING_EXECUTION_SPEED, // EcoQoS ON
}
SetProcessInformation(handle, ProcessPowerThrottling, unsafe.Pointer(&state), uint32(unsafe.Sizeof(state)))
```

---

## 2. SetWinEventHook

**DLL:** `user32.dll`  
**Header:** `winuser.h`  
**Min Client:** Windows 2000 Professional

### C Signature
```c
HWINEVENTHOOK SetWinEventHook(
    DWORD        eventMin,         // [in]
    DWORD        eventMax,         // [in]
    HMODULE      hmodWinEventProc, // [in] NULL for out-of-context
    WINEVENTPROC pfnWinEventProc,  // [in] callback
    DWORD        idProcess,        // [in] 0 = all processes
    DWORD        idThread,         // [in] 0 = all threads
    DWORD        dwFlags           // [in]
);
// Returns: HWINEVENTHOOK (handle), or 0 on failure
```

### Constants
```
EVENT_SYSTEM_FOREGROUND    = 0x0003
EVENT_MIN                  = 0x00000001
EVENT_MAX                  = 0x7FFFFFFF

WINEVENT_OUTOFCONTEXT      = 0x0000
WINEVENT_SKIPOWNPROCESS    = 0x0002
WINEVENT_SKIPOWNTHREAD     = 0x0004
WINEVENT_INCONTEXT         = 0x0001
```

### WinEventProc Callback Signature
```c
void CALLBACK WinEventProc(
    HWINEVENTHOOK hWinEventHook,  // handle to event hook instance
    DWORD         event,          // event constant
    HWND          hwnd,           // window handle (or NULL)
    LONG          idObject,       // object identifier
    LONG          idChild,        // child ID (CHILDID_SELF = 0)
    DWORD         idEventThread,  // thread ID that generated event
    DWORD         dwmsEventTime   // time in ms
);
// Returns: void
```

### Critical Requirement
> **The client thread that calls SetWinEventHook MUST have a message loop in order to receive events.**
> For WINEVENT_OUTOFCONTEXT, events are delivered on the same thread that called SetWinEventHook.

### Go Call (manual LazyProc — NOT in x/sys/windows)
```go
var (
    moduser32              = windows.NewLazySystemDLL("user32.dll")
    procSetWinEventHook    = moduser32.NewProc("SetWinEventHook")
    procUnhookWinEvent     = moduser32.NewProc("UnhookWinEvent")
)

const (
    EVENT_SYSTEM_FOREGROUND = 0x0003
    WINEVENT_OUTOFCONTEXT   = 0x0000
    WINEVENT_SKIPOWNPROCESS = 0x0002
)

// Go callback — must match WinEventProc signature exactly
// 7 parameters → use syscall.NewCallback
func winEventCallback(
    hWinEventHook uintptr,
    event uint32,
    hwnd uintptr,
    idObject int32,
    idChild int32,
    idEventThread uint32,
    dwmsEventTime uint32,
) uintptr {
    // Handle EVENT_SYSTEM_FOREGROUND here
    return 0
}

func installHook() (uintptr, error) {
    cb := syscall.NewCallback(winEventCallback)
    r1, _, err := procSetWinEventHook.Call(
        uintptr(EVENT_SYSTEM_FOREGROUND), // eventMin
        uintptr(EVENT_SYSTEM_FOREGROUND), // eventMax
        0,                                // hmodWinEventProc (NULL for out-of-context)
        cb,                               // pfnWinEventProc
        0,                                // idProcess (0 = all)
        0,                                // idThread (0 = all)
        uintptr(WINEVENT_OUTOFCONTEXT|WINEVENT_SKIPOWNPROCESS), // dwFlags
    )
    if r1 == 0 {
        return 0, err
    }
    return r1, nil
}
```

---

## 3. Message Loop (GetMessage / TranslateMessage / DispatchMessage)

**DLL:** `user32.dll`  
**Header:** `winuser.h`

### C Signatures
```c
BOOL GetMessageW(
    LPMSG lpMsg,           // [out]
    HWND  hWnd,            // [in, optional] NULL = any window on current thread
    UINT  wMsgFilterMin,   // [in] 0 = no filter
    UINT  wMsgFilterMax    // [in] 0 = no filter
);
// Returns: nonzero (message other than WM_QUIT), 0 (WM_QUIT), -1 (error)

BOOL TranslateMessage(const MSG *lpMsg);  // [in]
LRESULT DispatchMessageW(const MSG *lpMsg); // [in]
```

### MSG Struct (C)
```c
typedef struct tagMSG {
    HWND   hwnd;      // offset  0, size 8 (64-bit) / 4 (32-bit)  — handle/pointer
    UINT   message;   // offset  8, size 4
    WPARAM wParam;    // offset 16, size 8 (64-bit) / 4 (32-bit)  — uintptr
    LPARAM lParam;    // offset 24, size 8 (64-bit) / 4 (32-bit)  — uintptr
    DWORD  time;      // offset 32, size 4
    POINT  pt;        // offset 36, size 8 (two LONG = 2×4)
    DWORD  lPrivate;  // offset 44, size 4
} MSG;
// Total size (64-bit): 48 bytes
```

### Go Struct (for 64-bit Windows)
```go
type POINT struct {
    X, Y int32
}

type MSG struct {
    Hwnd    uintptr
    Message uint32
    WParam  uintptr
    LParam  uintptr
    Time    uint32
    Pt      POINT
    LPrivate uint32
}
```

### Go Call (manual LazyProc — NOT in x/sys/windows)
```go
var (
    procGetMessageW       = moduser32.NewProc("GetMessageW")
    procTranslateMessage  = moduser32.NewProc("TranslateMessage")
    procDispatchMessageW  = moduser32.NewProc("DispatchMessageW")
)

func messageLoop() {
    var msg MSG
    for {
        r1, _, err := procGetMessageW.Call(
            uintptr(unsafe.Pointer(&msg)),
            0, // hWnd = NULL (all windows on this thread)
            0, // wMsgFilterMin
            0, // wMsgFilterMax
        )
        // r1 == 0 means WM_QUIT
        if int32(r1) <= 0 {
            if int32(r1) == -1 {
                // error
                log.Printf("GetMessage error: %v", err)
            }
            break
        }
        procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
        procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
    }
}
```

### Critical: Thread Pinning
```go
// The message loop and SetWinEventHook MUST run on the same OS thread.
// Use runtime.LockOSThread() at the start of the goroutine.
func runEventLoop() {
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()
    
    hook, _ := installHook()
    defer procUnhookWinEvent.Call(hook)
    
    messageLoop()
}
```

---

## 4. OpenProcess / CloseHandle

### OpenProcess
**DLL:** `kernel32.dll`  
**Header:** `processthreadsapi.h`

```c
HANDLE OpenProcess(
    DWORD dwDesiredAccess,  // [in] access rights
    BOOL  bInheritHandle,   // [in] FALSE typically
    DWORD dwProcessId       // [in] PID
);
// Returns: HANDLE, or NULL on failure
```

### CloseHandle
**DLL:** `kernel32.dll`
```c
BOOL CloseHandle(HANDLE hObject);
```

### Process Access Rights Constants
```
PROCESS_SET_INFORMATION            = 0x0200
PROCESS_QUERY_INFORMATION          = 0x0400
PROCESS_QUERY_LIMITED_INFORMATION  = 0x1000
PROCESS_ALL_ACCESS                 = 0x001FFFFF  (Windows Vista+)
```

### Required Access for Each Operation
| Operation | Required Access |
|-----------|----------------|
| SetProcessInformation | `PROCESS_SET_INFORMATION` (0x0200) |
| QueryFullProcessImageName | `PROCESS_QUERY_LIMITED_INFORMATION` (0x1000) |
| SetPriorityClass | `PROCESS_SET_INFORMATION` (0x0200) |

### Combination for throttler:
```go
const throttleAccess = 0x0200 | 0x1000  // PROCESS_SET_INFORMATION | PROCESS_QUERY_LIMITED_INFORMATION = 0x1200
```

### Go (ALREADY in x/sys/windows)
```go
// Both are available directly:
handle, err := windows.OpenProcess(0x1200, false, pid)
windows.CloseHandle(handle)
```

---

## 5. GetWindowThreadProcessId

**DLL:** `user32.dll`  
**Header:** `winuser.h`

```c
DWORD GetWindowThreadProcessId(
    HWND    hWnd,          // [in]
    LPDWORD lpdwProcessId  // [out, optional]
);
// Returns: thread ID that created the window, 0 on failure
```

### Go (ALREADY in x/sys/windows)
```go
var pid uint32
tid, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
// tid = thread ID, pid = process ID
```

---

## 6. QueryFullProcessImageName

**DLL:** `kernel32.dll`  
**Header:** `winbase.h`  
**Unicode:** `QueryFullProcessImageNameW`

```c
BOOL QueryFullProcessImageNameW(
    HANDLE hProcess,   // [in] needs PROCESS_QUERY_INFORMATION or PROCESS_QUERY_LIMITED_INFORMATION
    DWORD  dwFlags,    // [in] 0 = Win32 path, 1 = native path
    LPWSTR lpExeName,  // [out]
    PDWORD lpdwSize    // [in, out] size in characters
);
```

### Constants
```
PROCESS_NAME_NATIVE = 0x00000001
```

### Go (ALREADY in x/sys/windows)
```go
func getProcessImageName(handle windows.Handle) (string, error) {
    buf := make([]uint16, 1024)
    size := uint32(len(buf))
    err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size)
    if err != nil {
        return "", err
    }
    return windows.UTF16ToString(buf[:size]), nil
}
```

---

## 7. SetPriorityClass

**DLL:** `kernel32.dll`  
**Header:** `processthreadsapi.h`

```c
BOOL SetPriorityClass(
    HANDLE hProcess,         // [in] needs PROCESS_SET_INFORMATION
    DWORD  dwPriorityClass   // [in]
);
```

### Priority Class Constants
```
IDLE_PRIORITY_CLASS           = 0x00000040
BELOW_NORMAL_PRIORITY_CLASS   = 0x00004000
NORMAL_PRIORITY_CLASS         = 0x00000020
ABOVE_NORMAL_PRIORITY_CLASS   = 0x00008000
HIGH_PRIORITY_CLASS           = 0x00000080
REALTIME_PRIORITY_CLASS       = 0x00000100
PROCESS_MODE_BACKGROUND_BEGIN = 0x00100000
PROCESS_MODE_BACKGROUND_END   = 0x00200000
```

### Go (ALREADY in x/sys/windows)
```go
// Throttle:
err := windows.SetPriorityClass(handle, 0x40) // IDLE_PRIORITY_CLASS

// Restore:
err := windows.SetPriorityClass(handle, 0x20) // NORMAL_PRIORITY_CLASS
```

---

## 8. EnumChildWindows

**DLL:** `user32.dll`  
**Header:** `winuser.h`

```c
BOOL EnumChildWindows(
    HWND        hWndParent,  // [in, optional] NULL = equivalent to EnumWindows
    WNDENUMPROC lpEnumFunc,  // [in] callback
    LPARAM      lParam       // [in] app-defined value passed to callback
);
// Return value is not used.
```

### EnumChildProc Callback
```c
BOOL CALLBACK EnumChildProc(
    HWND   hwnd,    // handle to child window
    LPARAM lParam   // app-defined value
);
// Return TRUE to continue, FALSE to stop
```

### Go (ALREADY in x/sys/windows)
```go
// windows.EnumChildWindows signature:
// func EnumChildWindows(hwnd HWND, enumFunc uintptr, param unsafe.Pointer)

// Callback for EnumChildWindows:
func enumChildProc(hwnd uintptr, lParam uintptr) uintptr {
    // Collect child window handles or process IDs
    // Return 1 (TRUE) to continue enumeration, 0 (FALSE) to stop
    return 1
}

cb := syscall.NewCallback(enumChildProc)
windows.EnumChildWindows(parentHwnd, cb, unsafe.Pointer(nil))
```

---

## 9. GetForegroundWindow

**DLL:** `user32.dll`  
**Header:** `winuser.h`

```c
HWND GetForegroundWindow();
// Returns: handle to foreground window, or NULL
```

### Go (ALREADY in x/sys/windows)
```go
hwnd := windows.GetForegroundWindow()
```

---

## 10. Availability Matrix

| API | DLL | In x/sys/windows? | Notes |
|-----|-----|-------------------|-------|
| `OpenProcess` | kernel32.dll | **YES** | `windows.OpenProcess(access, inherit, pid)` |
| `CloseHandle` | kernel32.dll | **YES** | `windows.CloseHandle(handle)` |
| `SetPriorityClass` | kernel32.dll | **YES** | `windows.SetPriorityClass(handle, class)` |
| `GetPriorityClass` | kernel32.dll | **YES** | `windows.GetPriorityClass(handle)` |
| `QueryFullProcessImageName` | kernel32.dll | **YES** | `windows.QueryFullProcessImageName(handle, flags, buf, &size)` |
| `GetWindowThreadProcessId` | user32.dll | **YES** | `windows.GetWindowThreadProcessId(hwnd, &pid)` |
| `EnumChildWindows` | user32.dll | **YES** | `windows.EnumChildWindows(hwnd, cb, param)` |
| `EnumWindows` | user32.dll | **YES** | `windows.EnumWindows(cb, param)` |
| `GetForegroundWindow` | user32.dll | **YES** | `windows.GetForegroundWindow()` |
| `GetClassName` | user32.dll | **YES** | `windows.GetClassName(hwnd, buf, maxCount)` |
| `IsWindowVisible` | user32.dll | **YES** | `windows.IsWindowVisible(hwnd)` |
| **SetProcessInformation** | kernel32.dll | **NO** | Must use LazyProc |
| **SetWinEventHook** | user32.dll | **NO** | Must use LazyProc |
| **UnhookWinEvent** | user32.dll | **NO** | Must use LazyProc |
| **GetMessageW** | user32.dll | **NO** | Must use LazyProc |
| **TranslateMessage** | user32.dll | **NO** | Must use LazyProc |
| **DispatchMessageW** | user32.dll | **NO** | Must use LazyProc |

---

## 11. Go Calling Patterns

### Pattern A: Using x/sys/windows built-in functions

These are type-safe wrappers already generated by `go generate`:

```go
import "golang.org/x/sys/windows"

// Open a process with throttle + query access
handle, err := windows.OpenProcess(
    0x0200|0x1000, // PROCESS_SET_INFORMATION | PROCESS_QUERY_LIMITED_INFORMATION
    false,
    pid,
)
if err != nil {
    return err
}
defer windows.CloseHandle(handle)
```

### Pattern B: Manual LazyProc for missing APIs

```go
import (
    "syscall"
    "unsafe"
    "golang.org/x/sys/windows"
)

var (
    modkernel32 = windows.NewLazySystemDLL("kernel32.dll")
    moduser32   = windows.NewLazySystemDLL("user32.dll")

    procSetProcessInformation = modkernel32.NewProc("SetProcessInformation")
    procSetWinEventHook       = moduser32.NewProc("SetWinEventHook")
    procUnhookWinEvent        = moduser32.NewProc("UnhookWinEvent")
    procGetMessageW           = moduser32.NewProc("GetMessageW")
    procTranslateMessage      = moduser32.NewProc("TranslateMessage")
    procDispatchMessageW      = moduser32.NewProc("DispatchMessageW")
)
```

### Pattern C: Creating a WinEventProc callback in Go

```go
// The callback function signature must match the Win32 calling convention.
// Each parameter maps to a uintptr-sized argument.
// syscall.NewCallback converts a Go function to a C callback pointer.
// 
// CRITICAL: The function must have the EXACT right number of parameters.
// WinEventProc has 7 parameters. The return must be uintptr.

func winEventProc(
    hWinEventHook uintptr,  // HWINEVENTHOOK
    event         uint32,   // DWORD
    hwnd          uintptr,  // HWND
    idObject      int32,    // LONG
    idChild       int32,    // LONG
    idEventThread uint32,   // DWORD
    dwmsEventTime uint32,   // DWORD
) uintptr {
    if event == EVENT_SYSTEM_FOREGROUND {
        // hwnd is the newly focused window
        var pid uint32
        windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
        // ... throttle/unthrottle based on pid
    }
    return 0
}

var winEventCallback = syscall.NewCallback(winEventProc)
```

### Pattern D: Running a Win32 message loop from Go

```go
func runWin32EventLoop() {
    // MUST lock OS thread — message loop and hook must be on same thread
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()

    // Install the foreground window change hook
    hookHandle, _, err := procSetWinEventHook.Call(
        uintptr(EVENT_SYSTEM_FOREGROUND), // eventMin
        uintptr(EVENT_SYSTEM_FOREGROUND), // eventMax
        0,                                // hmodWinEventProc = NULL
        winEventCallback,                 // our Go callback
        0,                                // idProcess = 0 (all)
        0,                                // idThread = 0 (all)
        uintptr(WINEVENT_OUTOFCONTEXT|WINEVENT_SKIPOWNPROCESS),
    )
    if hookHandle == 0 {
        log.Fatalf("SetWinEventHook failed: %v", err)
    }
    defer procUnhookWinEvent.Call(hookHandle)

    // Message pump — required for out-of-context hooks
    var msg MSG
    for {
        ret, _, _ := procGetMessageW.Call(
            uintptr(unsafe.Pointer(&msg)),
            0, 0, 0,
        )
        if int32(ret) <= 0 {
            break // WM_QUIT or error
        }
        procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
        procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
    }
}
```

### Pattern E: EnumChildWindows callback in Go

```go
// Collecting all child-window PIDs for a given parent window
func enumChildWindowsForPIDs(parentHwnd windows.HWND) []uint32 {
    var pids []uint32
    var mu sync.Mutex

    cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
        var pid uint32
        windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
        if pid != 0 {
            mu.Lock()
            pids = append(pids, pid)
            mu.Unlock()
        }
        return 1 // continue enumeration
    })

    windows.EnumChildWindows(parentHwnd, cb, unsafe.Pointer(nil))
    return pids
}
```

---

## Complete Constants Summary

```go
// Process access rights
const (
    PROCESS_TERMINATE                 = 0x0001
    PROCESS_CREATE_THREAD             = 0x0002
    PROCESS_SET_SESSIONID             = 0x0004
    PROCESS_VM_OPERATION              = 0x0008
    PROCESS_VM_READ                   = 0x0010
    PROCESS_VM_WRITE                  = 0x0020
    PROCESS_DUP_HANDLE                = 0x0040
    PROCESS_CREATE_PROCESS            = 0x0080
    PROCESS_SET_QUOTA                 = 0x0100
    PROCESS_SET_INFORMATION           = 0x0200
    PROCESS_QUERY_INFORMATION         = 0x0400
    PROCESS_SUSPEND_RESUME            = 0x0800
    PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
    PROCESS_SET_LIMITED_INFORMATION   = 0x2000
    PROCESS_ALL_ACCESS                = 0x001FFFFF
)

// Priority classes
const (
    NORMAL_PRIORITY_CLASS         = 0x00000020
    IDLE_PRIORITY_CLASS           = 0x00000040
    HIGH_PRIORITY_CLASS           = 0x00000080
    REALTIME_PRIORITY_CLASS       = 0x00000100
    BELOW_NORMAL_PRIORITY_CLASS   = 0x00004000
    ABOVE_NORMAL_PRIORITY_CLASS   = 0x00008000
    PROCESS_MODE_BACKGROUND_BEGIN = 0x00100000
    PROCESS_MODE_BACKGROUND_END   = 0x00200000
)

// Power throttling
const (
    ProcessPowerThrottling                           = 4
    PROCESS_POWER_THROTTLING_CURRENT_VERSION  uint32 = 1
    PROCESS_POWER_THROTTLING_EXECUTION_SPEED  uint32 = 0x1
    PROCESS_POWER_THROTTLING_IGNORE_TIMER_RESOLUTION uint32 = 0x2
)

// WinEvent constants
const (
    EVENT_SYSTEM_FOREGROUND = 0x0003
    WINEVENT_OUTOFCONTEXT   = 0x0000
    WINEVENT_INCONTEXT      = 0x0001
    WINEVENT_SKIPOWNPROCESS = 0x0002
    WINEVENT_SKIPOWNTHREAD  = 0x0004
)
```

---

## Complete Go Struct Definitions

```go
type PROCESS_POWER_THROTTLING_STATE struct {
    Version     uint32 // offset 0
    ControlMask uint32 // offset 4
    StateMask   uint32 // offset 8
}
// unsafe.Sizeof = 12

type POINT struct {
    X int32 // offset 0
    Y int32 // offset 4
}

type MSG struct {
    Hwnd     uintptr // offset 0  (8 bytes on 64-bit)
    Message  uint32  // offset 8
    _        [4]byte // padding to align WParam on 64-bit
    WParam   uintptr // offset 16
    LParam   uintptr // offset 24
    Time     uint32  // offset 32
    Pt       POINT   // offset 36 (8 bytes)
    LPrivate uint32  // offset 44
}
// unsafe.Sizeof = 48 on 64-bit
// Note: MSG is tricky due to alignment. Alternatively, allocate 48 bytes
// and use the structure as-is. The kernel will fill it.

// Simpler MSG if you don't need to read fields (just pass to Translate/Dispatch):
type MSG struct {
    Hwnd    syscall.Handle
    Message uint32
    WParam  uintptr
    LParam  uintptr
    Time    uint32
    Pt      POINT
}
```

---

## Architecture: Putting It All Together

```
┌─────────────────────────────────────────────┐
│  Goroutine (OS-thread-locked)               │
│                                             │
│  1. runtime.LockOSThread()                  │
│  2. SetWinEventHook(EVENT_SYSTEM_FOREGROUND)│
│  3. Message Loop:                           │
│     for { GetMessage → Translate → Dispatch}│
│                                             │
│  WinEventProc callback fires:               │
│    → GetWindowThreadProcessId(hwnd) → pid   │
│    → Previous foreground pid:               │
│       OpenProcess → SetPriorityClass(IDLE)  │  
│       → SetProcessInformation(EcoQoS ON)    │
│       → CloseHandle                         │
│    → New foreground pid:                    │
│       OpenProcess → SetPriorityClass(NORMAL)│
│       → SetProcessInformation(EcoQoS OFF)   │
│       → CloseHandle                         │
└─────────────────────────────────────────────┘
```
