package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image/color"
	"log"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type Model struct {
	BackendCapabilities       AudioBackendCapabilities
	AdvancedSources           []AudioSource
	AdvancedRecordingStreams  []AdvancedRecordingStream
	RecordingClientCandidates []RecordingClientCandidate
	RecordingStreamInferences []AdvancedRecordingInference
	PreferredRecordingSources map[string]string
	Status                    *AudioStatus
	Expanded                  map[string]bool
	NeedsRebuild              bool
	LastVolumeChange          time.Time
	LastMicVolumeChange       time.Time
	RefreshInFlight           bool
}

type Msg interface {
	isMsg()
}

type Cmd func(dispatch func(Msg))

type RefreshRequested struct{}

func (RefreshRequested) isMsg() {}

type RefreshCompleted struct {
	Status *AudioStatus
	Err    error
}

func (RefreshCompleted) isMsg() {}

type BackendCapabilitiesRefreshed struct {
	Capabilities AudioBackendCapabilities
}

func (BackendCapabilitiesRefreshed) isMsg() {}

type AdvancedDiscoveryRefreshed struct {
	Sources    []AudioSource
	Streams    []AdvancedRecordingStream
	Candidates []RecordingClientCandidate
	Inferences []AdvancedRecordingInference
}

func (AdvancedDiscoveryRefreshed) isMsg() {}

type ToggleExpand struct {
	Key string
}

func (ToggleExpand) isMsg() {}

type SetSinkRequested struct {
	SinkName string
}

func (SetSinkRequested) isMsg() {}

type SetSourceRequested struct {
	SourceName string
}

func (SetSourceRequested) isMsg() {}

type MoveSinkInputRequested struct {
	StreamID string
	SinkName string
}

func (MoveSinkInputRequested) isMsg() {}

type MoveSourceOutputRequested struct {
	StreamID   string
	SourceName string
}

func (MoveSourceOutputRequested) isMsg() {}

type MoveAdvancedRecordingRequested struct {
	StreamNodeID string
	Application  string
	SourceName   string
}

func (MoveAdvancedRecordingRequested) isMsg() {}

type SetPreferredRecordingSourceRequested struct {
	Application string
	SourceName  string
}

func (SetPreferredRecordingSourceRequested) isMsg() {}

type MainVolumeChanged struct {
	Volume int
	At     time.Time
}

func (MainVolumeChanged) isMsg() {}

type ApplyMainVolume struct {
	Volume int
}

func (ApplyMainVolume) isMsg() {}

type MainMuteToggled struct {
	Muted bool
}

func (MainMuteToggled) isMsg() {}

type MicVolumeChanged struct {
	Volume int
	At     time.Time
}

func (MicVolumeChanged) isMsg() {}

type ApplyMicVolume struct {
	Volume int
}

func (ApplyMicVolume) isMsg() {}

type MicMuteToggled struct {
	Muted bool
}

func (MicMuteToggled) isMsg() {}

type StreamVolumeChanged struct {
	StreamID string
	Volume   int
}

func (StreamVolumeChanged) isMsg() {}

type ApplyStreamVolume struct {
	StreamID string
	Volume   int
}

func (ApplyStreamVolume) isMsg() {}

type StreamMuteToggled struct {
	StreamID string
	Muted    bool
}

func (StreamMuteToggled) isMsg() {}

type SourceOutputVolumeChanged struct {
	StreamID string
	Volume   int
}

func (SourceOutputVolumeChanged) isMsg() {}

type ApplySourceOutputVolume struct {
	StreamID string
	Volume   int
}

func (ApplySourceOutputVolume) isMsg() {}

type SourceOutputMuteToggled struct {
	StreamID string
	Muted    bool
}

func (SourceOutputMuteToggled) isMsg() {}

type AdvancedRecordingMoveCompleted struct {
	StreamNodeID string
	Application  string
	SourceName   string
	Err          error
}

func (AdvancedRecordingMoveCompleted) isMsg() {}

type PreferredRecordingSourcesSaved struct {
	Err error
}

func (PreferredRecordingSourcesSaved) isMsg() {}

type PreferredRecordingSourceApplied struct {
	StreamNodeID string
	Application  string
	SourceName   string
	Err          error
}

func (PreferredRecordingSourceApplied) isMsg() {}

type CommandResult struct {
	Operation      string
	RequestRefresh bool
	Err            error
}

func (CommandResult) isMsg() {}

type AudioApp struct {
	window     fyne.Window
	root       *fyne.Container
	scroll     *container.Scroll
	actionChan chan Msg
	model      Model

	// Persistent widget references - don't recreate on refresh.
	volumeSlider    *widget.Slider
	volumeLabel     *widget.Label
	micVolumeSlider *widget.Slider
	micVolumeLabel  *widget.Label

	// UI-local guard to prevent OnChanged from firing during programmatic updates.
	updatingSliders bool

	// Debounce timers are shell-level runtime state, not model state.
	mainVolumeTimer               *time.Timer
	micVolumeTimer                *time.Timer
	streamVolumeTimers            map[string]*time.Timer
	sourceOutputVolumeTimers      map[string]*time.Timer
	preferredRecordingSourceApply map[string]time.Time

	// Immutable window title from CLI.
	title string
}

func main() {
	title := flag.String("title", "Audio Indicator", "Window title")
	diagnostics := flag.Bool("diagnostics", false, "Print backend diagnostics and exit")
	moveAdvancedStreamID := flag.String("move-advanced-stream-id", "", "Move a live advanced recording stream and exit")
	moveAdvancedStreamSource := flag.String("move-advanced-stream-source", "", "Target source name for -move-advanced-stream-id")
	setPreferredRecordingApp := flag.String("set-preferred-recording-app", "", "Application label for a preferred recording source")
	setPreferredRecordingSource := flag.String("set-preferred-recording-source", "", "Source name for -set-preferred-recording-app")
	applyPreferredRecordingSources := flag.Bool("apply-preferred-recording-sources", false, "Apply saved preferred recording sources to matching live streams and exit")
	watchRecordingState := flag.Bool("watch-recording-state", false, "Continuously print recording stream state and inferred owners")
	flag.Parse()

	if *diagnostics {
		if err := printDiagnostics(); err != nil {
			log.Fatalf("diagnostics failed: %v", err)
		}
		return
	}

	if *moveAdvancedStreamID != "" || *moveAdvancedStreamSource != "" {
		if err := moveAdvancedStreamCLI(*moveAdvancedStreamID, *moveAdvancedStreamSource); err != nil {
			log.Fatalf("advanced stream move failed: %v", err)
		}
		return
	}

	if *setPreferredRecordingApp != "" || *setPreferredRecordingSource != "" {
		if err := setPreferredRecordingSourceCLI(*setPreferredRecordingApp, *setPreferredRecordingSource); err != nil {
			log.Fatalf("set preferred recording source failed: %v", err)
		}
		return
	}

	if *applyPreferredRecordingSources {
		if err := applyPreferredRecordingSourcesCLI(); err != nil {
			log.Fatalf("apply preferred recording sources failed: %v", err)
		}
		return
	}

	if *watchRecordingState {
		if err := watchRecordingStateCLI(); err != nil {
			log.Fatalf("watch recording state failed: %v", err)
		}
		return
	}

	fyneApp := app.New()
	fyneApp.Settings().SetTheme(&DarkTheme{})

	initialStatus := getStatus()
	initialAdvancedSources := GetAdvancedSources()
	initialAdvancedStreams := GetAdvancedRecordingStreams()
	initialRecordingCandidates := GetRecordingClientCandidates()
	preferences, err := loadAppPreferences()
	if err != nil {
		log.Printf("[PREFERENCES] Failed to load preferences: %v", err)
	}
	appInstance := &AudioApp{
		actionChan: make(chan Msg, 100),
		model: Model{
			BackendCapabilities:       GetAudioBackendCapabilities(),
			AdvancedSources:           initialAdvancedSources,
			AdvancedRecordingStreams:  initialAdvancedStreams,
			RecordingClientCandidates: initialRecordingCandidates,
			RecordingStreamInferences: InferAdvancedRecordingOwners(initialAdvancedStreams, initialRecordingCandidates),
			PreferredRecordingSources: preferences.PreferredRecordingSources,
			Status:                    initialStatus,
			Expanded:                  make(map[string]bool),
			NeedsRebuild:              false,
		},
		streamVolumeTimers:            make(map[string]*time.Timer),
		sourceOutputVolumeTimers:      make(map[string]*time.Timer),
		preferredRecordingSourceApply: make(map[string]time.Time),
		title:                         *title,
	}

	appInstance.window = fyneApp.NewWindow(*title)
	appInstance.window.Resize(fyne.NewSize(380, 600))

	appInstance.root = appInstance.makeAudioPopup()
	rightSpacer := container.NewGridWrap(fyne.NewSize(16, 0))
	contentWithMargin := container.NewBorder(nil, nil, nil, rightSpacer, appInstance.root)
	appInstance.scroll = container.NewVScroll(contentWithMargin)
	appInstance.scroll.SetMinSize(fyne.NewSize(0, 400))

	header := appInstance.makeHeader()
	paddedScroll := container.NewPadded(appInstance.scroll)
	appInstance.window.SetContent(container.NewBorder(header, nil, nil, nil, paddedScroll))

	log.Printf("[STARTUP] Window size: %v", appInstance.window.Canvas().Size())
	log.Printf("[STARTUP] Root container size: %v", appInstance.root.Size())
	log.Printf("[STARTUP] Root min size: %v", appInstance.root.MinSize())

	go appInstance.runPolling()
	go appInstance.runCoordinator()

	appInstance.window.ShowAndRun()
}

