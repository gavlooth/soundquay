package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Action types - like Clojure keywords
type Action int

const (
	ActionRefreshStatus Action = iota
	ActionToggleExpand
	ActionSetSink
	ActionSetSource
	ActionMoveSinkInput
)

func (a Action) String() string {
	return [...]string{"RefreshStatus", "ToggleExpand", "SetSink", "SetSource", "MoveSinkInput"}[a]
}

// ActionMsg carries the action and its data through the channel
type ActionMsg struct {
	Action Action
	Data   interface{}
}

// Action data types
type ExpandData struct {
	StreamID string
}

type SetSinkData struct {
	SinkName string
}

type SetSourceData struct {
	SourceName string
}

type MoveSinkInputData struct {
	StreamID string
	SinkName string
}

type AudioApp struct {
	window      fyne.Window
	status      *AudioStatus
	expanded    map[string]bool
	root        *fyne.Container
	scroll      *container.Scroll // Persistent scroll container
	actionChan  chan ActionMsg    // Single channel for all actions
	triggerFetch chan struct{}    // Signal to fetch status (alts! pattern)

	// Persistent widget references - don't recreate on refresh
	volumeSlider    *widget.Slider
	volumeLabel     *widget.Label
	micVolumeSlider *widget.Slider
	micVolumeLabel  *widget.Label

	// Track last user interaction to avoid overwriting pending changes
	lastVolumeChange    time.Time
	lastMicVolumeChange time.Time

	// Flag to prevent OnChanged from firing during programmatic updates
	updatingSliders bool

	// Flag to force full UI rebuild (e.g., when toggling device lists)
	needsRebuild bool

	// Window title from CLI
	title string
}

// Dispatch sends an action to the coordinator goroutine
func (a *AudioApp) Dispatch(action Action, data interface{}) {
	log.Printf("[DISPATCH] → %s", action)
	select {
	case a.actionChan <- ActionMsg{Action: action, Data: data}:
		log.Printf("[DISPATCH] ✓ %s queued", action)
	default:
		log.Printf("[DISPATCH] ✗ %s DROPPED (channel full)", action)
	}
}

// RequestFetch signals the fetcher to refresh status (non-blocking)
func (a *AudioApp) RequestFetch() {
	select {
	case a.triggerFetch <- struct{}{}:
		log.Println("[FETCH] Requested")
	default:
		log.Println("[FETCH] Already pending (coalesced)")
	}
}

func main() {
	// CLI flags
	title := flag.String("title", "Audio Indicator", "Window title")
	flag.Parse()

	fyneApp := app.New()
	fyneApp.Settings().SetTheme(&DarkTheme{})

	appInstance := &AudioApp{
		expanded:     make(map[string]bool),
		actionChan:   make(chan ActionMsg, 100), // Buffered like your Clojure main-action-channel
		triggerFetch: make(chan struct{}, 1),    // Buffered 1 for non-blocking signal
		title:        *title,
	}

	appInstance.window = fyneApp.NewWindow(*title)
	appInstance.window.Resize(fyne.NewSize(380, 600))

	// Initial status load (blocking, but only at startup)
	appInstance.status = getStatus()

	// Create root container with persistent scroll
	appInstance.root = appInstance.makeAudioPopup()
	// Add right margin for scrollbar spacing
	rightSpacer := container.NewGridWrap(fyne.NewSize(16, 0))
	contentWithMargin := container.NewBorder(nil, nil, nil, rightSpacer, appInstance.root)
	appInstance.scroll = container.NewVScroll(contentWithMargin)
	appInstance.scroll.SetMinSize(fyne.NewSize(0, 400))

	// Header outside scroll, scroll area with padding
	header := appInstance.makeHeader()
	paddedScroll := container.NewPadded(appInstance.scroll)
	appInstance.window.SetContent(container.NewBorder(header, nil, nil, nil, paddedScroll))

	// Log initial sizes for debugging layout
	log.Printf("[STARTUP] Window size: %v", appInstance.window.Canvas().Size())
	log.Printf("[STARTUP] Root container size: %v", appInstance.root.Size())
	log.Printf("[STARTUP] Root min size: %v", appInstance.root.MinSize())

	// Fetcher goroutine - alts! pattern: responds to BOTH timer AND manual triggers
	// Single source of truth for status fetching, coalesces rapid requests
	go func() {
		log.Println("[FETCHER] Started")
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-appInstance.triggerFetch:
				log.Println("[FETCHER] Manual trigger")
			case <-ticker.C:
				log.Println("[FETCHER] Timer trigger")
			}

			// Coalesced fetch - only ONE getStatus regardless of trigger source
			start := time.Now()
			getStatus()
			log.Printf("[FETCHER] getStatus() took %v", time.Since(start))
			appInstance.Dispatch(ActionRefreshStatus, nil)
		}
	}()

	// Coordinator goroutine - handles actions, no polling here
	go func() {
		log.Println("[COORDINATOR] Started")
		for action := range appInstance.actionChan {
			log.Printf("[COORDINATOR] ← Received %s", action.Action)
			start := time.Now()
			appInstance.handleAction(action)
			log.Printf("[COORDINATOR] ✓ Handled %s in %v", action.Action, time.Since(start))
		}
	}()

	appInstance.window.ShowAndRun()
}

