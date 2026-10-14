package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"soundquay/internal/model"
	"soundquay/internal/pulse"
)

type fakeAudio struct {
	mu       sync.Mutex
	snapshot pulse.Snapshot
	// laterSnapshots, when set, are returned one per Snapshot call before snapshot.
	laterSnapshots []pulse.Snapshot
	events         chan pulse.Event
	calls          []string
	failures       map[string]error
}

func (f *fakeAudio) record(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.failures[call]
}
func (f *fakeAudio) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}
func (f *fakeAudio) Subscribe(context.Context) <-chan pulse.Event { return f.events }
func (f *fakeAudio) Snapshot(context.Context) (pulse.Snapshot, error) {
	err := f.record("snapshot")
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.laterSnapshots) > 0 {
		s := f.laterSnapshots[0]
		f.laterSnapshots = f.laterSnapshots[1:]
		return s, err
	}
	return f.snapshot, err
}
func (f *fakeAudio) SetDefault(_ context.Context, d pulse.Direction, name string) error {
	return f.record(fmt.Sprintf("default:%d:%s", d, name))
}
func (f *fakeAudio) SetVolume(_ context.Context, d pulse.Direction, name string, value int) error {
	return f.record(fmt.Sprintf("volume:%d:%s:%d", d, name, value))
}
func (f *fakeAudio) SetMute(_ context.Context, d pulse.Direction, name string, value bool) error {
	return f.record(fmt.Sprintf("mute:%d:%s:%t", d, name, value))
}
func (f *fakeAudio) SetPort(_ context.Context, d pulse.Direction, name, port string) error {
	return f.record(fmt.Sprintf("port:%d:%s:%s", d, name, port))
}
func (f *fakeAudio) SetCardProfile(_ context.Context, name, profile string) error {
	return f.record("profile:" + name + ":" + profile)
}
func (f *fakeAudio) MoveStream(_ context.Context, d pulse.Direction, index uint32, name string) error {
	return f.record(fmt.Sprintf("move:%d:%d:%s", d, index, name))
}
func (f *fakeAudio) SetStreamVolume(_ context.Context, d pulse.Direction, index uint32, value int) error {
	return f.record(fmt.Sprintf("stream-volume:%d:%d:%d", d, index, value))
}
func (f *fakeAudio) SetStreamMute(_ context.Context, d pulse.Direction, index uint32, value bool) error {
	return f.record(fmt.Sprintf("stream-mute:%d:%d:%t", d, index, value))
}
func executePending(t *testing.T, c *coordinator) {
	t.Helper()
	for _, cmd := range c.commands.take() {
		if err := cmd.run(context.Background()); err != nil {
			t.Fatalf("%s: %v", cmd.op, err)
		}
	}
}

