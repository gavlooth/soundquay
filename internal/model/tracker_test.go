package model

import (
	"reflect"
	"testing"

	"soundquay/internal/pulse"
)

func expectMoves(t *testing.T, got []Move, want ...Move) {
	t.Helper()
	if len(got) != len(want) || len(want) != 0 && !reflect.DeepEqual(got, want) {
		t.Fatalf("moves = %+v, want %+v", got, want)
	}
}

func TestTrackerMovesNewStreamOnce(t *testing.T) {
	s := machineSnapshot()
	s.Outputs = []pulse.Stream{chromeStream(pulse.Output, 200, 77)}
	routes := []Route{{App: "chrome", Direction: RouteOutput, Device: jabraSink}}
	tracker := NewTracker()
	expectMoves(t, tracker.Plan(s, routes), Move{Direction: pulse.Output, StreamIndex: 200, DeviceName: jabraSink})
	expectMoves(t, tracker.Plan(s, routes))
	s.Outputs[0].DeviceIndex = 89
	expectMoves(t, tracker.Plan(s, routes))
}

func TestTrackerAlreadyOnRouteDevice(t *testing.T) {
	s := machineSnapshot()
	s.Outputs = []pulse.Stream{chromeStream(pulse.Output, 200, 88)}
	routes := []Route{{App: "chrome", Direction: RouteOutput, Device: jabraSink}}
	tracker := NewTracker()
	expectMoves(t, tracker.Plan(s, routes))
	s.Outputs[0].DeviceIndex = 77
	expectMoves(t, tracker.Plan(s, routes))
}

func TestTrackerWaitsForDeviceAndMovesOnce(t *testing.T) {
	s := machineSnapshot()
	jabra := s.Sinks[1]
	s.Sinks = append(s.Sinks[:1], s.Sinks[2:]...)
	s.Outputs = []pulse.Stream{chromeStream(pulse.Output, 200, 77)}
	routes := []Route{{App: "chrome", Direction: RouteOutput, Device: jabraSink}}
	tracker := NewTracker()
	expectMoves(t, tracker.Plan(s, routes))
	expectMoves(t, tracker.Plan(s, routes))
	jabra.Index = 500
	s.Sinks = append(s.Sinks, jabra)
	expectMoves(t, tracker.Plan(s, routes), Move{Direction: pulse.Output, StreamIndex: 200, DeviceName: jabraSink})
	expectMoves(t, tracker.Plan(s, routes))
}

func TestTrackerWaitingStreamMovedElsewhereIsLeftAlone(t *testing.T) {
	for _, appearsImmediately := range []bool{false, true} {
		t.Run(map[bool]string{false: "external move before device", true: "external move with device"}[appearsImmediately], func(t *testing.T) {
			s := machineSnapshot()
			jabra := s.Sinks[1]
			s.Sinks = append(s.Sinks[:1], s.Sinks[2:]...)
			s.Outputs = []pulse.Stream{chromeStream(pulse.Output, 200, 77)}
			routes := []Route{{App: "chrome", Direction: RouteOutput, Device: jabraSink}}
			tracker := NewTracker()
			expectMoves(t, tracker.Plan(s, routes))
			s.Outputs[0].DeviceIndex = 89
			if appearsImmediately {
				s.Sinks = append(s.Sinks, jabra)
			}
			expectMoves(t, tracker.Plan(s, routes))
			if !appearsImmediately {
				s.Sinks = append(s.Sinks, jabra)
			}
			s.Outputs[0].DeviceIndex = 77
			expectMoves(t, tracker.Plan(s, routes))
		})
	}
}

func TestTrackerRemovedStreamCanBePlannedAgain(t *testing.T) {
	s := machineSnapshot()
	s.Outputs = []pulse.Stream{chromeStream(pulse.Output, 200, 77)}
	routes := []Route{{App: "chrome", Direction: RouteOutput, Device: jabraSink}}
	tracker := NewTracker()
	expectMoves(t, tracker.Plan(s, routes), Move{Direction: pulse.Output, StreamIndex: 200, DeviceName: jabraSink})
	s.Outputs = nil
	expectMoves(t, tracker.Plan(s, routes))
	s.Outputs = []pulse.Stream{chromeStream(pulse.Output, 200, 77)}
	expectMoves(t, tracker.Plan(s, routes), Move{Direction: pulse.Output, StreamIndex: 200, DeviceName: jabraSink})
}

