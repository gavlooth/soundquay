package main

import (
	"testing"
	"time"
)

func newTestApp() *AudioApp {
	return &AudioApp{
		actionChan: make(chan Msg, 8),
		model: Model{
			Status: &AudioStatus{
				Volume:     50,
				MicVolume:  40,
				Sinks:      []AudioSink{},
				Sources:    []AudioSource{},
				Streams:    []AudioStream{},
				MicStreams: []MicStream{},
			},
			Expanded:                  make(map[string]bool),
			PreferredRecordingSources: make(map[string]string),
		},
		streamVolumeTimers:            make(map[string]*time.Timer),
		sourceOutputVolumeTimers:      make(map[string]*time.Timer),
		preferredRecordingSourceApply: make(map[string]time.Time),
	}
}

func TestHandleMessageToggleExpand(t *testing.T) {
	app := newTestApp()

	cmds := app.handleMessage(ToggleExpand{Key: "output_devices"})

	if len(cmds) != 0 {
		t.Fatalf("expected no commands, got %d", len(cmds))
	}
	if !app.model.Expanded["output_devices"] {
		t.Fatalf("expected output_devices to be expanded")
	}
	if !app.model.NeedsRebuild {
		t.Fatalf("expected rebuild flag to be set")
	}
}

func TestHandleMessageSetSinkRequestedOptimisticUpdate(t *testing.T) {
	app := newTestApp()
	app.model.Status.Sinks = []AudioSink{
		{ID: "1", Name: "sink.a", Default: true},
		{ID: "2", Name: "sink.b", Default: false},
	}
	app.model.Status.Streams = []AudioStream{
		{ID: "stream-1", SinkName: "1"},
		{ID: "stream-2", SinkName: "1"},
	}

	cmds := app.handleMessage(SetSinkRequested{SinkName: "sink.b"})

	if len(cmds) != 1 {
		t.Fatalf("expected one command, got %d", len(cmds))
	}
	if !app.model.Status.Sinks[1].Default || app.model.Status.Sinks[0].Default {
		t.Fatalf("expected sink.b to become default")
	}
	for _, stream := range app.model.Status.Streams {
		if stream.SinkName != "2" {
			t.Fatalf("expected stream %s to be optimistically moved to sink ID 2, got %q", stream.ID, stream.SinkName)
		}
	}
	if !app.model.NeedsRebuild {
		t.Fatalf("expected rebuild flag to be set")
	}
}

func TestHandleMessageMoveSourceOutputRequestedOptimisticUpdate(t *testing.T) {
	app := newTestApp()
	app.model.Status.Sources = []AudioSource{
		{ID: "1", Name: "source.a"},
		{ID: "2", Name: "source.b"},
	}
	app.model.Status.MicStreams = []MicStream{
		{ID: "mic-1", SourceName: "1"},
	}

	cmds := app.handleMessage(MoveSourceOutputRequested{
		StreamID:   "mic-1",
		SourceName: "source.b",
	})

	if len(cmds) != 1 {
		t.Fatalf("expected one command, got %d", len(cmds))
	}
	if app.model.Status.MicStreams[0].SourceName != "2" {
		t.Fatalf("expected mic stream to be optimistically moved to source ID 2, got %q", app.model.Status.MicStreams[0].SourceName)
	}
	if !app.model.NeedsRebuild {
		t.Fatalf("expected rebuild flag to be set")
	}
}

