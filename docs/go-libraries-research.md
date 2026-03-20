# Go Libraries & Patterns Research for EnergyStarGo

Comprehensive, code-ready reference for building EnergyStarGo as a Windows 11 service with system tray, CLI, configuration, logging, and process enumeration.

---

## Table of Contents

1. [Windows Service (`golang.org/x/sys/windows/svc`)](#1-windows-service)
2. [System Tray](#2-system-tray)
3. [CLI Framework](#3-cli-framework)
4. [Configuration Files](#4-configuration-files)
5. [Logging](#5-logging)
6. [Process Enumeration](#6-process-enumeration)

---

## 1. Windows Service

**Import paths:**
```
golang.org/x/sys/windows/svc          — service handler, Run, state machine
golang.org/x/sys/windows/svc/mgr      — SCM manager, install/remove/control services
golang.org/x/sys/windows/svc/eventlog — Windows Event Log registration
golang.org/x/sys/windows/svc/debug    — console-mode service runner for debugging
```

**Latest version:** `golang.org/x/sys v0.42.0` (published Mar 3, 2026)

### 1.1 Core Types & Constants

```go
// States
svc.Stopped         // SERVICE_STOPPED
svc.StartPending    // SERVICE_START_PENDING
svc.StopPending     // SERVICE_STOP_PENDING
svc.Running         // SERVICE_RUNNING
svc.ContinuePending // SERVICE_CONTINUE_PENDING
svc.PausePending    // SERVICE_PAUSE_PENDING
svc.Paused          // SERVICE_PAUSED

// Commands (sent by SCM to handler)
svc.Stop, svc.Pause, svc.Continue, svc.Interrogate, svc.Shutdown
svc.ParamChange, svc.SessionChange, svc.PreShutdown

// Accepted flags (what the service tells SCM it handles)
svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue
svc.AcceptSessionChange | svc.AcceptPreShutdown
```

### 1.2 Handler Interface

```go
type Handler interface {
    Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (svcSpecificEC bool, exitCode uint32)
}
```

- `args` — service name followed by start parameters
- `r` — channel of incoming state change requests from SCM
- `s` — channel to send status updates back to SCM
- Return `(false, 0)` for clean exit

### 1.3 ChangeRequest & Status

```go
type ChangeRequest struct {
    Cmd           Cmd
    EventType     uint32
    EventData     uintptr
    CurrentStatus Status
    Context       uintptr
}

type Status struct {
    State                   State
    Accepts                 Accepted
    CheckPoint              uint32  // progress during lengthy operation
    WaitHint                uint32  // estimated time for pending op (ms)
    ProcessId               uint32
    Win32ExitCode           uint32
    ServiceSpecificExitCode uint32
}
```

### 1.4 Minimal Service Handler

```go
package main

import (
    "fmt"
    "golang.org/x/sys/windows/svc"
    "golang.org/x/sys/windows/svc/debug"
    "golang.org/x/sys/windows/svc/eventlog"
)

type energyStarService struct{}

func (s *energyStarService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
    const accepted = svc.AcceptStop | svc.AcceptShutdown
    changes <- svc.Status{State: svc.StartPending}

    // — Initialize your throttler here —

    changes <- svc.Status{State: svc.Running, Accepts: accepted}

loop:
    for {
        select {
        case c := <-r:
            switch c.Cmd {
            case svc.Interrogate:
                changes <- c.CurrentStatus
            case svc.Stop, svc.Shutdown:
                changes <- svc.Status{State: svc.StopPending}
                // — Cleanup your throttler here —
                break loop
            }
        }
    }
    return false, 0
}
```

### 1.5 Detecting Service vs Interactive Mode

```go
func main() {
    inService, err := svc.IsWindowsService()
    if err != nil {
        log.Fatalf("failed to determine if running as service: %v", err)
    }
    if inService {
        // Run as Windows service
        svc.Run("EnergyStarGo", &energyStarService{})
        return
    }
    // Run interactively (CLI mode, debug mode, install/remove, etc.)
}
```

> **Note:** `svc.IsWindowsService()` replaces the deprecated `IsAnInteractiveSession()`. It checks if the parent process is `services.exe` with session ID 0.

### 1.6 Debug Mode (Console Runner)

```go
import "golang.org/x/sys/windows/svc/debug"

// debug.Run() sends Ctrl+C as svc.Stop, runs handler on console
err := debug.Run("EnergyStarGo", &energyStarService{})
```

`debug.Log` interface is shared with `eventlog.Log`:
```go
type Log interface {
    Close() error
    Info(eid uint32, msg string) error
    Warning(eid uint32, msg string) error
    Error(eid uint32, msg string) error
}
```
`debug.New(name)` returns a `Log` that prints to stderr.

### 1.7 Service Installation

```go
import (
    "golang.org/x/sys/windows/svc/mgr"
    "golang.org/x/sys/windows/svc/eventlog"
)

func installService(name, displayName, exePath string) error {
    m, err := mgr.Connect()
    if err != nil {
        return err
    }
    defer m.Disconnect()

    s, err := m.CreateService(name, exePath, mgr.Config{
        DisplayName: displayName,
        StartType:   mgr.StartAutomatic,
        Description: "EnergyStarGo EcoQoS process throttler",
    })
    if err != nil {
        return err
    }
    defer s.Close()

    // Register event log source
    err = eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
    if err != nil {
        s.Delete()
        return fmt.Errorf("InstallAsEventCreate failed: %w", err)
    }

    // Optional: set recovery actions (restart on failure)
    ra := []mgr.RecoveryAction{
        {Type: mgr.ServiceRestart, Delay: 5 * time.Second},
        {Type: mgr.ServiceRestart, Delay: 30 * time.Second},
        {Type: mgr.NoAction, Delay: 0},
    }
    s.SetRecoveryActions(ra, 86400) // reset period in seconds

    return nil
}
```

### 1.8 Service Removal / Control

```go
func removeService(name string) error {
    m, err := mgr.Connect()
    if err != nil {
        return err
    }
    defer m.Disconnect()

    s, err := m.OpenService(name)
    if err != nil {
        return fmt.Errorf("service %s not installed", name)
    }
    defer s.Close()

    err = s.Delete()
    if err != nil {
        return err
    }
    return eventlog.Remove(name)
}

func controlService(name string, cmd svc.Cmd, targetState svc.State) error {
    m, err := mgr.Connect()
    if err != nil {
        return err
    }
    defer m.Disconnect()

    s, err := m.OpenService(name)
    if err != nil {
        return err
    }
    defer s.Close()

    status, err := s.Control(cmd)
    if err != nil {
        return err
    }

    timeout := time.Now().Add(10 * time.Second)
    for status.State != targetState {
        if time.Now().After(timeout) {
            return fmt.Errorf("timeout waiting for state %d", targetState)
        }
        time.Sleep(300 * time.Millisecond)
        status, err = s.Query()
        if err != nil {
            return err
        }
    }
    return nil
}
```

### 1.9 mgr.Config Fields

```go
type Config struct {
    ServiceType      uint32   // default: SERVICE_WIN32_OWN_PROCESS
    StartType        uint32   // StartManual, StartAutomatic, StartDisabled
    ErrorControl     uint32
    BinaryPathName   string
    LoadOrderGroup   string
    TagId            uint32
    Dependencies     []string
    ServiceStartName string   // account (e.g., "LocalSystem")
    DisplayName      string
    Password         string
    Description      string
    SidType          uint32   // SERVICE_SID_TYPE_*
    DelayedAutoStart bool     // start after other auto-start services + delay
}
```

### 1.10 Gotchas

- **Only one service per process** — `svc.Run` uses a package-level global `theService`.
- **`svc.Run` blocks** until the handler's `Execute` returns.
- **Event IDs 1–1000** if using `EventCreate.exe` as the message file (via `InstallAsEventCreate`).
- **Admin required** for `mgr.Connect()`, `CreateService`, `Delete`.
- **`svc.IsWindowsService()`** uses `NtQueryInformationProcess` + `NtQuerySystemInformation` (no WMI dependency).
- **StartType defaults to `StartManual`** if you pass 0.

---

## 2. System Tray

### 2.1 Option A: `github.com/getlantern/systray` v1.2.2

**Import:** `github.com/getlantern/systray`  
**License:** Apache-2.0 | **Stars:** ~3.7k  
**Requires CGO:** Yes (Windows uses Win32 API via C, Linux uses GTK)

#### Key API

```go
// Blocks until Quit() is called or the process exits.
func Run(onReady func(), onExit func())

// Also available: RunWithExternalLoop(onReady, onExit) — for integrating
// with an existing event loop. Returns start/end functions.

func SetIcon(iconBytes []byte)       // .ico bytes on Windows
func SetTitle(title string)          // macOS menu bar only; no-op on Windows
func SetTooltip(tooltip string)

func AddMenuItem(title, tooltip string) *MenuItem
func AddSeparator()
func Quit()

// MenuItem methods:
type MenuItem struct {
    ClickedCh chan struct{} // receive click events
}
func (m *MenuItem) AddSubMenuItem(title, tooltip string) *MenuItem
func (m *MenuItem) SetTitle(title string)
func (m *MenuItem) SetTooltip(tooltip string)
func (m *MenuItem) SetIcon(iconBytes []byte)
func (m *MenuItem) Check() / Uncheck() / Checked() bool
func (m *MenuItem) Enable() / Disable()
func (m *MenuItem) Show() / Hide()
```

#### Minimal Example

```go
package main

import (
    _ "embed"
    "github.com/getlantern/systray"
)

//go:embed icon.ico
var iconData []byte

func main() {
    systray.Run(onReady, onExit)
}

func onReady() {
    systray.SetIcon(iconData)
    systray.SetTooltip("EnergyStarGo")

    mStatus := systray.AddMenuItem("Status: Running", "")
    mStatus.Disable()

    systray.AddSeparator()
    mQuit := systray.AddMenuItem("Quit", "Exit EnergyStarGo")

    go func() {
        <-mQuit.ClickedCh
        systray.Quit()
    }()
}

func onExit() {
    // Cleanup
}
```

#### Build Flag

```bash
go build -ldflags "-H=windowsgui"  # hides console window
```

### 2.2 Option B: `github.com/energye/systray` v1.0.3

**Import:** `github.com/energye/systray`  
**Fork of getlantern/systray** — removes GTK dependency on Linux (uses DBus), adds click handlers.

#### Additional API over getlantern

```go
func SetOnClick(fn func())      // left-click on tray icon
func SetOnDClick(fn func())     // double-click on tray icon
func SetOnRClick(fn func(menu IMenu))  // right-click; call menu.ShowMenu() to show

// IMenu interface allows controlling when context menu appears
```

#### When to Choose

| Feature | getlantern/systray | energye/systray |
|---------|-------------------|-----------------|
| Maturity | Well-established | Active fork |
| CGO on Linux | Yes (GTK) | No (DBus) |
| Left-click handling | No (menu only) | `SetOnClick` |
| Double-click handling | No | `SetOnDClick` |
| Right-click control | Auto-shows menu | `SetOnRClick(menu IMenu)` |
| Sub-menus | Yes | Yes |
| Check/Uncheck items | Yes | Yes |

**Recommendation for EnergyStarGo:** Use `energye/systray` — no GTK dep matters less on Windows, but the click handlers are useful for toggling throttling on/off with a click.

### 2.3 Gotchas

- **`systray.Run()` blocks** — run your service logic in goroutines launched from `onReady`.
- **Icon format:** Windows expects `.ico` (not PNG). Embed with `//go:embed`.
- **Must call from main goroutine** on macOS (irrelevant for Windows-only, but good to know).
- **`RunWithExternalLoop`** returns `(start, end func())` — call `start()` to init, `end()` when done. Useful if you already have a message loop.

### 2.4 Integrating Tray with Service

The tray only works interactively (session 0 services can't show UI). Pattern:

```
┌──────────────────────────┐    ┌──────────────────────────┐
│  Windows Service         │    │  Tray Application        │
│  (Session 0)             │◄──►│  (User Session)          │
│                          │    │                          │
│  - Throttling logic      │IPC │  - systray.Run()         │
│  - WinEventHook+MsgLoop  │    │  - Status display        │
│  - Process enumeration   │    │  - Start/Stop controls   │
└──────────────────────────┘    └──────────────────────────┘
```

Or run everything in a single interactive process (no service) for simplicity during development.

---

## 3. CLI Framework

### 3.1 Option A: `github.com/spf13/cobra` v1.10.2

**Import:** `github.com/spf13/cobra`  
**License:** Apache-2.0 | **Used by:** 184k+ packages (kubectl, docker, Hugo, etc.)

#### Key API

```go
type Command struct {
    Use     string        // one-line usage (e.g., "install")
    Short   string        // short description
    Long    string        // long description
    Run     func(cmd *Command, args []string)       // action
    RunE    func(cmd *Command, args []string) error  // action with error
    Args    PositionalArgs // e.g., cobra.NoArgs, cobra.ExactArgs(1)

    // Lifecycle hooks (run in order):
    PersistentPreRun  func(cmd *Command, args []string)  // inherited by children
    PreRun            func(cmd *Command, args []string)
    // Run/RunE
    PostRun           func(cmd *Command, args []string)
    PersistentPostRun func(cmd *Command, args []string) // inherited by children
}

func (c *Command) AddCommand(cmds ...*Command)
func (c *Command) Flags() *pflag.FlagSet           // local flags
func (c *Command) PersistentFlags() *pflag.FlagSet  // inherited by children
func (c *Command) Execute() error                   // entry point
```

#### Minimal Example

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
    Use:   "energystar",
    Short: "EcoQoS process throttler for Windows 11",
}

var installCmd = &cobra.Command{
    Use:   "install",
    Short: "Install as Windows service",
    RunE: func(cmd *cobra.Command, args []string) error {
        return installService("EnergyStarGo", "EnergyStarGo Service", exePath())
    },
}

var removeCmd = &cobra.Command{
    Use:   "remove",
    Short: "Remove Windows service",
    RunE: func(cmd *cobra.Command, args []string) error {
        return removeService("EnergyStarGo")
    },
}

var runCmd = &cobra.Command{
    Use:   "run",
    Short: "Run interactively (debug mode)",
    RunE: func(cmd *cobra.Command, args []string) error {
        return debug.Run("EnergyStarGo", &energyStarService{})
    },
}

func init() {
    rootCmd.AddCommand(installCmd, removeCmd, runCmd)
    rootCmd.PersistentFlags().StringP("config", "c", "", "config file path")
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

### 3.2 Option B: `github.com/urfave/cli/v2` v2.27.7

**Import:** `github.com/urfave/cli/v2`  
**License:** MIT | **Note:** v3 is available but v2 is more widely adopted.

#### Key API

```go
type App struct {
    Name, Usage, Version string
    Commands []*Command
    Flags    []Flag
    Action   ActionFunc      // default action
    Before   BeforeFunc      // runs before any command
    After    AfterFunc       // runs after any command
}

func (a *App) Run(arguments []string) error

type Command struct {
    Name, Usage string
    Action      func(c *cli.Context) error
    Flags       []Flag
    Subcommands []*Command
}

// Flag types:
&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "config file"}
&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}}
&cli.IntFlag{Name: "port", Value: 8080}

// Context methods:
c.String("config")
c.Bool("verbose")
c.Int("port")
c.IsSet("config")
```

#### Minimal Example

```go
package main

import (
    "os"
    "github.com/urfave/cli/v2"
)

func main() {
    app := &cli.App{
        Name:  "energystar",
        Usage: "EcoQoS process throttler",
        Commands: []*cli.Command{
            {
                Name:  "install",
                Usage: "Install as Windows service",
                Action: func(c *cli.Context) error {
                    return installService("EnergyStarGo", "EnergyStarGo", exePath())
                },
            },
            {
                Name:  "remove",
                Usage: "Remove Windows service",
                Action: func(c *cli.Context) error {
                    return removeService("EnergyStarGo")
                },
            },
            {
                Name:  "run",
                Usage: "Run interactively",
                Action: func(c *cli.Context) error {
                    return debug.Run("EnergyStarGo", &energyStarService{})
                },
            },
        },
        Flags: []cli.Flag{
            &cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "config file"},
        },
    }

    app.Run(os.Args)
}
```

### 3.3 Comparison & Recommendation

| Feature | cobra | urfave/cli v2 |
|---------|-------|---------------|
| Adoption | Massive (kubectl, docker) | Very large |
| Flag library | pflag (POSIX) | Built-in |
| Viper integration | Native (OnInitialize) | Manual |
| Shell completions | Built-in (bash/zsh/fish/ps) | Built-in |
| Help generation | Automatic | Automatic |
| Subcommand nesting | Unlimited | Unlimited |
| Weight | Medium (~pflag dep) | Light |

**Recommendation:** Use **cobra** — better viper integration for config, and the EnergyStarGo CLI mirrors the pattern used by the official x/sys/windows/svc example (subcommands: install, remove, start, stop, run).

### 3.4 Pattern: Service-Aware main()

```go
func main() {
    inService, err := svc.IsWindowsService()
    if err != nil {
        log.Fatal(err)
    }
    if inService {
        svc.Run("EnergyStarGo", &energyStarService{})
        return
    }
    // Only parse CLI when running interactively
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

---

## 4. Configuration Files

### 4.1 Option A: `gopkg.in/yaml.v3`

**Import:** `gopkg.in/yaml.v3`  
**License:** MIT/Apache-2.0

```go
import "gopkg.in/yaml.v3"

type Config struct {
    Whitelist    []string `yaml:"whitelist"`
    PollInterval int      `yaml:"poll_interval"` // seconds
    EnableTray   bool     `yaml:"enable_tray"`
    LogLevel     string   `yaml:"log_level"`
}

func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, err
    }
    return &cfg, nil
}
```

Example `config.yaml`:
```yaml
whitelist:
  - explorer.exe
  - dwm.exe
  - taskmgr.exe
poll_interval: 5
enable_tray: true
log_level: info
```

### 4.2 Option B: `github.com/BurntSushi/toml`

**Import:** `github.com/BurntSushi/toml`  
**License:** MIT

```go
import "github.com/BurntSushi/toml"

type Config struct {
    Whitelist    []string `toml:"whitelist"`
    PollInterval int      `toml:"poll_interval"`
    EnableTray   bool     `toml:"enable_tray"`
    LogLevel     string   `toml:"log_level"`
}

func LoadConfig(path string) (*Config, error) {
    var cfg Config
    _, err := toml.DecodeFile(path, &cfg)
    return &cfg, err
}
```

### 4.3 Option C: `github.com/spf13/viper`

**Import:** `github.com/spf13/viper`  
**License:** MIT

Viper unifies config files (YAML/TOML/JSON/INI), environment variables, CLI flags, and remote config.

```go
import (
    "github.com/spf13/viper"
    "github.com/spf13/cobra"
)

func initConfig() {
    viper.SetConfigName("config")
    viper.SetConfigType("yaml")
    viper.AddConfigPath(".")               // current directory
    viper.AddConfigPath("$APPDATA/EnergyStarGo") // Windows app data

    viper.SetDefault("poll_interval", 5)
    viper.SetDefault("enable_tray", true)
    viper.SetDefault("log_level", "info")

    viper.SetEnvPrefix("ENERGYSTAR")
    viper.AutomaticEnv()

    if err := viper.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            log.Fatalf("config error: %v", err)
        }
    }
}

// Bind cobra flags to viper:
func init() {
    cobra.OnInitialize(initConfig)
    rootCmd.PersistentFlags().StringP("config", "c", "", "config file")
    viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))
}

// Access anywhere:
whitelist := viper.GetStringSlice("whitelist")
interval := viper.GetInt("poll_interval")
```

### 4.4 Comparison

| Feature | yaml.v3 | BurntSushi/toml | viper |
|---------|---------|-----------------|-------|
| Simplicity | High | High | Medium |
| Dependencies | Minimal | Minimal | Many (yaml, toml, pflag, etc.) |
| Env vars | No | No | Built-in |
| CLI flag binding | No | No | cobra integration |
| Watch/reload | No | No | `viper.WatchConfig()` |
| Format support | YAML only | TOML only | All formats |

**Recommendation:** Use **viper** with cobra for full config flexibility. If you want minimal deps, `gopkg.in/yaml.v3` with a simple struct is perfectly sufficient.

### 4.5 Config File Location Pattern (Windows)

```go
func defaultConfigPath() string {
    appData := os.Getenv("APPDATA") // e.g., C:\Users\<user>\AppData\Roaming
    if appData == "" {
        appData = "."
    }
    return filepath.Join(appData, "EnergyStarGo", "config.yaml")
}

// For service context (runs as SYSTEM), use the executable directory:
func serviceConfigPath() string {
    exe, _ := os.Executable()
    return filepath.Join(filepath.Dir(exe), "config.yaml")
}
```

---

## 5. Logging

### 5.1 Windows Event Log

```go
import "golang.org/x/sys/windows/svc/eventlog"

// Registration (during install):
eventlog.InstallAsEventCreate("EnergyStarGo", eventlog.Error|eventlog.Warning|eventlog.Info)

// Usage:
elog, err := eventlog.Open("EnergyStarGo")
if err != nil { ... }
defer elog.Close()

elog.Info(1, "service started")
elog.Warning(2, "process access denied: notepad.exe")
elog.Error(3, fmt.Sprintf("fatal error: %v", err))

// Removal (during uninstall):
eventlog.Remove("EnergyStarGo")
```

**Event IDs:** Must be 1–1000 when using EventCreate.exe as the message file.

**Interface:** `eventlog.Log` satisfies `debug.Log`, so you can swap them:

```go
var elog debug.Log  // shared interface

if isDebug {
    elog = debug.New("EnergyStarGo")  // prints to stderr
} else {
    elog, err = eventlog.Open("EnergyStarGo")  // writes to Event Log
}
```

### 5.2 `log/slog` (Standard Library, Go 1.21+)

```go
import "log/slog"

// JSON handler (for file logging):
handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
})
logger := slog.New(handler)

