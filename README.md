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
