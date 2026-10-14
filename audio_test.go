package main

import (
	"fmt"
	"testing"
)

func TestParseSinkInputs(t *testing.T) {
	out := `Sink Input #42
        Sink: 5
        Volume: front-left: 32768 / 50% / -18.06 dB,   front-right: 32768 / 50% / -18.06 dB
        Mute: no
        application.name = "Firefox"
        application.process.binary = "firefox"
Sink Input #43
        Sink: 7
        Volume: mono: 65536 / 100% / 0.00 dB
        Mute: yes
        application.name = "mpv"
        application.process.binary = "mpv"
`

	streams := parseSinkInputs(out)

	if len(streams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(streams))
	}
	if streams[0].ID != "42" || streams[0].SinkName != "5" || streams[0].Volume != 50 || streams[0].Muted {
		t.Fatalf("unexpected first stream: %+v", streams[0])
	}
	if streams[0].Name != "Firefox" || streams[0].Binary != "firefox" {
		t.Fatalf("unexpected first stream metadata: %+v", streams[0])
	}
	if streams[1].ID != "43" || streams[1].SinkName != "7" || streams[1].Volume != 100 || !streams[1].Muted {
		t.Fatalf("unexpected second stream: %+v", streams[1])
	}
}

func TestParseSinkInputsFallsBackToMediaName(t *testing.T) {
	out := `Sink Input #39
        Sink: 5
        Volume: front-left: 32768 / 50% / -18.06 dB,   front-right: 32768 / 50% / -18.06 dB
        Mute: no
        media.name = "Echo-Cancel Playback"
        node.name = "Echo Cancellation Playback"
`

	streams := parseSinkInputs(out)

	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}
	if streams[0].Name != "Echo-Cancel Playback" {
		t.Fatalf("expected media.name fallback, got %+v", streams[0])
	}
}

func TestParseSourceOutputs(t *testing.T) {
	out := `Source Output #17
        Source: 9
        Volume: mono: 32768 / 50% / -18.06 dB
        Mute: no
        application.name = "Zoom"
        application.process.binary = "zoom"
`

	streams := parseSourceOutputs(out)

	if len(streams) != 1 {
		t.Fatalf("expected 1 mic stream, got %d", len(streams))
	}
	if streams[0].ID != "17" || streams[0].SourceName != "9" || streams[0].Volume != 50 || streams[0].Muted {
		t.Fatalf("unexpected mic stream: %+v", streams[0])
	}
	if streams[0].Name != "Zoom" || streams[0].Binary != "zoom" {
		t.Fatalf("unexpected mic stream metadata: %+v", streams[0])
	}
}

func TestParseSourceOutputsFallsBackToMediaName(t *testing.T) {
	out := `Source Output #17
        Source: 9
        Volume: mono: 32768 / 50% / -18.06 dB
        Mute: no
        media.name = "Echo-Cancel Capture"
        node.name = "Echo Cancellation Capture"
`

	streams := parseSourceOutputs(out)

	if len(streams) != 1 {
		t.Fatalf("expected 1 mic stream, got %d", len(streams))
	}
	if streams[0].Name != "Echo-Cancel Capture" {
		t.Fatalf("expected media.name fallback, got %+v", streams[0])
	}
}

func TestParseSinks(t *testing.T) {
	out := `Sink #1
        State: RUNNING
        Name: alsa_output.pci-0000_00_1f.3.analog-stereo
        Description: Built-in Audio Analog Stereo
        Mute: no
        Volume: front-left: 32768 / 50% / -18.06 dB,   front-right: 32768 / 50% / -18.06 dB
        device.description = "Built-in Audio Analog Stereo"
Sink #2
        State: IDLE
        Name: alsa_output.usb-Plantronics
        Mute: yes
        Volume: mono: 65536 / 100% / 0.00 dB
        node.name = "Plantronics USB"
`

	sinks := parseSinks(out, "alsa_output.usb-Plantronics", "88")

	if len(sinks) != 2 {
		t.Fatalf("expected 2 sinks, got %d", len(sinks))
	}
	if sinks[0].Name != "alsa_output.pci-0000_00_1f.3.analog-stereo" || sinks[0].Description != "Built-in Audio Analog Stereo" {
		t.Fatalf("unexpected first sink: %+v", sinks[0])
	}
	if sinks[0].Default {
		t.Fatalf("did not expect first sink to be default")
	}
	if sinks[1].Name != "alsa_output.usb-Plantronics" || !sinks[1].Default || sinks[1].PipeWireID != "88" {
		t.Fatalf("unexpected second sink: %+v", sinks[1])
	}
	if sinks[1].Description != "Plantronics USB" || sinks[1].Volume != 100 || !sinks[1].Muted {
		t.Fatalf("unexpected second sink details: %+v", sinks[1])
	}
}

