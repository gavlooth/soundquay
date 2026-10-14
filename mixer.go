package main

import (
	"cmp"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	"soundquay/internal/model"
	"soundquay/internal/pulse"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// guardedSlider keeps a snapshot from moving the handle during a user's edit.
type guardedSlider struct {
	widget.Slider
	dragging  bool
	updating  bool
	changedAt time.Time
	percent   *widget.Label
}

func newGuardedSlider(change func(int)) *guardedSlider {
	s := &guardedSlider{percent: widget.NewLabel("0%")}
	s.Min = 0
	s.Max = 150
	s.Step = 1
	s.ExtendBaseWidget(s)
	s.OnChanged = func(value float64) {
		s.percent.SetText(fmt.Sprintf("%d%%", int(value)))
		if !s.updating {
			s.changedAt = time.Now()
			change(int(value))
		}
	}
	return s
}
func (s *guardedSlider) Dragged(event *fyne.DragEvent) { s.dragging = true; s.Slider.Dragged(event) }
func (s *guardedSlider) DragEnd()                      { s.Slider.DragEnd(); s.dragging = false; s.changedAt = time.Now() }
func (s *guardedSlider) update(value int, now time.Time) {
	if s.dragging || (!s.changedAt.IsZero() && now.Sub(s.changedAt) < time.Second) {
		return
	}
	s.updating = true
	s.SetValue(float64(value))
	s.percent.SetText(fmt.Sprintf("%d%%", value))
	s.updating = false
}

type deviceRow struct {
	direction pulse.Direction
	entries   []model.SwitchEntry
	labels    []string
	picker    *widget.Select
	slider    *guardedSlider
	mute      *widget.Button
	muted     bool
	updating  bool
	object    fyne.CanvasObject
}

func newDeviceRow(d pulse.Direction, dispatch func(Msg)) *deviceRow {
	r := &deviceRow{direction: d}
	r.picker = widget.NewSelect(nil, func(label string) {
		if r.updating {
			return
		}
		if i := slices.Index(r.labels, label); i >= 0 {
			dispatch(SelectDevice{Entry: r.entries[i]})
		}
	})
	r.slider = newGuardedSlider(func(value int) { dispatch(VolumeChanged{Key: routeDirection(d), Direction: d, Value: value}) })
	r.mute = newMuteButton(func() { dispatch(SetMuted{Direction: d, Muted: !r.muted}) })
	title := "Output"
	if d == pulse.Input {
		title = "Input"
	}
	label := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	volume := container.NewBorder(nil, nil, nil, container.NewHBox(r.slider.percent, r.mute), r.slider)
	r.object = container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(70, label.MinSize().Height), label), nil, container.NewGridWithColumns(2, r.picker, volume))
	return r
}
func switchLabels(entries []model.SwitchEntry) []string {
	labels := make([]string, len(entries))
	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Label]++
	}
	for i, e := range entries {
		labels[i] = e.Label
		if counts[e.Label] > 1 {
			labels[i] += " [" + e.ID + "]"
		}
	}
	return labels
}
func (r *deviceRow) update(entries []model.SwitchEntry, value int, muted bool) {
	r.updating = true
	r.entries = entries
	r.labels = switchLabels(entries)
	setOptions(r.picker, r.labels)
	selected := ""
	for i, e := range entries {
		if e.Active {
			selected = r.labels[i]
			break
		}
	}
	r.picker.SetSelected(selected)
	r.muted = muted
	setMuteText(r.mute, muted)
	r.slider.update(value, time.Now())
	r.updating = false
}
func setOptions(picker *widget.Select, options []string) {
	if !slices.Equal(picker.Options, options) {
		picker.Options = options
		picker.Refresh()
	}
}
func newMuteButton(tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", theme.VolumeUpIcon(), tapped)
	button.Importance = widget.LowImportance
	return button
}