type DiagnosticsSnapshot struct {
	BackendCapabilities       AudioBackendCapabilities     `json:"backend_capabilities"`
	AdvancedSources           []AudioSource                `json:"advanced_sources"`
	AdvancedStreams           []AdvancedRecordingStream    `json:"advanced_streams"`
	RecordingClientCandidates []RecordingClientCandidate   `json:"recording_client_candidates"`
	RecordingStreamInferences []AdvancedRecordingInference `json:"recording_stream_inferences"`
	PreferredRecordingSources map[string]string            `json:"preferred_recording_sources"`
	AudioBackendChecks        []DiagnosticCommandResult    `json:"audio_backend_checks"`
	Status                    *AudioStatus                 `json:"status"`
}

func printDiagnostics() error {
	preferences, err := loadAppPreferences()
	if err != nil {
		log.Printf("[PREFERENCES] Failed to load preferences for diagnostics: %v", err)
	}
	advancedStreams := GetAdvancedRecordingStreams()
	recordingCandidates := GetRecordingClientCandidates()

	snapshot := DiagnosticsSnapshot{
		BackendCapabilities:       GetAudioBackendCapabilities(),
		AdvancedSources:           GetAdvancedSources(),
		AdvancedStreams:           advancedStreams,
		RecordingClientCandidates: recordingCandidates,
		RecordingStreamInferences: InferAdvancedRecordingOwners(advancedStreams, recordingCandidates),
		PreferredRecordingSources: preferences.PreferredRecordingSources,
		AudioBackendChecks:        GetAudioBackendDiagnostics(),
		Status:                    getStatus(),
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	return nil
}

func moveAdvancedStreamCLI(streamNodeID, sourceName string) error {
	if strings.TrimSpace(streamNodeID) == "" {
		return fmt.Errorf("-move-advanced-stream-id is required")
	}
	if strings.TrimSpace(sourceName) == "" {
		return fmt.Errorf("-move-advanced-stream-source is required")
	}

	if err := moveAdvancedRecordingStream(streamNodeID, sourceName); err != nil {
		return err
	}

	time.Sleep(1 * time.Second)
	advancedStreams := GetAdvancedRecordingStreams()
	recordingCandidates := GetRecordingClientCandidates()

	snapshot := DiagnosticsSnapshot{
		BackendCapabilities:       GetAudioBackendCapabilities(),
		AdvancedSources:           GetAdvancedSources(),
		AdvancedStreams:           advancedStreams,
		RecordingClientCandidates: recordingCandidates,
		RecordingStreamInferences: InferAdvancedRecordingOwners(advancedStreams, recordingCandidates),
		AudioBackendChecks:        GetAudioBackendDiagnostics(),
		PreferredRecordingSources: map[string]string{},
		Status:                    getStatus(),
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	return nil
}

func setPreferredRecordingSourceCLI(application, sourceName string) error {
	if strings.TrimSpace(application) == "" {
		return fmt.Errorf("-set-preferred-recording-app is required")
	}
	if strings.TrimSpace(sourceName) == "" {
		return fmt.Errorf("-set-preferred-recording-source is required")
	}

	preferences, err := loadAppPreferences()
	if err != nil {
		return err
	}
	if preferences.PreferredRecordingSources == nil {
		preferences.PreferredRecordingSources = make(map[string]string)
	}
	preferences.PreferredRecordingSources[application] = sourceName
	if err := saveAppPreferences(preferences); err != nil {
		return err
	}

	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	return nil
}

func applyPreferredRecordingSourcesCLI() error {
	preferences, err := loadAppPreferences()
	if err != nil {
		return err
	}

	appInstance := &AudioApp{
		model: Model{
			AdvancedSources:           GetAdvancedSources(),
			AdvancedRecordingStreams:  GetAdvancedRecordingStreams(),
			PreferredRecordingSources: preferences.PreferredRecordingSources,
		},
		preferredRecordingSourceApply: make(map[string]time.Time),
	}

	requests := appInstance.preferredRecordingMovesToApply(time.Now())
	for _, request := range requests {
		if err := moveAdvancedRecordingStream(request.StreamNodeID, request.SourceName); err != nil {
			return err
		}
	}

	time.Sleep(1 * time.Second)

	return printDiagnostics()
}

func watchRecordingStateCLI() error {
	log.Println("[WATCH] Recording-state watch started. Press Ctrl+C to stop.")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	lastSnapshot := ""
	for {
		preferences, err := loadAppPreferences()
		if err != nil {
			return err
		}
		advancedStreams := GetAdvancedRecordingStreams()
		recordingCandidates := GetRecordingClientCandidates()
		payload := struct {
			AdvancedStreams           []AdvancedRecordingStream    `json:"advanced_streams"`
			RecordingStreamInferences []AdvancedRecordingInference `json:"recording_stream_inferences"`
			PreferredRecordingSources map[string]string            `json:"preferred_recording_sources"`
		}{
			AdvancedStreams:           advancedStreams,
			RecordingStreamInferences: InferAdvancedRecordingOwners(advancedStreams, recordingCandidates),
			PreferredRecordingSources: preferences.PreferredRecordingSources,
		}

		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}

		current := string(data)
		if current != lastSnapshot {
			fmt.Println(current)
			lastSnapshot = current
		}

		<-ticker.C
	}
}

func (a *AudioApp) Dispatch(msg Msg) {
	log.Printf("[DISPATCH] → %T", msg)
	select {
	case a.actionChan <- msg:
		log.Printf("[DISPATCH] ✓ %T queued", msg)
	default:
		log.Printf("[DISPATCH] ✗ %T DROPPED (channel full)", msg)
	}
}

func (a *AudioApp) runPolling() {
	log.Println("[POLLER] Started")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		a.Dispatch(RefreshRequested{})
	}
}

func (a *AudioApp) runCoordinator() {
	log.Println("[COORDINATOR] Started")
	for msg := range a.actionChan {
		log.Printf("[COORDINATOR] ← Received %T", msg)
		start := time.Now()
		cmds := a.handleMessage(msg)
		a.refreshUI()
		for _, cmd := range cmds {
			if cmd == nil {
				continue
			}
			go cmd(a.Dispatch)
		}
		log.Printf("[COORDINATOR] ✓ Handled %T in %v", msg, time.Since(start))
	}
}

