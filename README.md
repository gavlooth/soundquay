# Audio Indicator - Fyne Audio Popup

A Fyne-based GUI application that replicates EWW audio indicator functionality for Wayland compositors (Sway, Hyprland, etc).

## Features

- **Main Volume Control**: Slider with 5% increments and mute toggle
- **Output Device Selection**: List of available audio sinks with default indicator
- **Microphone Control**: Volume slider and mute toggle for microphone
- **Input Device Selection**: List of available audio sources
- **Per-App Audio Routing**:
  - Volume slider for each playing application
  - Mute toggle per application
  - Device selection dropdown (click ▼ to expand)
  - Click any output device to move app's audio
- **Recording Applications**: Control for microphone-using applications
- **Dark Theme**: Matches EWW dark theme (#3a3a3a background, #7aa2d4 accent)
- **Real-time Updates**: Background polling updates audio status every 2 seconds

## Installation

```bash
cd /home/heefoo/audio-indicator
go build -o audio-indicator
```

## Usage

### Standalone
```bash
./audio-indicator
```

### Diagnostics
```bash
./audio-indicator -diagnostics
```

This prints backend capabilities, command probes, current audio status, saved preferred recording sources, discovered PipeWire source nodes, discovered active recording streams, likely recording-related PipeWire clients, and an inferred candidate list for each active recording stream as JSON.

### Move a Live Recording Stream
```bash
./audio-indicator -move-advanced-stream-id 36 -move-advanced-stream-source alsa_input.usb-_Jabra_Link_390_6CFBED5C5C34-00.mono-fallback
```

This applies the move through the same WirePlumber-aware path the GUI uses, then prints a fresh diagnostics snapshot.

### Save and Apply Preferred Recording Sources
```bash
./audio-indicator -set-preferred-recording-app "Echo-Cancel Capture" -set-preferred-recording-source alsa_input.usb-_Jabra_Link_390_6CFBED5C5C34-00.mono-fallback
./audio-indicator -apply-preferred-recording-sources
```

The first command updates the saved preference file. The second applies saved preferred sources to any currently matching live recording streams, then prints diagnostics.

### Watch Recording State
```bash
./audio-indicator -watch-recording-state
```

This polls every 2 seconds and only prints when the active recording streams, inferred owner candidates, or saved preferred sources change. It is intended for validating behavior across a restarted recording session.

### Manual Validation
1. Start a recording app and make it request microphone access.
2. Run `./audio-indicator -diagnostics`.
3. Confirm the app appears under `advanced_streams` or `status.mic_streams`.
   If the session manager exposes a filter-owned stream instead of the app directly, check `recording_client_candidates` for likely app-side clients such as `Google Chrome input` or `WEBRTC VoiceEngine`.
4. Use the GUI to set a preferred source for that app.
5. Run `./audio-indicator -diagnostics` again and confirm `preferred_recording_sources` contains the app name and source.
6. Stop and restart the app's recording session, then confirm the preferred source is reapplied.

### Integration with Status Bar (Waybar, EWW, etc.)

**Waybar example** (`~/.config/waybar/config.jsonc`):
```json
"custom/audio": {
  "exec": "~/.config/eww/tools/audiobar status waybar",
  "on-click": "/home/heefoo/audio-indicator/audio-indicator &",
  "signal": 10
}
```

**EWW example** (`eww.yuck`):
```yuck
(button :onclick "/home/heefoo/audio-indicator/audio-indicator &"
  (label :text "󰕾"))
```

**Sway keybinding** (`~/.config/sway/config`):
```conf
bindsym $mod+Shift+a exec /home/heefoo/audio-indicator/audio-indicator
```

## Audio Backend

Supports both:
- **PipeWire** (via `wpctl` and `pactl`)
- **PulseAudio** (via `pactl`)

The application automatically detects which is available.

When PipeWire and WirePlumber metadata are available, `Applications Recording` distinguishes between:
- **Current source**: the live source attached to an active recording stream right now
- **Preferred source**: the saved source the app will try to reapply when that recording app creates a matching capture stream again

If no live recording stream exists yet, the preferred source is deferred until the next matching active capture stream appears.

## Dependencies

- Go 1.19+
- Fyne 2.7+
- PipeWire or PulseAudio

```bash
go get fyne.io/fyne/v2
```

## Building

Standard build:
```bash
go build
```

Release build (optimized):
```bash
go build -ldflags="-s -w" -o audio-indicator
```

## Files

- `main.go` - Fyne GUI implementation
- `audio.go` - Audio management functions (PulseAudio/PipeWire)
- `go.mod` - Go module dependencies

## Key Differences from EWW

1. **Native GUI**: Fyne provides native widgets instead of EWW's DOM-based rendering
2. **Independent**: Doesn't require EWW daemon running
3. **Scroll Support**: To be implemented (currently uses slider drag)
4. **Click-to-Expand**: Per-app device dropdowns work the same as EWW

## Nerd Font Icons Used

| Icon | Purpose |
|-------|---------|
| 󰕾 | Audio / Volume (unmuted) |
| 󰝟 | Volume (muted) |
| 󰍬 | Microphone (unmuted) |
| 󰍭 | Microphone (muted) |
| 󰐵 | Default device |
| 󰐶 | Non-default device |
| 󰅖 | Close button |
| ▼ / ▲ | Expand/collapse dropdown |

## License

Same as EWW configuration - personal use.
