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

type AudioApp struct {
	window       fyne.Window
	status       *AudioStatus
	expanded     map[string]bool
	root         *fyne.Container
	updateSignal  chan struct{}
}

func main() {
	a := app.New()
	a.Settings().SetTheme(&DarkTheme{})

	appInstance := &AudioApp{
		expanded: make(map[string]bool),
	}

	appInstance.window = a.NewWindow("Audio Indicator")
	appInstance.window.Resize(fyne.NewSize(380, 600))

	// Initial status load
	appInstance.status = GetCurrentStatus()

	// Create root container
	appInstance.root = appInstance.makeAudioPopup()
	appInstance.window.SetContent(appInstance.root)

	// Handle UI updates on main thread
	go func() {
		for range appInstance.updateSignal {
			appInstance.refreshUI()
		}
	}()

	// Start background audio updates
	go func() {
		for {
			time.Sleep(2 * time.Second)
			status := GetCurrentStatus()
			appInstance.status = status

			// Signal UI to update on main thread
			select {
			case appInstance.updateSignal <- struct{}{}:
			default:
			}
		}
	}()

	appInstance.window.ShowAndRun()
}

func (a *AudioApp) refreshUI() {
	// Recreate the entire content
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
		setMute(muteBtnState)
		muteBtn.SetText(getMuteIcon(muteBtnState))
		// Signal UI update
		a.refreshUI()
	}

	slider.OnChanged = func(value float64) {
		vol := int(value)
		volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		setVolume(vol)
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
		sink := sink // capture variable
		checkState := sink.Default
		check := widget.NewCheck(
			fmt.Sprintf("%s %s", getSinkIcon(checkState), sink.Description),
			nil,
		)
		check.Checked = checkState
		check.OnChanged = func(checked bool) {
			if checked {
				setSink(sink.Name)
				checkState = checked
				// Uncheck other sinks
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
		setMicMute(muteBtnState)
		muteBtn.SetText(getMicMuteIcon(muteBtnState))
	}

	slider.OnChanged = func(value float64) {
		vol := int(value)
		volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		setMicVolume(vol)
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
		source := source // capture variable
		checkState := source.Default
		check := widget.NewCheck(
			fmt.Sprintf("%s %s", getSourceIcon(checkState), source.Description),
			nil,
		)
		check.Checked = checkState
		check.OnChanged = func(checked bool) {
			if checked {
				setSource(source.Name)
				checkState = checked
				// Uncheck other sources
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
		setStreamMute(stream.ID, muteBtnState)
		muteBtn.SetText(getMuteIcon(muteBtnState))
	}

	sinkBtn := widget.NewButton(getSinkExpandIcon(a.expanded[stream.ID]), func() {
		a.expanded[stream.ID] = !a.expanded[stream.ID]
		a.refreshUI()
	})

	slider.OnChanged = func(value float64) {
		vol := int(value)
		volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		setStreamVolume(stream.ID, vol)
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

	// Add dropdown for sink selection if expanded
	if a.expanded[stream.ID] {
		sinkList := container.NewVBox()
		for _, sink := range a.status.Sinks {
			sink := sink // capture variable
			sinkItem := widget.NewButton(
				fmt.Sprintf("%s %s", getSinkIcon(sink.Name == stream.SinkName), sink.Description),
				func() {
					moveSinkInput(stream.ID, sink.Name)
					a.expanded[stream.ID] = false
					a.refreshUI()
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
		setSourceOutputMute(stream.ID, muteBtnState)
		muteBtn.SetText(getMicMuteIcon(muteBtnState))
	}

	slider.OnChanged = func(value float64) {
		vol := int(value)
		volumeLabel.SetText(fmt.Sprintf("%d%%", vol))
		setSourceOutputVolume(stream.ID, vol)
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