// setMuteText shows a crossed speaker in the warning color while muted.
func setMuteText(button *widget.Button, muted bool) {
	icon, importance := theme.VolumeUpIcon(), widget.LowImportance
	if muted {
		icon, importance = theme.VolumeMuteIcon(), widget.WarningImportance
	}
	if button.Icon != icon || button.Importance != importance {
		button.Icon = icon
		button.Importance = importance
		button.Refresh()
	}
}

// appDirectionLine is one line of an app row: volume, mute and device picker for
// either the app's playback or its recording streams.
type appDirectionLine struct {
	direction    pulse.Direction
	slider       *guardedSlider
	mute         *widget.Button
	muted        bool
	picker       *widget.Select
	names        []string
	labels       []string
	volumeBox    *fyne.Container
	noStreamNote *widget.Label
	// elsewhere names the device a stream is really on when that differs from the
	// saved device, or from the default when the line follows the default.
	elsewhere *widget.Label
	object    fyne.CanvasObject
}

type appRow struct {
	key           string
	name          *widget.Label
	output, input *appDirectionLine
	saved         *widget.Label
	object        fyne.CanvasObject
	updating      bool
}

func newAppDirectionLine(r *appRow, direction pulse.Direction, dispatch func(Msg)) *appDirectionLine {
	key, title, idle := "app:"+r.key+":out", "Out", "not playing"
	if direction == pulse.Input {
		key, title, idle = "app:"+r.key+":in", "In", "not recording; the device applies when it starts"
	}
	l := &appDirectionLine{direction: direction, noStreamNote: widget.NewLabelWithStyle(idle, fyne.TextAlignLeading, fyne.TextStyle{Italic: true}), elsewhere: widget.NewLabel("")}
	l.elsewhere.Importance = widget.WarningImportance
	l.elsewhere.Wrapping = fyne.TextWrapWord
	l.elsewhere.Hide()
	l.slider = newGuardedSlider(func(value int) {
		dispatch(VolumeChanged{Key: key, App: r.key, Direction: direction, Value: value})
	})
	l.mute = newMuteButton(func() { dispatch(SetMuted{App: r.key, Direction: direction, Muted: !l.muted}) })
	l.picker = widget.NewSelect(nil, func(label string) {
		if r.updating {
			return
		}
		if i := slices.Index(l.labels, label); i >= 0 {
			dispatch(ChooseAppDevice{App: r.key, Direction: direction, Device: l.names[i]})
		}
	})
	l.volumeBox = container.NewBorder(nil, nil, nil, container.NewHBox(l.slider.percent, l.mute), l.slider)
	volume := container.NewStack(l.volumeBox, l.noStreamNote)
	picker := container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(28, 0), widget.NewLabel(title)), nil, l.picker)
	l.object = container.NewVBox(container.NewGridWithColumns(2, volume, picker), l.elsewhere)
	return l
}

