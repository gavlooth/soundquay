package main

import (
	"context"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"soundquay/internal/model"
	"soundquay/internal/pulse"
)

type Msg interface{ isMsg() }
type SnapshotReady struct {
	Snapshot pulse.Snapshot
	Err      error
	Reset    bool
}

func (SnapshotReady) isMsg() {}

type WriteDone struct {
	Op        string
	Err       error
	RouteMove *model.Move // set when the write applied a saved route
}

func (WriteDone) isMsg() {}

type SelectDevice struct{ Entry model.SwitchEntry }

func (SelectDevice) isMsg() {}

type ChooseAppDevice struct {
	App, Name string
	Direction pulse.Direction
	Device    string
}

func (ChooseAppDevice) isMsg() {}

type DeleteRoute struct{ App, Direction string }

func (DeleteRoute) isMsg() {}

// ResetApps forgets every saved app device and sends every running app's streams to
// the current default output and input.
type ResetApps struct{}

func (ResetApps) isMsg() {}

type DevicePreference struct {
	Device, Nickname string
	Hidden           bool
}

func (DevicePreference) isMsg() {}

type VolumeChanged struct {
	Key, App  string
	Direction pulse.Direction
	Value     int
}

func (VolumeChanged) isMsg() {}

type applyVolume struct {
	Key      string
	Revision uint64
}

func (applyVolume) isMsg() {}

type SetMuted struct {
	App       string
	Direction pulse.Direction
	Muted     bool
}

func (SetMuted) isMsg() {}

type inbox[T any] struct {
	mu      sync.Mutex
	pending []T
	wake    chan struct{}
}

