package main

import (
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"soundquay/internal/model"
)

type preferenceRow struct {
	name      string
	label     *widget.Label
	hidden    *widget.Check
	nickname  *widget.Entry
	updating  bool
	changedAt time.Time
	object    fyne.CanvasObject
}

func newPreferenceRow(device model.DeviceChoice, dispatch func(Msg)) *preferenceRow {
	r := &preferenceRow{name: device.Name, label: widget.NewLabel(device.Label), nickname: widget.NewEntry()}
	r.nickname.SetPlaceHolder("Nickname")
	r.hidden = widget.NewCheck("Hide", func(hidden bool) {
		if !r.updating {
			dispatch(DevicePreference{Device: r.name, Nickname: r.nickname.Text, Hidden: hidden})
		}
	})
	// Nicknames are saved on Enter or the Save button, not on every keystroke.
	save := func() {
		dispatch(DevicePreference{Device: r.name, Nickname: r.nickname.Text, Hidden: r.hidden.Checked})
	}
	r.nickname.OnChanged = func(string) {
		if !r.updating {
			r.changedAt = time.Now()
		}
	}
	r.nickname.OnSubmitted = func(string) { save() }
	r.object = container.NewVBox(r.label, container.NewBorder(nil, nil, r.hidden, widget.NewButton("Save", save), r.nickname))
	return r
}

type preferences struct {
	window                fyne.Window
	devices               map[string]*preferenceRow
	routes                map[string]*savedRouteRow
	deviceList, routeList *fyne.Container
	deviceKeys, routeKeys []string
	dispatch              func(Msg)
}

func newPreferences(app fyne.App, dispatch func(Msg)) *preferences {
	p := &preferences{window: app.NewWindow("Audio Preferences"), devices: make(map[string]*preferenceRow), routes: make(map[string]*savedRouteRow), deviceList: container.NewVBox(), routeList: container.NewVBox(), dispatch: dispatch}
	p.window.SetContent(container.NewVScroll(container.NewVBox(widget.NewLabel("Devices"), p.deviceList, widget.NewSeparator(), widget.NewLabel("Saved routes"), p.routeList)))
	p.window.Resize(fyne.NewSize(650, 500))
	p.window.SetCloseIntercept(p.window.Hide)
	return p
}
func (p *preferences) update(state renderedState) {
	keys := make([]string, 0, len(state.View.AllDevices))
	for _, device := range state.View.AllDevices {
		keys = append(keys, device.Name)
		row := p.devices[device.Name]
		if row == nil {
			row = newPreferenceRow(device, p.dispatch)
			p.devices[device.Name] = row
		}
		row.updating = true
		row.label.SetText(device.Label)
		row.hidden.SetChecked(slices.Contains(state.Config.HiddenDevices, device.Name))
		if row.changedAt.IsZero() || row.nickname.Text == state.Config.Nicknames[device.Name] {
			row.nickname.SetText(state.Config.Nicknames[device.Name])
			row.changedAt = time.Time{}
		}
		row.updating = false
	}
	if !sameKeys(keys, p.deviceKeys) {
		objects := make([]fyne.CanvasObject, 0, len(keys))
		for _, key := range keys {
			objects = append(objects, p.devices[key].object)
		}
		p.deviceList.Objects = objects
		p.deviceList.Refresh()
		p.deviceKeys = keys
		for key := range p.devices {
			if !slices.Contains(keys, key) {
				delete(p.devices, key)
			}
		}
	}
	routeKeys := make([]string, 0, len(state.Config.Routes))
	for _, route := range state.Config.Routes {
		key := routeKey(route)
		routeKeys = append(routeKeys, key)
		row := p.routes[key]
		if row == nil {
			row = newSavedRouteRow(route, p.dispatch)
			p.routes[key] = row
		}
		row.label.SetText(routeLabel(route, state.View))
	}
	if !sameKeys(routeKeys, p.routeKeys) {
		objects := make([]fyne.CanvasObject, 0, len(routeKeys))
		for _, key := range routeKeys {
			objects = append(objects, p.routes[key].object)
		}
		p.routeList.Objects = objects
		p.routeList.Refresh()
		p.routeKeys = routeKeys
		for key := range p.routes {
			if !slices.Contains(routeKeys, key) {
				delete(p.routes, key)
			}
		}
	}
}