func TestTrackerNonAppsAndUnidentifiedStreamsAreNeverMoved(t *testing.T) {
	s := machineSnapshot()
	owned := chromeStream(pulse.Output, 200, 77)
	owned.HasOwner = true
	virtual := chromeStream(pulse.Output, 201, 77)
	virtual.Properties["node.virtual"] = "true"
	unknown := pulse.Stream{Index: 202, DeviceIndex: 77, Properties: map[string]string{"node.name": "anonymous"}}
	s.Outputs = []pulse.Stream{owned, virtual, unknown}
	routes := []Route{{App: "chrome", Direction: RouteOutput, Device: jabraSink}, {App: "node:anonymous", Direction: RouteOutput, Device: jabraSink}}
	tracker := NewTracker()
	expectMoves(t, tracker.Plan(s, routes))
	owned.HasOwner = false
	virtual.Properties["node.virtual"] = "false"
	unknown.Properties["application.process.binary"] = "chrome"
	s.Outputs = []pulse.Stream{owned, virtual, unknown}
	expectMoves(t, tracker.Plan(s, routes))
}

func TestTrackerResetReplansWithoutMovingAlreadyRoutedStreams(t *testing.T) {
	s := machineSnapshot()
	s.Outputs = []pulse.Stream{chromeStream(pulse.Output, 200, 77), chromeStream(pulse.Output, 201, 88)}
	routes := []Route{{App: "chrome", Direction: RouteOutput, Device: jabraSink}}
	tracker := NewTracker()
	move := Move{Direction: pulse.Output, StreamIndex: 200, DeviceName: jabraSink}
	expectMoves(t, tracker.Plan(s, routes), move)
	expectMoves(t, tracker.Plan(s, routes))
	tracker.Reset()
	expectMoves(t, tracker.Plan(s, routes), move)
}

func TestTrackerSeparatesPlaybackAndCaptureIndexes(t *testing.T) {
	s := machineSnapshot()
	s.Outputs = []pulse.Stream{chromeStream(pulse.Output, 200, 77)}
	s.Inputs = []pulse.Stream{chromeStream(pulse.Input, 200, 90)}
	routes := []Route{{App: "chrome", Direction: RouteOutput, Device: jabraSink}, {App: "chrome", Direction: RouteInput, Device: ankerSource}}
	tracker := NewTracker()
	expectMoves(t, tracker.Plan(s, routes), Move{Direction: pulse.Output, StreamIndex: 200, DeviceName: jabraSink}, Move{Direction: pulse.Input, StreamIndex: 200, DeviceName: ankerSource})
	expectMoves(t, tracker.Plan(s, routes))
}

func TestTrackerDeletedRouteEndsWaiting(t *testing.T) {
	s := machineSnapshot()
	jabra := s.Sinks[1]
	s.Sinks = append(s.Sinks[:1], s.Sinks[2:]...)
	s.Outputs = []pulse.Stream{chromeStream(pulse.Output, 200, 77)}
	routes := []Route{{App: "chrome", Direction: RouteOutput, Device: jabraSink}}
	tracker := NewTracker()
	expectMoves(t, tracker.Plan(s, routes))
	expectMoves(t, tracker.Plan(s, nil))
	s.Sinks = append(s.Sinks, jabra)
	expectMoves(t, tracker.Plan(s, routes))
}

func TestTrackerNoRouteIsHandledWithoutLaterAutomaticMove(t *testing.T) {
	s := machineSnapshot()
	s.Outputs = []pulse.Stream{chromeStream(pulse.Output, 200, 77)}
	var tracker Tracker
	expectMoves(t, tracker.Plan(s, nil))
	expectMoves(t, tracker.Plan(s, []Route{{App: "chrome", Direction: RouteOutput, Device: jabraSink}}))
}