func TestHandleMessageMoveAdvancedRecordingRequestedOptimisticUpdate(t *testing.T) {
	app := newTestApp()
	app.model.AdvancedSources = []AudioSource{
		{ID: "104", Name: "alsa_input.usb-jabra", Description: "Jabra Link 390 Mono"},
	}
	app.model.AdvancedRecordingStreams = []AdvancedRecordingStream{
		{
			StreamNodeID:    "150",
			Application:     "Google Chrome input",
			CurrentSourceID: "78",
			CurrentSource:   "Built-in Audio Analog Stereo",
		},
	}

	cmds := app.handleMessage(MoveAdvancedRecordingRequested{
		StreamNodeID: "150",
		Application:  "Google Chrome input",
		SourceName:   "alsa_input.usb-jabra",
	})

	if len(cmds) != 2 {
		t.Fatalf("expected two commands, got %d", len(cmds))
	}
	if app.model.AdvancedRecordingStreams[0].CurrentSourceID != "104" {
		t.Fatalf("expected advanced recording source id to update, got %+v", app.model.AdvancedRecordingStreams[0])
	}
	if app.model.AdvancedRecordingStreams[0].CurrentSource != "Jabra Link 390 Mono" {
		t.Fatalf("expected advanced recording source label to update, got %+v", app.model.AdvancedRecordingStreams[0])
	}
	if app.model.PreferredRecordingSources["Google Chrome input"] != "alsa_input.usb-jabra" {
		t.Fatalf("expected preferred source to be recorded, got %+v", app.model.PreferredRecordingSources)
	}
	if !app.model.NeedsRebuild {
		t.Fatalf("expected rebuild flag to be set")
	}
}

func TestBuildApplicationGroupsUsesRecordingInference(t *testing.T) {
	app := newTestApp()
	app.model.Status.Streams = []AudioStream{
		{ID: "play-1", Name: "Google Chrome"},
	}
	app.model.Status.MicStreams = []MicStream{
		{ID: "36", Name: "Echo-Cancel Capture"},
	}
	app.model.AdvancedRecordingStreams = []AdvancedRecordingStream{
		{StreamNodeID: "36", Application: "Echo-Cancel Capture"},
	}
	app.model.RecordingStreamInferences = []AdvancedRecordingInference{
		{
			StreamNodeID: "36",
			Candidates: []RecordingClientCandidate{
				{Application: "Google Chrome input", ProcessBinary: "chrome"},
			},
		},
	}

	groups := app.buildApplicationGroups()

	if len(groups) != 1 {
		t.Fatalf("expected one merged Chrome group, got %+v", groups)
	}

	group := groups[0]
	if group.Name != "Google Chrome" {
		t.Fatalf("expected normalized Chrome group name, got %+v", group)
	}
	if len(group.MicStreams) != 1 || len(group.PlaybackStreams) != 1 {
		t.Fatalf("expected merged Chrome group to include sound and mic streams, got %+v", group)
	}
}

func TestHandleMessageSetPreferredRecordingSourceRequested(t *testing.T) {
	app := newTestApp()

	cmds := app.handleMessage(SetPreferredRecordingSourceRequested{
		Application: "Google Chrome input",
		SourceName:  "alsa_input.usb-jabra",
	})

	if len(cmds) != 1 {
		t.Fatalf("expected one command, got %d", len(cmds))
	}
	if app.model.PreferredRecordingSources["Google Chrome input"] != "alsa_input.usb-jabra" {
		t.Fatalf("expected preferred source to update, got %+v", app.model.PreferredRecordingSources)
	}
	if !app.model.NeedsRebuild {
		t.Fatalf("expected rebuild flag to be set")
	}
}

func TestPreferredRecordingMovesToApply(t *testing.T) {
	app := newTestApp()
	app.model.PreferredRecordingSources["Google Chrome input"] = "alsa_input.usb-jabra"
	app.model.AdvancedSources = []AudioSource{
		{ID: "104", Name: "alsa_input.usb-jabra", Description: "Jabra Link 390 Mono"},
	}
	app.model.AdvancedRecordingStreams = []AdvancedRecordingStream{
		{
			StreamNodeID:    "150",
			Application:     "Google Chrome input",
			CurrentSourceID: "78",
			CurrentSource:   "Built-in Audio Analog Stereo",
		},
	}

	requests := app.preferredRecordingMovesToApply(time.Unix(100, 0))

	if len(requests) != 1 {
		t.Fatalf("expected one preferred move request, got %d", len(requests))
	}
	if requests[0].StreamNodeID != "150" || requests[0].SourceName != "alsa_input.usb-jabra" {
		t.Fatalf("unexpected request: %+v", requests[0])
	}

	secondPass := app.preferredRecordingMovesToApply(time.Unix(101, 0))
	if len(secondPass) != 0 {
		t.Fatalf("expected cooldown to suppress immediate reapply, got %+v", secondPass)
	}
}