func TestParseSources(t *testing.T) {
	out := `Source #3
        State: RUNNING
        Name: alsa_input.pci-0000_00_1f.3.analog-stereo
        Mute: no
        Volume: front-left: 26214 / 40% / -23.98 dB,   front-right: 26214 / 40% / -23.98 dB
        device.description = "Built-in Audio Analog Stereo"
Source #4
        State: SUSPENDED
        Name: alsa_input.usb-Plantronics
        Mute: yes
        Volume: mono: 32768 / 50% / -18.06 dB
        node.name = "Plantronics USB Mic"
`

	sources := parseSources(out, "alsa_input.usb-Plantronics", "91")

	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if sources[0].Description != "Built-in Audio Analog Stereo" || sources[0].Volume != 40 || sources[0].Muted {
		t.Fatalf("unexpected first source: %+v", sources[0])
	}
	if !sources[1].Default || sources[1].PipeWireID != "91" {
		t.Fatalf("unexpected second source defaults: %+v", sources[1])
	}
	if sources[1].Description != "Plantronics USB Mic" || sources[1].Volume != 50 || !sources[1].Muted {
		t.Fatalf("unexpected second source details: %+v", sources[1])
	}
}

func TestParseWpctlRuntime(t *testing.T) {
	out := `PipeWire 'pipewire-0' [1.4.10, user@host, cookie:123]
 └─ Clients:
        33. WirePlumber                         [1.4.10, user@host, pid:2015]
`

	pipeWireRunning, wirePlumberRunning := parseWpctlRuntime(out)

	if !pipeWireRunning {
		t.Fatalf("expected PipeWire runtime to be detected")
	}
	if !wirePlumberRunning {
		t.Fatalf("expected WirePlumber runtime to be detected")
	}
}

func TestDetectAudioBackendCapabilities(t *testing.T) {
	run := func(name string, args ...string) (string, error) {
		key := name + " " + fmt.Sprint(args)
		switch key {
		case "wpctl [status]":
			return "PipeWire 'pipewire-0'\n 33. WirePlumber\n", nil
		case "pw-metadata [-n settings]":
			return `Found "settings" metadata 32`, nil
		default:
			return "", nil
		}
	}

	lookPath := func(file string) (string, error) {
		switch file {
		case "pactl", "wpctl", "pw-metadata", "pw-cli", "wireplumber":
			return "/usr/bin/" + file, nil
		default:
			return "", fmt.Errorf("not found")
		}
	}

	capabilities := detectAudioBackendCapabilities(run, lookPath)

	if !capabilities.HasPactl || !capabilities.HasWpctl || !capabilities.HasPWMetadata {
		t.Fatalf("expected core command capabilities to be detected: %+v", capabilities)
	}
	if !capabilities.PipeWireRunning || !capabilities.WirePlumberRunning {
		t.Fatalf("expected runtime detection to succeed: %+v", capabilities)
	}
	if !capabilities.MetadataReadable || !capabilities.MetadataWritable {
		t.Fatalf("expected metadata capability detection to succeed: %+v", capabilities)
	}
	if !capabilities.AdvancedRouting {
		t.Fatalf("expected advanced routing capability to be enabled: %+v", capabilities)
	}
	if capabilities.SelectedBackend != "pipewire" {
		t.Fatalf("expected selected backend to switch to pipewire, got %q", capabilities.SelectedBackend)
	}
}

func TestNewAudioBackendUsesPipeWireWhenAdvancedRoutingAvailable(t *testing.T) {
	backend := newAudioBackend(func(name string, args ...string) (string, error) {
		return "", nil
	}, AudioBackendCapabilities{
		SelectedBackend: "pipewire",
		AdvancedRouting: true,
	})

	if backend.Name() != "pipewire" {
		t.Fatalf("expected pipewire backend, got %q", backend.Name())
	}
}

