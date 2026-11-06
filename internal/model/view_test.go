package model

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"soundquay/internal/pulse"
)

func entryFor(t *testing.T, entries []SwitchEntry, card, port string) SwitchEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.CardName == card && entry.PortName == port {
			return entry
		}
	}
	t.Fatalf("missing entry for %s/%s: %+v", card, port, entries)
	return SwitchEntry{}
}

func TestBuildDeviceChoices(t *testing.T) {
	s := machineSnapshot()
	s.Info.DefaultSinkName = hdmiSink
	s.Sinks[0].Mute = true
	unavailable := pulse.CardPort{Name: "hdmi-output-1", Description: "HDMI / DisplayPort 2", Direction: pulse.Output, Availability: pulse.NotAvailable, Profiles: []string{"output:hdmi-stereo+input:analog-stereo"}}
	s.Cards[0].Ports = append(s.Cards[0].Ports, unavailable)
	s.Cards[0].Ports = append(s.Cards[0].Ports, pulse.CardPort{Name: "unused", Direction: pulse.Output, Availability: pulse.Available, Profiles: []string{"off", "pro-audio", "unavailable-profile"}})
	s.Cards[0].Profiles = append(s.Cards[0].Profiles, pulse.CardProfile{Name: "off", Available: true, Priority: 99999}, pulse.CardProfile{Name: "pro-audio", Available: true, Priority: 99999}, pulse.CardProfile{Name: "unavailable-profile", Available: false, Priority: 99999})
	c := Config{Version: ConfigVersion, HiddenDevices: []string{loopSink, loopSource, hdmiSink}, Nicknames: map[string]string{jabraSink: "Headset", ankerSource: "Desk microphone"}}
	v := Build(s, c)
	if v.DefaultOutput != hdmiSink || v.OutputVolumePct != 85 || !v.OutputMute || v.DefaultInput != jabraSource || v.InputVolumePct != 85 {
		t.Fatalf("default values: %+v", v)
	}
	hdmi := entryFor(t, v.Outputs, pciCard, "hdmi-output-0")
	if hdmi.Label != "Built-in Audio: HDMI / DisplayPort" || !hdmi.Active || hdmi.DeviceName != hdmiSink || hdmi.Profile != "" {
		t.Fatalf("HDMI entry: %+v", hdmi)
	}
	headphones := entryFor(t, v.Outputs, pciCard, "analog-output-headphones")
	if headphones.Label != "Built-in Audio: Headphones" || headphones.DeviceName != "" || headphones.Profile != "output:analog-stereo+input:analog-stereo" || headphones.Active {
		t.Fatalf("headphones entry does not preserve microphone: %+v", headphones)
	}
	jabra := entryFor(t, v.Outputs, jabraCard, "analog-output")
	if jabra.Label != "Headset" || jabra.Active {
		t.Fatalf("headset entry: %+v", jabra)
	}
	for _, entry := range v.Outputs {
		if entry.CardName == loopCard || entry.PortName == "hdmi-output-1" || entry.PortName == "unused" {
			t.Fatalf("hidden or unavailable entry: %+v", entry)
		}
	}
	for _, choices := range [][]DeviceChoice{v.OutputDevices, v.InputDevices} {
		for _, choice := range choices {
			if choice.Name == loopSink || choice.Name == loopSource {
				t.Fatalf("hidden picker choice: %+v", choice)
			}
		}
	}
	for _, choices := range [][]DeviceChoice{v.InputDevices, v.AllDevices} {
		for _, choice := range choices {
			if slices.ContainsFunc(s.Sources, func(d pulse.Device) bool { return d.Name == choice.Name && d.IsMonitor }) {
				t.Fatalf("monitor listed: %+v", choice)
			}
		}
	}
	if len(v.AllDevices) != 7 || len(v.OutputDevices) != 2 || len(v.InputDevices) != 3 || len(v.Outputs) != 3 || len(v.Inputs) != 3 {
		t.Fatalf("choice counts: %+v", v)
	}
	if !slices.Contains(v.InputDevices, DeviceChoice{Name: ankerSource, Label: "Desk microphone"}) {
		t.Fatalf("missing microphone nickname: %+v", v.InputDevices)
	}
}

