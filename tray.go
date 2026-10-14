package main

import (
	_ "embed"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/systray"
	"soundquay/internal/model"
	"soundquay/internal/pulse"
)

//go:embed icons/volume.png
var volumePNG []byte

//go:embed icons/muted.png
var mutedPNG []byte

var volumeIcon = fyne.NewStaticResource("volume.png", volumePNG)
var mutedIcon = fyne.NewStaticResource("muted.png", mutedPNG)

type tray struct {
	app                     desktop.App
	dispatch                func(Msg)
	show, preferences, quit func()
	key                     string
	icon                    fyne.Resource
}

func newTray(app fyne.App, dispatch func(Msg), toggle, show, preferences, quit func()) *tray {
	desktopApp, ok := app.(desktop.App)
	if !ok {
		return nil
	}
	// Share visibility tracking with socket commands instead of the driver's private toggle.
	systray.SetOnTapped(func() { fyne.Do(toggle) })
	return &tray{app: desktopApp, dispatch: dispatch, show: show, preferences: preferences, quit: quit}
}
func trayKey(view model.View) string {
	var b strings.Builder
	add := func(value string) { b.WriteString(strconv.Itoa(len(value))); b.WriteByte(':'); b.WriteString(value) }
	for _, entries := range [][]model.SwitchEntry{view.Outputs, view.Inputs} {
		add("devices")
		for _, entry := range entries {
			add(entry.ID)
			add(entry.Label)
			add(entry.DeviceName)
			add(entry.CardName)
			add(entry.PortName)
			add(entry.Profile)
			add(strconv.FormatBool(entry.Active))
		}
	}
	for _, choices := range [][]model.DeviceChoice{view.OutputDevices, view.InputDevices} {
		add("choices")
		for _, choice := range choices {
			add(choice.Name)
			add(choice.Label)
		}
	}
	for _, a := range view.Apps {
		add(a.Key)
		add(a.Name)
		add(a.OutputRoute)
		add(a.InputRoute)
		add(strconv.FormatBool(a.Routable))
		add(strconv.FormatBool(len(a.Inputs) > 0 || a.InputRoute != ""))
	}
	add(strconv.FormatBool(view.OutputMute))
	add(strconv.FormatBool(view.InputMute))
	return b.String()
}
func (t *tray) update(view model.View) {
	if t == nil {
		return
	}
	key := trayKey(view)
	if key == t.key {
		return
	}
	t.key = key
	var items []*fyne.MenuItem
	for i, entries := range [][]model.SwitchEntry{view.Outputs, view.Inputs} {
		title := "Output"
		if i == 1 {
			title = "Input"
		}
		heading := fyne.NewMenuItem(title, nil)
		heading.Disabled = true
		items = append(items, heading)
		for _, entry := range entries {
			item := fyne.NewMenuItem(entry.Label, func() { t.dispatch(SelectDevice{Entry: entry}) })
			item.Checked = entry.Active
			items = append(items, item)
		}
	}
	items = append(items, fyne.NewMenuItemSeparator())
	apps := fyne.NewMenuItem("Apps", nil)
	appItems := make([]*fyne.MenuItem, 0, len(view.Apps))
	for _, a := range view.Apps {
		output := t.appChoices(a, pulse.Output, view.OutputDevices, a.OutputRoute)
		directions := []*fyne.MenuItem{output}
		if len(a.Inputs) > 0 || a.InputRoute != "" {
			directions = append(directions, t.appChoices(a, pulse.Input, view.InputDevices, a.InputRoute))
		}
		item := fyne.NewMenuItem(a.Name, nil)
		item.ChildMenu = fyne.NewMenu(a.Name, directions...)
		appItems = append(appItems, item)
	}
	apps.ChildMenu = fyne.NewMenu("Apps", appItems...)
	apps.Disabled = len(appItems) == 0
	items = append(items, apps, fyne.NewMenuItemSeparator())
	outputMute := fyne.NewMenuItem("Mute output", func() { t.dispatch(SetMuted{Direction: pulse.Output, Muted: !view.OutputMute}) })
	outputMute.Checked = view.OutputMute
	inputMute := fyne.NewMenuItem("Mute microphone", func() { t.dispatch(SetMuted{Direction: pulse.Input, Muted: !view.InputMute}) })
	inputMute.Checked = view.InputMute
	items = append(items, outputMute, inputMute, fyne.NewMenuItem("Open mixer…", t.show), fyne.NewMenuItem("Preferences…", t.preferences), fyne.NewMenuItem("Quit", t.quit))
	icon := volumeIcon
	if view.OutputMute {
		icon = mutedIcon
	}
	if t.icon != icon {
		t.app.SetSystemTrayIcon(icon)
		t.icon = icon
	}
	t.app.SetSystemTrayMenu(fyne.NewMenu("Audio Indicator", items...))
}
func (t *tray) appChoices(a model.App, d pulse.Direction, choices []model.DeviceChoice, selected string) *fyne.MenuItem {
	title := "Output"
	if d == pulse.Input {
		title = "Input"
	}
	names, labels := choiceOptions(choices, selected)
	items := make([]*fyne.MenuItem, 0, len(names))
	for i, name := range names {
		item := fyne.NewMenuItem(labels[i], func() { t.dispatch(ChooseAppDevice{App: a.Key, Name: a.Name, Direction: d, Device: name}) })
		item.Checked = name == selected
		item.Disabled = !a.Routable
		items = append(items, item)
	}
	result := fyne.NewMenuItem(title, nil)
	result.ChildMenu = fyne.NewMenu(title, items...)
	return result
}