logger.Info("throttling process", "pid", 1234, "name", "chrome.exe")
logger.Warn("access denied", "pid", 5678, "error", err)

// Text handler (human-readable):
handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
})
```

#### Custom Level Mapping

```go
// Set as default logger:
slog.SetDefault(logger)

// Then use package-level functions:
slog.Info("msg", "key", "value")
slog.Error("msg", "error", err)
```

#### Writing to File

```go
f, err := os.OpenFile("energystar.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
if err != nil { ... }
defer f.Close()

handler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})
logger := slog.New(handler)
```

### 5.3 File Rotation: `gopkg.in/natefinish/lumberjack.v2`

**Import:** `gopkg.in/natefinish/lumberjack.v2`  
**License:** MIT

Lumberjack is an `io.WriteCloser` that rotates log files by size/age/count.

```go
import (
    "log/slog"
    "gopkg.in/natefinish/lumberjack.v2"
)

writer := &lumberjack.Logger{
    Filename:   `C:\ProgramData\EnergyStarGo\energystar.log`,
    MaxSize:    10,    // megabytes before rotation
    MaxBackups: 3,     // number of old files to keep
    MaxAge:     28,    // days to retain old files
    Compress:   true,  // gzip rotated files
}
defer writer.Close()

handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{
    Level: slog.LevelInfo,
})
slog.SetDefault(slog.New(handler))
```

### 5.4 Combined Logging Strategy

```go
import (
    "io"
    "log/slog"
    "golang.org/x/sys/windows/svc/debug"
    "golang.org/x/sys/windows/svc/eventlog"
    "gopkg.in/natefinish/lumberjack.v2"
)

