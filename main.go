package main

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"image/color"
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
	window     fyne.Window
	status     *AudioStatus
	expanded   map[string]bool
	root       *fyne.Container
	actionChan chan ActionMsg // Single channel for all actions
}

// Dispatch sends an action to the coordinator goroutine
func (a *AudioApp) Dispatch(action Action, data interface{}) {
	select {
	case a.actionChan <- ActionMsg{Action: action, Data: data}:
	default:
		// Channel full, drop action (backpressure)
	}
}

func main() {
	fyneApp := app.New()
	fyneApp.Settings().SetTheme(&DarkTheme{})

	appInstance := &AudioApp{
		expanded:   make(map[string]bool),
		actionChan: make(chan ActionMsg, 100), // Buffered like your Clojure main-action-channel
	}

	appInstance.window = fyneApp.NewWindow("Audio Indicator")
	appInstance.window.Resize(fyne.NewSize(380, 600))

	// Initial status load (blocking, but only at startup)
	appInstance.status = getStatus()

	// Create root container
	appInstance.root = appInstance.makeAudioPopup()
	appInstance.window.SetContent(appInstance.root)

	// Single coordinator goroutine - like your Clojure go-loop
	// All state mutations happen here, serialized
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case action := <-appInstance.actionChan:
				// Handle action (like evolve-flow multimethod dispatch)
				appInstance.handleAction(action)

			case <-ticker.C:
				// Periodic poll (like your timeout-chan in alts!)
				// Spawn goroutine for blocking I/O, then dispatch result
				go func() {
					getStatus() // Blocking I/O in separate goroutine
					appInstance.Dispatch(ActionRefreshStatus, nil)
				}()
			}
		}
	}()

	appInstance.window.ShowAndRun()
}

// handleAction processes actions sequentially (like evolve-flow)
func (a *AudioApp) handleAction(msg ActionMsg) {
	switch msg.Action {
	case ActionRefreshStatus:
		a.status = GetCurrentStatus()
		a.refreshUI()

	case ActionToggleExpand:
		if data, ok := msg.Data.(ExpandData); ok {
			a.expanded[data.StreamID] = !a.expanded[data.StreamID]
			a.refreshUI()
		}

	case ActionSetSink:
		if data, ok := msg.Data.(SetSinkData); ok {
			go setSink(data.SinkName) // Blocking I/O in goroutine
			// Refresh will happen on next poll or can dispatch refresh
			a.Dispatch(ActionRefreshStatus, nil)
		}

	case ActionSetSource:
		if data, ok := msg.Data.(SetSourceData); ok {
			go setSource(data.SourceName)
			a.Dispatch(ActionRefreshStatus, nil)
		}

	case ActionMoveSinkInput:
		if data, ok := msg.Data.(MoveSinkInputData); ok {
			go moveSinkInput(data.StreamID, data.SinkName)
			a.expanded[data.StreamID] = false
			a.refreshUI()
		}
	}
}

func (a *AudioApp) refreshUI() {
	newRoot := a.makeAudioPopup()
	a.root.Objects = newRoot.Objects
	a.root.Refresh()
}

func (a *AudioApp) makeAudioPopup() *fyne.Container {
	return container.NewVBox(
		a.makeHeader(),
		widget.NewSeparator(),
		a.makeMainVolume(),
		a.makeOutputDevices(),
		a.makeMicrophoneSection(),
		a.makeInputDevices(),
		a.makeApplicationsPlaying(),
		a.makeApplicationsRecording(),
	)
}

func (a *AudioApp) makeHeader() *fyne.Container {
	return container.NewHBox(
		widget.NewLabel("󰕾 Audio"),
		container.NewGridWithColumns(1),
		widget.NewButton("󰅖", func() {
			a.window.Close()
		}),
	)
}

func (a *AudioApp) makeMainVolume() *fyne.Container {
	label := widget.NewLabel("Volume")

	volume := a.status.Volume
	slider := widget.NewSlider(0, 100)
	slider.SetValue(float64(volume))
	slider.Step = 5

	volumeLabel := widget.NewLabel(fmt.Sprintf("%d%%", volume))
	isMuted := a.status.Muted
	muteBtnState := isMuted

	muteBtn := widget.NewButton(getMuteIcon(muteBtnState), nil)
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		go setMute(muteBtnState) // Blocking I/O in goroutine
		muteBtn.SetText(getMuteIcon(muteBtnState))
	}

	slider.OnChanged = func(value float64) {
		vol := int(value)
		volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		setVolume(vol) // Already debounced in audio.go
	}

	return container.NewVBox(
		label,
		container.NewHBox(
			slider,
			volumeLabel,
			muteBtn,
		),
	)
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
	slider := widget.NewSlider(0, 100)
	slider.SetValue(float64(volume))
	slider.Step = 5

	volumeLabel := widget.NewLabel(fmt.Sprintf("%d%%", volume))
	isMuted := a.status.MicMuted
	muteBtnState := isMuted

	muteBtn := widget.NewButton(getMicMuteIcon(muteBtnState), nil)
	muteBtn.OnTapped = func() {
		muteBtnState = !muteBtnState
		go setMicMute(muteBtnState)
		muteBtn.SetText(getMicMuteIcon(muteBtnState))
	}

	slider.OnChanged = func(value float64) {
		vol := int(value)
		volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		setMicVolume(vol) // Already debounced
	}

	return container.NewVBox(
		label,
		container.NewHBox(
			slider,
			volumeLabel,
			muteBtn,
		),
	)
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

	streamContainer := container.NewVBox(
		container.NewHBox(
			nameLabel,
			slider,
			volumeLabel,
			muteBtn,
			sinkBtn,
		),
	)

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
	nameLabel := widget.NewLabel("󰍬")
	appLabel := widget.NewLabel(stream.Name)

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

	return container.NewHBox(
		nameLabel,
		appLabel,
		slider,
		volumeLabel,
		muteBtn,
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
		return 4
	case theme.SizeNameInnerPadding:
		return 2
	default:
		return theme.DefaultTheme().Size(name)
	}
}