func newAppRow(key string, dispatch func(Msg)) *appRow {
	r := &appRow{key: key, name: widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), saved: widget.NewLabel("saved")}
	r.saved.Importance = widget.HighImportance
	r.output = newAppDirectionLine(r, pulse.Output, dispatch)
	r.input = newAppDirectionLine(r, pulse.Input, dispatch)
	r.object = card(container.NewBorder(nil, nil, nil, r.saved, r.name), r.output.object, r.input.object)
	return r
}
func choiceOptions(choices []model.DeviceChoice, selected string) (names, labels []string) {
	names = []string{""}
	labels = []string{"Follow default"}
	counts := map[string]int{}
	for _, choice := range choices {
		counts[choice.Label]++
	}
	for _, choice := range choices {
		names = append(names, choice.Name)
		label := choice.Label
		if counts[label] > 1 || label == "Follow default" {
			label += " [" + choice.Name + "]"
		}
		labels = append(labels, label)
	}
	if selected != "" && !slices.Contains(names, selected) {
		names = append(names, selected)
		labels = append(labels, selected+" (unavailable)")
	}
	return
}
func selectName(picker *widget.Select, names, labels []string, name string) {
	setOptions(picker, labels)
	i := slices.Index(names, name)
	if i >= 0 {
		picker.SetSelected(labels[i])
	} else {
		picker.ClearSelected()
	}
}
func (l *appDirectionLine) update(streams []model.AppStream, choices []model.DeviceChoice, route, defaultName string, routable bool) {
	l.names, l.labels = choiceOptions(choices, route)
	selectName(l.picker, l.names, l.labels, route)
	if routable {
		l.picker.Enable()
	} else {
		l.picker.Disable()
	}
	l.updateElsewhere(streams, choices, cmp.Or(route, defaultName))
	if len(streams) == 0 {
		l.volumeBox.Hide()
		l.noStreamNote.Show()
		return
	}
	l.noStreamNote.Hide()
	l.volumeBox.Show()
	volume, muted := 0, true
	for _, stream := range streams {
		volume += stream.VolumePct
		muted = muted && stream.Mute
	}
	l.muted = muted
	setMuteText(l.mute, muted)
	l.slider.update(volume/len(streams), time.Now())
}

func (l *appDirectionLine) updateElsewhere(streams []model.AppStream, choices []model.DeviceChoice, expected string) {
	var other []string
	for _, stream := range streams {
		if stream.DeviceName == "" || stream.DeviceName == expected {
			continue
		}
		label := stream.DeviceName
		for _, choice := range choices {
			if choice.Name == stream.DeviceName {
				label = choice.Label
				break
			}
		}
		if !slices.Contains(other, label) {
			other = append(other, label)
		}
	}
	if len(other) == 0 {
		l.elsewhere.Hide()
		return
	}
	verb := "Now playing on "
	if l.direction == pulse.Input {
		verb = "Now recording from "
	}
	l.elsewhere.SetText(verb + strings.Join(other, ", ") + " — pick a device or use Reset apps to align")
	l.elsewhere.Show()
}

func (r *appRow) update(a model.App, v model.View) {
	r.updating = true
	r.name.SetText(a.Name)
	r.output.update(a.Outputs, v.OutputDevices, a.OutputRoute, v.DefaultOutput, a.Routable)
	r.input.update(a.Inputs, v.InputDevices, a.InputRoute, v.DefaultInput, a.Routable)
	if a.OutputRoute != "" || a.InputRoute != "" {
		r.saved.SetText("saved")
		r.saved.Show()
	} else {
		r.saved.Hide()
	}
	r.updating = false
}

type savedRouteRow struct {
	label  *widget.Label
	object fyne.CanvasObject
}

func newSavedRouteRow(route model.Route, dispatch func(Msg)) *savedRouteRow {
	label := widget.NewLabel("")
	button := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() { dispatch(DeleteRoute{App: route.App, Direction: route.Direction}) })
	button.Importance = widget.LowImportance
	return &savedRouteRow{label: label, object: container.NewBorder(nil, nil, nil, button, label)}
}
func routeLabel(route model.Route, v model.View) string {
	name := route.Device
	for _, device := range v.AllDevices {
		if device.Name == name {
			name = device.Label
			break
		}
	}
	direction := "Out"
	if route.Direction == model.RouteInput {
		direction = "In"
	}
	return route.Name + "  " + direction + " → " + name
}

type mixer struct {
	window             fyne.Window
	output, input      *deviceRow
	apps               map[string]*appRow
	routes             map[string]*savedRouteRow
	appList, routeList *fyne.Container
	routeSection       *fyne.Container
	appKeys, routeKeys []string
	status             *widget.Label
	dispatch           func(Msg)
	visible            bool
}

var (
	cardColor    = color.RGBA{0x37, 0x3a, 0x41, 0xff}
	sectionColor = color.RGBA{0x8d, 0x93, 0x9e, 0xff}
	accentColor  = color.RGBA{0x7a, 0xa2, 0xd4, 0xff}
	headerColor  = color.RGBA{0x24, 0x26, 0x2b, 0xff}
)