func (a *AudioApp) handleMessage(msg Msg) []Cmd {
	switch m := msg.(type) {
	case RefreshRequested:
		if a.model.RefreshInFlight {
			log.Println("[REFRESH] Request coalesced while fetch is in flight")
			return nil
		}

		a.model.RefreshInFlight = true
		return []Cmd{refreshStatusCmd()}

	case RefreshCompleted:
		if m.Err != nil {
			a.model.RefreshInFlight = false
			log.Printf("[REFRESH] Fetch failed: %v", m.Err)
			return nil
		}

		oldStatus := a.model.Status
		a.model.Status = m.Status
		a.model.NeedsRebuild = true

		if oldStatus != nil {
			log.Printf("[STATUS] BEFORE: Vol=%d%% Muted=%v MicVol=%d%% MicMuted=%v",
				oldStatus.Volume, oldStatus.Muted, oldStatus.MicVolume, oldStatus.MicMuted)
		}
		if m.Status != nil {
			log.Printf("[STATUS] AFTER:  Vol=%d%% Muted=%v MicVol=%d%% MicMuted=%v",
				m.Status.Volume, m.Status.Muted, m.Status.MicVolume, m.Status.MicMuted)
		}

	case BackendCapabilitiesRefreshed:
		a.model.BackendCapabilities = m.Capabilities
		a.model.NeedsRebuild = true

	case AdvancedDiscoveryRefreshed:
		a.model.AdvancedSources = m.Sources
		a.model.AdvancedRecordingStreams = m.Streams
		a.model.RecordingClientCandidates = m.Candidates
		a.model.RecordingStreamInferences = m.Inferences
		a.model.RefreshInFlight = false
		a.model.NeedsRebuild = true
		return a.buildPreferredRecordingSourceApplyCmds(time.Now())

	case ToggleExpand:
		a.model.Expanded[m.Key] = !a.model.Expanded[m.Key]
		a.model.NeedsRebuild = true

	case SetSinkRequested:
		streamIDs := a.optimisticallySetDefaultSink(m.SinkName)
		a.model.NeedsRebuild = true
		return []Cmd{runOperationCmd("set-sink", true, func() error {
			if err := setSink(m.SinkName); err != nil {
				return err
			}
			for _, streamID := range streamIDs {
				if err := moveSinkInput(streamID, m.SinkName); err != nil {
					return err
				}
			}
			return nil
		})}

	case SetSourceRequested:
		streamIDs := a.optimisticallySetDefaultSource(m.SourceName)
		a.model.NeedsRebuild = true
		return []Cmd{runOperationCmd("set-source", true, func() error {
			if err := setSource(m.SourceName); err != nil {
				return err
			}
			for _, streamID := range streamIDs {
				if err := moveSourceOutput(streamID, m.SourceName); err != nil {
					return err
				}
			}
			return nil
		})}

	case MoveSinkInputRequested:
		a.optimisticallyMoveSinkInput(m.StreamID, m.SinkName)
		a.model.NeedsRebuild = true
		return []Cmd{runOperationCmd("move-sink-input", true, func() error {
			return moveSinkInput(m.StreamID, m.SinkName)
		})}

	case MoveSourceOutputRequested:
		a.optimisticallyMoveSourceOutput(m.StreamID, m.SourceName)
		a.model.NeedsRebuild = true
		return []Cmd{runOperationCmd("move-source-output", true, func() error {
			return moveSourceOutput(m.StreamID, m.SourceName)
		})}

	case MoveAdvancedRecordingRequested:
		a.optimisticallyMoveAdvancedRecording(m.StreamNodeID, m.SourceName)
		a.setPreferredRecordingSource(m.Application, m.SourceName)
		a.model.NeedsRebuild = true
		return []Cmd{
			savePreferredRecordingSourcesCmd(a.model.PreferredRecordingSources),
			moveAdvancedRecordingCmd(m.StreamNodeID, m.Application, m.SourceName),
		}

	case SetPreferredRecordingSourceRequested:
		a.setPreferredRecordingSource(m.Application, m.SourceName)
		a.model.NeedsRebuild = true
		return []Cmd{savePreferredRecordingSourcesCmd(a.model.PreferredRecordingSources)}

	case MainVolumeChanged:
		if a.model.Status != nil {
			a.model.Status.Volume = m.Volume
		}
		a.model.LastVolumeChange = m.At
		a.scheduleMainVolume(m.Volume)

	case ApplyMainVolume:
		return []Cmd{runOperationCmd("set-main-volume", false, func() error {
			return setVolume(m.Volume)
		})}

	case MainMuteToggled:
		if a.model.Status != nil {
			a.model.Status.Muted = m.Muted
		}
		return []Cmd{runOperationCmd("set-main-mute", false, func() error {
			return setMute(m.Muted)
		})}

	case MicVolumeChanged:
		if a.model.Status != nil {
			a.model.Status.MicVolume = m.Volume
		}
		a.model.LastMicVolumeChange = m.At
		a.scheduleMicVolume(m.Volume)

	case ApplyMicVolume:
		return []Cmd{runOperationCmd("set-mic-volume", false, func() error {
			return setMicVolume(m.Volume)
		})}

	case MicMuteToggled:
		if a.model.Status != nil {
			a.model.Status.MicMuted = m.Muted
		}
		return []Cmd{runOperationCmd("set-mic-mute", false, func() error {
			return setMicMute(m.Muted)
		})}

	case StreamVolumeChanged:
		if stream := a.findStream(m.StreamID); stream != nil {
			stream.Volume = m.Volume
		}
		a.scheduleStreamVolume(m.StreamID, m.Volume)

	case ApplyStreamVolume:
		return []Cmd{runOperationCmd("set-stream-volume", false, func() error {
			return setStreamVolume(m.StreamID, m.Volume)
		})}

	case StreamMuteToggled:
		if stream := a.findStream(m.StreamID); stream != nil {
			stream.Muted = m.Muted
		}
		return []Cmd{runOperationCmd("set-stream-mute", false, func() error {
			return setStreamMute(m.StreamID, m.Muted)
		})}

	case SourceOutputVolumeChanged:
		if stream := a.findMicStream(m.StreamID); stream != nil {
			stream.Volume = m.Volume
		}
		a.scheduleSourceOutputVolume(m.StreamID, m.Volume)

	case ApplySourceOutputVolume:
		return []Cmd{runOperationCmd("set-source-output-volume", false, func() error {
			return setSourceOutputVolume(m.StreamID, m.Volume)
		})}

	case SourceOutputMuteToggled:
		if stream := a.findMicStream(m.StreamID); stream != nil {
			stream.Muted = m.Muted
		}
		return []Cmd{runOperationCmd("set-source-output-mute", false, func() error {
			return setSourceOutputMute(m.StreamID, m.Muted)
		})}

	case AdvancedRecordingMoveCompleted:
		if m.Err != nil {
			log.Printf("[PIPEWIRE] Failed to move recording stream %s (%s) to %s: %v", m.StreamNodeID, m.Application, m.SourceName, m.Err)
			return nil
		}
		return []Cmd{dispatchMsgCmd(RefreshRequested{})}

	case PreferredRecordingSourcesSaved:
		if m.Err != nil {
			log.Printf("[PREFERENCES] Failed to save preferred recording sources: %v", m.Err)
		}

	case PreferredRecordingSourceApplied:
		if m.Err != nil {
			log.Printf("[PREFERENCES] Failed to auto-apply preferred source %s for %s: %v", m.SourceName, m.Application, m.Err)
			return nil
		}
		return []Cmd{dispatchMsgCmd(RefreshRequested{})}

	case CommandResult:
		if m.Err != nil {
			log.Printf("[COMMAND] %s failed: %v", m.Operation, m.Err)
		}
		if m.RequestRefresh {
			return []Cmd{dispatchMsgCmd(RefreshRequested{})}
		}
	}

	return nil
}

