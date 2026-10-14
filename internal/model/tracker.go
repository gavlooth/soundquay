package model

import "soundquay/internal/pulse"

// unlinkedDevice is the device index pactl reports for a stream not yet connected to a device.
const unlinkedDevice = 4294967295

type streamID struct {
	direction pulse.Direction
	index     uint32
}

type streamState struct {
	done   bool
	device uint32
	seen   uint64
}

// Tracker remembers streams already handled and streams waiting for a device.
// The coordinator owns it and calls Plan and Reset from one goroutine.
type Tracker struct {
	streams map[streamID]streamState
	seen    uint64
}

func NewTracker() *Tracker {
	return &Tracker{streams: make(map[streamID]streamState)}
}

// Reset forgets stream decisions after the audio server reconnects.
func (t *Tracker) Reset() {
	clear(t.streams)
	t.seen = 0
}

// Forget drops what the tracker knows about one stream, so the next Plan treats it as
// new. The coordinator calls it when moving the stream to its saved device failed.
func (t *Tracker) Forget(direction pulse.Direction, index uint32) {
	delete(t.streams, streamID{direction: direction, index: index})
}

// Plan returns each stream move once, leaving external moves alone.
func (t *Tracker) Plan(s pulse.Snapshot, routes []Route) []Move {
	if t.streams == nil {
		t.streams = make(map[streamID]streamState)
	}
	t.seen++
	var moves []Move
	plan := func(streams []pulse.Stream, devices []pulse.Device, direction pulse.Direction, routeDirection string) {
		for _, stream := range streams {
			id := streamID{direction: direction, index: stream.Index}
			state, exists := t.streams[id]
			state.seen = t.seen
			if state.done {
				t.streams[id] = state
				continue
			}
			// A stream can appear before it is linked (device index 4294967295); getting
			// its first real device then is not a move by someone else.
			if exists && state.device == unlinkedDevice {
				state.device = stream.DeviceIndex
			}
			if !isApp(stream.HasOwner, stream.Properties) || exists && stream.DeviceIndex != state.device {
				state.done = true
				t.streams[id] = state
				continue
			}
			key, routable := AppKey(stream.Properties)
			var deviceName string
			if routable {
				for _, route := range routes {
					if route.App == key && route.Direction == routeDirection {
						deviceName = route.Device
						break
					}
				}
			}
			state.device = stream.DeviceIndex
			state.done = deviceName == ""
			if deviceName != "" {
				for _, device := range devices {
					if device.Name != deviceName || monitor(device) {
						continue
					}
					if device.Index != stream.DeviceIndex {
						moves = append(moves, Move{Direction: direction, StreamIndex: stream.Index, DeviceName: deviceName})
					}
					state.done = true
					break
				}
			}
			t.streams[id] = state
		}
	}
	plan(s.Outputs, s.Sinks, pulse.Output, RouteOutput)
	plan(s.Inputs, s.Sources, pulse.Input, RouteInput)
	for id, state := range t.streams {
		if state.seen != t.seen {
			delete(t.streams, id)
		}
	}
	return moves
}
