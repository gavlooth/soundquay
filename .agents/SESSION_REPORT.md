# Session Reports

## 2026-10-14 — Code review of audio-indicator

- **Objective**: Review the app (audio.go, main.go, preferences.go, launch.sh, service file).
- **Workspace**: /home/heefoo/audio-indicator, working copy on top of `main` (8507006f) with large uncommitted WIP (backend interface, PipeWire advanced recording routing, preferences, message coordinator, tests).
- **Changes**: Added `.gitignore` (`/audio-indicator`) because jj refused to snapshot the 30 MiB binary. No source changes.
- **Coverage**: Review only; 100% of Go sources and scripts read.
- **Commands**: `go vet ./...` (clean), `go test -race ./...` (pass), `-diagnostics` run on this machine (0.4 s, 39 execs incl. detection), throwaway probe tests (deleted).
- **Confirmed defects (probe-verified)**:
  1. `rankRecordingCandidatesForStream` ignores `stream.ClientID`; every capture stream gets the same top candidate (probe: OBS and Firefox streams both inferred as "Google Chrome input"). UI labels and grouping follow this guess, and a source picked on such a row is saved under the guessed name. Auto-apply looks up the stream's raw `Application`, so it does not apply one preference to every stream; it can apply a preference set on the OBS row to the real Chrome stream instead (code-read).
  2. In `makeApplicationGroupItem`, advanced rows that also have a pactl mic stream get title "Source Route"; `makeAdvancedRecordingItemWithTitle` uses the title as the preference key. Probe: preference saved as `{"Source Route": ...}`, auto-apply never matches, a permanent "Source Route" group appears. No UI/CLI path deletes preferences.
- **Code-read defects (not runtime-verified)**: every poll sets `NeedsRebuild` three times → slider widgets replaced mid-drag and the 3 s grace period is bypassed; dropped `AdvancedDiscoveryRefreshed` leaves `RefreshInFlight` true forever; `PipeWireID` assigns the default sink's id to every sink and source; launch.sh trap never runs after `exec` (stale PID + reuse → SIGUSR1 to unrelated process); Go ignores SIGUSR1 so "bring to front" is a no-op (verified with a test program); regexes compiled per line; ~16 pactl/wpctl spawns per status poll with sinks/sources listed twice; dead code (`subscribe`, `GetCurrentStatus`, `StartBackgroundUpdate`, `UpdateStatus`, old `makeApplicationsPlaying/Recording` tree); AGENTS.md describes the old `updateSignal` design and "no tests".
- **Recommendation**: Fix (1) by matching `candidate.ClientID == stream.ClientID` first and only using heuristics when the stream's own client is generic; fix (2) by keying preferences on the application label, not the row title. Then stop full rebuilds on unchanged status.
- **Unresolved**: None blocking. Next: user decides which fixes to implement.

Signature: omp/claude-opus-5-5

## 2026-10-14 — Redesign: tray switcher with per-app routes

- **Objective**: Design the app as a Sound Switcher Indicator–style tray switcher with per-app device selection, for this machine.
- **Workspace**: /home/heefoo/audio-indicator. Machine: Sway + Waybar (tray present), PipeWire 1.6.9, WirePlumber 0.5.18, pipewire-pulse.
- **Changes**: Added `DESIGN.md`. No code changes.
- **Coverage**: Design only (0% implemented). Basis: DESIGN.md sections 1–8.
- **Commands / facts gathered**: `pactl -f json list …` works (removes regex parsing); Chrome playback stream has `application.process.binary=chrome`; Chrome volume is 105%; PCI card exposes HDMI and headphones on different profiles; WirePlumber `node.stream.restore-target` and `linking.follow-default-target` are on; no `target` entries in `~/.local/state/wireplumber/restore-stream` yet; Fyne 2.7.2 tray supports submenus, checkmarks, `SetSystemTrayWindow`.
- **Decisions**: one process with tray (right-click switcher menu, left-click mixer) and a unix-socket `toggle`; event-driven `pactl subscribe` + JSON snapshots; all writes via `pactl`; delete pw-cli/pw-metadata path and owner guessing; app identity from the stream's own properties; saved routes applied once per stream; keyed widgets updated in place.
- **Found while gathering**: Waybar launches `~/.local/bin/audio-indicator -title 'Audio Mixer'` but the Sway float rule matches `"Audio Indicator"`; each Waybar click starts a new instance.
- **Negative memory**: Guessing the recording owner from client heuristics produced wrong labels; do not reintroduce it.
- **Next actions**: run validations V1–V5 in DESIGN.md §7 (Chrome capture properties and move, WirePlumber not undoing moves, Waybar rendering Fyne submenus, PCI port switch), then implement per DESIGN.md §5.
- **Blockers**: V1–V3 need a live Chrome call.

Signature: omp/claude-opus-5-5

## 2026-10-14 — Rewrite implemented per DESIGN.md

