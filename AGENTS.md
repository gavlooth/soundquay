# Audio Indicator - Agent Guide

This guide helps agents work effectively in this codebase.

## Project Overview

A Fyne-based GUI application that provides audio control for Wayland compositors (Sway, Hyprland, etc). It replicates EWW audio indicator functionality with a native GUI.

**Key Features:**
- Main volume control with mute toggle
- Output/input device selection
- Microphone control
- Per-app audio routing with device selection dropdowns
- Dark theme matching EWW style
- Real-time updates (2-second polling)

## Audio Backend

The application uses command-line tools to control audio:
- **Primary**: `pactl` (PulseAudio PipeWire API)
- **Secondary**: `wpctl` (PipeWire control)

Works with both PulseAudio and PipeWire - automatically detects available backend.

## Build Commands

```bash
# Standard build
go build

# Build with specific output name
go build -o audio-indicator

# Release build (optimized, smaller binary)
go build -ldflags="-s -w" -o audio-indicator
```

## Run Commands

```bash
# Standalone
./audio-indicator

# Via launch script (handles single instance)
./launch.sh

# Background execution for status bar integration
./audio-indicator &
```

## Project Structure

```
audio-indicator/
├── main.go              # Fyne GUI implementation
├── audio.go             # Audio backend operations
├── go.mod               # Go module definition
├── go.sum               # Dependency checksums
├── README.md            # Documentation
├── launch.sh            # Launch script with PID management
├── audio-indicator.service  # Systemd service file
└── audio-indicator      # Compiled binary
```

## Code Organization

### main.go (GUI Layer)
- **AudioApp struct**: Main application state
  - `window`: Fyne window
  - `status`: Current audio status
  - `expanded`: Map tracking expanded dropdown states
  - `updateSignal`: Channel for UI update signaling
- **Pattern**: One-way data flow - background goroutine updates status, signals UI refresh via channel
- **UI updates**: Complete container recreation via `refreshUI()` on every update (2s interval)
- **Custom theme**: `DarkTheme` implements `fyne.Theme` interface for EWW-matching colors

### audio.go (Backend Layer)
- **Data structures**:
  - `AudioStatus`: Complete audio state
  - `AudioSink`/`AudioSource`: Device information
  - `AudioStream`/`MicStream`: Per-application audio streams
- **Pattern**: `pactl list` output parsing with regex for state extraction
- **Concurrency**: `statusMutex` protects `currentStatus` for thread-safe access
- **State management**: `GetCurrentStatus()` returns thread-safe snapshot, `getStatus()` updates internal state

## Naming Conventions

- **Functions**: `PascalCase` for exported
- **Variables**: `camelCase` for local, exported structs
- **Constants**: Not used (values embedded in code)
- **GUI widget naming**: `make{Component}()` pattern (e.g., `makeHeader()`, `makeMainVolume()`)

## Code Patterns

### Variable Capture in Closures
```go
// Pattern: Capture loop variable to avoid closure bug
for _, sink := range a.status.Sinks {
    sink := sink // Required - create local copy
    check := widget.NewCheck(...)
    check.OnChanged = func(checked bool) {
        // Use captured sink, not loop variable
        setSink(sink.Name)
    }
}
```

### UI Update Pattern
```go
// Background goroutine collects data
go func() {
    for {
        time.Sleep(2 * time.Second)
        status := GetCurrentStatus()
        appInstance.status = status

        // Signal UI thread to update
        select {
        case appInstance.updateSignal <- struct{}{}:
        default:
        }
    }
}()

// Main thread handles UI updates
go func() {
    for range appInstance.updateSignal {
        appInstance.refreshUI()
    }
}()
```

### Audio Command Pattern
All audio operations use `runCmd()` wrapper around `exec.Command()`:
```go
func setVolume(volume int) {
    _, _ = runCmd("pactl", "set-sink-volume", "@DEFAULT_SINK@", fmt.Sprintf("%d%%", volume))
}
```

**Note**: Errors are silently ignored in audio commands (`_, _ = ...`).