func TestBuildDefaultAndAvailablePorts(t *testing.T) {
	s := machineSnapshot()
	s.Sinks[0].Ports = append(s.Sinks[0].Ports, pulse.DevicePort{Name: "analog-output-headphones", Availability: pulse.Available})
	s.Sinks[0].ActivePort = "analog-output-headphones"
	s.Info.DefaultSinkName = hdmiSink
	v := Build(s, Config{})
	if entryFor(t, v.Outputs, pciCard, "hdmi-output-0").Active || !entryFor(t, v.Outputs, pciCard, "analog-output-headphones").Active {
		t.Fatalf("active port not used: %+v", v.Outputs)
	}
	s.Cards[0].Ports[1].Availability = pulse.NotAvailable
	v = Build(s, Config{Nicknames: map[string]string{hdmiSink: "Odyssey G5"}})
	if entryFor(t, v.Outputs, pciCard, "hdmi-output-0").Label != "Odyssey G5" {
		t.Fatalf("device nickname missing: %+v", v.Outputs)
	}
}

func TestBuildCardlessDevicesAndFilterSources(t *testing.T) {
	s := pulse.Snapshot{Info: pulse.ServerInfo{DefaultSourceName: "echo-cancel-source"}, Sources: []pulse.Device{
		{Direction: pulse.Input, Index: 10, Name: "echo-cancel-source", Description: "Clean microphone", HasOwner: true, Properties: map[string]string{"node.virtual": "true"}},
		{Direction: pulse.Input, Index: 11, Name: "other", Properties: map[string]string{"device.class": "monitor"}},
		{Direction: pulse.Input, Index: 12, Name: "something.monitor"},
	}}
	v := Build(s, Config{HiddenDevices: []string{"echo-cancel-source"}})
	want := []SwitchEntry{{Direction: pulse.Input, ID: "echo-cancel-source", DeviceName: "echo-cancel-source", Label: "Clean microphone", Active: true}}
	if !reflect.DeepEqual(v.Inputs, want) || len(v.InputDevices) != 1 || len(v.AllDevices) != 1 {
		t.Fatalf("filter source is not an input choice: %+v", v)
	}
}

func TestBuildGroupsLiveAndSavedApps(t *testing.T) {
	s := machineSnapshot()
	playback := chromeStream(pulse.Output, 200, 88)
	capture := chromeStream(pulse.Input, 210, 91)
	capture.Properties["application.name"] = "Google Chrome input"
	capture.Mute = true
	internal := chromeStream(pulse.Output, 300, 88)
	internal.HasOwner = true
	virtual := chromeStream(pulse.Input, 400, 91)
	virtual.Properties["node.virtual"] = "true"
	s.Outputs = []pulse.Stream{playback, internal, {Index: 100, DeviceIndex: 77, Properties: map[string]string{"node.name": "Raw sound"}}}
	s.Inputs = []pulse.Stream{capture, virtual}
	c := Config{Routes: []Route{{App: "chrome", Name: "Old Chrome name", Direction: RouteInput, Device: ankerSource}, {App: "firefox", Name: "Firefox", Direction: RouteOutput, Device: hdmiSink}, {App: "a", Name: "firefox", Direction: RouteOutput, Device: jabraSink}}}
	v := Build(s, c)
	if len(v.Apps) != 4 {
		t.Fatalf("apps: %+v", v.Apps)
	}
	keys := make([]string, 0, len(v.Apps))
	for _, app := range v.Apps {
		keys = append(keys, app.Key)
	}
	if !reflect.DeepEqual(keys, []string{"a", "firefox", "chrome", "node:Raw sound"}) {
		t.Fatalf("apps not sorted: %v", keys)
	}
	chrome := v.Apps[2]
	if chrome.Name != "Google Chrome input" || !chrome.Running || !chrome.Routable || chrome.InputRoute != ankerSource || len(chrome.Outputs) != 1 || len(chrome.Inputs) != 1 {
		t.Fatalf("chrome row: %+v", chrome)
	}
	if chrome.Outputs[0].DeviceName != jabraSink || chrome.Outputs[0].VolumePct != 105 || chrome.Outputs[0].MediaName != "Playback" || chrome.Inputs[0].DeviceName != ankerSource || !chrome.Inputs[0].Mute {
		t.Fatalf("stream values: %+v", chrome)
	}
	if v.Apps[1].Running || v.Apps[1].Name != "Firefox" || v.Apps[1].OutputRoute != hdmiSink {
		t.Fatalf("saved app: %+v", v.Apps[1])
	}
	if v.Apps[3].Routable || !v.Apps[3].Running || v.Apps[3].Name != "Raw sound" {
		t.Fatalf("unidentified stream: %+v", v.Apps[3])
	}
}