func TestDetectAudioBackendCapabilitiesWithoutMetadataAccess(t *testing.T) {
	run := func(name string, args ...string) (string, error) {
		key := name + " " + fmt.Sprint(args)
		switch key {
		case "wpctl [status]":
			return "PipeWire 'pipewire-0'\n 33. WirePlumber\n", nil
		case "pw-metadata [-n settings]":
			return "", fmt.Errorf("access denied")
		default:
			return "", nil
		}
	}

	lookPath := func(file string) (string, error) {
		switch file {
		case "pactl", "wpctl", "pw-metadata", "pw-cli", "wireplumber":
			return "/usr/bin/" + file, nil
		default:
			return "", fmt.Errorf("not found")
		}
	}

	capabilities := detectAudioBackendCapabilities(run, lookPath)
	if capabilities.AdvancedRouting {
		t.Fatalf("expected advanced routing to be disabled without metadata access: %+v", capabilities)
	}
	if capabilities.SelectedBackend != "pactl" {
		t.Fatalf("expected pactl fallback without metadata access, got %q", capabilities.SelectedBackend)
	}
}

func TestParsePWCLIAndDiscoverAdvancedRecordingStreams(t *testing.T) {
	run := func(name string, args ...string) (string, error) {
		key := name + " " + fmt.Sprint(args)
		switch key {
		case "pw-cli [ls Client]":
			return `id 110, type PipeWire:Interface:Client/3
		application.name = "Google Chrome input"
`, nil
		case "pw-cli [ls Node]":
			return `id 104, type PipeWire:Interface:Node/3
		client.id = "47"
		node.description = "Jabra Link 390 Mono"
		node.name = "alsa_input.usb-jabra"
		node.nick = "Jabra Link 390"
		media.class = "Audio/Source"
id 150, type PipeWire:Interface:Node/3
		client.id = "110"
		node.description = "WebRTC Capture"
		node.name = "chrome-capture"
		media.class = "Stream/Input/Audio"
`, nil
		case "pw-cli [ls Link]":
			return `id 80, type PipeWire:Interface:Link/3
		link.output.node = "104"
		link.input.node = "150"
`, nil
		default:
			return "", fmt.Errorf("unexpected command: %s", key)
		}
	}

	streams := discoverAdvancedRecordingStreams(run)
	if len(streams) != 1 {
		t.Fatalf("expected 1 advanced recording stream, got %d", len(streams))
	}
	if streams[0].Application != "Google Chrome input" {
		t.Fatalf("unexpected application: %+v", streams[0])
	}
	if streams[0].CurrentSourceID != "104" {
		t.Fatalf("unexpected source id: %+v", streams[0])
	}
	if streams[0].CurrentSource != "Jabra Link 390 Mono" {
		t.Fatalf("unexpected source label: %+v", streams[0])
	}
}

func TestDiscoverAdvancedRecordingStreamsForSecondApp(t *testing.T) {
	run := func(name string, args ...string) (string, error) {
		key := name + " " + fmt.Sprint(args)
		switch key {
		case "pw-cli [ls Client]":
			return `id 210, type PipeWire:Interface:Client/3
		application.name = "OBS Studio"
`, nil
		case "pw-cli [ls Node]":
			return `id 104, type PipeWire:Interface:Node/3
		node.description = "USB Podcast Mic"
		node.name = "alsa_input.usb-podcast"
		media.class = "Audio/Source"
id 250, type PipeWire:Interface:Node/3
		client.id = "210"
		node.description = "OBS Capture"
		node.name = "obs-capture"
		media.class = "Stream/Input/Audio"
`, nil
		case "pw-cli [ls Link]":
			return `id 80, type PipeWire:Interface:Link/3
		link.output.node = "104"
		link.input.node = "250"
`, nil
		default:
			return "", fmt.Errorf("unexpected command: %s", key)
		}
	}

	streams := discoverAdvancedRecordingStreams(run)
	if len(streams) != 1 {
		t.Fatalf("expected 1 advanced recording stream, got %d", len(streams))
	}
	if streams[0].Application != "OBS Studio" {
		t.Fatalf("unexpected application: %+v", streams[0])
	}
	if streams[0].CurrentSource != "USB Podcast Mic" {
		t.Fatalf("unexpected source label: %+v", streams[0])
	}
}