func setupLogging(isService bool) (*slog.Logger, debug.Log, error) {
    // File logging with rotation (always)
    fileWriter := &lumberjack.Logger{
        Filename:   logFilePath(),
        MaxSize:    10,
        MaxBackups: 3,
        Compress:   true,
    }

    handler := slog.NewJSONHandler(fileWriter, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    })
    logger := slog.New(handler)

    // Event log (service) or stderr (interactive)
    var elog debug.Log
    if isService {
        var err error
        elog, err = eventlog.Open("EnergyStarGo")
        if err != nil {
            return nil, nil, err
        }
    } else {
        elog = debug.New("EnergyStarGo")
    }

    return logger, elog, nil
}
```

### 5.5 Gotchas

- **Event Log** is for high-level service lifecycle events (start, stop, errors), not verbose logs.
- **slog** is for structured application logging to files.
- **Lumberjack** handles rotation — don't roll your own.
- **Service account (SYSTEM)** needs write access to the log directory.
- **`C:\ProgramData\EnergyStarGo\`** is a good location for service logs (writable by SYSTEM).

---

## 6. Process Enumeration

All functions below are available in `golang.org/x/sys/windows`.

### 6.1 Toolhelp32 Snapshot (Primary Method)

Used by the standard library itself in `getProcessEntry()`.

```go
import (
    "unsafe"
    "golang.org/x/sys/windows"
)