func newInbox[T any]() *inbox[T] { return &inbox[T]{wake: make(chan struct{}, 1)} }
func (q *inbox[T]) push(v T, replace func(T) bool) {
	q.mu.Lock()
	replaced := false
	if replace != nil {
		for i := len(q.pending) - 1; i >= 0; i-- {
			if replace(q.pending[i]) {
				q.pending[i] = v
				replaced = true
				break
			}
		}
	}
	if !replaced {
		q.pending = append(q.pending, v)
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
func (q *inbox[T]) take() []T {
	q.mu.Lock()
	result := q.pending
	q.pending = nil
	q.mu.Unlock()
	return result
}

type audioClient interface {
	pulse.Subscriber
	Snapshot(context.Context) (pulse.Snapshot, error)
	SetDefault(context.Context, pulse.Direction, string) error
	SetVolume(context.Context, pulse.Direction, string, int) error
	SetMute(context.Context, pulse.Direction, string, bool) error
	SetPort(context.Context, pulse.Direction, string, string) error
	SetCardProfile(context.Context, string, string) error
	MoveStream(context.Context, pulse.Direction, uint32, string) error
	SetStreamVolume(context.Context, pulse.Direction, uint32, int) error
	SetStreamMute(context.Context, pulse.Direction, uint32, bool) error
}
type command struct {
	op  string
	run func(context.Context) error
	// routeMove is set for moves that apply a saved route, so a failure can be planned again.
	routeMove *model.Move
}
type volumePending struct {
	value    VolumeChanged
	revision uint64
	timer    *time.Timer
}
type renderedState struct {
	View   model.View
	Config model.Config
	Error  string
}
type coordinator struct {
	client    audioClient
	path      string
	config    model.Config
	tracker   *model.Tracker
	snapshot  pulse.Snapshot
	view      model.View
	lastError string
	inbox     *inbox[Msg]
	commands  *inbox[command]
	volumes   map[string]volumePending
	revision  uint64
	render    func(renderedState)
	save      func(string, model.Config) error
}

func newCoordinator(client audioClient, path string, cfg model.Config, render func(renderedState)) *coordinator {
	return &coordinator{client: client, path: path, config: cfg, tracker: model.NewTracker(), inbox: newInbox[Msg](), commands: newInbox[command](), volumes: make(map[string]volumePending), render: render, save: model.SaveConfig}
}
func (c *coordinator) Dispatch(msg Msg) {
	var replace func(Msg) bool
	if volume, ok := msg.(VolumeChanged); ok {
		replace = func(old Msg) bool { v, ok := old.(VolumeChanged); return ok && v.Key == volume.Key }
	}
	c.inbox.push(msg, replace)
}
func (c *coordinator) Run(ctx context.Context) {
	go c.execute(ctx)
	go snapshotLoop(ctx, c.client, c.client.Snapshot, c.Dispatch, 100*time.Millisecond)
	c.publish()
	defer func() {
		for _, v := range c.volumes {
			v.timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.inbox.wake:
			for _, msg := range c.inbox.take() {
				c.handle(ctx, msg)
			}
		}
	}
}
func snapshotLoop(ctx context.Context, sub pulse.Subscriber, snapshot func(context.Context) (pulse.Snapshot, error), dispatch func(Msg), delay time.Duration) {
	events := sub.Subscribe(ctx)
	var timer *time.Timer
	var tick <-chan time.Time
	reset := false
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				if tick == nil {
					return
				}
				continue
			}
			reset = reset || event.Kind == pulse.EventResync
			if tick == nil {
				timer = time.NewTimer(delay)
				tick = timer.C
			}
		case <-tick:
			tick = nil
			s, err := snapshot(ctx)
			if ctx.Err() != nil {
				return
			}
			dispatch(SnapshotReady{Snapshot: s, Err: err, Reset: reset})
			reset = false
			if events == nil {
				return
			}
		}
	}
}
func (c *coordinator) execute(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.commands.wake:
			for _, cmd := range c.commands.take() {
				if ctx.Err() != nil {
					return
				}
				c.Dispatch(WriteDone{Op: cmd.op, Err: cmd.run(ctx), RouteMove: cmd.routeMove})
			}
		}
	}
}
func (c *coordinator) write(op string, run func(context.Context) error) {
	c.commands.push(command{op: op, run: run}, nil)
}
func (c *coordinator) publish() {
	if c.render != nil {
		c.render(renderedState{View: c.view, Config: c.config, Error: c.lastError})
	}
}
func (c *coordinator) report(op string, err error) {
	if err != nil {
		c.lastError = strings.Join(strings.Fields(op+": "+err.Error()), " ")
		log.Print(c.lastError)
	} else {
		c.lastError = ""
	}
}
func (c *coordinator) persist(cfg model.Config) bool {
	if err := c.save(c.path, cfg); err != nil {
		c.report("Save preferences", err)
		return false
	}
	c.config = cfg
	c.view = model.Build(c.snapshot, c.config)
	c.report("Save preferences", nil)
	return true
}
func routeDirection(d pulse.Direction) string {
	if d == pulse.Input {
		return model.RouteInput
	}
	return model.RouteOutput
}
func (c *coordinator) app(key string) (model.App, bool) {
	for _, a := range c.view.Apps {
		if a.Key == key {
			return a, true
		}
	}
	return model.App{}, false
}
func streams(a model.App, d pulse.Direction) []model.AppStream {
	if d == pulse.Input {
		return a.Inputs
	}
	return a.Outputs
}
func (c *coordinator) defaultDevice(d pulse.Direction) string {
	if d == pulse.Input {
		return c.view.DefaultInput
	}
	return c.view.DefaultOutput
}
func (c *coordinator) handle(ctx context.Context, msg Msg) {
	switch m := msg.(type) {
	case SnapshotReady:
		if m.Reset {
			c.tracker.Reset()
		}
		if m.Err != nil {
			c.report("Read audio state", m.Err)
			break
		}
		c.snapshot = m.Snapshot
		c.view = model.Build(c.snapshot, c.config)
		for _, move := range c.tracker.Plan(c.snapshot, c.config.Routes) {
			c.commands.push(command{op: "Apply saved route", routeMove: &move, run: func(ctx context.Context) error {
				return c.client.MoveStream(ctx, move.Direction, move.StreamIndex, move.DeviceName)
			}}, nil)
		}
	case WriteDone:
		// A later successful write must not hide an earlier failure from the same batch,
		// so only failures touch the error line; the next user change clears it.
		if m.Err != nil {
			c.report(m.Op, m.Err)
		}
		if m.Err != nil && m.RouteMove != nil {
			// Plan this stream again on the next snapshot instead of leaving it on the wrong device.
			c.tracker.Forget(m.RouteMove.Direction, m.RouteMove.StreamIndex)
		}
	case SelectDevice:
		c.report("Select device", nil)
		c.write("Select device", func(ctx context.Context) error { return selectSwitchEntry(ctx, c.client, m.Entry) })
	case ChooseAppDevice:
		a, ok := c.app(m.App)
		if !ok || !a.Routable {
			break
		}
		cfg := c.config.WithoutRoute(a.Key, routeDirection(m.Direction))
		if m.Device != "" {
			cfg = c.config.WithRoute(a.Key, a.Name, routeDirection(m.Direction), m.Device)
		}
		if !c.persist(cfg) {
			break
		}
		target := m.Device
		if target == "" {
			target = c.defaultDevice(m.Direction)
		}
		if target == "" && len(streams(a, m.Direction)) > 0 {
			c.report("Move app", fmt.Errorf("no default device"))
			break
		}
		for _, stream := range streams(a, m.Direction) {
			c.write("Move "+a.Name, func(ctx context.Context) error { return c.client.MoveStream(ctx, m.Direction, stream.Index, target) })
		}
	case ResetApps:
		cfg := c.config
		cfg.Routes = nil
		if !c.persist(cfg) {
			break
		}
		for _, a := range c.view.Apps {
			for _, direction := range []pulse.Direction{pulse.Output, pulse.Input} {
				target := c.defaultDevice(direction)
				if target == "" {
					continue
				}
				for _, stream := range streams(a, direction) {
					if stream.DeviceName == target {
						continue
					}
					c.write("Move "+a.Name+" to default", func(ctx context.Context) error {
						return c.client.MoveStream(ctx, direction, stream.Index, target)
					})
				}
			}
		}
	case DeleteRoute:
		if a, ok := c.app(m.App); ok && a.Running && a.Routable {
			direction := pulse.Output
			if m.Direction == model.RouteInput {
				direction = pulse.Input
			}
			// Deleting a route of a running app sends its live streams back to the default device.
			c.handle(ctx, ChooseAppDevice{App: m.App, Name: a.Name, Direction: direction})
			return
		}
		c.persist(c.config.WithoutRoute(m.App, m.Direction))
	case DevicePreference:
		cfg := c.config
		cfg.HiddenDevices = slices.Clone(c.config.HiddenDevices)
		cfg.Routes = slices.Clone(c.config.Routes)
		cfg.Nicknames = maps.Clone(c.config.Nicknames)
		if cfg.Nicknames == nil {
			cfg.Nicknames = make(map[string]string)
		}
		if m.Nickname == "" {
			delete(cfg.Nicknames, m.Device)
		} else {
			cfg.Nicknames[m.Device] = m.Nickname
		}
		cfg.HiddenDevices = slices.DeleteFunc(cfg.HiddenDevices, func(name string) bool { return name == m.Device })
		if m.Hidden {
			cfg.HiddenDevices = append(cfg.HiddenDevices, m.Device)
		}
		c.persist(cfg)
	case VolumeChanged:
		if c.lastError != "" {
			// A new user change clears the previous failure from the error line.
			c.lastError = ""
			c.publish()
		}
		if old, ok := c.volumes[m.Key]; ok {
			old.timer.Stop()
		}
		c.revision++
		rev := c.revision
		c.volumes[m.Key] = volumePending{value: m, revision: rev, timer: time.AfterFunc(50*time.Millisecond, func() { c.Dispatch(applyVolume{Key: m.Key, Revision: rev}) })}
		return
	case applyVolume:
		pending, ok := c.volumes[m.Key]
		if !ok || pending.revision != m.Revision {
			break
		}
		delete(c.volumes, m.Key)
		v := pending.value
		if v.App == "" {
			target := c.defaultDevice(v.Direction)
			if target != "" {
				c.write("Set volume", func(ctx context.Context) error { return c.client.SetVolume(ctx, v.Direction, target, v.Value) })
			}
		} else if a, ok := c.app(v.App); ok {
			for _, stream := range streams(a, v.Direction) {
				c.write("Set "+a.Name+" volume", func(ctx context.Context) error {
					return c.client.SetStreamVolume(ctx, stream.Direction, stream.Index, v.Value)
				})
			}
		}
		return
	case SetMuted:
		c.report("Set mute", nil)
		if m.App == "" {
			target := c.defaultDevice(m.Direction)
			if target != "" {
				c.write("Set mute", func(ctx context.Context) error { return c.client.SetMute(ctx, m.Direction, target, m.Muted) })
			}
		} else if a, ok := c.app(m.App); ok {
			for _, stream := range streams(a, m.Direction) {
				c.write("Set "+a.Name+" mute", func(ctx context.Context) error {
					return c.client.SetStreamMute(ctx, stream.Direction, stream.Index, m.Muted)
				})
			}
		}
	}
	c.publish()
}
func selectSwitchEntry(ctx context.Context, client audioClient, entry model.SwitchEntry) error {
	target := entry.DeviceName
	needsPort := entry.PortName != ""
	if entry.Profile != "" {
		if err := client.SetCardProfile(ctx, entry.CardName, entry.Profile); err != nil {
			return err
		}
		// The server creates the card's new sink or source shortly after the profile
		// switch; look for it for at most profileDeviceWait.
		deadline := time.Now().Add(profileDeviceWait)
		for {
			s, err := client.Snapshot(ctx)
			if err != nil {
				return err
			}
			target, needsPort = findPortDevice(s, entry)
			if target != "" || !time.Now().Before(deadline) {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(profileDevicePoll):
			}
		}
	}
	if target == "" {
		return fmt.Errorf("device for %s is not present", entry.Label)
	}
	if needsPort {
		if err := client.SetPort(ctx, entry.Direction, target, entry.PortName); err != nil {
			return err
		}
	}
	return client.SetDefault(ctx, entry.Direction, target)
}

const (
	profileDeviceWait = 2 * time.Second
	profileDevicePoll = 100 * time.Millisecond
)

// findPortDevice returns the device on the entry's card that has the entry's port,
// and whether that port still has to be made active.
func findPortDevice(s pulse.Snapshot, entry model.SwitchEntry) (name string, needsPort bool) {
	devices := s.Sinks
	if entry.Direction == pulse.Input {
		devices = s.Sources
	}
	for _, device := range devices {
		if device.CardName != entry.CardName {
			continue
		}
		for _, port := range device.Ports {
			if port.Name == entry.PortName {
				return device.Name, device.ActivePort != entry.PortName
			}
		}
	}
	return "", false
}
