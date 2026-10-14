# Audio Indicator

A single-process tray switcher and audio mixer for Sway + Waybar and PipeWire
through pipewire-pulse. It also works with a PulseAudio server that supports
`pactl -f json`. See [DESIGN.md](DESIGN.md) for the device, app identity, and saved
route rules.

## Install

Requires Go 1.25.5+, Fyne 2.7.2's Linux build dependencies (C compiler, OpenGL and
X11 development libraries), and `pactl`. Waybar must include its `tray` module.
The Fyne Linux desktop driver uses XWayland on Sway.

```sh
mkdir -p ~/.local/bin
go build -o ~/.local/bin/soundquay .
```

Ensure `~/.local/bin` is in the desktop session's `PATH`. Use this installed copy
in Sway and Waybar, rather than the old repository binary or launch script.

## Usage

```sh
soundquay          # start the tray; an existing instance shows its mixer
soundquay toggle   # show/hide the existing mixer
soundquay show     # show and focus the existing mixer
soundquay quit     # stop the existing process
soundquay snapshot # JSON {"snapshot": ..., "view": ...}; no GUI startup
```

`toggle`, `show`, and `quit` exit with a message and status 1 if no instance is
running. The process listens on `$XDG_RUNTIME_DIR/soundquay.sock`; a stale
socket is replaced on startup. Command replies are `ok` followed by a newline.

Right-click the tray icon for Output/Input device ports, app routes, output and
microphone mute, the mixer, preferences, or Quit. The tray icon reflects output
mute. Left-click toggles the mixer. Closing either window hides it; Quit stops the
process. If the tray is unavailable, the socket commands still open the mixer.

The mixer uses 0–150% sliders, mute buttons, default-device dropdowns, and one row
per running app. Each app has an Out line and an In line, each with its own
volume, mute and device picker; the In picker is shown even before the app records,
so a microphone can be chosen in advance. Out/In
choices save separate routes; `Follow default` deletes that direction's route and
moves its live streams to the current default. `saved` identifies saved choices.
When a stream is on a different device than its saved one (or than the default,
when it follows the default), the line says where it really is. **Reset apps**
(header, asks first) forgets every saved app device and sends every app's streams
to the current default output and input.
Unavailable route targets remain visible. Saved routes for apps that are not
running have Delete buttons. Streams with no stable application identity can
change volume/mute but cannot save a route.

A saved route is applied once to a new stream, or later when its device returns
if nobody has moved that stream meanwhile. External app moves are not repeatedly
undone. Default changes leave routed apps alone; WirePlumber handles default
following for unrouted apps. Selecting a port on an inactive card profile changes
the profile, finds the resulting device, selects its port, and changes the default.

Preferences lists all non-monitor devices, including hidden ones. Hide and
nicknames are persisted; press Enter or Save for a nickname. A hidden current
default remains in the switcher. Preferences also lists every saved route for
deletion. Settings live at `~/.config/soundquay/config.json` (or the
corresponding `$XDG_CONFIG_HOME` path) and are written using a synced temporary
file and rename. Unsupported config versions are errors, not silently replaced.

Snapshots follow `pactl subscribe` events, collected for 100 ms per refresh, with
no idle polling. Slider writes wait 50 ms after the latest value. Snapshot values
do not replace an active drag or a slider edit made in the previous second.
Command errors appear as one line at the bottom of the mixer and on stderr.

## Desktop integration

Edit these files yourself; the application does not modify desktop settings.
Remove the old `launch.sh`/repository-path invocation and old
`soundquay.service` autostart (disable the old user service if installed).

In `~/.config/sway/config`:

```sway
exec soundquay
for_window [title="Audio Mixer"] floating enable, border none, move position mouse
bindsym $mod+Shift+a exec soundquay toggle
```

In the Waybar configuration, keep `tray` in the module list. Change the existing
`custom/audio` click action to exactly:

```json
"on-click": "soundquay toggle"
```

For example, keep the existing volume readout and scroll actions, replacing only
the click command:

```json
"custom/audio": {
  "exec": "~/.config/eww/tools/audiobar status waybar",
  "on-click": "soundquay toggle",
  "signal": 10
}
```

`audiobar` is separate from this project. The fixed mixer title is `Audio Mixer`;
there are no title or advanced recording CLI flags.

## Development

- `internal/pulse`: JSON snapshots, subscribe process/restarts, audio writes.
- `internal/model`: app identity, switch entries, view, config, stream route tracker.
- `coordinator.go`: one state owner, non-blocking inbox, snapshot refreshes and writes.
- `tray.go`, `mixer.go`, `prefs.go`: tray rendering and keyed Fyne widgets.
- `instance.go`, `main.go`: Unix socket, CLI, application startup/shutdown.
- `icons/`: embedded static volume/mute PNG icons for the tray.

```sh
go vet ./...
go test -race ./...
```

Tests focus on transitions, command errors, config persistence, burst refreshes,
slider edits, stream routing, profile switching, and socket behavior.
