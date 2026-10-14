# Audio Indicator - Agent Guide

This guide helps agents work effectively in this codebase. [DESIGN.md](DESIGN.md)
is the behavior specification.

## Project Overview

A Fyne tray switcher and mixer for Sway + Waybar, PipeWire and WirePlumber through
pipewire-pulse. One process provides the tray, mixer, preferences, and Unix-socket
commands. It switches default device ports and remembers separate per-app output
and input routes.

**Key Features:**
- Port-aware output/input switching, including inactive card profiles
- Default and per-app volume (0–150%) and mute
- Separate saved output/input device choices per app
- Hide/rename devices and delete saved routes
- Keyed widgets and per-slider edit protection
- Subscription-driven snapshots, with no idle polling
- Dark theme (#3a3a3a background, #7aa2d4 accent)

## Audio Backend

All reads and writes use `internal/pulse` and `pactl`. Read JSON with `pactl -f json`;
do not reintroduce regex parsing, `pw-cli`, `wpctl` discovery, or recording-owner
inference. The subscribe process restarts with delay and requests a full refresh
after every start. Errors must be returned or logged, never silently discarded.

## Build Commands

```sh
go build -o ~/.local/bin/soundquay .
go vet ./...
go test -race ./...
```

Do not run `jj`/`git` commands that create commits or describe changes; the parent
handles version control. Do not touch `.agents/`, `.jj/`, `.crush/`, or `.claude/`.

## Run Commands

```sh
soundquay
soundquay toggle
soundquay show
soundquay quit
soundquay snapshot
```

No arguments starts the tray, or sends `show` to an existing process. Socket
commands require a running process. `snapshot` emits JSON without starting Fyne.
No legacy CLI flags or launch-script paths remain.

## Project Structure

```text
internal/pulse/    Command runner, JSON types, snapshots, subscription, audio writes
internal/model/    Identity, config, view, port choices, saved-route stream tracker
coordinator.go    Typed messages, inbox, state owner, command worker, refresh loop
tray.go           Device/app menus and rendered-menu comparison
icons/            Embedded static volume/mute PNGs for Linux tray decoding
mixer.go          Keyed rows, sliders, theme, mixer visibility
prefs.go          Keyed device preferences and saved routes
instance.go       Runtime Unix socket and command client
main.go           CLI, Fyne startup, cancellation and shutdown
DESIGN.md         Behavior and desktop integration specification
README.md         Install, usage, exact Sway/Waybar configuration lines
```

## Code Organization

### Coordinator

One goroutine owns the last snapshot, `model.View`, `model.Config`,
`model.Tracker`, and last error. UI and socket work sends typed `Msg` values.
`Dispatch` appends to a mutex-protected slice and signals a one-slot wake channel;
it never waits for the state owner or drops messages. Pending slider values
replace values with the same slider key. Audio writes run in a separate command
worker and each reports `WriteDone`. Config writes originate only from the state
owner. Render snapshots are immutable once sent to the UI.

The refresh loop collects subscribe events for 100 ms, then requests one snapshot.
A resync resets the stream tracker. Build the view after a successful snapshot;
plan saved route moves and enqueue each write. Debounce volume writes for 50 ms
per slider. Profile selection is one ordered command: profile, snapshot, resulting
device's port if needed, then default device.

### Model

Device names are stable config identities; card-port IDs identify switch entries.
Apps are grouped by `AppKey`, with the newest stream's application name displayed.
Do not invent ownership heuristics. Monitor sources are excluded. Hidden devices
stay in `AllDevices` for preferences; a hidden current default remains selectable.
Per-stream route state applies a route once and does not pull externally moved
streams back. Saved routes are app + direction, not live stream indexes.

### Fyne UI

Schedule background renders with `fyne.Do`. Keep app/device widgets in maps keyed
by identity and update them in place. Rebuild a container only when its keys
change. Suppress callbacks while setting widget values from a snapshot. A slider
being dragged or edited in the previous second ignores its snapshot value;
other sliders still update. The mixer title is always `Audio Mixer`, and close
hides it. Tray and socket toggles share UI-thread visibility state. The tray menu
is rebuilt only when its displayed labels, checked choices, or command targets
change, not on volume-only updates.

## Naming Conventions

Names say what happens in plain domain words. Avoid abstract slogans or ownership
jargon in names, messages, comments, docs, and tests. Describe maxima as limits,
queue entries as slots, and sequence positions as steps. Go exports use PascalCase;
local names use camelCase.

## Code Patterns

- Reuse the pulse/model exported contracts; discuss API changes with their owners.
- Use argument-based command execution, not shell interpolation.
- Keep ordered multi-write device changes in one command worker task.
- Config changes copy maps/slices before rendering or persisting; no shared mutation.
- Save config with same-directory temporary file, sync, and rename.
- Do not silently replace invalid config or mask command failures.
- Keep callback guards local to their row, not a global UI pause.
- No stubs, placeholders, unfinished code notes, or fake fallback data.

## Important Gotchas

1. PipeWire JSON can report 105% volume, unset owner values as null or the unsigned
   sentinel, and card profiles/ports as maps rather than arrays.
2. Different app streams can share a key; volume/mute applies to every stream,
   while routing applies only to the chosen direction.
3. Choosing Follow default deletes the saved route and moves current streams.
   Changing the default alone must not explicitly move routed app streams.
4. A saved target may be absent. Keep its route visible and wait only while the
   original stream has not been moved elsewhere.
5. Fyne does not expose window visibility; app-owned visibility tracking must
   cover tray taps, socket commands, and close interception.
6. Replace only stale Unix socket files, not arbitrary files. Socket access is
   limited to the current user with mode 0600 in `XDG_RUNTIME_DIR`.
7. Never edit `~/.config/*` for desktop integration. Document exact changes instead.

## Dependencies

- Go 1.25.5
- Fyne v2.7.2 and its desktop build libraries
- fyne.io/systray for a shared tray/socket toggle callback
- `pactl` with JSON support
- Sway with XWayland and Waybar's `tray` module on the target machine

## Testing

Tests are deterministic and behavior-focused: transitions, limits, errors, burst
refreshes, command order, app selection, stream tracker decisions, and Unix socket
round trips. Do not test forwarding/wiring or source text. Use captured JSON
fixtures or fake runners/subscribers; do not depend on live audio hardware in
unit tests. Live desktop smoke checks belong to explicit integration verification.

## Integration

See README for the complete examples. Sway starts `exec soundquay` and
matches `for_window [title="Audio Mixer"] floating enable, border none, move position mouse`.
Waybar retains its readout/scroll actions and uses
`"on-click": "soundquay toggle"`; include its `tray` module.

## Common Tasks

### Adding an Audio Action

Add a typed message in the coordinator, use an existing pulse operation, enqueue
an error-reporting command, then update the keyed UI. Add a behavior test for
its transition or failure, not the forwarding path.

### Modifying Theme

Edit `DarkTheme` in `mixer.go`. Preserve the existing dark colors unless requested.

### Changing Refresh Timing

The snapshot loop owns the 100 ms event collection interval. Volume writes use
50 ms per-slider timers; slider snapshot protection lasts one second. Keep these
independent.

## Audio Command Reference

`internal/pulse` owns `info`, device/card/stream lists, `subscribe`, default, volume,
mute, port/profile changes, and stream moves. Main-package code must not execute
its own audio shell commands.