func TestPreferredRecordingMovesToApplySkipsMissingTargetSource(t *testing.T) {
	app := newTestApp()
	app.model.PreferredRecordingSources["Google Chrome input"] = "alsa_input.usb-missing"
	app.model.AdvancedSources = []AudioSource{
		{ID: "104", Name: "alsa_input.usb-jabra", Description: "Jabra Link 390 Mono"},
	}
	app.model.AdvancedRecordingStreams = []AdvancedRecordingStream{
		{
			StreamNodeID:    "150",
			Application:     "Google Chrome input",
			CurrentSourceID: "78",
			CurrentSource:   "Built-in Audio Analog Stereo",
		},
	}

	requests := app.preferredRecordingMovesToApply(time.Unix(100, 0))
	if len(requests) != 0 {
		t.Fatalf("expected missing target source to suppress apply, got %+v", requests)
	}
}

func TestHandleMessageRefreshSplitMessages(t *testing.T) {
	app := newTestApp()
	app.model.RefreshInFlight = true

	status := &AudioStatus{
		Volume:     60,
		MicVolume:  55,
		Sinks:      []AudioSink{},
		Sources:    []AudioSource{},
		Streams:    []AudioStream{},
		MicStreams: []MicStream{},
	}

	if cmds := app.handleMessage(RefreshCompleted{Status: status}); len(cmds) != 0 {
		t.Fatalf("expected no commands from status refresh, got %d", len(cmds))
	}
	if app.model.Status != status {
		t.Fatalf("expected status to update")
	}
	if !app.model.RefreshInFlight {
		t.Fatalf("expected refresh to remain in flight until advanced discovery completes")
	}

	capabilities := AudioBackendCapabilities{SelectedBackend: "pipewire", AdvancedRouting: true}
	if cmds := app.handleMessage(BackendCapabilitiesRefreshed{Capabilities: capabilities}); len(cmds) != 0 {
		t.Fatalf("expected no commands from capabilities refresh, got %d", len(cmds))
	}
	if app.model.BackendCapabilities.SelectedBackend != "pipewire" {
		t.Fatalf("expected capabilities to update, got %+v", app.model.BackendCapabilities)
	}

	cmds := app.handleMessage(AdvancedDiscoveryRefreshed{
		Sources: []AudioSource{{ID: "104", Name: "alsa_input.usb-jabra", Description: "Jabra Link 390 Mono"}},
		Streams: []AdvancedRecordingStream{},
	})
	if len(cmds) != 0 {
		t.Fatalf("expected no auto-apply commands when there are no advanced streams, got %d", len(cmds))
	}
	if app.model.RefreshInFlight {
		t.Fatalf("expected refresh to end after advanced discovery")
	}
}

func TestHandleMessageAdvancedRecordingMoveCompletedRequestsRefresh(t *testing.T) {
	app := newTestApp()

	cmds := app.handleMessage(AdvancedRecordingMoveCompleted{
		StreamNodeID: "150",
		Application:  "Google Chrome input",
		SourceName:   "alsa_input.usb-jabra",
		Err:          nil,
	})

	if len(cmds) != 1 {
		t.Fatalf("expected one follow-up command, got %d", len(cmds))
	}
}

func TestHandleMessagePreferredRecordingSourceAppliedRequestsRefresh(t *testing.T) {
	app := newTestApp()

	cmds := app.handleMessage(PreferredRecordingSourceApplied{
		StreamNodeID: "150",
		Application:  "Google Chrome input",
		SourceName:   "alsa_input.usb-jabra",
		Err:          nil,
	})

	if len(cmds) != 1 {
		t.Fatalf("expected one follow-up command, got %d", len(cmds))
	}
}
