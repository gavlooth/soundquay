package pulse

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type subscriberProcess struct {
	output io.ReadCloser
	wait   func() error
}

type startSubscriber func(context.Context) (subscriberProcess, error)

func startPactlSubscriber(ctx context.Context) (subscriberProcess, error) {
	cmd := exec.CommandContext(ctx, "pactl", "subscribe")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return subscriberProcess{}, fmt.Errorf("pactl subscribe stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		if closeErr := stdout.Close(); closeErr != nil {
			log.Printf("close pactl subscribe stdout: %v", closeErr)
		}
		return subscriberProcess{}, commandError("pactl", []string{"subscribe"}, err, stderr.String())
	}
	return subscriberProcess{output: stdout, wait: func() error {
		if err := cmd.Wait(); err != nil {
			return commandError("pactl", []string{"subscribe"}, err, stderr.String())
		}
		return nil
	}}, nil
}

func parseEvent(line string) (Event, bool) {
	parts := strings.Fields(line)
	if len(parts) != 5 || parts[0] != "Event" || parts[2] != "on" || !strings.HasPrefix(parts[4], "#") {
		return Event{}, false
	}
	var kind EventKind
	switch parts[1] {
	case "'new'":
		kind = EventNew
	case "'change'":
		kind = EventChange
	case "'remove'":
		kind = EventRemove
	default:
		return Event{}, false
	}
	var index uint64
	var err error
	if parts[4] == "#-1" {
		index = math.MaxUint32
	} else {
		index, err = strconv.ParseUint(parts[4][1:], 10, 32)
		if err != nil {
			return Event{}, false
		}
	}
	return Event{Kind: kind, Facility: parts[3], Index: uint32(index)}, true
}

// Subscribe reports a resync after each successful start, then forwards events.
// Cancellation also interrupts a send to a consumer that stopped reading.
func (c *Client) Subscribe(ctx context.Context) <-chan Event {
	events := make(chan Event)
	go func() {
		defer close(events)
		delay := c.retryDelay
		for ctx.Err() == nil {
			started := time.Now()
			processCtx, stop := context.WithCancel(ctx)
			process, err := c.start(processCtx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("start pactl subscribe: %v", err)
				}
			} else {
				c.readEvents(ctx, process, events, stop)
				if time.Since(started) > c.stableAfter {
					delay = c.retryDelay
				}
			}
			stop()
			if ctx.Err() != nil {
				return
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			delay = min(c.retryMax, delay*2)
		}
	}()
	return events
}

func (c *Client) readEvents(ctx context.Context, process subscriberProcess, events chan<- Event, stop context.CancelFunc) {
	send := func(event Event) bool {
		select {
		case events <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}
	if send(Event{Kind: EventResync}) {
		scanner := bufio.NewScanner(process.output)
		for scanner.Scan() {
			if event, ok := parseEvent(scanner.Text()); ok && !send(event) {
				break
			}
		}
		if err := scanner.Err(); err != nil {
			if ctx.Err() == nil {
				log.Printf("read pactl subscribe: %v", err)
			}
			stop()
		}
	}
	if err := process.output.Close(); err != nil && ctx.Err() == nil {
		log.Printf("close pactl subscribe stdout: %v", err)
	}
	if err := process.wait(); err != nil && ctx.Err() == nil {
		log.Printf("pactl subscribe exited: %v", err)
	}
}
