package model

import (
	"cmp"
	"slices"
	"strings"

	"soundquay/internal/pulse"
)

// Build derives switch choices and application rows without changing its inputs.
func Build(s pulse.Snapshot, c Config) View {
	v := View{DefaultOutput: s.Info.DefaultSinkName, DefaultInput: s.Info.DefaultSourceName}
	hidden := make(map[string]bool, len(c.HiddenDevices))
	for _, name := range c.HiddenDevices {
		hidden[name] = true
	}
	outputs := make(map[uint32]string, len(s.Sinks))
	inputs := make(map[uint32]string, len(s.Sources))
	for _, d := range s.Sinks {
		outputs[d.Index] = d.Name
		if d.Name == v.DefaultOutput {
			v.OutputVolumePct, v.OutputMute = d.VolumePct, d.Mute
		}
	}
	for _, d := range s.Sources {
		inputs[d.Index] = d.Name
		if d.Name == v.DefaultInput {
			v.InputVolumePct, v.InputMute = d.VolumePct, d.Mute
		}
	}
	v.Outputs, v.OutputDevices = deviceEntries(s.Sinks, s.Cards, pulse.Output, v.DefaultOutput, hidden, c.Nicknames)
	v.Inputs, v.InputDevices = deviceEntries(s.Sources, s.Cards, pulse.Input, v.DefaultInput, hidden, c.Nicknames)
	for _, devices := range [][]pulse.Device{s.Sinks, s.Sources} {
		for _, d := range devices {
			if !monitor(d) {
				v.AllDevices = append(v.AllDevices, DeviceChoice{Name: d.Name, Label: deviceLabel(d, c.Nicknames)})
			}
		}
	}
	sortChoices(v.AllDevices)

	apps := make(map[string]int)
	latest := make(map[string]uint32)
	for _, route := range c.Routes {
		index, exists := apps[route.App]
		if !exists {
			index = len(v.Apps)
			apps[route.App] = index
			v.Apps = append(v.Apps, App{Key: route.App, Name: route.Name, Routable: !strings.HasPrefix(route.App, "node:")})
		}
		app := &v.Apps[index]
		if app.Name == "" {
			app.Name = route.Name
		}
		switch route.Direction {
		case RouteOutput:
			app.OutputRoute = route.Device
		case RouteInput:
			app.InputRoute = route.Device
		}
	}
	addStreams := func(streams []pulse.Stream, direction pulse.Direction, names map[uint32]string) {
		for _, stream := range streams {
			if !isApp(stream.HasOwner, stream.Properties) {
				continue
			}
			key, routable := AppKey(stream.Properties)
			index, exists := apps[key]
			if !exists {
				index = len(v.Apps)
				apps[key] = index
				v.Apps = append(v.Apps, App{Key: key})
			}
			app := &v.Apps[index]
			app.Running, app.Routable = true, routable
			if index, ok := latest[key]; !ok || stream.Index > index {
				latest[key] = stream.Index
				app.Name = stream.Properties["application.name"]
				if app.Name == "" {
					route, ok := c.RouteFor(key, RouteOutput)
					if !ok || route.Name == "" {
						route, _ = c.RouteFor(key, RouteInput)
					}
					app.Name = route.Name
				}
				if app.Name == "" {
					app.Name = stream.Properties["node.name"]
					if app.Name == "" {
						app.Name = key
					}
				}
			}
			item := AppStream{Direction: direction, Index: stream.Index, DeviceName: names[stream.DeviceIndex], VolumePct: stream.VolumePct, Mute: stream.Mute, MediaName: stream.Properties["media.name"]}
			if direction == pulse.Output {
				app.Outputs = append(app.Outputs, item)
			} else {
				app.Inputs = append(app.Inputs, item)
			}
		}
	}
	addStreams(s.Outputs, pulse.Output, outputs)
	addStreams(s.Inputs, pulse.Input, inputs)
	lowerNames := make(map[string]string, len(v.Apps))
	for i := range v.Apps {
		app := &v.Apps[i]
		if app.Name == "" {
			app.Name = app.Key
		}
		slices.SortFunc(app.Outputs, func(a, b AppStream) int { return cmp.Compare(a.Index, b.Index) })
		slices.SortFunc(app.Inputs, func(a, b AppStream) int { return cmp.Compare(a.Index, b.Index) })
		lowerNames[app.Key] = strings.ToLower(app.Name)
	}
	slices.SortFunc(v.Apps, func(a, b App) int {
		if order := strings.Compare(lowerNames[a.Key], lowerNames[b.Key]); order != 0 {
			return order
		}
		return strings.Compare(a.Key, b.Key)
	})
	return v
}

func monitor(d pulse.Device) bool {
	return d.IsMonitor || d.Properties["device.class"] == "monitor" || strings.HasSuffix(d.Name, ".monitor")
}

func deviceLabel(d pulse.Device, nicknames map[string]string) string {
	if name := nicknames[d.Name]; name != "" {
		return name
	}
	if d.Description != "" {
		return d.Description
	}
	return d.Name
}

