# Refactor TODO

- [x] Introduce a `Model` type in `main.go` to hold all mutable app state: `Status`, `Expanded`, `NeedsRebuild`, `LastVolumeChange`, and `LastMicVolumeChange`.
- [x] Remove duplicated mutable state fields from `AudioApp` and replace them with a single `model Model`.
- [x] Define a `Msg` interface and concrete message types for every state transition and async completion path.
- [x] Replace the `Action` enum, `ActionMsg`, and `interface{}` payload pattern with typed messages.
- [x] Change `Dispatch` to accept only `Msg` values.
- [x] Make the coordinator goroutine the sole owner of `model` mutations.
- [x] Remove all direct writes to app state from widget callbacks in `main.go`.
- [x] Convert section toggle buttons to dispatch-only messages such as `ToggleExpand`.
- [x] Convert output-device selection to a request message such as `SetSinkRequested`.
- [x] Convert input-device selection to a request message such as `SetSourceRequested`.
- [x] Convert per-stream sink moves to a request message such as `MoveSinkInputRequested`.
- [x] Convert per-mic-stream source moves to a request message such as `MoveSourceOutputRequested`.
- [x] Convert main volume slider changes to a message such as `MainVolumeChanged`.
- [x] Convert mic volume slider changes to a message such as `MicVolumeChanged`.
- [x] Convert mute button interactions to messages instead of direct audio calls.
- [x] Keep per-application controls implemented through the shared message/model flow rather than special-case UI state paths.
- [x] Add completion/result messages for async refreshes, such as `RefreshCompleted`.
- [x] Add completion/result messages for async device and stream move operations where follow-up refresh is needed.
- [x] Introduce a small `Cmd` abstraction for side effects that run outside the coordinator.
- [x] Move `getStatus()` refresh work behind a dispatched command rather than calling it inline from mixed contexts.
- [x] Change the polling/timer goroutine to dispatch `RefreshRequested` only.
- [x] Change manual refresh triggers to dispatch `RefreshRequested` only.
- [x] Ensure every background goroutine reports back by dispatching a typed completion or error message instead of mutating app state.
- [x] Refactor `handleAction` into a typed message handler or reducer that updates `model` and returns commands.
- [x] Keep optimistic UI updates inside the coordinator only, never inside callbacks.
- [x] Make render methods read from `a.model` rather than scattered `AudioApp` fields.
- [x] Refactor `refreshUI()` into a render-from-model function that performs no business-state mutation outside explicitly UI-local widget references.
- [x] Ensure `fyne.Do` or `fyne.DoAndWait` is used only for UI rendering and widget updates, not for owning business state.
- [x] Review slider grace-period logic and keep those timestamps in `Model` only.
- [x] Decide whether persistent widget references stay in `AudioApp` as UI runtime state or are rebuilt fully each render, and apply that consistently.
- [x] Remove any remaining direct mutation paths that bypass dispatch.
- [x] Remove any dead code left over from the old `Action` and `handleAction` system.
- [x] Re-run `gofmt` on all changed Go files.
- [x] Re-run `go build ./...` and fix compilation issues from the type migration.
- [x] Run `go test ./...` after refactor, even if there are still no test files, to verify module integrity.
- [x] After the refactor is stable, complete per-application playback features so each app can independently control output device, volume, and mute through the shared message/model flow.
- [x] After the refactor is stable, complete per-application recording features so each app can independently control microphone source, volume, and mute through the shared message/model flow.
- [x] Add minimal unit tests for reducer or message-handling logic once the state machine is explicit.
- [x] Add minimal parsing and backend tests for `audio.go` to reduce regression risk after the architectural change.
- [x] Do a final pass to confirm the rule: only the coordinator mutates the model, UI renders it, and async work dispatches back into it.

# WirePlumber Integration TODO