// card draws objects on a rounded, slightly lighter panel.
func card(objects ...fyne.CanvasObject) fyne.CanvasObject {
	background := canvas.NewRectangle(cardColor)
	background.CornerRadius = 8
	return container.NewStack(background, container.NewPadded(container.NewVBox(objects...)))
}

// sectionTitle is a small upper-case heading above a group of cards.
func sectionTitle(text string) fyne.CanvasObject {
	title := canvas.NewText(strings.ToUpper(text), sectionColor)
	title.TextSize = 11
	title.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewPadded(title)
}

func newMixer(app fyne.App, dispatch func(Msg)) *mixer {
	// A regular window: Fyne recenters splash windows every time they are shown.
	window := app.NewWindow(mixerTitle)
	m := &mixer{window: window, output: newDeviceRow(pulse.Output, dispatch), input: newDeviceRow(pulse.Input, dispatch), apps: make(map[string]*appRow), routes: make(map[string]*savedRouteRow), appList: container.NewVBox(), routeList: container.NewVBox(), status: widget.NewLabel(""), dispatch: dispatch}
	m.status.Truncation = fyne.TextTruncateEllipsis
	m.status.Importance = widget.DangerImportance
	// Sway draws no title bar for this window, so the header moves the window and
	// carries the hide button.
	hideButton := widget.NewButtonWithIcon("", theme.WindowCloseIcon(), m.hide)
	hideButton.Importance = widget.LowImportance
	resetButton := widget.NewButtonWithIcon("Reset apps", theme.ViewRefreshIcon(), func() {
		dialog.ShowConfirm("Reset apps",
			"Send every app to the default output and input, and forget all saved app devices?",
			func(confirmed bool) {
				if confirmed {
					dispatch(ResetApps{})
				}
			}, window)
	})
	resetButton.Importance = widget.LowImportance
	icon := canvas.NewImageFromResource(theme.NewPrimaryThemedResource(theme.VolumeUpIcon()))
	icon.SetMinSize(fyne.NewSize(20, 20))
	icon.FillMode = canvas.ImageFillContain
	title := widget.NewLabelWithStyle(mixerTitle, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	// Only the title area starts a window move, so the buttons keep their taps.
	moveArea := newMoveBar(window, container.NewPadded(container.NewHBox(container.NewCenter(icon), title)))
	headerBackground := canvas.NewRectangle(headerColor)
	headerRow := container.NewStack(headerBackground, container.NewBorder(nil, nil, nil, container.NewPadded(container.NewHBox(resetButton, hideButton)), moveArea))
	accentLine := canvas.NewRectangle(accentColor)
	accentLine.SetMinSize(fyne.NewSize(0, 2))
	header := container.NewVBox(headerRow, accentLine)
	m.routeSection = container.NewVBox(sectionTitle("Saved devices for apps not running"), card(m.routeList))
	m.routeSection.Hide()
	content := container.NewVBox(
		sectionTitle("Devices"), card(m.output.object, m.input.object),
		sectionTitle("Apps"), m.appList,
		m.routeSection,
	)
	body := container.NewPadded(container.NewVScroll(content))
	window.SetContent(container.NewBorder(header, container.NewPadded(m.status), nil, nil, body))
	window.Resize(fyne.NewSize(780, 560))
	window.SetCloseIntercept(m.hide)
	return m
}
func (m *mixer) show() {
	m.visible = true
	m.window.Show()
	m.window.RequestFocus()
	go keepMixerInsideUsableArea()
}
func (m *mixer) hide() { m.visible = false; m.window.Hide() }
func (m *mixer) toggle() {
	if m.visible {
		m.hide()
	} else {
		m.show()
	}
}
func (m *mixer) update(state renderedState) {
	v := state.View
	m.output.update(v.Outputs, v.OutputVolumePct, v.OutputMute)
	m.input.update(v.Inputs, v.InputVolumePct, v.InputMute)
	keys := make([]string, 0, len(v.Apps))
	running := map[string]bool{}
	for _, a := range v.Apps {
		if !a.Running {
			continue
		}
		running[a.Key] = true
		keys = append(keys, a.Key)
		row := m.apps[a.Key]
		if row == nil {
			row = newAppRow(a.Key, m.dispatch)
			m.apps[a.Key] = row
		}
		row.update(a, v)
	}
	if !sameKeys(keys, m.appKeys) {
		objects := make([]fyne.CanvasObject, 0, len(keys))
		for _, key := range keys {
			objects = append(objects, m.apps[key].object)
		}
		m.appList.Objects = objects
		m.appList.Refresh()
		m.appKeys = keys
		for key := range m.apps {
			if !running[key] {
				delete(m.apps, key)
			}
		}
	}
	routeKeys := make([]string, 0)
	for _, route := range state.Config.Routes {
		if running[route.App] {
			continue
		}
		key := route.App + "\x00" + route.Direction
		routeKeys = append(routeKeys, key)
		row := m.routes[key]
		if row == nil {
			row = newSavedRouteRow(route, m.dispatch)
			m.routes[key] = row
		}
		row.label.SetText(routeLabel(route, v))
	}
	if !sameKeys(routeKeys, m.routeKeys) {
		objects := make([]fyne.CanvasObject, 0, len(routeKeys))
		for _, key := range routeKeys {
			objects = append(objects, m.routes[key].object)
		}
		m.routeList.Objects = objects
		m.routeList.Refresh()
		m.routeKeys = routeKeys
		for key := range m.routes {
			if !slices.Contains(routeKeys, key) {
				delete(m.routes, key)
			}
		}
	}
	if len(routeKeys) == 0 {
		m.routeSection.Hide()
	} else {
		m.routeSection.Show()
	}
	m.status.SetText(state.Error)
}

type DarkTheme struct{}

func (*DarkTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.RGBA{0x2c, 0x2e, 0x33, 0xff}
	case theme.ColorNameButton:
		return color.RGBA{0x43, 0x47, 0x4f, 0xff}
	case theme.ColorNameInputBackground, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return color.RGBA{0x27, 0x29, 0x2e, 0xff}
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return color.RGBA{0x48, 0x4c, 0x55, 0xff}
	case theme.ColorNameForeground:
		return color.RGBA{0xd3, 0xd7, 0xde, 0xff}
	case theme.ColorNamePlaceHolder:
		return color.RGBA{0x8d, 0x93, 0x9e, 0xff}
	case theme.ColorNamePrimary, theme.ColorNameFocus:
		return accentColor
	case theme.ColorNameHover:
		return color.RGBA{0x4c, 0x51, 0x5a, 0xff}
	case theme.ColorNameDisabled:
		return color.RGBA{0x7a, 0x7f, 0x88, 0xff}
	case theme.ColorNameWarning:
		return color.RGBA{0xe5, 0xa5, 0x4b, 0xff}
	default:
		return theme.DefaultTheme().Color(name, theme.VariantDark)
	}
}
func (*DarkTheme) Font(style fyne.TextStyle) fyne.Resource    { return theme.DefaultTheme().Font(style) }
func (*DarkTheme) Icon(name fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(name) }
func (*DarkTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 6
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameScrollBar:
		return 10
	case theme.SizeNameScrollBarSmall:
		return 4
	default:
		return theme.DefaultTheme().Size(name)
	}
}
func routeKey(route model.Route) string {
	return strings.Join([]string{route.App, route.Direction}, "\x00")
}
func sameKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if slices.Equal(a, b) {
		return true
	}
	for _, key := range a {
		if !slices.Contains(b, key) {
			return false
		}
	}
	return true
}
