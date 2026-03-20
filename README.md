# EnergyStarGo

A Go port of [EnergyStar](https://github.com/imbushuo/EnergyStar) — an automatic EcoQoS process throttler for Windows 11 that improves battery life and reduces heat on laptops.

[![License: GPL v2](https://img.shields.io/badge/License-GPL%20v2-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8)](https://go.dev/)
[![Windows 11](https://img.shields.io/badge/Windows-11%20build%2022000%2B-0078D4)](https://www.microsoft.com/windows/windows-11)

## What It Does

EnergyStarGo monitors foreground/background process transitions on Windows 11 and automatically:

- **Throttles background processes** using EcoQoS (Efficiency Mode), reducing their CPU priority and enabling power-efficient scheduling
- **Boosts foreground processes** back to normal priority when you switch to them
- **Performs periodic sweeps** to catch any processes that were missed

This is the same mechanism that Windows 11's built-in Efficiency Mode uses — EnergyStarGo just applies it automatically to all background processes.

## Requirements

- **Windows 11** (build 22000 or later) — EcoQoS APIs are not available on Windows 10
- Windows 11 22H2+ recommended for best results

## Top Features

| # | Feature | Description |
|---|---------|-------------|
| 1 | **Throttle Profiles** | `balanced` (default) and `aggressive` profiles control how many processes are exempted from throttling — tune for responsiveness vs. maximum battery savings |
| 2 | **EcoQoS (Efficiency Mode)** | Applies `SetProcessInformation(ProcessPowerThrottling)` + `IDLE_PRIORITY_CLASS` to background processes, the same API Windows 11 uses internally |
| 3 | **Real-time Foreground Detection** | Uses `SetWinEventHook(EVENT_SYSTEM_FOREGROUND)` to instantly boost the process you switch to, with zero polling overhead |
| 4 | **UWP App Support** | Resolves UWP apps behind `ApplicationFrameHost.exe` via `EnumChildWindows` so modern apps are boosted correctly |
| 5 | **Non-blocking System Tray** | System tray icon with status, stats, Pause/Resume, Restore, and Exit — all callbacks run with 1-second timeouts so the UI never hangs |
| 6 | **Windows Service Mode** | Install as a Windows service with automatic recovery; survives login/logout and runs without a visible window |
| 7 | **Zerolog Structured Logging** | Fast, zero-allocation structured logging via `zerolog` with configurable levels (debug/info/warn/error) and optional file output |
| 8 | **Housekeeping Sweeps** | Periodic re-scan (configurable interval, 5 min default) throttles processes spawned after startup; aggressive profile defaults to 2 min |
| 9 | **Restore on Exit** | Optionally restores all throttled processes to normal priority on exit, so nothing is left in an unexpected state |
| 10 | **JSON Configuration** | Persistent JSON config with profile, bypass list overrides, log settings, and housekeeping interval; config auto-discovered next to the executable |

## Throttle Profiles

EnergyStarGo ships with two built-in profiles selected by the `--profile` flag or `"profile"` JSON key.

### `balanced` (default)

Protects audio, input, DWM, the shell, and commonly-used system services so the laptop feels responsive while throttling background work (search indexing, widgets, update notifications, print spooler, etc.).

Exempt: `dwm`, `audiodg`, `ctfmon`, `explorer`, `svchost`, `lsass`, `csrss`, `wininit`, `winlogon`, `services`, `audiodg`, `inputapp`, `textinputhost`, `shellexperiencehost`, `applicationframehost`, `searchhost`, `sihost`, `runtimebroker`, `taskhostw`, `wmiprvse`, `conhost`, `dllhost`, `taskmgr`, `lockapp`, `msmpeng`, `mpcmdrun`, `securityhealthservice`, `fontdrvhost`, `procmon`, `procmon64`

### `aggressive`

Absolute minimum exemption list for maximum battery savings. Only kernel, auth, realtime audio/DWM, and input processes are exempt. Everything else — including indexing, update notifications, widgets, shell extras — is throttled.

Exempt: `smss`, `csrss`, `wininit`, `winlogon`, `lsass`, `services`, `svchost`, `dwm`, `audiodg`, `ctfmon`, `inputapp`, `explorer`, `applicationframehost`, `msmpeng`

Switch profiles at runtime:
```powershell
energystar.exe run --profile aggressive --tray
```

Or set in config:
```json
{ "profile": "aggressive" }
```

## Installation

### Build from Source

```bash
# Build for Windows amd64
make build

# Build for Windows arm64
make build-arm64
```

### Direct Build

```bash
GOOS=windows GOARCH=amd64 go build -o energystar.exe ./cmd/energystar/
```

## Usage

### Interactive Mode

```powershell
# Run in foreground (balanced profile)
energystar.exe run

# Run with system tray icon
energystar.exe run --tray

# Maximum battery savings
energystar.exe run --profile aggressive --tray

# Run with verbose logging
energystar.exe run --verbose

# Run with custom config
energystar.exe run --config myconfig.json

# Run with additional bypass processes
energystar.exe run --bypass "chrome.exe,firefox.exe,spotify.exe"
```

### Windows Service

```powershell
# Install as a service (requires admin)
energystar.exe install

# Install with custom config
energystar.exe install --config C:\path\to\config.json

# Start the service
energystar.exe start

# Check service status
energystar.exe status

# Remove the service
energystar.exe uninstall
```

### Configuration

```powershell
# Generate default configuration file
energystar.exe config

# Generate to a specific path
energystar.exe config --output myconfig.json
```

### All Commands

```
energystar <command> [flags]

Commands:
  run          Run in foreground (interactive mode)
  install      Install as a Windows service
  uninstall    Remove the Windows service
  start        Start the installed service
  status       Query service status
  config       Generate default configuration file
  version      Print version information

Run Flags:
  --tray                  Show system tray icon
  --config <path>         Path to configuration file
  --log-file <path>       Path to log file
  --log-level <level>     Log level: debug, info, warn, error
  --housekeeping <secs>   Housekeeping interval in seconds (0 = profile default)
  --restore-on-exit       Restore process priorities on exit
  --bypass <proc,...>     Additional processes to bypass (comma-separated)
  --profile <name>        Throttle profile: balanced (default) or aggressive
  --verbose               Enable verbose/debug logging
```

## Configuration File

```json
{
  "housekeeping_interval_seconds": 300,
  "profile": "balanced",
  "bypass_processes": [],
  "extra_bypass_processes": ["myapp.exe"],
  "log_file": "energystar.log",
  "log_level": "info",
  "enable_event_log": false,
  "restore_on_exit": true,
  "min_build_number": 22000,
  "boost_foreground_only": false
}
```

Set `"profile": "aggressive"` and leave `"bypass_processes"` empty to use the aggressive built-in list, or populate `"bypass_processes"` to fully override the list.

## Architecture

```
cmd/energystar/          Main entry point + CLI
internal/
  winapi/                Windows API syscalls, structs, constants
  throttle/              Core EcoQoS throttling engine + Win32 message loop
  config/                Configuration + throttle profiles
  logger/                Zerolog-based structured logging
  service/               Windows service lifecycle (install/start/stop)
  tray/                  System tray icon (pure Win32, no CGo)
```

### How It Works

1. **Startup**: Checks Windows 11, installs a `WinEventHook` for `EVENT_SYSTEM_FOREGROUND`, and throttles all user-session background processes (initial sweep).

2. **Foreground Change**: Hook fires on every window switch:
   - Identifies the new foreground process (with UWP resolution via `EnumChildWindows`)
   - Removes EcoQoS throttling from it (boost)
   - Applies EcoQoS + `IDLE_PRIORITY_CLASS` to the previous foreground process

3. **Housekeeping**: Periodic re-scan catches processes spawned after startup.

4. **Shutdown**: `Stop()` posts `WM_QUIT` via `PostThreadMessage` to the message loop thread, unblocking `GetMessage` immediately without races.

## Bypass List Philosophy

The `balanced` profile was reviewed against real battery-laptop usage to optimise the list:

| What changed vs. naïve lists | Reason |
|-------------------------------|--------|
| `msedge.exe` removed from exempt | Edge has built-in power management; let it participate in EcoQoS |
| `searchindexer.exe` not exempted | Background indexing is exactly the workload EcoQoS targets |
| `widgets.exe` not exempted | Non-essential cosmetic feature; 200 ms refresh lag is unnoticeable |
| `spoolsv.exe` not exempted | Print spooler is async; queued jobs aren't latency-sensitive |
| `dwm.exe`, `audiodg.exe` always exempt | Throttling DWM causes visual stutter; throttling audiodg causes audio pops — both are immediately perceptible |
| `ctfmon.exe`, `inputapp.exe` always exempt | Keyboard/touch input latency is instantly noticeable |

## Credits

Based on [EnergyStar](https://github.com/imbushuo/EnergyStar) by [@imbushuo](https://github.com/imbushuo).

## License

Copyright (C) 2024 Kyle Sanderson

This program is free software; you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation; version 2 of the License only.

See [LICENSE](LICENSE) for the full text.


## What It Does

EnergyStarGo monitors foreground/background process transitions on Windows 11 and automatically:

- **Throttles background processes** using EcoQoS (Efficiency Mode), reducing their CPU priority and enabling power-efficient scheduling
- **Boosts foreground processes** back to normal priority when you switch to them
- **Performs periodic sweeps** to catch any processes that were missed

This is the same mechanism that Windows 11's built-in Efficiency Mode uses — EnergyStarGo just applies it automatically to all background processes.

## Requirements

- **Windows 11** (build 22000 or later) — EcoQoS APIs are not available on Windows 10
- Windows 11 22H2+ recommended for best results

## Features

| Feature | Description |
|---------|-------------|
| **EcoQoS Throttling** | Automatically applies Efficiency Mode to background processes |
| **Foreground Detection** | Uses Win32 `SetWinEventHook` to detect foreground window changes in real-time |
| **UWP Support** | Properly resolves UWP apps behind `ApplicationFrameHost.exe` |
| **Windows Service** | Install and run as a Windows service with auto-restart recovery |
| **System Tray** | System tray icon with status, pause/resume, and manual process restore |
| **Configurable Bypass List** | Built-in whitelist of 40+ system processes, plus user-configurable additions |
| **Housekeeping Sweeps** | Periodic re-scan (default: 5 min) catches newly spawned processes |
| **Restore on Exit** | Optionally restores all process priorities when the application exits |
| **Structured Logging** | `slog`-based logging with configurable levels and file output |
| **CLI Interface** | Full command-line interface with subcommands and flags |
| **JSON Config** | Optional JSON configuration file for persistent settings |

## Installation

### Build from Source

```bash
# Build for Windows amd64
make build

# Build for Windows arm64
make build-arm64

# Build both
make cross-compile
```

### Direct Build

```bash
GOOS=windows GOARCH=amd64 go build -o energystar.exe ./cmd/energystar/
```

## Usage

### Interactive Mode

```powershell
# Run in foreground
energystar.exe run

# Run with system tray icon
energystar.exe run --tray

# Run with verbose logging
energystar.exe run --verbose

# Run with custom config
energystar.exe run --config myconfig.json

# Run with additional bypass processes
energystar.exe run --bypass "chrome.exe,firefox.exe,spotify.exe"
```

### Windows Service

```powershell
# Install as a service (requires admin)
energystar.exe install

# Install with custom config
energystar.exe install --config C:\path\to\config.json

# Start the service
energystar.exe start

# Check service status
energystar.exe status

# Remove the service
energystar.exe uninstall
```

### Configuration

```powershell
# Generate default configuration file
energystar.exe config

# Generate to a specific path
energystar.exe config --output myconfig.json
```

### All Commands

```
energystar <command> [flags]

Commands:
  run          Run in foreground (interactive mode)
  install      Install as a Windows service
  uninstall    Remove the Windows service
  start        Start the installed service
  status       Query service status
  config       Generate default configuration file
  version      Print version information

Run Flags:
  --tray                  Show system tray icon
  --config <path>         Path to configuration file
  --log-file <path>       Path to log file
  --log-level <level>     Log level: debug, info, warn, error
  --housekeeping <secs>   Housekeeping interval in seconds (default: 300)
  --restore-on-exit       Restore process priorities on exit
  --bypass <proc,...>     Additional processes to bypass (comma-separated)
  --verbose               Enable verbose/debug logging
```

## Configuration File

The JSON configuration file supports:

```json
{
  "housekeeping_interval_seconds": 300,
  "bypass_processes": ["csrss.exe", "dwm.exe", "..."],
  "extra_bypass_processes": ["myapp.exe"],
  "log_file": "energystar.log",
  "log_level": "info",
  "enable_event_log": false,
  "restore_on_exit": true,
  "min_build_number": 22000,
  "boost_foreground_only": false
}
```

## Architecture

```
cmd/energystar/          Main entry point with CLI
internal/
  winapi/                Windows API declarations (syscalls, structs, constants)
  throttle/              Core EcoQoS throttling engine
  config/                Configuration management
  logger/                Structured logging
  service/               Windows service support
  tray/                  System tray icon
```

### How It Works

1. **Startup**: Checks Windows 11 version, installs a `WinEventHook` for `EVENT_SYSTEM_FOREGROUND`, and performs an initial sweep throttling all background processes in the current session.

2. **Foreground Change**: When the user switches windows, the hook callback:
   - Identifies the new foreground process (with special UWP handling via `EnumChildWindows`)
   - Removes EcoQoS throttling from the foreground process (boosts it)
   - Applies EcoQoS throttling to the previous foreground process

3. **Housekeeping**: Every 5 minutes (configurable), re-scans all processes to catch any that were spawned after startup.

4. **EcoQoS**: Uses `SetProcessInformation` with `ProcessPowerThrottling` class to enable/disable efficiency mode, combined with `SetPriorityClass` for IDLE/NORMAL priority.

## Default Bypass List

The following processes are never throttled:

- **System critical**: `csrss.exe`, `smss.exe`, `svchost.exe`, `dwm.exe`, `lsass.exe`, `services.exe`, `wininit.exe`, `winlogon.exe`
- **Shell**: `explorer.exe`, `sihost.exe`, `SearchHost.exe`, `StartMenuExperienceHost.exe`, `ShellExperienceHost.exe`
- **Input**: `ctfmon.exe`, `ChsIME.exe`, `TextInputHost.exe`
- **Audio**: `audiodg.exe`
- **Security**: `MsMpEng.exe`, `SecurityHealthService.exe`
- **Task managers**: `taskmgr.exe`, `procmon.exe`
- **Browser (self-managed)**: `msedge.exe`
- And more — see `internal/config/config.go` for the full list.

## Credits

Based on [EnergyStar](https://github.com/imbushuo/EnergyStar) by [@imbushuo](https://github.com/imbushuo).

## License

MIT