- [x] Define the goal precisely: per-app recording source should be assignable even when `pactl source-outputs` is not sufficient.
- [x] Keep the current `pactl` path as a fallback so the app still works on non-WirePlumber setups.
- [x] Detect the active backend at startup: `pactl` only, PipeWire plus WirePlumber, or unsupported.
- [x] Add a backend capability struct in `audio.go` so the UI can know whether advanced per-app routing is available.
- [x] Add a small diagnostic command layer for `wpctl`, `pw-metadata`, and optionally `pw-cli`.
- [x] Implement a probe function that confirms WirePlumber is present and usable.
- [x] Implement a probe function that confirms PipeWire metadata can be read.
- [x] Implement a probe function that confirms PipeWire metadata can be written.

- [x] Inspect live playback and recording nodes with `wpctl status`.
- [x] Inspect live Chrome/WebRTC recording nodes with `wpctl inspect <id>`.
- [x] Capture the actual properties exposed by Chrome recording streams on your system.
- [x] Identify which stream properties are stable enough for matching across sessions.
- [x] Identify which source-node properties are stable enough to target reliably.
- [x] Document those matching properties in code comments or a short design note.

- [x] Introduce a backend interface in `audio.go` for stream discovery and routing.
- [x] Keep the current PulseAudio-compatible `pactl` implementation behind that interface.
- [x] Add a PipeWire/WirePlumber implementation behind the same interface.
- [x] Make `main.go` talk only to the backend interface, not to `pactl`-specific functions.

- [x] Implement listing of active recording streams through the PipeWire/WirePlumber path.
- [x] Map active recording streams back to user-facing app names.
- [x] Map available input sources to stable PipeWire node identities.
- [x] Implement moving a live recording stream to a selected source using the WirePlumber-aware path.
- [ ] Verify that the move succeeds for an active Chrome recording stream.
- [x] Fall back to the current `pactl move-source-output` path if WirePlumber routing is unavailable.

- [x] Decide whether to rely only on WirePlumber restore behavior or also store app preferences in this app.
- [ ] If relying on WirePlumber restore, validate that moved capture streams are restored on the next matching session.
- [x] If WirePlumber restore is not reliable enough, add app-side persistence for preferred source per app.
- [x] Define a local config format for saved app routing preferences.
- [x] Save per-app preferred recording source when the user changes it.
- [x] When a matching recording stream appears, automatically reapply the saved source.
- [x] Make sure auto-reapply goes through the same message/model flow as the rest of the app.

- [x] Add a UI concept for preferred source for this app separate from currently active stream source.
- [x] Show saved preferred source for apps when known.
- [x] If no live recording stream exists, allow setting a future preferred source for supported apps.
- [x] When the app later creates a live recording stream, apply that saved preference automatically.
- [x] If true prebinding before stream creation is not technically possible, label the UI honestly as apply on next active recording stream.

- [x] Add typed messages for backend capability detection.
- [x] Add typed messages for PipeWire/WirePlumber stream discovery results.
- [x] Add typed messages for live stream move requests and completions.
- [x] Add typed messages for preference save/apply requests and completions.
- [x] Keep all backend completions flowing back through the coordinator only.
- [x] Do not let backend code mutate UI model state directly.

- [x] Add a small backend-status indicator so users know whether advanced routing is available.
- [x] In `Applications Recording`, distinguish between live stream routing and saved preferred source.
- [x] Show a clear message when no live recording streams exist.
- [x] Show whether a displayed source change is immediate or deferred until the next stream.
- [x] Keep the per-app controls generic, not browser-specific or vendor-specific.

- [x] Add unit tests for backend capability detection.
- [x] Add unit tests for matching app identities from PipeWire properties.
- [x] Add unit tests for persisted preferred-source selection logic.
- [x] Add reducer tests for the new WirePlumber-related messages.
- [x] Add integration-style tests around parsing PipeWire CLI output if CLI parsing is used.
- [ ] Test with Chrome recording microphone input.
- [x] Test with a second recording app to confirm the matching is not Chrome-specific.
- [x] Test failure cases: no WirePlumber, no metadata access, no live stream, target source missing.

- [x] Ship the WirePlumber path behind automatic capability detection first.
- [x] Keep the current `pactl` behavior as fallback until the new path is stable.
- [x] Add a short README section explaining the difference between live per-stream routing and saved future preferences.
- [ ] Only remove old assumptions after real-world validation on your machine.