func TestInboxRetainsMessagesAndLatestSliderValues(t *testing.T) {
	c := newCoordinator(&fakeAudio{}, "", model.Config{Version: model.ConfigVersion}, nil)
	var workers sync.WaitGroup
	const count = 1000
	for worker := range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range count {
				c.Dispatch(WriteDone{Op: strconv.Itoa(worker*count + i)})
			}
		}()
	}
	workers.Wait()
	for i := range 1000 {
		c.Dispatch(VolumeChanged{Key: "output", Value: i})
		c.Dispatch(VolumeChanged{Key: "app:chrome", Value: 1000 - i})
	}
	pending := c.inbox.take()
	seen := make(map[string]bool)
	sliders := make(map[string]int)
	for _, message := range pending {
		switch m := message.(type) {
		case WriteDone:
			if seen[m.Op] {
				t.Fatalf("duplicate %s", m.Op)
			}
			seen[m.Op] = true
		case VolumeChanged:
			if _, ok := sliders[m.Key]; ok {
				t.Fatalf("duplicate slider %s", m.Key)
			}
			sliders[m.Key] = m.Value
		}
	}
	if len(seen) != 8*count {
		t.Fatalf("received %d messages, want %d", len(seen), 8*count)
	}
	if !reflect.DeepEqual(sliders, map[string]int{"output": 999, "app:chrome": 1}) {
		t.Fatalf("slider values: %v", sliders)
	}
}
func TestSnapshotDebounceCollectsBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fake := &fakeAudio{events: make(chan pulse.Event, 10)}
		results := make(chan SnapshotReady, 10)
		go snapshotLoop(ctx, fake, fake.Snapshot, func(msg Msg) { results <- msg.(SnapshotReady) }, 100*time.Millisecond)
		fake.events <- pulse.Event{Kind: pulse.EventResync}
		synctest.Wait()
		time.Sleep(40 * time.Millisecond)
		fake.events <- pulse.Event{Kind: pulse.EventChange}
		fake.events <- pulse.Event{Kind: pulse.EventNew}
		synctest.Wait()
		time.Sleep(59 * time.Millisecond)
		synctest.Wait()
		if calls := fake.callLog(); len(calls) != 0 {
			t.Fatalf("early snapshot: %v", calls)
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if calls := fake.callLog(); len(calls) != 1 || len(results) != 1 {
			t.Fatalf("calls %v, results %d", calls, len(results))
		}
		if result := <-results; !result.Reset {
			t.Fatal("resync was lost")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if calls := fake.callLog(); len(calls) != 1 {
			t.Fatalf("idle snapshots: %v", calls)
		}
		fake.events <- pulse.Event{Kind: pulse.EventChange}
		synctest.Wait()
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if result := <-results; result.Reset {
			t.Fatal("later refresh unexpectedly resets tracker")
		}
		cancel()
		synctest.Wait()
	})
}
func TestProfileSelectionUsesResultingDevice(t *testing.T) {
	for _, direction := range []pulse.Direction{pulse.Output, pulse.Input} {
		t.Run(routeDirection(direction), func(t *testing.T) {
			device := pulse.Device{Name: "new-device", CardName: "card", ActivePort: "old-port", Ports: []pulse.DevicePort{{Name: "headphones"}}}
			fake := &fakeAudio{}
			if direction == pulse.Output {
				fake.snapshot.Sinks = []pulse.Device{device}
			} else {
				fake.snapshot.Sources = []pulse.Device{device}
			}
			entry := model.SwitchEntry{Direction: direction, CardName: "card", PortName: "headphones", Profile: "analog", Label: "Headphones"}
			if err := selectSwitchEntry(context.Background(), fake, entry); err != nil {
				t.Fatal(err)
			}
			want := []string{"profile:card:analog", "snapshot", fmt.Sprintf("port:%d:new-device:headphones", direction), fmt.Sprintf("default:%d:new-device", direction)}
			if !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("calls %v, want %v", fake.calls, want)
			}
		})
	}
}
func TestProfileSelectionStopsAfterFailure(t *testing.T) {
	failure := errors.New("profile unavailable")
	fake := &fakeAudio{failures: map[string]error{"profile:card:analog": failure}}
	err := selectSwitchEntry(context.Background(), fake, model.SwitchEntry{CardName: "card", Profile: "analog"})
	if !errors.Is(err, failure) || len(fake.calls) != 1 {
		t.Fatalf("error %v, calls %v", err, fake.calls)
	}
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeAudio{}
		err := selectSwitchEntry(context.Background(), fake, model.SwitchEntry{CardName: "card", Profile: "analog", PortName: "missing"})
		calls := fake.callLog()
		if err == nil || calls[0] != "profile:card:analog" || len(calls) < 3 || slices.ContainsFunc(calls[1:], func(c string) bool { return c != "snapshot" }) {
			t.Fatalf("error %v, calls %v (want profile switch, then only snapshots until giving up)", err, calls)
		}
	})
}
func TestProfileSelectionWaitsForDeviceToAppear(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		device := pulse.Device{Name: "headset", CardName: "card", ActivePort: "headphones", Ports: []pulse.DevicePort{{Name: "headphones"}}}
		fake := &fakeAudio{laterSnapshots: []pulse.Snapshot{{}, {}}, snapshot: pulse.Snapshot{Sinks: []pulse.Device{device}}}
		entry := model.SwitchEntry{CardName: "card", PortName: "headphones", Profile: "analog"}
		if err := selectSwitchEntry(context.Background(), fake, entry); err != nil {
			t.Fatal(err)
		}
		want := []string{"profile:card:analog", "snapshot", "snapshot", "snapshot", "default:0:headset"}
		if got := fake.callLog(); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls %v, want %v", got, want)
		}
	})
}
func TestVolumeAndMuteChangesClearTheErrorLine(t *testing.T) {
	for _, msg := range []Msg{
		SetMuted{Direction: pulse.Output, Muted: true},
		VolumeChanged{Key: "output", Direction: pulse.Output, Value: 40},
	} {
		c := newCoordinator(&fakeAudio{}, "", model.Config{Version: model.ConfigVersion}, nil)
		c.lastError = "Move app: no such stream"
		c.handle(context.Background(), msg)
		for _, v := range c.volumes {
			v.timer.Stop()
		}
		if c.lastError != "" {
			t.Fatalf("%T kept error %q", msg, c.lastError)
		}
	}
}
func TestProfileSelectionSkipsCurrentPort(t *testing.T) {
	fake := &fakeAudio{snapshot: pulse.Snapshot{Sinks: []pulse.Device{{Name: "sink", CardName: "card", ActivePort: "port", Ports: []pulse.DevicePort{{Name: "port"}}}}}}
	if err := selectSwitchEntry(context.Background(), fake, model.SwitchEntry{CardName: "card", PortName: "port", Profile: "analog"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.calls, []string{"profile:card:analog", "snapshot", "default:0:sink"}) {
		t.Fatalf("calls %v", fake.calls)
	}
}
func appSnapshot() pulse.Snapshot {
	props := func(binary string) map[string]string {
		return map[string]string{"application.process.binary": binary, "application.name": binary}
	}
	return pulse.Snapshot{Info: pulse.ServerInfo{DefaultSinkName: "default", DefaultSourceName: "mic"}, Sinks: []pulse.Device{{Index: 1, Name: "default"}, {Index: 2, Name: "speakers"}}, Sources: []pulse.Device{{Index: 3, Name: "mic"}}, Outputs: []pulse.Stream{{Direction: pulse.Output, Index: 10, DeviceIndex: 1, Properties: props("chrome")}, {Direction: pulse.Output, Index: 11, DeviceIndex: 1, Properties: props("chrome")}, {Direction: pulse.Output, Index: 12, DeviceIndex: 1, Properties: props("spotify")}}, Inputs: []pulse.Stream{{Direction: pulse.Input, Index: 13, DeviceIndex: 3, Properties: props("chrome")}}}
}
func TestAppChoiceSavesRouteAndMovesItsOutputStreams(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	fake := &fakeAudio{}
	c := newCoordinator(fake, path, model.Config{Version: model.ConfigVersion}, nil)
	c.handle(context.Background(), SnapshotReady{Snapshot: appSnapshot()})
	c.handle(context.Background(), ChooseAppDevice{App: "chrome", Direction: pulse.Output, Device: "speakers"})
	saved, err := model.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	route, ok := saved.RouteFor("chrome", model.RouteOutput)
	if !ok || route.Device != "speakers" {
		t.Fatalf("saved routes %v", saved.Routes)
	}
	executePending(t, c)
	want := []string{"move:0:10:speakers", "move:0:11:speakers"}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls %v, want %v", fake.calls, want)
	}
	fake.calls = nil
	c.handle(context.Background(), ChooseAppDevice{App: "chrome", Direction: pulse.Output})
	executePending(t, c)
	saved, err = model.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saved.RouteFor("chrome", model.RouteOutput); ok {
		t.Fatal("Follow default kept the route")
	}
	if !reflect.DeepEqual(fake.calls, []string{"move:0:10:default", "move:0:11:default"}) {
		t.Fatalf("calls %v", fake.calls)
	}
	fake.calls = nil
	c.handle(context.Background(), ChooseAppDevice{App: "chrome", Direction: pulse.Input, Device: "mic"})
	executePending(t, c)
	if !reflect.DeepEqual(fake.calls, []string{"move:1:13:mic"}) {
		t.Fatalf("input calls %v", fake.calls)
	}
}
func TestDeletingRouteOfRunningAppMovesItsStreamsToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	fake := &fakeAudio{}
	cfg := model.Config{Version: model.ConfigVersion}.
		WithRoute("chrome", "chrome", model.RouteOutput, "speakers").
		WithRoute("gone", "Gone App", model.RouteOutput, "speakers")
	c := newCoordinator(fake, path, cfg, nil)
	c.handle(context.Background(), SnapshotReady{Snapshot: appSnapshot()})
	executePending(t, c)
	fake.calls = nil
	c.handle(context.Background(), DeleteRoute{App: "chrome", Direction: model.RouteOutput})
	executePending(t, c)
	if _, ok := c.config.RouteFor("chrome", model.RouteOutput); ok {
		t.Fatal("route kept")
	}
	if want := []string{"move:0:10:default", "move:0:11:default"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls %v, want %v", fake.calls, want)
	}
	fake.calls = nil
	c.handle(context.Background(), DeleteRoute{App: "gone", Direction: model.RouteOutput})
	executePending(t, c)
	if _, ok := c.config.RouteFor("gone", model.RouteOutput); ok || len(fake.calls) != 0 {
		t.Fatalf("not-running app: route kept %v, calls %v", ok, fake.calls)
	}
}
func TestFailedSavedRouteMoveIsPlannedAgain(t *testing.T) {
	fake := &fakeAudio{failures: map[string]error{"move:0:10:speakers": errors.New("no such stream")}}
	cfg := model.Config{Version: model.ConfigVersion}.WithRoute("chrome", "chrome", model.RouteOutput, "speakers")
	c := newCoordinator(fake, "", cfg, nil)
	runAndReport := func() {
		for _, cmd := range c.commands.take() {
			c.handle(context.Background(), WriteDone{Op: cmd.op, Err: cmd.run(context.Background()), RouteMove: cmd.routeMove})
		}
	}
	c.handle(context.Background(), SnapshotReady{Snapshot: appSnapshot()})
	runAndReport()
	fake.calls = nil
	delete(fake.failures, "move:0:10:speakers")
	c.handle(context.Background(), SnapshotReady{Snapshot: appSnapshot()})
	runAndReport()
	if want := []string{"move:0:10:speakers"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls after failure %v, want %v (only the failed stream is retried)", fake.calls, want)
	}
}
func TestAppMuteAndVolumeOnlyChangeTheChosenDirection(t *testing.T) {
	fake := &fakeAudio{}
	c := newCoordinator(fake, "", model.Config{Version: model.ConfigVersion}, nil)
	c.handle(context.Background(), SnapshotReady{Snapshot: appSnapshot()})
	executePending(t, c)
	fake.calls = nil
	c.handle(context.Background(), SetMuted{App: "chrome", Direction: pulse.Input, Muted: true})
	c.handle(context.Background(), VolumeChanged{Key: "app:chrome:in", App: "chrome", Direction: pulse.Input, Value: 70})
	pending := c.volumes["app:chrome:in"]
	pending.timer.Stop()
	c.handle(context.Background(), applyVolume{Key: "app:chrome:in", Revision: pending.revision})
	executePending(t, c)
	if want := []string{"stream-mute:1:13:true", "stream-volume:1:13:70"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls %v, want %v", fake.calls, want)
	}
}
func TestResetAppsForgetsRoutesAndMovesStrayStreamsToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	fake := &fakeAudio{failures: map[string]error{"move:0:10:default": errors.New("stream gone")}}
	cfg := model.Config{Version: model.ConfigVersion, Nicknames: map[string]string{"speakers": "Desk"}}.
		WithRoute("chrome", "chrome", model.RouteOutput, "speakers").
		WithRoute("chrome", "chrome", model.RouteInput, "headset-mic")
	c := newCoordinator(fake, path, cfg, nil)
	s := appSnapshot()
	s.Sources = append(s.Sources, pulse.Device{Index: 4, Name: "headset-mic"})
	s.Outputs[0].DeviceIndex = 2 // chrome stream 10 sits on "speakers"
	s.Outputs[2].DeviceIndex = 2 // spotify stream 12 sits on "speakers" without a route
	s.Inputs[0].DeviceIndex = 4  // chrome capture 13 sits on "headset-mic"
	c.handle(context.Background(), SnapshotReady{Snapshot: s})
	executePending(t, c)
	fake.calls = nil
	c.handle(context.Background(), ResetApps{})
	for _, cmd := range c.commands.take() {
		c.handle(context.Background(), WriteDone{Op: cmd.op, Err: cmd.run(context.Background())})
	}
	saved, err := model.LoadConfig(path)
	if err != nil || len(saved.Routes) != 0 || len(c.config.Routes) != 0 {
		t.Fatalf("routes kept: saved %v, live %v, err %v", saved.Routes, c.config.Routes, err)
	}
	if saved.Nicknames["speakers"] != "Desk" {
		t.Fatalf("reset touched other settings: %v", saved.Nicknames)
	}
	slices.Sort(fake.calls)
	if want := []string{"move:0:10:default", "move:0:12:default", "move:1:13:mic"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls %v, want %v (streams already on the default stay)", fake.calls, want)
	}
	if !strings.Contains(c.lastError, "stream gone") {
		t.Fatalf("failed move hidden by later successes: error line %q", c.lastError)
	}
}
func TestAppChoiceSaveFailureDoesNotMoveStreams(t *testing.T) {
	c := newCoordinator(&fakeAudio{}, "", model.Config{Version: model.ConfigVersion}, nil)
	c.handle(context.Background(), SnapshotReady{Snapshot: appSnapshot()})
	c.save = func(string, model.Config) error { return errors.New("disk full") }
	c.handle(context.Background(), ChooseAppDevice{App: "chrome", Direction: pulse.Output, Device: "speakers"})
	if len(c.config.Routes) != 0 || len(c.commands.take()) != 0 || c.lastError != "Save preferences: disk full" {
		t.Fatalf("routes %v, error %q", c.config.Routes, c.lastError)
	}
}
func TestVolumeWritesWaitForLatestSliderValue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeAudio{}
		c := newCoordinator(fake, "", model.Config{Version: model.ConfigVersion}, nil)
		c.handle(context.Background(), SnapshotReady{Snapshot: appSnapshot()})
		c.handle(context.Background(), VolumeChanged{Key: "output", Direction: pulse.Output, Value: 30})
		time.Sleep(30 * time.Millisecond)
		c.handle(context.Background(), VolumeChanged{Key: "output", Direction: pulse.Output, Value: 80})
		time.Sleep(49 * time.Millisecond)
		synctest.Wait()
		for _, m := range c.inbox.take() {
			c.handle(context.Background(), m)
		}
		if len(c.commands.take()) != 0 {
			t.Fatal("volume wrote before the last value settled")
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		for _, m := range c.inbox.take() {
			c.handle(context.Background(), m)
		}
		executePending(t, c)
		if !reflect.DeepEqual(fake.calls, []string{"volume:0:default:80"}) {
			t.Fatalf("calls %v", fake.calls)
		}
	})
}