func TestDiscoverAdvancedRecordingStreamsFallsBackFromGenericPipeWireClient(t *testing.T) {
	run := func(name string, args ...string) (string, error) {
		key := name + " " + fmt.Sprint(args)
		switch key {
		case "pw-cli [ls Client]":
			return `id 35, type PipeWire:Interface:Client/3
		application.name = "pipewire"
`, nil
		case "pw-cli [ls Node]":
			return `id 104, type PipeWire:Interface:Node/3
		node.description = "USB Podcast Mic"
		node.name = "alsa_input.usb-podcast"
		media.class = "Audio/Source"
id 250, type PipeWire:Interface:Node/3
		client.id = "35"
		node.description = "Echo-Cancel Capture"
		node.name = "Echo Cancellation Capture"
		media.class = "Stream/Input/Audio"
`, nil
		case "pw-cli [ls Link]":
			return `id 80, type PipeWire:Interface:Link/3
		link.output.node = "104"
		link.input.node = "250"
`, nil
		default:
			return "", fmt.Errorf("unexpected command: %s", key)
		}
	}

	streams := discoverAdvancedRecordingStreams(run)
	if len(streams) != 1 {
		t.Fatalf("expected 1 advanced recording stream, got %d", len(streams))
	}
	if streams[0].Application != "Echo-Cancel Capture" {
		t.Fatalf("expected generic pipewire client to fall back to node description, got %+v", streams[0])
	}
}

func TestDiscoverRecordingClientCandidates(t *testing.T) {
	run := func(name string, args ...string) (string, error) {
		key := name + " " + fmt.Sprint(args)
		switch key {
		case "pw-cli [ls Client]":
			return `id 35, type PipeWire:Interface:Client/3
		application.name = "pipewire"
id 94, type PipeWire:Interface:Client/3
		application.name = "Chromium input"
		application.process.binary = "chrome"
		application.process.id = "123"
id 99, type PipeWire:Interface:Client/3
		application.name = "WEBRTC VoiceEngine"
		application.process.binary = "chrome"
		application.process.id = "123"
id 107, type PipeWire:Interface:Client/3
		application.name = "Thunderbird"
		application.process.binary = "thunderbird"
		application.process.id = "456"
`, nil
		case "wpctl [inspect 94]":
			return `id 94, type PipeWire:Interface:Client
    application.name = "Chromium input"
    application.process.binary = "chrome"
    application.process.id = "123"
`, nil
		case "wpctl [inspect 99]":
			return `id 99, type PipeWire:Interface:Client
    application.name = "WEBRTC VoiceEngine"
    application.process.binary = "chrome"
    application.process.id = "123"
    pipewire.access.portal.app_id = "com.discordapp.Discord"
`, nil
		case "wpctl [inspect 107]":
			return `id 107, type PipeWire:Interface:Client
    application.name = "Thunderbird"
    application.process.binary = "thunderbird"
    application.process.id = "456"
`, nil
		default:
			return "", fmt.Errorf("unexpected command: %s", key)
		}
	}

	candidates := discoverRecordingClientCandidates(run)
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %+v", candidates)
	}
	if candidates[0].Application != "Chromium input" {
		t.Fatalf("expected Chromium input candidate first, got %+v", candidates)
	}
	if candidates[0].ProcessBinary != "chrome" || candidates[0].ProcessID != "123" {
		t.Fatalf("expected candidate enrichment from wpctl inspect, got %+v", candidates[0])
	}
	if candidates[1].Application != "Thunderbird" {
		t.Fatalf("expected Thunderbird candidate second, got %+v", candidates)
	}
	if candidates[2].Application != "WEBRTC VoiceEngine" {
		t.Fatalf("expected WEBRTC VoiceEngine candidate third, got %+v", candidates)
	}
	if candidates[2].PortalAppID != "com.discordapp.Discord" {
		t.Fatalf("expected portal app id enrichment, got %+v", candidates[2])
	}
}