// handleAction processes actions sequentially (like evolve-flow)
func (a *AudioApp) handleAction(msg ActionMsg) {
	switch msg.Action {
	case ActionRefreshStatus:
		oldStatus := a.status
		a.status = GetCurrentStatus()

		// Log before/after percentages for debugging reset issue
		if oldStatus != nil {
			log.Printf("[STATUS] BEFORE: Vol=%d%% Muted=%v MicVol=%d%% MicMuted=%v",
				oldStatus.Volume, oldStatus.Muted, oldStatus.MicVolume, oldStatus.MicMuted)
		}
		log.Printf("[STATUS] AFTER:  Vol=%d%% Muted=%v MicVol=%d%% MicMuted=%v",
			a.status.Volume, a.status.Muted, a.status.MicVolume, a.status.MicMuted)

		a.refreshUI()

	case ActionToggleExpand:
		if data, ok := msg.Data.(ExpandData); ok {
			log.Printf("[ACTION] ToggleExpand: %s -> %v", data.StreamID, !a.expanded[data.StreamID])
			a.expanded[data.StreamID] = !a.expanded[data.StreamID]
			a.needsRebuild = true
			a.refreshUI()
		}

	case ActionSetSink:
		if data, ok := msg.Data.(SetSinkData); ok {
			log.Printf("[ACTION] SetSink: %s", data.SinkName)
			// Optimistic update: mark sink as selected and move all streams
			// Find the sink ID for the given sink name
			var sinkID string
			for i := range a.status.Sinks {
				if a.status.Sinks[i].Name == data.SinkName {
					sinkID = a.status.Sinks[i].ID
					a.status.Sinks[i].Default = true
				} else {
					a.status.Sinks[i].Default = false
				}
			}
			for i := range a.status.Streams {
				a.status.Streams[i].SinkName = sinkID
			}
			a.needsRebuild = true
			a.refreshUI() // Immediate feedback

			// Do I/O in background: set default and move all streams
			streams := make([]string, len(a.status.Streams))
			for i, s := range a.status.Streams {
				streams[i] = s.ID
			}
			go func() {
				log.Println("[IO] setSink starting...")
				start := time.Now()
				setSink(data.SinkName)
				// Move all existing streams to the new sink
				for _, streamID := range streams {
					moveSinkInput(streamID, data.SinkName)
				}
				log.Printf("[IO] setSink took %v", time.Since(start))
				a.RequestFetch() // Signal fetcher instead of direct getStatus
			}()
		}

	case ActionSetSource:
		if data, ok := msg.Data.(SetSourceData); ok {
			// Optimistic update: mark source as selected and move all mic streams
			// Find the source ID for the given source name
			var sourceID string
			for i := range a.status.Sources {
				if a.status.Sources[i].Name == data.SourceName {
					sourceID = a.status.Sources[i].ID
					a.status.Sources[i].Default = true
				} else {
					a.status.Sources[i].Default = false
				}
			}
			for i := range a.status.MicStreams {
				a.status.MicStreams[i].SourceName = sourceID
			}
			a.needsRebuild = true
			a.refreshUI()

			// Do I/O in background: set default and move all mic streams
			micStreams := make([]string, len(a.status.MicStreams))
			for i, s := range a.status.MicStreams {
				micStreams[i] = s.ID
			}
			go func() {
				setSource(data.SourceName)
				// Move all existing mic streams to the new source
				for _, streamID := range micStreams {
					moveSourceOutput(streamID, data.SourceName)
				}
				a.RequestFetch() // Signal fetcher
			}()
		}

	case ActionMoveSinkInput:
		if data, ok := msg.Data.(MoveSinkInputData); ok {
			// Find the sink ID for the given sink name
			var sinkID string
			for _, sink := range a.status.Sinks {
				if sink.Name == data.SinkName {
					sinkID = sink.ID
					break
				}
			}
			// Optimistic update (keep dropdown open)
			for i := range a.status.Streams {
				if a.status.Streams[i].ID == data.StreamID {
					a.status.Streams[i].SinkName = sinkID
					break
				}
			}
			a.needsRebuild = true
			a.refreshUI() // Immediate feedback

			go func() {
				moveSinkInput(data.StreamID, data.SinkName)
				a.RequestFetch() // Signal fetcher
			}()
		}
	}
}