- **Objective**: Implement the tray switcher with per-app routes (DESIGN.md) using GPT-6.1 Sol subagents.
- **Workspace**: /home/heefoo/audio-indicator; new jj change on top of the described WIP change.
- **Changes**: New `internal/pulse` (pactl JSON snapshot, subscribe with restart/backoff, writes; fixtures from this machine), `internal/model` (view/switch entries, app identity, config with atomic save, route tracker), main package (`coordinator.go`, `tray.go`, `mixer.go`, `prefs.go`, `instance.go`, `main.go`, embedded PNG icons). Removed old `audio.go`, old `main.go`, `preferences.go`, their tests, `launch.sh`, `audio-indicator.service`, `TODO.md`, tracked `log`. README/AGENTS.md rewritten; DESIGN.md D2 and port-visibility rule updated. Parent fixes after review: deleting a running app's route moves its streams to the default; a failed saved-route move is planned again on the next snapshot (`Tracker.Forget`, regression test fails without it); fixtures scrubbed of host/user/machine id/PIDs/cookie; flock serializes stale-socket cleanup (regression test fails with a shared lock, passes with exclusive).
- **Coverage**: ~90%. Basis: all DESIGN.md §1–§6 behavior implemented; §7 validations V1–V3 (Chrome capture) and V5 (headphone port switch) not exercised; tray submenu rendering in Waybar not visually checked.
- **Commands**: `go vet ./...`; `go test -race -count=1 ./...` (all pass); live `audio-indicator snapshot`; end-to-end smoke with a temp `XDG_CONFIG_HOME` route `pacat → loopback sink`.
- **Results**: route applied to a new paplay stream; manual `pactl move-sink-input` back to Jabra was not undone; next paplay stream re-routed; mixer screenshot shows live devices, Chrome at 105%, saved route row; socket `toggle`/`quit` work; tray item registered with StatusNotifierWatcher.
- **Negative memory**: paplay's stream identity is binary `pacat`, not `paplay`. Fyne's Linux tray rejects SVG icons ("unknown format"). The eval kernel lacks `DISPLAY`; GUI smoke needs `DISPLAY=:0`.
- **Not done / user steps**: new binary not installed; `~/.local/bin/audio-indicator` is still the old build, and Waybar still calls it with `-title 'Audio Mixer'`, which the new CLI rejects. Update Waybar on-click and Sway lines per README before `go build -o ~/.local/bin/audio-indicator .`.
- **Next actions**: install + config changes; run V1–V5 (Chrome call, headphones) and check tray submenus visually.

Signature: omp/claude-opus-5-5

## 2026-10-14 — Installed as soundquay

- Renamed binary/module/socket/config dir to `soundquay` (matches framequay/letterquay); repo moved to `~/code/soundquay`.
- Installed `~/.local/bin/soundquay`; removed old `~/.local/bin/audio-indicator` and repo binary. Config now `~/.config/soundquay/config.json`, socket `$XDG_RUNTIME_DIR/soundquay.sock`.
- Sway: `$mod+Shift+a` → `soundquay toggle`; `for_window [title="Audio Mixer"] floating enable, border none, move position mouse`; `exec soundquay` autostart. Waybar `custom/audio` on-click → `soundquay toggle`. Backups: `config.bak-audio` next to each file.
- Verified: running, tray item registered, toggle opens a floating "Audio Mixer".

Signature: omp/claude-opus-5-5

## 2026-10-14 — Mixer placement and hide button

- Problem: the mixer opened under the mouse on Waybar (covering it), was recentered by Fyne on every show (splash window), and had no close button (splash window + Sway `border none`).
- Fix: regular Fyne window; header with title and hide button; `sway.go` moves the window inside its workspace's usable area (excludes Waybar) after each show, 8 px margin. Tests: `positionInside`, `findFloatingWindow`.
- Verified: cursor set on HDMI-A-1 bar → window at y 1838, bottom 2358 = usable bottom − 8; screenshot shows header with ✕. Installed to `~/.local/bin/soundquay` and restarted.

Signature: omp/claude-opus-5-5

## 2026-10-14 — Per-app input controls

- Each app row now has an Out line and an In line, each with its own volume slider, mute and device picker. The In picker is always shown for apps with an identity, so a recording device can be saved before the app records ("not recording; the device applies when it starts"). App volume/mute now change only the chosen direction (previously one slider changed playback and recording together). Test: `TestAppMuteAndVolumeOnlyChangeTheChosenDirection`.
- Verified with a live `parecord`: mixer shows parecord with an In slider at 100%, mute and In picker; Chrome shows Out controls and an In picker.

Signature: omp/claude-opus-5-5

## 2026-10-14 — Reset apps and "really on" hint

- Gap found: when the default device changes, apps with a saved device or a WirePlumber-remembered target stay where they are, and the mixer showed "Follow default" even when the stream was elsewhere.
- Added: per-line warning "Now playing on X / Now recording from X" when a stream's device differs from its saved device (or the default when following); header button **Reset apps** with confirmation: clears all saved routes and moves every app stream not on the default to the default. Chose a manual reset over automatic alignment on every default change, which would discard saved per-app choices. Test: `TestResetAppsForgetsRoutesAndMovesStrayStreamsToDefaults` (both directions, nicknames kept, a failed move stays on the error line). Successful writes no longer clear the error line; the next user change does.
- Verified live: paplay moved to loopback by pactl → warning shown in orange; header shows Reset apps. Reset itself not clicked live (would drop the user's Chrome route).

Signature: omp/claude-opus-5-5

## 2026-10-14 — Mixer look and draggable header

- Header is now a dark bar (icon, title, Reset apps with icon, ✕) with an accent line under it; pressing it sends `_NET_WM_MOVERESIZE` (via `github.com/jezek/xgb`) so Sway runs its own window move (`drag.go`). Body uses rounded cards with small section titles (Devices, Apps, saved devices for apps not running — hidden when empty); mute buttons are speaker icons (crossed and orange when muted); new darker palette, rounded inputs.
- Verified: build/tests pass; screenshot approved by user. Dragging not verifiable here (no way to inject a Wayland pointer press); if Sway ignores the move request, fallback is `border normal` in the Sway rule.

Signature: omp/claude-opus-5-5