func (a *AudioApp) refreshUI() {
	status := a.model.Status
	if status == nil {
		return
	}

	start := time.Now()
	log.Println("[REFRESH] Starting...")

	gracePeriod := 3 * time.Second
	lastVolumeChange := a.model.LastVolumeChange
	lastMicVolumeChange := a.model.LastMicVolumeChange
	needsRebuild := a.model.NeedsRebuild || a.volumeSlider == nil
	a.model.NeedsRebuild = false

	fyne.DoAndWait(func() {
		a.updatingSliders = true
		defer func() { a.updatingSliders = false }()

		if a.volumeSlider != nil {
			currentSliderVal := int(a.volumeSlider.Value)
			timeSinceChange := time.Since(lastVolumeChange)
			if timeSinceChange < gracePeriod {
				log.Printf("[REFRESH] Volume slider: %d%% (skipped - user changed %v ago)", currentSliderVal, timeSinceChange.Round(time.Millisecond))
			} else if currentSliderVal != status.Volume {
				log.Printf("[REFRESH] Volume slider: %d%% -> %d%% (updating)", currentSliderVal, status.Volume)
				a.volumeSlider.SetValue(float64(status.Volume))
				a.volumeLabel.SetText(fmt.Sprintf("%d%%", status.Volume))
			} else {
				log.Printf("[REFRESH] Volume slider: %d%% (unchanged)", currentSliderVal)
			}
		}

		if a.micVolumeSlider != nil {
			currentSliderVal := int(a.micVolumeSlider.Value)
			timeSinceChange := time.Since(lastMicVolumeChange)
			if timeSinceChange < gracePeriod {
				log.Printf("[REFRESH] Mic slider: %d%% (skipped - user changed %v ago)", currentSliderVal, timeSinceChange.Round(time.Millisecond))
			} else if currentSliderVal != status.MicVolume {
				log.Printf("[REFRESH] Mic slider: %d%% -> %d%% (updating)", currentSliderVal, status.MicVolume)
				a.micVolumeSlider.SetValue(float64(status.MicVolume))
				a.micVolumeLabel.SetText(fmt.Sprintf("%d%%", status.MicVolume))
			} else {
				log.Printf("[REFRESH] Mic slider: %d%% (unchanged)", currentSliderVal)
			}
		}

		if needsRebuild {
			scrollOffset := a.scroll.Offset
			buildStart := time.Now()

			a.volumeSlider = nil
			a.volumeLabel = nil
			a.micVolumeSlider = nil
			a.micVolumeLabel = nil

			newRoot := a.makeAudioPopup()
			log.Printf("[REFRESH] makeAudioPopup took %v", time.Since(buildStart))

			a.root.Objects = newRoot.Objects
			a.root.Refresh()
			a.scroll.Offset = scrollOffset
			a.scroll.Refresh()
		}
	})

	log.Printf("[REFRESH] ✓ Complete in %v", time.Since(start))
}

func (a *AudioApp) makeAudioPopup() *fyne.Container {
	items := []fyne.CanvasObject{
		a.makeMainVolume(),
		a.makeOutputDevices(),
		widget.NewSeparator(),
		a.makeMicrophoneSection(),
		a.makeInputDevices(),
		widget.NewSeparator(),
		a.makeBackendStatus(),
	}

	items = append(items,
		widget.NewSeparator(),
		a.makeApplicationsSection(),
	)

	return container.NewVBox(items...)
}

func (a *AudioApp) makeHeader() *fyne.Container {
	closeBtn := widget.NewButton("󰅖", func() {
		a.window.Close()
	})
	closeBtn.Importance = widget.LowImportance

	titleText := widget.NewRichTextFromMarkdown("# 󰕾 " + a.title)

	return container.NewBorder(nil, nil,
		titleText,
		closeBtn,
		nil,
	)
}

func (a *AudioApp) makeMainVolume() *fyne.Container {
	status := a.currentStatus()
	label := widget.NewLabel("Volume")

	volume := status.Volume
	log.Printf("[BUILD] Creating volume slider with value=%d%%", volume)

	a.volumeSlider = widget.NewSlider(0, 100)
	a.volumeSlider.SetValue(float64(volume))
	a.volumeSlider.Step = 5

	a.volumeLabel = widget.NewLabel(fmt.Sprintf("%d%%", volume))
	muteBtnState := status.Muted

	muteBtn := widget.NewButton(getMuteIcon(muteBtnState), nil)
	muteBtn.Importance = widget.LowImportance
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		muteBtn.SetText(getMuteIcon(muteBtnState))
		muteBtn.Refresh()
		a.Dispatch(MainMuteToggled{Muted: muteBtnState})
	}

	a.volumeSlider.OnChanged = func(value float64) {
		if a.updatingSliders {
			return
		}
		vol := int(value)
		a.volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		a.Dispatch(MainVolumeChanged{Volume: vol, At: time.Now()})
	}

	sliderRow := container.NewBorder(nil, nil, nil,
		container.NewHBox(a.volumeLabel, muteBtn),
		a.volumeSlider,
	)

	return container.NewVBox(label, sliderRow)
}

func (a *AudioApp) makeOutputDevices() *fyne.Container {
	status := a.currentStatus()
	expanded := a.model.Expanded["output_devices"]
	toggleIcon := "▼"
	if expanded {
		toggleIcon = "▲"
	}

	headerBtn := widget.NewButton(fmt.Sprintf("%s Output Device (%d)", toggleIcon, len(status.Sinks)), func() {
		a.Dispatch(ToggleExpand{Key: "output_devices"})
	})
	headerBtn.Alignment = widget.ButtonAlignLeading

	if !expanded {
		defaultName := "None"
		for _, sink := range status.Sinks {
			if sink.Default {
				defaultName = sink.Description
				break
			}
		}
		return container.NewVBox(
			headerBtn,
			widget.NewLabel("  "+defaultName),
		)
	}

	itemsContainer := container.NewVBox()
	for _, sink := range status.Sinks {
		sink := sink
		sinkBtn := widget.NewButton(
			fmt.Sprintf("%s %s", getSinkIcon(sink.Default), sink.Description),
			func() {
				a.Dispatch(SetSinkRequested{SinkName: sink.Name})
			},
		)
		sinkBtn.Alignment = widget.ButtonAlignLeading
		if sink.Default {
			sinkBtn.Importance = widget.HighImportance
		} else {
			sinkBtn.Importance = widget.LowImportance
		}
		itemsContainer.Add(sinkBtn)
	}

	return container.NewVBox(headerBtn, itemsContainer)
}

func (a *AudioApp) makeMicrophoneSection() *fyne.Container {
	status := a.currentStatus()
	label := widget.NewLabel("Microphone")

	volume := status.MicVolume
	log.Printf("[BUILD] Creating mic slider with value=%d%%", volume)

	a.micVolumeSlider = widget.NewSlider(0, 100)
	a.micVolumeSlider.SetValue(float64(volume))
	a.micVolumeSlider.Step = 5

	a.micVolumeLabel = widget.NewLabel(fmt.Sprintf("%d%%", volume))
	muteBtnState := status.MicMuted

	muteBtn := widget.NewButton(getMicMuteIcon(muteBtnState), nil)
	muteBtn.Importance = widget.LowImportance
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		muteBtn.SetText(getMicMuteIcon(muteBtnState))
		muteBtn.Refresh()
		a.Dispatch(MicMuteToggled{Muted: muteBtnState})
	}

	a.micVolumeSlider.OnChanged = func(value float64) {
		if a.updatingSliders {
			return
		}
		vol := int(value)
		a.micVolumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		a.Dispatch(MicVolumeChanged{Volume: vol, At: time.Now()})
	}

	sliderRow := container.NewBorder(nil, nil, nil,
		container.NewHBox(a.micVolumeLabel, muteBtn),
		a.micVolumeSlider,
	)

	return container.NewVBox(label, sliderRow)
}

func (a *AudioApp) makeInputDevices() *fyne.Container {
	status := a.currentStatus()
	expanded := a.model.Expanded["input_devices"]
	toggleIcon := "▼"
	if expanded {
		toggleIcon = "▲"
	}

	var realSources []AudioSource
	for _, source := range status.Sources {
		if !strings.HasSuffix(source.Name, ".monitor") {
			realSources = append(realSources, source)
		}
	}

	headerBtn := widget.NewButton(fmt.Sprintf("%s Input Device (%d)", toggleIcon, len(realSources)), func() {
		a.Dispatch(ToggleExpand{Key: "input_devices"})
	})
	headerBtn.Alignment = widget.ButtonAlignLeading

	if !expanded {
		defaultName := "None"
		for _, source := range realSources {
			if source.Default {
				defaultName = source.Description
				break
			}
		}
		return container.NewVBox(
			headerBtn,
			widget.NewLabel("  "+defaultName),
		)
	}

	itemsContainer := container.NewVBox()
	for _, source := range realSources {
		source := source
		sourceBtn := widget.NewButton(
			fmt.Sprintf("%s %s", getSourceIcon(source.Default), source.Description),
			func() {
				a.Dispatch(SetSourceRequested{SourceName: source.Name})
			},
		)
		sourceBtn.Alignment = widget.ButtonAlignLeading
		if source.Default {
			sourceBtn.Importance = widget.HighImportance
		} else {
			sourceBtn.Importance = widget.LowImportance
		}
		itemsContainer.Add(sourceBtn)
	}

	return container.NewVBox(headerBtn, itemsContainer)
}