func sortChoices(choices []DeviceChoice) {
	slices.SortFunc(choices, func(a, b DeviceChoice) int {
		if order := strings.Compare(a.Label, b.Label); order != 0 {
			return order
		}
		return strings.Compare(a.Name, b.Name)
	})
}

func deviceEntries(devices []pulse.Device, cards []pulse.Card, direction pulse.Direction, defaultName string, hidden map[string]bool, nicknames map[string]string) ([]SwitchEntry, []DeviceChoice) {
	var entries []SwitchEntry
	var choices []DeviceChoice
	byCard := make(map[string][]pulse.Device)
	knownCards := make(map[string]bool, len(cards))
	for _, card := range cards {
		knownCards[card.Name] = true
	}
	for _, d := range devices {
		if monitor(d) {
			continue
		}
		byCard[d.CardName] = append(byCard[d.CardName], d)
		if hidden[d.Name] && d.Name != defaultName {
			continue
		}
		label := deviceLabel(d, nicknames)
		choices = append(choices, DeviceChoice{Name: d.Name, Label: label})
		if d.CardName == "" || !knownCards[d.CardName] {
			entries = append(entries, SwitchEntry{Direction: direction, ID: d.Name, Label: label, DeviceName: d.Name, CardName: d.CardName, Active: d.Name == defaultName})
		}
	}
	for _, card := range cards {
		current := byCard[card.Name]
		if len(current) != 0 && !slices.ContainsFunc(current, func(d pulse.Device) bool { return !hidden[d.Name] || d.Name == defaultName }) {
			continue
		}
		start := len(entries)
		for _, port := range card.Ports {
			if port.Direction != direction || port.Availability == pulse.NotAvailable {
				continue
			}
			entry := SwitchEntry{Direction: direction, ID: "port:" + card.Name + ":" + port.Name, CardName: card.Name, PortName: port.Name, Label: port.Description}
			if entry.Label == "" {
				entry.Label = port.Name
			}
			var device *pulse.Device
			hasCurrentDevice := false
			for i := range current {
				d := &current[i]
				if slices.ContainsFunc(d.Ports, func(p pulse.DevicePort) bool { return p.Name == port.Name }) {
					hasCurrentDevice = true
					if hidden[d.Name] && d.Name != defaultName {
						continue
					}
					if device == nil || d.Name == defaultName || d.Name < device.Name && device.Name != defaultName {
						device = d
					}
				}
			}
			if device != nil {
				entry.DeviceName = device.Name
				entry.Active = device.Name == defaultName && device.ActivePort == port.Name
				entry.Label = deviceLabel(*device, nicknames)
			} else {
				if hasCurrentDevice || port.Availability != pulse.Available {
					continue
				}
				entry.Profile = chooseProfile(card, port, direction)
				if entry.Profile == "" {
					continue
				}
			}
			entries = append(entries, entry)
		}
		if len(entries)-start > 1 {
			for i := start; i < len(entries); i++ {
				if nicknames[entries[i].DeviceName] != "" {
					continue
				}
				for _, port := range card.Ports {
					if port.Name == entries[i].PortName {
						cardName := card.Description
						if cardName == "" {
							cardName = card.Name
						}
						portName := port.Description
						if portName == "" {
							portName = port.Name
						}
						entries[i].Label = cardName + ": " + portName
						break
					}
				}
			}
		}
	}
	slices.SortFunc(entries, func(a, b SwitchEntry) int {
		if order := strings.Compare(a.Label, b.Label); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	sortChoices(choices)
	return entries, choices
}

func chooseProfile(card pulse.Card, port pulse.CardPort, direction pulse.Direction) string {
	other := profilePart(card.ActiveProfile, pulse.Output)
	if direction == pulse.Output {
		other = profilePart(card.ActiveProfile, pulse.Input)
	}
	var best pulse.CardProfile
	found, keepsOther := false, false
	for _, profile := range card.Profiles {
		if !profile.Available || profile.Name == "off" || profile.Name == "pro-audio" || !slices.Contains(port.Profiles, profile.Name) {
			continue
		}
		candidateOther := profilePart(profile.Name, pulse.Output)
		if direction == pulse.Output {
			candidateOther = profilePart(profile.Name, pulse.Input)
		}
		keeps := other != "" && other == candidateOther
		if !found || keeps && !keepsOther || keeps == keepsOther && (profile.Priority > best.Priority || profile.Priority == best.Priority && profile.Name < best.Name) {
			best, found, keepsOther = profile, true, keeps
		}
	}
	return best.Name
}

func profilePart(profile string, direction pulse.Direction) string {
	prefix := "output:"
	if direction == pulse.Input {
		prefix = "input:"
	}
	for part := range strings.SplitSeq(profile, "+") {
		if strings.HasPrefix(part, prefix) {
			return part
		}
	}
	return ""
}