func TestInferAdvancedRecordingOwners(t *testing.T) {
	streams := []AdvancedRecordingStream{
		{
			StreamNodeID: "36",
			StreamName:   "Echo-Cancel Capture",
			Application:  "Echo-Cancel Capture",
		},
	}
	candidates := []RecordingClientCandidate{
		{ClientID: "94", Application: "Chromium input", ProcessBinary: "slack", ProcessID: "5595", Reason: "application name suggests capture client"},
		{ClientID: "110", Application: "Google Chrome input", ProcessBinary: "chrome", ProcessID: "89692", Reason: "application name suggests capture client"},
		{ClientID: "99", Application: "WEBRTC VoiceEngine", ProcessBinary: "Discord", ProcessID: "136", PortalAppID: "com.discordapp.Discord", Reason: "webrtc voice engine client"},
	}

	inferences := InferAdvancedRecordingOwners(streams, candidates)
	if len(inferences) != 1 {
		t.Fatalf("expected one inference, got %+v", inferences)
	}
	if len(inferences[0].Candidates) != 3 {
		t.Fatalf("expected three ranked candidates, got %+v", inferences[0].Candidates)
	}
	if inferences[0].Candidates[0].Application != "Google Chrome input" {
		t.Fatalf("expected Google Chrome input to rank first, got %+v", inferences[0].Candidates)
	}
}

func TestDiscoverAdvancedRecordingStreamsNoLiveStreams(t *testing.T) {
	run := func(name string, args ...string) (string, error) {
		key := name + " " + fmt.Sprint(args)
		switch key {
		case "pw-cli [ls Client]":
			return `id 110, type PipeWire:Interface:Client/3
		application.name = "Google Chrome input"
`, nil
		case "pw-cli [ls Node]":
			return `id 104, type PipeWire:Interface:Node/3
		node.description = "Jabra Link 390 Mono"
		node.name = "alsa_input.usb-jabra"
		media.class = "Audio/Source"
`, nil
		case "pw-cli [ls Link]":
			return ``, nil
		default:
			return "", fmt.Errorf("unexpected command: %s", key)
		}
	}

	streams := discoverAdvancedRecordingStreams(run)
	if len(streams) != 0 {
		t.Fatalf("expected no advanced recording streams, got %+v", streams)
	}
}

func TestDiscoverAdvancedSources(t *testing.T) {
	run := func(name string, args ...string) (string, error) {
		key := name + " " + fmt.Sprint(args)
		switch key {
		case "pw-cli [ls Node]":
			return `id 104, type PipeWire:Interface:Node/3
		node.description = "Jabra Link 390 Mono"
		node.name = "alsa_input.usb-jabra"
		node.nick = "Jabra Link 390"
		media.class = "Audio/Source"
id 150, type PipeWire:Interface:Node/3
		node.description = "Virtual Capture"
		node.name = "virtual-source"
		media.class = "Audio/Source"
		node.virtual = "true"
`, nil
		default:
			return "", fmt.Errorf("unexpected command: %s", key)
		}
	}

	sources := discoverAdvancedSources(run)
	if len(sources) != 1 {
		t.Fatalf("expected 1 non-virtual source, got %d", len(sources))
	}
	if sources[0].ID != "104" || sources[0].PipeWireID != "104" {
		t.Fatalf("unexpected source ids: %+v", sources[0])
	}
	if sources[0].Description != "Jabra Link 390 Mono" {
		t.Fatalf("unexpected source description: %+v", sources[0])
	}
}

func TestWirePlumberMoveInputStream(t *testing.T) {
	var gotName string
	var gotArgs []string

	run := func(name string, args ...string) (string, error) {
		gotName = name
		gotArgs = append([]string{}, args...)
		return "", nil
	}

	if err := wirePlumberMoveInputStream(run, "150", "alsa_input.usb-jabra"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotName != "pw-metadata" {
		t.Fatalf("expected pw-metadata command, got %q", gotName)
	}

	expected := []string{"-n", "default", "150", "target.object", "alsa_input.usb-jabra"}
	if len(gotArgs) != len(expected) {
		t.Fatalf("unexpected args length: got %v want %v", gotArgs, expected)
	}
	for i := range expected {
		if gotArgs[i] != expected[i] {
			t.Fatalf("unexpected args: got %v want %v", gotArgs, expected)
		}
	}
}

func TestWirePlumberMoveInputStreamRequiresTargetSource(t *testing.T) {
	run := func(name string, args ...string) (string, error) {
		return "", nil
	}

	if err := wirePlumberMoveInputStream(run, "150", ""); err == nil {
		t.Fatalf("expected empty target source to be rejected")
	}
	if err := wirePlumberMoveInputStream(run, "", "alsa_input.usb-jabra"); err == nil {
		t.Fatalf("expected empty stream node id to be rejected")
	}
}