type applicationGroup struct {
	Name            string
	PlaybackStreams []AudioStream
	MicStreams      []MicStream
	AdvancedStreams []AdvancedRecordingStream
	PreferredSource string
}

func (a *AudioApp) makeApplicationsSection() *fyne.Container {
	groups := a.buildApplicationGroups()
	expanded := a.model.Expanded["applications"]
	toggleIcon := "▼"
	if expanded {
		toggleIcon = "▲"
	}

	headerBtn := widget.NewButton(fmt.Sprintf("%s Applications (%d)", toggleIcon, len(groups)), func() {
		a.Dispatch(ToggleExpand{Key: "applications"})
	})
	headerBtn.Alignment = widget.ButtonAlignLeading

	if !expanded {
		return container.NewVBox(headerBtn)
	}

	if len(groups) == 0 {
		return container.NewVBox(
			headerBtn,
			widget.NewLabel("  No active application streams. Start playback or recording to see per-app controls."),
		)
	}

	items := container.NewVBox()
	for _, group := range groups {
		items.Add(a.makeApplicationGroupItem(group))
	}

	return container.NewVBox(headerBtn, items)
}

func (a *AudioApp) makeApplicationGroupItem(group applicationGroup) *fyne.Container {
	appKey := "app_" + group.Name
	summary := []string{}
	if len(group.PlaybackStreams) > 0 {
		summary = append(summary, "sound")
	}
	if len(group.MicStreams) > 0 || len(group.AdvancedStreams) > 0 || group.PreferredSource != "" {
		summary = append(summary, "mic")
	}

	headerBtn := widget.NewButton(
		fmt.Sprintf("%s %s", getSinkExpandIcon(a.model.Expanded[appKey]), group.Name),
		func() {
			a.Dispatch(ToggleExpand{Key: appKey})
		},
	)
	headerBtn.Alignment = widget.ButtonAlignLeading

	if !a.model.Expanded[appKey] {
		if len(summary) == 0 {
			return container.NewVBox(headerBtn)
		}
		return container.NewVBox(headerBtn, widget.NewLabel("  "+strings.Join(summary, " / ")))
	}

	items := container.NewVBox(headerBtn)
	if len(group.PlaybackStreams) > 0 {
		items.Add(widget.NewLabel("  󰕾 Sound"))
		for i, stream := range group.PlaybackStreams {
			title := streamDisplayName(stream, group.Name)
			if len(group.PlaybackStreams) == 1 {
				title = "Controls"
			} else if title == group.Name {
				title = fmt.Sprintf("Output %d", i+1)
			}
			items.Add(a.makeStreamItemWithTitle(stream, title))
		}
	}
	if len(group.MicStreams) > 0 || len(group.AdvancedStreams) > 0 || group.PreferredSource != "" {
		items.Add(widget.NewLabel("  󰍬 Mic"))
		for i, stream := range group.MicStreams {
			title := streamDisplayName(AudioStream{Name: stream.Name, Binary: stream.Binary}, group.Name)
			if len(group.MicStreams) == 1 {
				title = "Controls"
			} else if title == group.Name {
				title = fmt.Sprintf("Input %d", i+1)
			}
			items.Add(a.makeMicStreamItemWithTitle(stream, title))
		}
		for _, stream := range group.AdvancedStreams {
			title := a.advancedRecordingLabel(stream, group.Name)
			if a.findMicStream(stream.StreamNodeID) != nil {
				title = "Source Route"
			}
			items.Add(a.makeAdvancedRecordingItemWithTitle(stream, title, a.model.AdvancedSources))
		}
		if group.PreferredSource != "" && len(group.AdvancedStreams) == 0 && len(group.MicStreams) == 0 {
			items.Add(a.makePreferredRecordingPreferenceItem(group.Name, group.PreferredSource, a.model.AdvancedSources))
		}
	}

	return items
}

func (a *AudioApp) buildApplicationGroups() []applicationGroup {
	status := a.currentStatus()
	groups := make(map[string]*applicationGroup)
	advancedByID := make(map[string]AdvancedRecordingStream, len(a.model.AdvancedRecordingStreams))
	for _, stream := range a.model.AdvancedRecordingStreams {
		advancedByID[stream.StreamNodeID] = stream
	}

	for _, stream := range status.Streams {
		name := streamDisplayName(stream, "")
		group := ensureApplicationGroup(groups, name)
		group.PlaybackStreams = append(group.PlaybackStreams, stream)
	}

	for _, stream := range status.MicStreams {
		name := a.micStreamDisplayName(stream, advancedByID[stream.ID])
		group := ensureApplicationGroup(groups, name)
		group.MicStreams = append(group.MicStreams, stream)
		if advanced, ok := advancedByID[stream.ID]; ok {
			group.AdvancedStreams = append(group.AdvancedStreams, advanced)
		}
	}

	for _, stream := range a.model.AdvancedRecordingStreams {
		if a.findMicStream(stream.StreamNodeID) != nil {
			continue
		}
		name := a.advancedRecordingLabel(stream, "")
		group := ensureApplicationGroup(groups, name)
		group.AdvancedStreams = append(group.AdvancedStreams, stream)
	}

	for application, sourceName := range a.model.PreferredRecordingSources {
		group := ensureApplicationGroup(groups, application)
		group.PreferredSource = sourceName
	}

	ordered := make([]applicationGroup, 0, len(groups))
	for _, group := range groups {
		ordered = append(ordered, *group)
	}
	slices.SortFunc(ordered, func(a, b applicationGroup) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	return ordered
}

func ensureApplicationGroup(groups map[string]*applicationGroup, name string) *applicationGroup {
	name = normalizeApplicationGroupName(name)
	if group, ok := groups[name]; ok {
		return group
	}
	group := &applicationGroup{Name: name}
	groups[name] = group
	return group
}

func normalizeApplicationGroupName(name string) string {
	name = firstNonEmpty(strings.TrimSpace(name), "Unknown Application")
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, " input") {
		name = strings.TrimSpace(name[:len(name)-len(" input")])
	}
	return firstNonEmpty(strings.TrimSpace(name), "Unknown Application")
}

func streamDisplayName(stream AudioStream, fallback string) string {
	return firstNonEmpty(strings.TrimSpace(stream.Name), strings.TrimSpace(stream.Binary), strings.TrimSpace(fallback), "Unknown Application")
}

func (a *AudioApp) micStreamDisplayName(stream MicStream, advanced AdvancedRecordingStream) string {
	if inferred := a.inferredRecordingApplication(advanced.StreamNodeID); inferred != "" {
		return inferred
	}
	return firstNonEmpty(strings.TrimSpace(stream.Name), strings.TrimSpace(stream.Binary), a.advancedRecordingLabel(advanced, ""), "Unknown Application")
}

func (a *AudioApp) advancedRecordingLabel(stream AdvancedRecordingStream, fallback string) string {
	if inferred := a.inferredRecordingApplication(stream.StreamNodeID); inferred != "" {
		return inferred
	}
	return firstNonEmpty(strings.TrimSpace(stream.Application), strings.TrimSpace(stream.StreamName), strings.TrimSpace(fallback), "Unknown Application")
}

func (a *AudioApp) inferredRecordingApplication(streamNodeID string) string {
	for _, inference := range a.model.RecordingStreamInferences {
		if inference.StreamNodeID != streamNodeID || len(inference.Candidates) == 0 {
			continue
		}
		return firstNonEmpty(inference.Candidates[0].Application, inference.Candidates[0].ProcessBinary)
	}
	return ""
}

func (a *AudioApp) makeApplicationsPlaying() *fyne.Container {
	status := a.currentStatus()
	expanded := a.model.Expanded["apps_playing"]
	toggleIcon := "▼"
	if expanded {
		toggleIcon = "▲"
	}

	headerBtn := widget.NewButton(fmt.Sprintf("%s Applications Playing (%d)", toggleIcon, len(status.Streams)), func() {
		a.Dispatch(ToggleExpand{Key: "apps_playing"})
	})
	headerBtn.Alignment = widget.ButtonAlignLeading

	if len(status.Streams) == 0 || !expanded {
		return container.NewVBox(headerBtn)
	}

	streamsContainer := container.NewVBox()
	for _, stream := range status.Streams {
		streamsContainer.Add(a.makeStreamItem(stream))
	}

	return container.NewVBox(headerBtn, streamsContainer)
}