func enumerateProcesses() ([]windows.ProcessEntry32, error) {
    snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
    if err != nil {
        return nil, err
    }
    defer windows.CloseHandle(snapshot)

    var entry windows.ProcessEntry32
    entry.Size = uint32(unsafe.Sizeof(entry))

    err = windows.Process32First(snapshot, &entry)
    if err != nil {
        return nil, err
    }

    var processes []windows.ProcessEntry32
    for {
        processes = append(processes, entry)
        err = windows.Process32Next(snapshot, &entry)
        if err != nil {
            break // ERROR_NO_MORE_FILES
        }
    }
    return processes, nil
}
```

### 6.2 ProcessEntry32 Struct

```go
type ProcessEntry32 struct {
    Size            uint32
    Usage           uint32
    ProcessID       uint32
    DefaultHeapID   uintptr
    ModuleID        uint32
    Threads         uint32
    ParentProcessID uint32
    PriClassBase    int32
    Flags           uint32
    ExeFile         [MAX_PATH]uint16 // 260 wide chars
}
```

**Getting the EXE name:**
```go
exeName := windows.UTF16ToString(entry.ExeFile[:])
```

### 6.3 Toolhelp32 Constants

```go
const (
    TH32CS_SNAPHEAPLIST = 0x01
    TH32CS_SNAPPROCESS  = 0x02
    TH32CS_SNAPTHREAD   = 0x04
    TH32CS_SNAPMODULE   = 0x08
    TH32CS_SNAPMODULE32 = 0x10
    TH32CS_SNAPALL      = TH32CS_SNAPHEAPLIST | TH32CS_SNAPMODULE | TH32CS_SNAPPROCESS | TH32CS_SNAPTHREAD
    TH32CS_INHERIT      = 0x80000000
)
```

### 6.4 EnumProcesses (PSAPI Alternative)

```go
func enumAllPIDs() ([]uint32, error) {
    pids := make([]uint32, 2048)
    var bytesReturned uint32
    err := windows.EnumProcesses(pids, &bytesReturned)
    if err != nil {
        return nil, err
    }
    count := bytesReturned / 4 // each PID is uint32 = 4 bytes
    return pids[:count], nil
}
```

### 6.5 Get Full Process Image Name

```go
func getProcessName(pid uint32) (string, error) {
    handle, err := windows.OpenProcess(
        windows.PROCESS_QUERY_LIMITED_INFORMATION,
        false,
        pid,
    )
    if err != nil {
        return "", err
    }
    defer windows.CloseHandle(handle)

    buf := make([]uint16, 1024)
    size := uint32(len(buf))
    err = windows.QueryFullProcessImageName(handle, 0, &buf[0], &size)
    if err != nil {
        return "", err
    }
    return windows.UTF16ToString(buf[:size]), nil
}
```

### 6.6 ProcessIdToSessionId

```go
func getSessionID(pid uint32) (uint32, error) {
    var sessionID uint32
    err := windows.ProcessIdToSessionId(pid, &sessionID)
    return sessionID, err
}
```

### 6.7 Complete Process Enumeration Pattern for EnergyStarGo

```go
// Enumerate all processes, get names, filter by session
func getThrottleTargets(currentForegroundPID uint32, whitelist map[string]bool) ([]uint32, error) {
    snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
    if err != nil {
        return nil, err
    }
    defer windows.CloseHandle(snapshot)

    var entry windows.ProcessEntry32
    entry.Size = uint32(unsafe.Sizeof(entry))

    if err = windows.Process32First(snapshot, &entry); err != nil {
        return nil, err
    }

    var targets []uint32
    for {
        pid := entry.ProcessID
        if pid != 0 && pid != currentForegroundPID {
            exeName := windows.UTF16ToString(entry.ExeFile[:])
            if !whitelist[strings.ToLower(exeName)] {
                // Check session — only throttle user-session processes
                var sessionID uint32
                if windows.ProcessIdToSessionId(pid, &sessionID) == nil && sessionID != 0 {
                    targets = append(targets, pid)
                }
            }
        }
        if err = windows.Process32Next(snapshot, &entry); err != nil {
            break
        }
    }
    return targets, nil
}
```

### 6.8 Thread Enumeration (Bonus)

```go
type ThreadEntry32 struct {
    Size           uint32
    Usage          uint32
    ThreadID       uint32
    OwnerProcessID uint32
    BasePri        int32
    DeltaPri       int32
    Flags          uint32
}
```

Available via `windows.Thread32First` / `windows.Thread32Next` with `TH32CS_SNAPTHREAD`.

### 6.9 Module Enumeration

```go
type ModuleEntry32 struct {
    Size         uint32
    ModuleID     uint32
    ProcessID    uint32
    GlblcntUsage uint32
    ProccntUsage uint32
    ModBaseAddr  uintptr
    ModBaseSize  uint32
    ModuleHandle Handle
    Module       [MAX_MODULE_NAME32 + 1]uint16 // 256 wide chars
    ExePath      [MAX_PATH]uint16
}
```

Available via `windows.Module32First` / `windows.Module32Next` with `TH32CS_SNAPMODULE`.

### 6.10 Gotchas

- **Always set `entry.Size`** before calling `Process32First` / `Process32Next`. Forgetting this is the #1 bug.
- **PID 0** is the System Idle Process — skip it.
- **Access denied** is expected for system processes when calling `OpenProcess`. Handle gracefully.
- **Snapshot is a point-in-time** — processes may exit between snapshot and `OpenProcess`.
- **`EnumProcesses`** returns only PIDs (no names), so you need `OpenProcess` + `QueryFullProcessImageName` for each. Toolhelp32 gives names directly via `ExeFile`.
- **`QueryFullProcessImageName`** gives the full path (e.g., `C:\Windows\explorer.exe`). `ProcessEntry32.ExeFile` gives only the filename (`explorer.exe`).
- **Session 0** is the service session — user processes are in session ≥ 1.

---

## Quick Dependency Summary

```
go get golang.org/x/sys@latest           # windows service, process APIs
go get github.com/energye/systray        # system tray (or github.com/getlantern/systray)
go get github.com/spf13/cobra            # CLI framework
go get github.com/spf13/viper            # config management (optional, pulls in yaml/toml)
go get gopkg.in/yaml.v3                  # YAML config (if not using viper)
go get gopkg.in/natefinish/lumberjack.v2  # log file rotation
```

## Suggested Project Structure

```
EnergyStarGo/
├── cmd/
│   └── energystar/
│       └── main.go          # CLI entry point + service detection
├── internal/
│   ├── service/
│   │   ├── handler.go       # svc.Handler implementation
│   │   ├── install.go       # install/remove via mgr
│   │   └── manage.go        # start/stop/control
│   ├── throttler/
│   │   ├── throttler.go     # EcoQoS + SetPriorityClass logic
│   │   ├── winevent.go      # SetWinEventHook + message loop
│   │   └── process.go       # process enumeration, filtering
│   ├── tray/
│   │   └── tray.go          # system tray UI
│   ├── config/
│   │   └── config.go        # config loading (viper or yaml)
│   └── logging/
│       └── logging.go       # slog + eventlog + lumberjack setup
├── configs/
│   └── config.yaml          # default config file
├── docs/
│   ├── win32-api-reference.md
│   └── go-libraries-research.md
├── go.mod
└── go.sum
```
