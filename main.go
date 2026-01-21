package main

import (
	"flag"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"image/color"
	"log"
	"time"
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
	}

	appInstance.window = fyneApp.NewWindow(*title)
	appInstance.window.Resize(fyne.NewSize(380, 600))

	// Initial status load (blocking, but only at startup)
	appInstance.status = getStatus()

	// Create root container with persistent scroll
	appInstance.root = appInstance.makeAudioPopup()
	appInstance.scroll = container.NewVScroll(appInstance.root)
	appInstance.window.SetContent(appInstance.scroll)

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
			a.refreshUI()
		}

	case ActionSetSink:
		if data, ok := msg.Data.(SetSinkData); ok {
			log.Printf("[ACTION] SetSink: %s", data.SinkName)
			// Optimistic update: mark sink as selected immediately
			for i := range a.status.Sinks {
				a.status.Sinks[i].Default = (a.status.Sinks[i].Name == data.SinkName)
			}
			a.refreshUI() // Immediate feedback

			// Do I/O in background, then signal fetcher (not getStatus directly)
			go func() {
				log.Println("[IO] setSink starting...")
				start := time.Now()
				setSink(data.SinkName)
				log.Printf("[IO] setSink took %v", time.Since(start))
				a.RequestFetch() // Signal fetcher instead of direct getStatus
			}()
		}

	case ActionSetSource:
		if data, ok := msg.Data.(SetSourceData); ok {
			// Optimistic update
			for i := range a.status.Sources {
				a.status.Sources[i].Default = (a.status.Sources[i].Name == data.SourceName)
			}
			a.refreshUI()

			go func() {
				setSource(data.SourceName)
				a.RequestFetch() // Signal fetcher
			}()
		}

	case ActionMoveSinkInput:
		if data, ok := msg.Data.(MoveSinkInputData); ok {
			// Optimistic update
			a.expanded[data.StreamID] = false
			for i := range a.status.Streams {
				if a.status.Streams[i].ID == data.StreamID {
					a.status.Streams[i].SinkName = data.SinkName
					break
				}
			}
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

		// Only rebuild full UI if widgets don't exist yet
		if a.volumeSlider == nil {
			scrollOffset := a.scroll.Offset
			buildStart := time.Now()
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
		a.makeHeader(),
		widget.NewSeparator(),
		a.makeMainVolume(),
		widget.NewSeparator(),
		a.makeOutputDevices(),
		widget.NewSeparator(),
		a.makeMicrophoneSection(),
		widget.NewSeparator(),
		a.makeInputDevices(),
		widget.NewSeparator(),
		a.makeApplicationsPlaying(),
		widget.NewSeparator(),
		a.makeApplicationsRecording(),
	)
}

func (a *AudioApp) makeHeader() *fyne.Container {
	// Border layout: title on left, close button on right, spacer in center
	return container.NewBorder(nil, nil,
		widget.NewLabel("󰕾 Audio"),
		widget.NewButton("󰅖", func() {
			a.window.Close()
		}),
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
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		go setMute(muteBtnState) // Blocking I/O in goroutine
		muteBtn.SetText(getMuteIcon(muteBtnState))
	}

	a.volumeSlider.OnChanged = func(value float64) {
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
	label := widget.NewLabel("Output Device")

	var items []*widget.Check
	for _, sink := range a.status.Sinks {
		sink := sink
		checkState := sink.Default
		check := widget.NewCheck(
			fmt.Sprintf("%s %s", getSinkIcon(checkState), sink.Description),
			nil,
		)
		check.Checked = checkState
		check.OnChanged = func(checked bool) {
			if checked {
				// Dispatch action instead of direct call
				a.Dispatch(ActionSetSink, SetSinkData{SinkName: sink.Name})
				for _, item := range items {
					if item != check {
						item.SetChecked(false)
					}
				}
			}
		}
		items = append(items, check)
	}

	itemsContainer := container.NewVBox()
	for _, item := range items {
		itemsContainer.Add(item)
	}

	return container.NewVBox(
		label,
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
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		go setMicMute(muteBtnState)
		muteBtn.SetText(getMicMuteIcon(muteBtnState))
	}

	a.micVolumeSlider.OnChanged = func(value float64) {
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
	label := widget.NewLabel("Input Device")

	var items []*widget.Check
	for _, source := range a.status.Sources {
		source := source
		checkState := source.Default
		check := widget.NewCheck(
			fmt.Sprintf("%s %s", getSourceIcon(checkState), source.Description),
			nil,
		)
		check.Checked = checkState
		check.OnChanged = func(checked bool) {
			if checked {
				a.Dispatch(ActionSetSource, SetSourceData{SourceName: source.Name})
				for _, item := range items {
					if item != check {
						item.SetChecked(false)
					}
				}
			}
		}
		items = append(items, check)
	}

	itemsContainer := container.NewVBox()
	for _, item := range items {
		itemsContainer.Add(item)
	}

	return container.NewVBox(
		label,
		itemsContainer,
	)
}

func (a *AudioApp) makeApplicationsPlaying() *fyne.Container {
	label := widget.NewLabel("Applications Playing")

	if len(a.status.Streams) == 0 {
		return container.NewVBox(label)
	}

	streamsContainer := container.NewVBox()
	for _, stream := range a.status.Streams {
		streamsContainer.Add(a.makeStreamItem(stream))
	}

	return container.NewVBox(
		label,
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
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		go setStreamMute(stream.ID, muteBtnState)
		muteBtn.SetText(getMuteIcon(muteBtnState))
	}

	// Dispatch action for expand toggle
	sinkBtn := widget.NewButton(getSinkExpandIcon(a.expanded[stream.ID]), func() {
		a.Dispatch(ActionToggleExpand, ExpandData{StreamID: stream.ID})
	})

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
			sinkItem := widget.NewButton(
				fmt.Sprintf("%s %s", getSinkIcon(sink.Name == stream.SinkName), sink.Description),
				func() {
					a.Dispatch(ActionMoveSinkInput, MoveSinkInputData{
						StreamID: stream.ID,
						SinkName: sink.Name,
					})
				},
			)
			sinkList.Add(sinkItem)
		}
		streamContainer.Add(sinkList)
	}

	return streamContainer
}

func (a *AudioApp) makeApplicationsRecording() *fyne.Container {
	label := widget.NewLabel("Applications Recording")

	if len(a.status.MicStreams) == 0 {
		return container.NewVBox(label)
	}

	streamsContainer := container.NewVBox()
	for _, stream := range a.status.MicStreams {
		streamsContainer.Add(a.makeMicStreamItem(stream))
	}

	return container.NewVBox(
		label,
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
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		go setSourceOutputMute(stream.ID, muteBtnState)
		muteBtn.SetText(getMicMuteIcon(muteBtnState))
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
	default:
		return theme.DefaultTheme().Size(name)
	}
}