func (a *AudioApp) makeStreamItem(stream AudioStream) *fyne.Container {
	return a.makeStreamItemWithTitle(stream, streamDisplayName(stream, ""))
}

func (a *AudioApp) makeStreamItemWithTitle(stream AudioStream, title string) *fyne.Container {
	nameLabel := widget.NewLabel(title)

	volume := stream.Volume
	slider := widget.NewSlider(0, 100)
	slider.SetValue(float64(volume))
	slider.Step = 5

	volumeLabel := widget.NewLabel(fmt.Sprintf("%d%%", volume))
	muteBtnState := stream.Muted

	muteBtn := widget.NewButton(getMuteIcon(muteBtnState), nil)
	muteBtn.Importance = widget.LowImportance
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		muteBtn.SetText(getMuteIcon(muteBtnState))
		muteBtn.Refresh()
		a.Dispatch(StreamMuteToggled{StreamID: stream.ID, Muted: muteBtnState})
	}

	sinkBtn := widget.NewButton(getSinkExpandIcon(a.model.Expanded[stream.ID]), nil)
	sinkBtn.Importance = widget.LowImportance
	sinkBtn.OnTapped = func() {
		a.Dispatch(ToggleExpand{Key: stream.ID})
	}

	slider.OnChanged = func(value float64) {
		vol := int(value)
		volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		a.Dispatch(StreamVolumeChanged{StreamID: stream.ID, Volume: vol})
	}

	sliderRow := container.NewBorder(nil, nil,
		nameLabel,
		container.NewHBox(volumeLabel, muteBtn, sinkBtn),
		slider,
	)

	streamContainer := container.NewVBox(sliderRow)

	if a.model.Expanded[stream.ID] {
		sinkList := container.NewVBox()
		for _, sink := range a.currentStatus().Sinks {
			sink := sink
			isSelected := sink.ID == stream.SinkName
			sinkItem := widget.NewButton(
				fmt.Sprintf("%s %s", getSinkIcon(isSelected), sink.Description),
				func() {
					a.Dispatch(MoveSinkInputRequested{
						StreamID: stream.ID,
						SinkName: sink.Name,
					})
				},
			)
			sinkItem.Alignment = widget.ButtonAlignLeading
			if isSelected {
				sinkItem.Importance = widget.HighImportance
			} else {
				sinkItem.Importance = widget.LowImportance
			}
			sinkList.Add(sinkItem)
		}
		streamContainer.Add(sinkList)
	}

	return streamContainer
}

func (a *AudioApp) makeApplicationsRecording() *fyne.Container {
	status := a.currentStatus()
	advancedSources := a.model.AdvancedSources
	advancedStreams := a.model.AdvancedRecordingStreams
	preferredSources := a.model.PreferredRecordingSources
	expanded := a.model.Expanded["apps_recording"]
	toggleIcon := "▼"
	if expanded {
		toggleIcon = "▲"
	}

	recordingCount := len(status.MicStreams)
	if recordingCount == 0 {
		recordingCount = len(advancedStreams)
	}

	headerBtn := widget.NewButton(fmt.Sprintf("%s Applications Recording (%d)", toggleIcon, recordingCount), func() {
		a.Dispatch(ToggleExpand{Key: "apps_recording"})
	})
	headerBtn.Alignment = widget.ButtonAlignLeading

	if !expanded {
		return container.NewVBox(headerBtn)
	}

	items := container.NewVBox()
	if len(status.MicStreams) == 0 && len(advancedStreams) == 0 && len(preferredSources) == 0 {
		items.Add(widget.NewLabel("  No active recording streams. Set the default input device before an app starts recording."))
	}

	if len(status.MicStreams) == 0 && len(advancedStreams) > 0 {
		for _, stream := range advancedStreams {
			items.Add(a.makeAdvancedRecordingItem(stream, advancedSources))
		}
		items.Add(widget.NewLabel("  Live PipeWire capture streams detected. Source changes apply immediately to the active stream."))
	}

	for _, stream := range status.MicStreams {
		items.Add(a.makeMicStreamItem(stream))
	}

	if len(preferredSources) > 0 && len(advancedSources) > 0 {
		if len(items.Objects) > 0 {
			items.Add(widget.NewSeparator())
		}
		items.Add(widget.NewLabel("  Saved preferred sources"))
		items.Add(widget.NewLabel("  These apply to the next matching active recording stream."))
		items.Add(a.makePreferredRecordingPreferences(advancedSources))
	}

	return container.NewVBox(headerBtn, items)
}

func (a *AudioApp) makeAdvancedRecordingItem(stream AdvancedRecordingStream, sources []AudioSource) *fyne.Container {
	return a.makeAdvancedRecordingItemWithTitle(stream, recordingApplicationLabel(stream), sources)
}

func (a *AudioApp) makeAdvancedRecordingItemWithTitle(stream AdvancedRecordingStream, title string, sources []AudioSource) *fyne.Container {
	streamKey := "pwrec_" + stream.StreamNodeID
	application := firstNonEmpty(title, recordingApplicationLabel(stream))
	preferredSource := a.model.PreferredRecordingSources[application]
	sourceBtn := widget.NewButton(getSinkExpandIcon(a.model.Expanded[streamKey]), nil)
	sourceBtn.Importance = widget.LowImportance
	sourceBtn.OnTapped = func() {
		a.Dispatch(ToggleExpand{Key: streamKey})
	}

	sourceLabel := "unknown source"
	if stream.CurrentSource != "" {
		sourceLabel = stream.CurrentSource
	}

	headerRow := container.NewBorder(nil, nil,
		widget.NewLabel("󰍬 "+application),
		sourceBtn,
		nil,
	)

	streamContainer := container.NewVBox(
		headerRow,
		widget.NewLabel("  Current source: "+sourceLabel),
	)

	if preferredSource != "" {
		streamContainer.Add(widget.NewLabel("  Preferred source: " + a.resolveAdvancedSourceLabel(preferredSource)))
	}

	if a.model.Expanded[streamKey] {
		sourceList := container.NewVBox()
		for _, source := range sources {
			source := source
			isSelected := source.Name == stream.CurrentSource || source.ID == stream.CurrentSourceID
			sourceItem := widget.NewButton(
				fmt.Sprintf("%s %s", getSourceIcon(isSelected), source.Description),
				func() {
					a.Dispatch(MoveAdvancedRecordingRequested{
						StreamNodeID: stream.StreamNodeID,
						Application:  application,
						SourceName:   source.Name,
					})
				},
			)
			sourceItem.Alignment = widget.ButtonAlignLeading
			if isSelected {
				sourceItem.Importance = widget.HighImportance
			} else {
				sourceItem.Importance = widget.LowImportance
			}
			sourceList.Add(sourceItem)
		}
		streamContainer.Add(sourceList)
	}

	return streamContainer
}

func (a *AudioApp) makePreferredRecordingPreferences(sources []AudioSource) *fyne.Container {
	items := container.NewVBox()
	for _, application := range sortedRecordingPreferenceApplications(a.model.PreferredRecordingSources) {
		items.Add(a.makePreferredRecordingPreferenceItem(application, a.model.PreferredRecordingSources[application], sources))
	}
	return items
}

func (a *AudioApp) makePreferredRecordingPreferenceItem(application string, preferredSourceName string, sources []AudioSource) *fyne.Container {
	preferenceKey := "prefrec_" + application
	sourceBtn := widget.NewButton(getSinkExpandIcon(a.model.Expanded[preferenceKey]), nil)
	sourceBtn.Importance = widget.LowImportance
	sourceBtn.OnTapped = func() {
		a.Dispatch(ToggleExpand{Key: preferenceKey})
	}

	headerRow := container.NewBorder(nil, nil,
		widget.NewLabel("󰍬 "+application),
		sourceBtn,
		nil,
	)

	streamContainer := container.NewVBox(
		headerRow,
		widget.NewLabel("  Preferred source: "+a.resolveAdvancedSourceLabel(preferredSourceName)),
	)

	if a.model.Expanded[preferenceKey] {
		sourceList := container.NewVBox()
		for _, source := range sources {
			source := source
			isSelected := source.Name == preferredSourceName
			sourceItem := widget.NewButton(
				fmt.Sprintf("%s %s", getSourceIcon(isSelected), source.Description),
				func() {
					a.Dispatch(SetPreferredRecordingSourceRequested{
						Application: application,
						SourceName:  source.Name,
					})
				},
			)
			sourceItem.Alignment = widget.ButtonAlignLeading
			if isSelected {
				sourceItem.Importance = widget.HighImportance
			} else {
				sourceItem.Importance = widget.LowImportance
			}
			sourceList.Add(sourceItem)
		}
		streamContainer.Add(sourceList)
	}

	return streamContainer
}