### Regex Parsing Pattern
`pactl list` output parsing follows consistent pattern:
```go
scanner := bufio.NewScanner(strings.NewReader(out))
var sink *AudioSink
reVolume := regexp.MustCompile(`volume:\s+front-left:\s+(\d+)/\s+`)

for scanner.Scan() {
    line := scanner.Text()
    line = strings.TrimSpace(line)

    if strings.Contains(line, "Sink #") {
        if sink != nil {
            sinks = append(sinks, *sink)
        }
        sink = &AudioSink{}
        // Parse ID...
    } else if sink != nil {
        // Parse properties with regex matches
    }
}
```

## Important Gotchas

### 1. Variable Capture in Closures
**Critical**: Must capture loop variables in local copy before using in closures. See pattern above.

### 2. Channel Update Signal
The `updateSignal` channel is non-blocking. If full (channel buffered 0), the update is silently dropped:
```go
select {
case appInstance.updateSignal <- struct{}{}:
default:
    // Update dropped, not an error
}
```

### 3. UI Refresh Approach
Every 2 seconds, the entire UI tree is recreated. This is simple but potentially inefficient. Don't optimize prematurely - this is the current design.

### 4. Thread Safety
Always use `GetCurrentStatus()` to read status (uses read lock). Don't access `currentStatus` directly from other packages.

### 5. Hardcoded Paths
- Binary path: `/home/heefoo/audio-indicator/audio-indicator`
- PID file: `$HOME/.cache/audio-indicator.pid`

These are hardcoded in launch.sh and service file. Changes require updating both files.

### 6. Nerd Font Icons
Icons are Nerd Font glyphs embedded as strings. Requires Nerd Font installed and configured in terminal/GUI:
- 󰕾 Volume (unmuted)
- 󰝟 Volume (muted)
- 󰍬 Microphone (unmuted)
- 󰍭 Microphone (muted)
- 󰐵 Default device
- 󰐶 Non-default device
- 󰅖 Close button
- ▼/▲ Expand/collapse dropdown

## Dependencies

- **Go**: 1.25.5 (see go.mod)
- **Fyne**: fyne.io/fyne/v2 v2.7.2
- **Audio**: PipeWire or PulseAudio (via pactl command)
- **System**: Nerd Font (for icon rendering)

## Testing

**No tests present** - project lacks test coverage. Consider adding:
- Unit tests for regex parsing in audio.go
- Integration tests with mock `pactl` output
- GUI tests for Fyne interactions

## Integration

### Status Bar Integration

**Waybar**:
```json
"custom/audio": {
  "exec": "~/.config/eww/tools/audiobar status waybar",
  "on-click": "/home/heefoo/audio-indicator/audio-indicator &",
  "signal": 10
}
```

**EWW**:
```yuck
(button :onclick "/home/heefoo/audio-indicator/audio-indicator &"
  (label :text "󰕾"))
```

**Sway keybinding**:
```conf
bindsym $mod+Shift+a exec /home/heefoo/audio-indicator/audio-indicator
```

## Common Tasks

### Adding a New Audio Control
1. Add data field to `AudioStatus` struct in audio.go
2. Add parsing function (or extend existing) using regex pattern
3. Create GUI widget in main.go: `make{Component}()`
4. Add widget to `makeAudioPopup()` container
5. Implement control functions using `pactl` commands

### Modifying Theme
Edit `DarkTheme` struct methods in main.go:
- `Color()`: Define color palette
- `Size()`: Adjust padding/margins
- Current colors match EWW dark theme (#3a3a3a background, #7aa2d4 accent)

### Changing Polling Interval
Modify `time.Sleep()` value in main.go background goroutine (currently 2 seconds).

## Audio Command Reference

Common `pactl` commands used:
- `pactl list sinks` - List output devices
- `pactl list sources` - List input devices
- `pactl list sink-inputs` - List playing applications
- `pactl list source-outputs` - List recording applications
- `pactl set-sink-volume @DEFAULT_SINK@ X%` - Set main volume
- `pactl set-sink-mute @DEFAULT_SINK@ true|false` - Mute/unmute main
- `pactl set-sink-input-volume ID X%` - Set app volume
- `pactl move-sink-input ID SINK` - Move app to device
- `pactl subscribe` - Listen for audio events (not used in current implementation)
