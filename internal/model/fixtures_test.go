package model

import "soundquay/internal/pulse"

const (
	pciCard       = "alsa_card.pci-0000_00_1f.3"
	hdmiSink      = "alsa_output.pci-0000_00_1f.3.hdmi-stereo"
	builtInSource = "alsa_input.pci-0000_00_1f.3.analog-stereo"
	jabraCard     = "alsa_card.usb-_Jabra_Link_390_6CFBED5C5C34-00"
	jabraSink     = "alsa_output.usb-_Jabra_Link_390_6CFBED5C5C34-00.analog-stereo"
	jabraSource   = "alsa_input.usb-_Jabra_Link_390_6CFBED5C5C34-00.mono-fallback"
	ankerCard     = "alsa_card.usb-Anker_PowerConf_C200_Anker_PowerConf_C200_ACNV9P1E47205441-02"
	ankerSource   = "alsa_input.usb-Anker_PowerConf_C200_Anker_PowerConf_C200_ACNV9P1E47205441-02.analog-stereo"
	loopCard      = "alsa_card.platform-snd_aloop.0"
	loopSink      = "alsa_output.platform-snd_aloop.0.analog-stereo"
	loopSource    = "alsa_input.platform-snd_aloop.0.analog-stereo"
)

// The names and profile relationships come from this machine's pactl JSON.
// Headphones are plugged in for the profile-switch scenarios in these tests.
func machineSnapshot() pulse.Snapshot {
	d := func(direction pulse.Direction, index uint32, name, description, card, port string) pulse.Device {
		return pulse.Device{Direction: direction, Index: index, Name: name, Description: description, CardName: card, VolumePct: 85, ActivePort: port, Ports: []pulse.DevicePort{{Name: port, Description: port, Availability: pulse.Available}}}
	}
	profile := func(name string, priority, sinks, sources int) pulse.CardProfile {
		return pulse.CardProfile{Name: name, Available: true, Priority: priority, Sinks: sinks, Sources: sources}
	}
	port := func(name, description string, direction pulse.Direction, profiles ...string) pulse.CardPort {
		return pulse.CardPort{Name: name, Description: description, Direction: direction, Availability: pulse.AvailabilityUnknown, Profiles: profiles}
	}
	s := pulse.Snapshot{
		Info: pulse.ServerInfo{DefaultSinkName: jabraSink, DefaultSourceName: jabraSource},
		Sinks: []pulse.Device{
			d(pulse.Output, 77, hdmiSink, "Built-in Audio Digital Stereo (HDMI) [Odyssey G5]", pciCard, "hdmi-output-0"),
			d(pulse.Output, 88, jabraSink, "Jabra Link 390 Analog Stereo", jabraCard, "analog-output"),
			d(pulse.Output, 89, loopSink, "Loopback Analog Stereo", loopCard, "analog-output"),
		},
		Sources: []pulse.Device{
			d(pulse.Input, 78, builtInSource, "Built-in Audio Analog Stereo", pciCard, "analog-input-internal-mic"),
			d(pulse.Input, 90, jabraSource, "Jabra Link 390 Mono", jabraCard, "analog-input-headset-mic"),
			d(pulse.Input, 91, ankerSource, "Anker PowerConf C200 Analog Stereo", ankerCard, "analog-input-mic"),
			d(pulse.Input, 92, loopSource, "Loopback Analog Stereo", loopCard, "analog-input"),
		},
		Cards: []pulse.Card{
			{Name: pciCard, Description: "Built-in Audio", ActiveProfile: "output:hdmi-stereo+input:analog-stereo",
				Profiles: []pulse.CardProfile{profile("output:hdmi-stereo+input:analog-stereo", 5965, 1, 1), profile("output:analog-stereo", 9000, 1, 0), profile("output:analog-stereo+input:analog-stereo", 6565, 1, 1)},
				Ports:    []pulse.CardPort{port("hdmi-output-0", "HDMI / DisplayPort", pulse.Output, "output:hdmi-stereo+input:analog-stereo"), port("analog-output-headphones", "Headphones", pulse.Output, "output:analog-stereo", "output:analog-stereo+input:analog-stereo"), port("analog-input-internal-mic", "Internal Microphone", pulse.Input, "output:hdmi-stereo+input:analog-stereo", "output:analog-stereo+input:analog-stereo")}},
			{Name: jabraCard, Description: "Jabra Link 390", ActiveProfile: "output:analog-stereo+input:mono-fallback",
				Profiles: []pulse.CardProfile{profile("output:analog-stereo+input:mono-fallback", 6501, 1, 1)},
				Ports:    []pulse.CardPort{port("analog-output", "Analog Output", pulse.Output, "output:analog-stereo+input:mono-fallback"), port("analog-input-headset-mic", "Headset Microphone", pulse.Input, "output:analog-stereo+input:mono-fallback")}},
			{Name: ankerCard, Description: "Anker PowerConf C200", ActiveProfile: "input:analog-stereo", Profiles: []pulse.CardProfile{profile("input:analog-stereo", 65, 0, 1)}, Ports: []pulse.CardPort{port("analog-input-mic", "Microphone", pulse.Input, "input:analog-stereo")}},
			{Name: loopCard, Description: "Loopback", ActiveProfile: "output:analog-stereo+input:analog-stereo", Profiles: []pulse.CardProfile{profile("output:analog-stereo+input:analog-stereo", 6565, 1, 1)}, Ports: []pulse.CardPort{port("analog-output", "Analog Output", pulse.Output, "output:analog-stereo+input:analog-stereo"), port("analog-input", "Analog Input", pulse.Input, "output:analog-stereo+input:analog-stereo")}},
		},
	}
	s.Cards[0].Ports[1].Availability = pulse.Available
	for _, sink := range s.Sinks {
		s.Sources = append(s.Sources, pulse.Device{Direction: pulse.Input, Index: sink.Index, Name: sink.Name + ".monitor", Description: "Monitor of " + sink.Description, CardName: sink.CardName, IsMonitor: true, Properties: map[string]string{"device.class": "monitor"}})
	}
	return s
}

func chromeStream(direction pulse.Direction, index, device uint32) pulse.Stream {
	return pulse.Stream{Direction: direction, Index: index, DeviceIndex: device, VolumePct: 105, Properties: map[string]string{"application.name": "Google Chrome", "application.process.binary": "chrome", "node.name": "Chromium", "media.name": "Playback"}}
}