func (a *AudioApp) makeBackendStatus() *fyne.Container {
	capabilities := a.model.BackendCapabilities
	advancedSources := a.model.AdvancedSources
	advancedStreams := a.model.AdvancedRecordingStreams

	statusText := "Backend: pactl fallback"
	detailText := "  PulseAudio-compatible routing is active."

	if capabilities.AdvancedRouting {
		statusText = fmt.Sprintf("Backend: %s", backendDisplayName(capabilities.SelectedBackend))
		detailText = fmt.Sprintf("  Advanced discovery sees %d source nodes and %d live capture streams.", len(advancedSources), len(advancedStreams))
	} else if capabilities.PipeWireRunning || capabilities.WirePlumberRunning {
		statusText = "Backend: PipeWire detected, using pactl compatibility"
		detailText = "  Advanced metadata-based routing is not fully enabled yet."
	}

	return container.NewVBox(
		widget.NewLabel(statusText),
		widget.NewLabel(detailText),
	)
}

func (a *AudioApp) makeMicStreamItem(stream MicStream) *fyne.Container {
	return a.makeMicStreamItemWithTitle(stream, streamDisplayName(AudioStream{Name: stream.Name, Binary: stream.Binary}, ""))
}

func (a *AudioApp) makeMicStreamItemWithTitle(stream MicStream, title string) *fyne.Container {
	nameLabel := widget.NewLabel("󰍬 " + title)

	volume := stream.Volume
	slider := widget.NewSlider(0, 100)
	slider.SetValue(float64(volume))
	slider.Step = 5

	volumeLabel := widget.NewLabel(fmt.Sprintf("%d%%", volume))
	muteBtnState := stream.Muted

	muteBtn := widget.NewButton(getMicMuteIcon(muteBtnState), nil)
	muteBtn.Importance = widget.LowImportance
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		muteBtn.SetText(getMicMuteIcon(muteBtnState))
		muteBtn.Refresh()
		a.Dispatch(SourceOutputMuteToggled{StreamID: stream.ID, Muted: muteBtnState})
	}

	streamKey := "mic_" + stream.ID
	sourceBtn := widget.NewButton(getSinkExpandIcon(a.model.Expanded[streamKey]), nil)
	sourceBtn.Importance = widget.LowImportance
	sourceBtn.OnTapped = func() {
		a.Dispatch(ToggleExpand{Key: streamKey})
	}

	slider.OnChanged = func(value float64) {
		vol := int(value)
		volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		a.Dispatch(SourceOutputVolumeChanged{StreamID: stream.ID, Volume: vol})
	}

	sliderRow := container.NewBorder(nil, nil,
		nameLabel,
		container.NewHBox(volumeLabel, muteBtn, sourceBtn),
		slider,
	)

	streamContainer := container.NewVBox(sliderRow)

	if a.model.Expanded[streamKey] {
		sourceList := container.NewVBox()
		for _, source := range a.currentStatus().Sources {
			source := source
			if strings.HasSuffix(source.Name, ".monitor") {
				continue
			}
			isSelected := source.ID == stream.SourceName
			sourceItem := widget.NewButton(
				fmt.Sprintf("%s %s", getSourceIcon(isSelected), source.Description),
				func() {
					a.Dispatch(MoveSourceOutputRequested{
						StreamID:   stream.ID,
						SourceName: source.Name,
					})
				},
			)
			sourceItem.Alignment = widget.ButtonAlignLeading
			if isSelected {
				sourceItem.Importance = widget.HighImportance
			} else {
				sourceItem.Importance = widget.LowImportance
			}
			sourceList.Add(sourceItem)
		}
		streamContainer.Add(sourceList)
	}

	return streamContainer
}

func (a *AudioApp) currentStatus() *AudioStatus {
	if a.model.Status != nil {
		return a.model.Status
	}

	return &AudioStatus{
		Streams:    []AudioStream{},
		MicStreams: []MicStream{},
		Sinks:      []AudioSink{},
		Sources:    []AudioSource{},
	}
}

func (a *AudioApp) findStream(streamID string) *AudioStream {
	status := a.currentStatus()
	for i := range status.Streams {
		if status.Streams[i].ID == streamID {
			return &status.Streams[i]
		}
	}
	return nil
}

func (a *AudioApp) findMicStream(streamID string) *MicStream {
	status := a.currentStatus()
	for i := range status.MicStreams {
		if status.MicStreams[i].ID == streamID {
			return &status.MicStreams[i]
		}
	}
	return nil
}

func (a *AudioApp) optimisticallySetDefaultSink(sinkName string) []string {
	status := a.currentStatus()

	var sinkID string
	for i := range status.Sinks {
		if status.Sinks[i].Name == sinkName {
			sinkID = status.Sinks[i].ID
			status.Sinks[i].Default = true
		} else {
			status.Sinks[i].Default = false
		}
	}

	streams := make([]string, len(status.Streams))
	for i := range status.Streams {
		status.Streams[i].SinkName = sinkID
		streams[i] = status.Streams[i].ID
	}

	return streams
}

func (a *AudioApp) optimisticallySetDefaultSource(sourceName string) []string {
	status := a.currentStatus()

	var sourceID string
	for i := range status.Sources {
		if status.Sources[i].Name == sourceName {
			sourceID = status.Sources[i].ID
			status.Sources[i].Default = true
		} else {
			status.Sources[i].Default = false
		}
	}

	streams := make([]string, len(status.MicStreams))
	for i := range status.MicStreams {
		status.MicStreams[i].SourceName = sourceID
		streams[i] = status.MicStreams[i].ID
	}

	return streams
}

func (a *AudioApp) optimisticallyMoveSinkInput(streamID, sinkName string) {
	status := a.currentStatus()

	var sinkID string
	for _, sink := range status.Sinks {
		if sink.Name == sinkName {
			sinkID = sink.ID
			break
		}
	}

	for i := range status.Streams {
		if status.Streams[i].ID == streamID {
			status.Streams[i].SinkName = sinkID
			break
		}
	}
}

func (a *AudioApp) optimisticallyMoveSourceOutput(streamID, sourceName string) {
	status := a.currentStatus()

	var sourceID string
	for _, source := range status.Sources {
		if source.Name == sourceName {
			sourceID = source.ID
			break
		}
	}

	for i := range status.MicStreams {
		if status.MicStreams[i].ID == streamID {
			status.MicStreams[i].SourceName = sourceID
			break
		}
	}
}

func (a *AudioApp) optimisticallyMoveAdvancedRecording(streamNodeID, sourceName string) {
	for i := range a.model.AdvancedRecordingStreams {
		if a.model.AdvancedRecordingStreams[i].StreamNodeID != streamNodeID {
			continue
		}

		a.model.AdvancedRecordingStreams[i].CurrentSource = sourceName
		for _, source := range a.model.AdvancedSources {
			if source.Name == sourceName {
				a.model.AdvancedRecordingStreams[i].CurrentSourceID = source.ID
				a.model.AdvancedRecordingStreams[i].CurrentSource = source.Description
				break
			}
		}
		return
	}
}

func (a *AudioApp) setPreferredRecordingSource(application, sourceName string) {
	application = strings.TrimSpace(application)
	if application == "" {
		return
	}
	if a.model.PreferredRecordingSources == nil {
		a.model.PreferredRecordingSources = make(map[string]string)
	}
	a.model.PreferredRecordingSources[application] = sourceName
}

func (a *AudioApp) resolveAdvancedSourceLabel(sourceName string) string {
	for _, source := range a.model.AdvancedSources {
		if source.Name == sourceName {
			return source.Description
		}
	}
	if sourceName == "" {
		return "not set"
	}
	return sourceName
}

