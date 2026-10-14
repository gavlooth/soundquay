package pulse

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseEvents(t *testing.T) {
	for _, check := range []struct {
		line string
		want Event
	}{
		{"Event 'new' on sink-input #12", Event{Kind: EventNew, Facility: "sink-input", Index: 12}},
		{"Event 'change' on source-output #13", Event{Kind: EventChange, Facility: "source-output", Index: 13}},
		{"Event 'remove' on client #10", Event{Kind: EventRemove, Facility: "client", Index: 10}},
		{"Event 'change' on card #1549", Event{Kind: EventChange, Facility: "card", Index: 1549}},
		{"Event 'change' on server #4294967295", Event{Kind: EventChange, Facility: "server", Index: math.MaxUint32}},
		{"Event 'change' on server #-1", Event{Kind: EventChange, Facility: "server", Index: math.MaxUint32}},
		{"  Event 'change' on sink #1550  ", Event{Kind: EventChange, Facility: "sink", Index: 1550}},
	} {
		got, ok := parseEvent(check.line)
		if !ok || got != check.want {
			t.Fatalf("%q: %+v, %v, want %+v", check.line, got, ok, check.want)
		}
	}
	for _, line := range []string{"", "pactl: disconnected", "Event 'unknown' on sink #1", "Event 'new' on sink 1", "Event 'new' on sink #", "Event 'change' on server #-2", "Event 'new' on sink #4294967296", "Event 'new' on sink #1 trailing", "Event 'new' near sink #1"} {
		if event, ok := parseEvent(line); ok {
			t.Fatalf("invalid event %q accepted: %+v", line, event)
		}
	}
}

func idleSubscriber(ctx context.Context) subscriberProcess {
	reader, writer := io.Pipe()
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		if err := writer.Close(); err != nil {
			panic(err)
		}
		close(done)
	}()
	return subscriberProcess{output: reader, wait: func() error { <-done; return nil }}
}

func receiveEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("subscriber closed before event")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber event did not arrive")
		return Event{}
	}
}

func expectClosed(t *testing.T, events <-chan Event) {
	t.Helper()
	select {
	case event, ok := <-events:
		if ok {
			t.Fatalf("event after cancellation: %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not close after cancellation")
	}
}

func TestSubscribeResyncEventsAndRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewClient(nil)
	c.retryDelay = time.Millisecond
	c.retryMax = 2 * time.Millisecond
	starts := 0
	c.start = func(ctx context.Context) (subscriberProcess, error) {
		starts++
		if starts == 1 {
			return subscriberProcess{output: io.NopCloser(strings.NewReader("unparseable\nEvent 'new' on sink-input #12\nEvent 'change' on server #4294967295\n")), wait: func() error { return errors.New("server restarted") }}, nil
		}
		return idleSubscriber(ctx), nil
	}
	events := c.Subscribe(ctx)
	if got := receiveEvent(t, events); got.Kind != EventResync {
		t.Fatalf("first event: %+v", got)
	}
	if got := receiveEvent(t, events); got != (Event{Kind: EventNew, Facility: "sink-input", Index: 12}) {
		t.Fatalf("new stream: %+v", got)
	}
	if got := receiveEvent(t, events); got != (Event{Kind: EventChange, Facility: "server", Index: math.MaxUint32}) {
		t.Fatalf("server event: %+v", got)
	}
	if got := receiveEvent(t, events); got.Kind != EventResync {
		t.Fatalf("restart event: %+v", got)
	}
	cancel()
	expectClosed(t, events)
}

func TestSubscribeRetriesStartFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewClient(nil)
	c.retryDelay = time.Millisecond
	c.retryMax = 2 * time.Millisecond
	starts := make(chan int, 3)
	attempt := 0
	c.start = func(ctx context.Context) (subscriberProcess, error) {
		attempt++
		starts <- attempt
		if attempt < 3 {
			return subscriberProcess{}, errors.New("pactl unavailable")
		}
		return idleSubscriber(ctx), nil
	}
	events := c.Subscribe(ctx)
	if got := receiveEvent(t, events); got.Kind != EventResync {
		t.Fatalf("successful start: %+v", got)
	}
	for want := 1; want <= 3; want++ {
		select {
		case got := <-starts:
			if got != want {
				t.Fatalf("attempt %d, want %d", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("missing retry")
		}
	}
	cancel()
	expectClosed(t, events)
}

func TestSubscribeCancellationWithoutReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewClient(nil)
	started := make(chan struct{})
	c.start = func(ctx context.Context) (subscriberProcess, error) {
		process := idleSubscriber(ctx)
		close(started)
		return process, nil
	}
	events := c.Subscribe(ctx)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not start")
	}
	cancel()
	// A resync send and cancellation can both be ready after this reader resumes.
	select {
	case _, ok := <-events:
		if ok {
			expectClosed(t, events)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow reader prevented cancellation")
	}
}

func TestSubscribeCancellationDuringRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewClient(nil)
	c.retryDelay = time.Hour
	failed := make(chan struct{})
	c.start = func(context.Context) (subscriberProcess, error) {
		close(failed)
		return subscriberProcess{}, errors.New("server offline")
	}
	events := c.Subscribe(ctx)
	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not attempt start")
	}
	cancel()
	expectClosed(t, events)
}
