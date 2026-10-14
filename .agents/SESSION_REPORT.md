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