func TestBuildKeepsBrowserOutputsDistinct(t *testing.T) {
	s := machineSnapshot()
	first := chromeStream(pulse.Output, 200, 88)
	first.Properties["media.name"] = "Music"
	second := chromeStream(pulse.Output, 201, 77)
	second.Properties["media.name"] = "Video"
	second.VolumePct, second.Mute = 40, true
	s.Outputs = []pulse.Stream{second, first}
	s.Inputs = []pulse.Stream{chromeStream(pulse.Input, 210, 91)}
	v := Build(s, Config{})
	if len(v.Apps) != 1 {
		t.Fatalf("browser grouping changed: %+v", v.Apps)
	}
	app := v.Apps[0]
	want := []AppStream{
		{Direction: pulse.Output, Index: 200, DeviceName: jabraSink, VolumePct: 105, MediaName: "Music"},
		{Direction: pulse.Output, Index: 201, DeviceName: hdmiSink, VolumePct: 40, Mute: true, MediaName: "Video"},
	}
	if app.Key != "chrome" || !app.SeparateOutputs || !reflect.DeepEqual(app.Outputs, want) || len(app.Inputs) != 1 {
		t.Fatalf("browser streams: %+v", app)
	}
}

func TestBuildRecognizesBrowserOutputIdentities(t *testing.T) {
	tests := []struct {
		property string
		names    []string
	}{
		{"application.id", []string{"net.imput.helium", "com.google.Chrome", "org.chromium.Chromium", "org.mozilla.firefox"}},
		{"application.process.binary", []string{"helium", "helium-browser", "chrome", "google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "firefox", "firefox-esr", "/usr/bin/firefox"}},
		{"application.name", []string{"Helium", "Chrome", "Google Chrome", "Chromium", "Firefox", "Mozilla Firefox"}},
	}
	for _, tt := range tests {
		for _, name := range tt.names {
			for _, value := range []string{name, strings.ToUpper(name), " " + name + " "} {
				t.Run(tt.property+"/"+value, func(t *testing.T) {
					props := map[string]string{tt.property: value}
					s := pulse.Snapshot{Outputs: []pulse.Stream{{Index: 10, Properties: props}}}
					v := Build(s, Config{})
					if len(v.Apps) != 1 || !v.Apps[0].SeparateOutputs || v.Apps[0].Key != value {
						t.Fatalf("browser detection changed identity or missed output: %+v", v.Apps)
					}
				})
			}
		}
	}
}

func TestBuildOnlySeparatesSupportedBrowserPlayback(t *testing.T) {
	for _, name := range []string{"Spotify", "My Firefox Player", "helium-helper", "not-google-chrome", "org.mozilla.firefox.Other", "org.chromium.ChromiumExtra", "net.imput.helium.other", "brave", ""} {
		t.Run(name, func(t *testing.T) {
			props := map[string]string{"application.id": name, "application.process.binary": name, "application.name": name}
			v := Build(pulse.Snapshot{Outputs: []pulse.Stream{{Index: 10, Properties: props}}}, Config{})
			if len(v.Apps) != 1 || v.Apps[0].SeparateOutputs {
				t.Fatalf("ordinary app has separate browser outputs: %+v", v.Apps)
			}
		})
	}
	props := map[string]string{"application.id": "org.mozilla.firefox"}
	v := Build(pulse.Snapshot{Inputs: []pulse.Stream{{Index: 10, Properties: props}}}, Config{})
	if len(v.Apps) != 1 || v.Apps[0].SeparateOutputs {
		t.Fatalf("capture-only browser has separate outputs: %+v", v.Apps)
	}
	v = Build(pulse.Snapshot{}, Config{Routes: []Route{{App: "org.mozilla.firefox", Name: "Firefox", Direction: RouteOutput, Device: hdmiSink}}})
	if len(v.Apps) != 1 || v.Apps[0].SeparateOutputs {
		t.Fatalf("saved-only browser has separate outputs: %+v", v.Apps)
	}
}

func TestBuildNewestStreamWithoutNameUsesSavedName(t *testing.T) {
	s := machineSnapshot()
	older := chromeStream(pulse.Output, 4, 88)
	newer := chromeStream(pulse.Output, 5, 88)
	delete(newer.Properties, "application.name")
	s.Outputs = []pulse.Stream{newer, older}
	v := Build(s, Config{Routes: []Route{{App: "chrome", Name: "Saved Chrome", Direction: RouteOutput, Device: jabraSink}}})
	if len(v.Apps) != 1 || v.Apps[0].Name != "Saved Chrome" {
		t.Fatalf("name: %+v", v.Apps)
	}
}

func TestBuildProfilePriorityWhenOtherDirectionIsAbsent(t *testing.T) {
	s := machineSnapshot()
	s.Cards[0].ActiveProfile = "output:hdmi-stereo"
	v := Build(s, Config{})
	if got := entryFor(t, v.Outputs, pciCard, "analog-output-headphones").Profile; got != "output:analog-stereo" {
		t.Fatalf("profile = %q, want highest priority output-only profile", got)
	}
}

func TestBuildInactivePortsRequireAvailableConnection(t *testing.T) {
	s := machineSnapshot()
	card := &s.Cards[1]
	card.Profiles = append(card.Profiles, pulse.CardProfile{Name: "output:iec958-stereo+input:mono-fallback", Available: true, Priority: 5501, Sinks: 1, Sources: 1})
	card.Ports = append(card.Ports, pulse.CardPort{Name: "iec958-stereo-output", Description: "Digital Output (S/PDIF)", Direction: pulse.Output, Availability: pulse.AvailabilityUnknown, Profiles: []string{"output:iec958-stereo+input:mono-fallback"}})

	for _, availability := range []pulse.Availability{pulse.AvailabilityUnknown, pulse.NotAvailable} {
		card.Ports[2].Availability = availability
		v := Build(s, Config{})
		if slices.ContainsFunc(v.Outputs, func(entry SwitchEntry) bool {
			return entry.CardName == jabraCard && entry.PortName == "iec958-stereo-output"
		}) {
			t.Fatalf("inactive digital port listed with availability %v: %+v", availability, v.Outputs)
		}
		analog := entryFor(t, v.Outputs, jabraCard, "analog-output")
		if analog.Label != "Jabra Link 390 Analog Stereo" || analog.Profile != "" || !analog.Active {
			t.Fatalf("only connected port should keep the device label: %+v", analog)
		}
	}

	card.Ports[2].Availability = pulse.Available
	v := Build(s, Config{})
	digital := entryFor(t, v.Outputs, jabraCard, "iec958-stereo-output")
	if digital.Label != "Jabra Link 390: Digital Output (S/PDIF)" || digital.DeviceName != "" || digital.Profile != "output:iec958-stereo+input:mono-fallback" || digital.Active {
		t.Fatalf("available inactive port should offer a profile switch: %+v", digital)
	}
	if analog := entryFor(t, v.Outputs, jabraCard, "analog-output"); analog.Label != "Jabra Link 390: Analog Output" {
		t.Fatalf("two listed ports should use port labels: %+v", analog)
	}
}