func (a *AudioApp) refreshUI() {
	start := time.Now()
	log.Println("[REFRESH] Starting...")

	// ALL UI operations must be on main Fyne thread - use DoAndWait to block until complete
	gracePeriod := 3 * time.Second // Don't overwrite user changes for 3 seconds

	fyne.DoAndWait(func() {
		// Set flag to prevent OnChanged from triggering setVolume during programmatic updates
		a.updatingSliders = true
		defer func() { a.updatingSliders = false }()

		// Update existing sliders instead of rebuilding entire UI
		if a.volumeSlider != nil {
			currentSliderVal := int(a.volumeSlider.Value)
			timeSinceChange := time.Since(a.lastVolumeChange)
			if timeSinceChange < gracePeriod {
				log.Printf("[REFRESH] Volume slider: %d%% (skipped - user changed %v ago)", currentSliderVal, timeSinceChange.Round(time.Millisecond))
			} else if currentSliderVal != a.status.Volume {
				log.Printf("[REFRESH] Volume slider: %d%% -> %d%% (updating)", currentSliderVal, a.status.Volume)
				a.volumeSlider.SetValue(float64(a.status.Volume))
				a.volumeLabel.SetText(fmt.Sprintf("%d%%", a.status.Volume))
			} else {
				log.Printf("[REFRESH] Volume slider: %d%% (unchanged)", currentSliderVal)
			}
		}

		if a.micVolumeSlider != nil {
			currentSliderVal := int(a.micVolumeSlider.Value)
			timeSinceChange := time.Since(a.lastMicVolumeChange)
			if timeSinceChange < gracePeriod {
				log.Printf("[REFRESH] Mic slider: %d%% (skipped - user changed %v ago)", currentSliderVal, timeSinceChange.Round(time.Millisecond))
			} else if currentSliderVal != a.status.MicVolume {
				log.Printf("[REFRESH] Mic slider: %d%% -> %d%% (updating)", currentSliderVal, a.status.MicVolume)
				a.micVolumeSlider.SetValue(float64(a.status.MicVolume))
				a.micVolumeLabel.SetText(fmt.Sprintf("%d%%", a.status.MicVolume))
			} else {
				log.Printf("[REFRESH] Mic slider: %d%% (unchanged)", currentSliderVal)
			}
		}

		// Rebuild full UI if widgets don't exist yet or if rebuild is requested
		if a.volumeSlider == nil || a.needsRebuild {
			a.needsRebuild = false
			scrollOffset := a.scroll.Offset
			buildStart := time.Now()

			// Clear slider references so they get recreated
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
	return container.NewVBox(
		a.makeMainVolume(),
		a.makeOutputDevices(),
		widget.NewSeparator(),
		a.makeMicrophoneSection(),
		a.makeInputDevices(),
		widget.NewSeparator(),
		a.makeApplicationsPlaying(),
		widget.NewSeparator(),
		a.makeApplicationsRecording(),
	)
}

func (a *AudioApp) makeHeader() *fyne.Container {
	closeBtn := widget.NewButton("󰅖", func() {
		a.window.Close()
	})
	closeBtn.Importance = widget.LowImportance

	// Title with larger font
	titleText := widget.NewRichTextFromMarkdown("# 󰕾 " + a.title)

	// Border layout: title on left, close button on right, spacer in center
	return container.NewBorder(nil, nil,
		titleText,
		closeBtn,
		nil, // Empty center acts as spacer
	)
}

func (a *AudioApp) makeMainVolume() *fyne.Container {
	label := widget.NewLabel("Volume")

	volume := a.status.Volume
	log.Printf("[BUILD] Creating volume slider with value=%d%%", volume)

	// Create and store persistent references
	a.volumeSlider = widget.NewSlider(0, 100)
	a.volumeSlider.SetValue(float64(volume))
	a.volumeSlider.Step = 5

	a.volumeLabel = widget.NewLabel(fmt.Sprintf("%d%%", volume))
	isMuted := a.status.Muted
	muteBtnState := isMuted

	muteBtn := widget.NewButton(getMuteIcon(muteBtnState), nil)
	muteBtn.Importance = widget.LowImportance
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		go setMute(muteBtnState)
		muteBtn.SetText(getMuteIcon(muteBtnState))
		muteBtn.Refresh()
	}

	a.volumeSlider.OnChanged = func(value float64) {
		if a.updatingSliders {
			return // Skip during programmatic updates
		}
		vol := int(value)
		log.Printf("[SLIDER] Main volume: status=%d%% -> slider=%d%%", a.status.Volume, vol)
		a.volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		a.lastVolumeChange = time.Now() // Mark as recently changed
		setVolume(vol)                  // Already debounced in audio.go
	}

	// Border layout: slider expands in center, controls on right
	sliderRow := container.NewBorder(nil, nil, nil,
		container.NewHBox(a.volumeLabel, muteBtn),
		a.volumeSlider,
	)

	return container.NewVBox(label, sliderRow)
}

func (a *AudioApp) makeOutputDevices() *fyne.Container {
	expanded := a.expanded["output_devices"]
	toggleIcon := "▼"
	if expanded {
		toggleIcon = "▲"
	}

	headerBtn := widget.NewButton(fmt.Sprintf("%s Output Device (%d)", toggleIcon, len(a.status.Sinks)), func() {
		a.expanded["output_devices"] = !a.expanded["output_devices"]
		a.needsRebuild = true
		a.Dispatch(ActionRefreshStatus, nil)
	})
	headerBtn.Alignment = widget.ButtonAlignLeading

	if !expanded {
		// Show just the default device name when collapsed
		defaultName := "None"
		for _, sink := range a.status.Sinks {
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
	for _, sink := range a.status.Sinks {
		sink := sink
		sinkBtn := widget.NewButton(
			fmt.Sprintf("%s %s", getSinkIcon(sink.Default), sink.Description),
			func() {
				a.Dispatch(ActionSetSink, SetSinkData{SinkName: sink.Name})
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

	return container.NewVBox(
		headerBtn,
		itemsContainer,
	)
}

func (a *AudioApp) makeMicrophoneSection() *fyne.Container {
	label := widget.NewLabel("Microphone")

	volume := a.status.MicVolume
	log.Printf("[BUILD] Creating mic slider with value=%d%%", volume)

	// Create and store persistent references
	a.micVolumeSlider = widget.NewSlider(0, 100)
	a.micVolumeSlider.SetValue(float64(volume))
	a.micVolumeSlider.Step = 5

	a.micVolumeLabel = widget.NewLabel(fmt.Sprintf("%d%%", volume))
	isMuted := a.status.MicMuted
	muteBtnState := isMuted

	muteBtn := widget.NewButton(getMicMuteIcon(muteBtnState), nil)
	muteBtn.Importance = widget.LowImportance
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		go setMicMute(muteBtnState)
		muteBtn.SetText(getMicMuteIcon(muteBtnState))
		muteBtn.Refresh()
	}

	a.micVolumeSlider.OnChanged = func(value float64) {
		if a.updatingSliders {
			return // Skip during programmatic updates
		}
		vol := int(value)
		log.Printf("[SLIDER] Mic volume: status=%d%% -> slider=%d%%", a.status.MicVolume, vol)
		a.micVolumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		a.lastMicVolumeChange = time.Now() // Mark as recently changed
		setMicVolume(vol)                  // Already debounced
	}

	// Border layout: slider expands in center
	sliderRow := container.NewBorder(nil, nil, nil,
		container.NewHBox(a.micVolumeLabel, muteBtn),
		a.micVolumeSlider,
	)

	return container.NewVBox(label, sliderRow)
}

func (a *AudioApp) makeInputDevices() *fyne.Container {
	expanded := a.expanded["input_devices"]
	toggleIcon := "▼"
	if expanded {
		toggleIcon = "▲"
	}

	// Filter out monitor sources (they're virtual loopback sources, not real mics)
	var realSources []AudioSource
	for _, source := range a.status.Sources {
		if !strings.HasSuffix(source.Name, ".monitor") {
			realSources = append(realSources, source)
		}
	}

	headerBtn := widget.NewButton(fmt.Sprintf("%s Input Device (%d)", toggleIcon, len(realSources)), func() {
		a.expanded["input_devices"] = !a.expanded["input_devices"]
		a.needsRebuild = true
		a.Dispatch(ActionRefreshStatus, nil)
	})
	headerBtn.Alignment = widget.ButtonAlignLeading

	if !expanded {
		// Show just the default device name when collapsed
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
				a.Dispatch(ActionSetSource, SetSourceData{SourceName: source.Name})
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

	return container.NewVBox(
		headerBtn,
		itemsContainer,
	)
}

func (a *AudioApp) makeApplicationsPlaying() *fyne.Container {
	expanded := a.expanded["apps_playing"]
	toggleIcon := "▼"
	if expanded {
		toggleIcon = "▲"
	}

	headerBtn := widget.NewButton(fmt.Sprintf("%s Applications Playing (%d)", toggleIcon, len(a.status.Streams)), func() {
		a.expanded["apps_playing"] = !a.expanded["apps_playing"]
		a.needsRebuild = true
		a.Dispatch(ActionRefreshStatus, nil)
	})
	headerBtn.Alignment = widget.ButtonAlignLeading

	if len(a.status.Streams) == 0 || !expanded {
		return container.NewVBox(headerBtn)
	}

	streamsContainer := container.NewVBox()
	for _, stream := range a.status.Streams {
		streamsContainer.Add(a.makeStreamItem(stream))
	}

	return container.NewVBox(
		headerBtn,
		streamsContainer,
	)
}

func (a *AudioApp) makeStreamItem(stream AudioStream) *fyne.Container {
	nameLabel := widget.NewLabel(stream.Name)

	volume := stream.Volume
	slider := widget.NewSlider(0, 100)
	slider.SetValue(float64(volume))
	slider.Step = 5

	volumeLabel := widget.NewLabel(fmt.Sprintf("%d%%", volume))
	isMuted := stream.Muted
	muteBtnState := isMuted

	muteBtn := widget.NewButton(getMuteIcon(muteBtnState), nil)
	muteBtn.Importance = widget.LowImportance
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		go setStreamMute(stream.ID, muteBtnState)
		muteBtn.SetText(getMuteIcon(muteBtnState))
		muteBtn.Refresh()
	}

	// Dispatch action for expand toggle
	sinkBtn := widget.NewButton(getSinkExpandIcon(a.expanded[stream.ID]), nil)
	sinkBtn.Importance = widget.LowImportance
	sinkBtn.OnTapped = func() {
		a.Dispatch(ActionToggleExpand, ExpandData{StreamID: stream.ID})
	}

	slider.OnChanged = func(value float64) {
		vol := int(value)
		volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		setStreamVolume(stream.ID, vol) // Already debounced
	}

	// Border layout: name on left, slider expands in center, controls on right
	sliderRow := container.NewBorder(nil, nil,
		nameLabel,
		container.NewHBox(volumeLabel, muteBtn, sinkBtn),
		slider,
	)

	streamContainer := container.NewVBox(sliderRow)

	if a.expanded[stream.ID] {
		sinkList := container.NewVBox()
		for _, sink := range a.status.Sinks {
			sink := sink
			// Compare by sink ID (index number) since stream.SinkName contains the index
			isSelected := sink.ID == stream.SinkName
			sinkItem := widget.NewButton(
				fmt.Sprintf("%s %s", getSinkIcon(isSelected), sink.Description),
				func() {
					a.Dispatch(ActionMoveSinkInput, MoveSinkInputData{
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
	expanded := a.expanded["apps_recording"]
	toggleIcon := "▼"
	if expanded {
		toggleIcon = "▲"
	}

	headerBtn := widget.NewButton(fmt.Sprintf("%s Applications Recording (%d)", toggleIcon, len(a.status.MicStreams)), func() {
		a.expanded["apps_recording"] = !a.expanded["apps_recording"]
		a.needsRebuild = true
		a.Dispatch(ActionRefreshStatus, nil)
	})
	headerBtn.Alignment = widget.ButtonAlignLeading

	if len(a.status.MicStreams) == 0 || !expanded {
		return container.NewVBox(headerBtn)
	}

	streamsContainer := container.NewVBox()
	for _, stream := range a.status.MicStreams {
		streamsContainer.Add(a.makeMicStreamItem(stream))
	}

	return container.NewVBox(
		headerBtn,
		streamsContainer,
	)
}

func (a *AudioApp) makeMicStreamItem(stream MicStream) *fyne.Container {
	nameLabel := widget.NewLabel("󰍬 " + stream.Name)

	volume := stream.Volume
	slider := widget.NewSlider(0, 100)
	slider.SetValue(float64(volume))

	volumeLabel := widget.NewLabel(fmt.Sprintf("%d%%", volume))
	isMuted := stream.Muted
	muteBtnState := isMuted

	muteBtn := widget.NewButton(getMicMuteIcon(muteBtnState), nil)
	muteBtn.Importance = widget.LowImportance
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		go setSourceOutputMute(stream.ID, muteBtnState)
		muteBtn.SetText(getMicMuteIcon(muteBtnState))
		muteBtn.Refresh()
	}

	slider.OnChanged = func(value float64) {
		vol := int(value)
		volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		setSourceOutputVolume(stream.ID, vol) // Already debounced
	}

	// Border layout: name on left, slider expands in center, controls on right
	return container.NewBorder(nil, nil,
		nameLabel,
		container.NewHBox(volumeLabel, muteBtn),
		slider,
	)
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

// Dark Theme
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