func (a *AudioApp) buildPreferredRecordingSourceApplyCmds(now time.Time) []Cmd {
	requests := a.preferredRecordingMovesToApply(now)
	if len(requests) == 0 {
		return nil
	}

	cmds := make([]Cmd, 0, len(requests))
	for _, request := range requests {
		request := request
		cmds = append(cmds, applyPreferredRecordingSourceCmd(request.StreamNodeID, request.Application, request.SourceName))
	}
	return cmds
}

func (a *AudioApp) preferredRecordingMovesToApply(now time.Time) []MoveAdvancedRecordingRequested {
	const applyCooldown = 8 * time.Second

	requests := make([]MoveAdvancedRecordingRequested, 0)
	for _, stream := range a.model.AdvancedRecordingStreams {
		application := recordingApplicationLabel(stream)
		preferredSourceName := a.model.PreferredRecordingSources[application]
		if preferredSourceName == "" {
			continue
		}
		if !a.advancedSourceExists(preferredSourceName) {
			continue
		}
		if a.advancedRecordingUsesSource(stream, preferredSourceName) {
			delete(a.preferredRecordingSourceApply, stream.StreamNodeID)
			continue
		}
		lastAttempt := a.preferredRecordingSourceApply[stream.StreamNodeID]
		if !lastAttempt.IsZero() && now.Sub(lastAttempt) < applyCooldown {
			continue
		}
		a.preferredRecordingSourceApply[stream.StreamNodeID] = now
		requests = append(requests, MoveAdvancedRecordingRequested{
			StreamNodeID: stream.StreamNodeID,
			Application:  application,
			SourceName:   preferredSourceName,
		})
	}

	return requests
}

func (a *AudioApp) advancedRecordingUsesSource(stream AdvancedRecordingStream, sourceName string) bool {
	for _, source := range a.model.AdvancedSources {
		if source.Name != sourceName {
			continue
		}
		return stream.CurrentSourceID == source.ID || stream.CurrentSource == source.Description
	}
	return stream.CurrentSource == sourceName
}

func (a *AudioApp) advancedSourceExists(sourceName string) bool {
	for _, source := range a.model.AdvancedSources {
		if source.Name == sourceName {
			return true
		}
	}
	return false
}

func refreshStatusCmd() Cmd {
	return func(dispatch func(Msg)) {
		status := getStatus()
		advancedSources := GetAdvancedSources()
		advancedStreams := GetAdvancedRecordingStreams()
		recordingCandidates := GetRecordingClientCandidates()
		dispatch(RefreshCompleted{Status: status})
		dispatch(BackendCapabilitiesRefreshed{Capabilities: GetAudioBackendCapabilities()})
		dispatch(AdvancedDiscoveryRefreshed{
			Sources:    advancedSources,
			Streams:    advancedStreams,
			Candidates: recordingCandidates,
			Inferences: InferAdvancedRecordingOwners(advancedStreams, recordingCandidates),
		})
	}
}

func dispatchMsgCmd(msg Msg) Cmd {
	return func(dispatch func(Msg)) {
		dispatch(msg)
	}
}

func runOperationCmd(operation string, requestRefresh bool, fn func() error) Cmd {
	return func(dispatch func(Msg)) {
		dispatch(CommandResult{
			Operation:      operation,
			RequestRefresh: requestRefresh,
			Err:            fn(),
		})
	}
}

func (a *AudioApp) scheduleMainVolume(volume int) {
	if a.mainVolumeTimer != nil {
		a.mainVolumeTimer.Stop()
	}
	a.mainVolumeTimer = time.AfterFunc(debounceDelay, func() {
		a.Dispatch(ApplyMainVolume{Volume: volume})
	})
}

func (a *AudioApp) scheduleMicVolume(volume int) {
	if a.micVolumeTimer != nil {
		a.micVolumeTimer.Stop()
	}
	a.micVolumeTimer = time.AfterFunc(debounceDelay, func() {
		a.Dispatch(ApplyMicVolume{Volume: volume})
	})
}

func (a *AudioApp) scheduleStreamVolume(streamID string, volume int) {
	if timer, ok := a.streamVolumeTimers[streamID]; ok {
		timer.Stop()
	}
	a.streamVolumeTimers[streamID] = time.AfterFunc(debounceDelay, func() {
		a.Dispatch(ApplyStreamVolume{StreamID: streamID, Volume: volume})
	})
}

func (a *AudioApp) scheduleSourceOutputVolume(streamID string, volume int) {
	if timer, ok := a.sourceOutputVolumeTimers[streamID]; ok {
		timer.Stop()
	}
	a.sourceOutputVolumeTimers[streamID] = time.AfterFunc(debounceDelay, func() {
		a.Dispatch(ApplySourceOutputVolume{StreamID: streamID, Volume: volume})
	})
}

func getMuteIcon(muted bool) string {
	if muted {
		return "󰝟"
	}
	return "󰕾"
}

func getMicMuteIcon(muted bool) string {
	if muted {
		return "󰍭"
	}
	return "󰍬"
}

func getSinkIcon(isDefault bool) string {
	if isDefault {
		return "󰐵"
	}
	return "󰐶"
}

func getSourceIcon(isDefault bool) string {
	if isDefault {
		return "󰐵"
	}
	return "󰐶"
}

func getSinkExpandIcon(expanded bool) string {
	if expanded {
		return "▲"
	}
	return "▼"
}

func backendDisplayName(name string) string {
	switch name {
	case "pipewire":
		return "PipeWire"
	case "pactl":
		return "pactl"
	default:
		return firstNonEmpty(name, "unknown")
	}
}

func recordingApplicationLabel(stream AdvancedRecordingStream) string {
	return firstNonEmpty(stream.Application, stream.StreamName)
}

func sortedRecordingPreferenceApplications(preferences map[string]string) []string {
	applications := make([]string, 0, len(preferences))
	for application := range preferences {
		applications = append(applications, application)
	}
	slices.Sort(applications)
	return applications
}

func copyPreferredRecordingSources(preferences map[string]string) map[string]string {
	copied := make(map[string]string, len(preferences))
	for application, source := range preferences {
		copied[application] = source
	}
	return copied
}

func savePreferredRecordingSourcesCmd(preferences map[string]string) Cmd {
	copied := copyPreferredRecordingSources(preferences)
	return func(dispatch func(Msg)) {
		dispatch(PreferredRecordingSourcesSaved{
			Err: saveAppPreferences(AppPreferences{PreferredRecordingSources: copied}),
		})
	}
}

func moveAdvancedRecordingCmd(streamNodeID, application, sourceName string) Cmd {
	return func(dispatch func(Msg)) {
		dispatch(AdvancedRecordingMoveCompleted{
			StreamNodeID: streamNodeID,
			Application:  application,
			SourceName:   sourceName,
			Err:          moveAdvancedRecordingStream(streamNodeID, sourceName),
		})
	}
}

func applyPreferredRecordingSourceCmd(streamNodeID, application, sourceName string) Cmd {
	return func(dispatch func(Msg)) {
		dispatch(PreferredRecordingSourceApplied{
			StreamNodeID: streamNodeID,
			Application:  application,
			SourceName:   sourceName,
			Err:          moveAdvancedRecordingStream(streamNodeID, sourceName),
		})
	}
}

type DarkTheme struct{}

func (t *DarkTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.RGBA{58, 58, 58, 255}
	case theme.ColorNameButton:
		return color.RGBA{78, 78, 78, 255}
	case theme.ColorNameForeground:
		return color.RGBA{176, 180, 188, 255}
	case theme.ColorNamePrimary:
		return color.RGBA{122, 162, 212, 255}
	case theme.ColorNameHover:
		return color.RGBA{94, 94, 94, 255}
	case theme.ColorNameDisabled:
		return color.RGBA{150, 150, 150, 255}
	default:
		return theme.DefaultTheme().Color(name, theme.VariantDark)
	}
}

func (t *DarkTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *DarkTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *DarkTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameInnerPadding:
		return 4
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNameScrollBar:
		return 16
	case theme.SizeNameScrollBarSmall:
		return 12
	default:
		return theme.DefaultTheme().Size(name)
	}
}
