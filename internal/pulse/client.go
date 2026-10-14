package pulse

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ExecRunner returns stdout, keeping command failures separate from JSON output.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, commandError(name, args, err, stderr.String())
	}
	return out, nil
}

func commandError(name string, args []string, err error, stderr string) error {
	if text := strings.TrimSpace(stderr); text != "" {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, text)
	}
	return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
}

// Client reads snapshots and changes devices and application streams using pactl.
type Client struct {
	run         Runner
	start       startSubscriber
	retryDelay  time.Duration
	retryMax    time.Duration
	stableAfter time.Duration
}

func NewClient(run Runner) *Client {
	if run == nil {
		run = ExecRunner
	}
	return &Client{run: run, start: startPactlSubscriber, retryDelay: 500 * time.Millisecond, retryMax: 5 * time.Second, stableAfter: 10 * time.Second}
}

func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var s Snapshot
	steps := []struct {
		args   []string
		decode func([]byte) error
	}{
		{[]string{"info"}, func(data []byte) error { var err error; s.Info, err = decodeInfo(data); return err }},
		{[]string{"list", "sinks"}, func(data []byte) error { var err error; s.Sinks, err = decodeDevices(data, Output); return err }},
		{[]string{"list", "sources"}, func(data []byte) error { var err error; s.Sources, err = decodeDevices(data, Input); return err }},
		{[]string{"list", "cards"}, func(data []byte) error { var err error; s.Cards, err = decodeCards(data); return err }},
		{[]string{"list", "sink-inputs"}, func(data []byte) error { var err error; s.Outputs, err = decodeStreams(data, Output); return err }},
		{[]string{"list", "source-outputs"}, func(data []byte) error { var err error; s.Inputs, err = decodeStreams(data, Input); return err }},
	}
	for _, step := range steps {
		args := append([]string{"-f", "json"}, step.args...)
		data, err := c.run(ctx, "pactl", args...)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read %s: %w", strings.Join(step.args, " "), err)
		}
		if err := step.decode(data); err != nil {
			return Snapshot{}, fmt.Errorf("decode %s: %w", strings.Join(step.args, " "), err)
		}
	}
	return s, nil
}

func deviceWord(d Direction) (string, error) {
	switch d {
	case Output:
		return "sink", nil
	case Input:
		return "source", nil
	default:
		return "", fmt.Errorf("unknown audio direction %d", d)
	}
}

func streamWord(d Direction) (string, error) {
	switch d {
	case Output:
		return "sink-input", nil
	case Input:
		return "source-output", nil
	default:
		return "", fmt.Errorf("unknown audio direction %d", d)
	}
}

func volumeArgument(pct int) string {
	return strconv.Itoa(min(150, max(0, pct))) + "%"
}

func muteArgument(mute bool) string {
	if mute {
		return "1"
	}
	return "0"
}

func (c *Client) write(ctx context.Context, args ...string) error {
	_, err := c.run(ctx, "pactl", args...)
	return err
}

func (c *Client) SetDefault(ctx context.Context, d Direction, deviceName string) error {
	word, err := deviceWord(d)
	if err != nil {
		return err
	}
	return c.write(ctx, "set-default-"+word, deviceName)
}

func (c *Client) SetVolume(ctx context.Context, d Direction, deviceName string, pct int) error {
	word, err := deviceWord(d)
	if err != nil {
		return err
	}
	return c.write(ctx, "set-"+word+"-volume", deviceName, volumeArgument(pct))
}

func (c *Client) SetMute(ctx context.Context, d Direction, deviceName string, mute bool) error {
	word, err := deviceWord(d)
	if err != nil {
		return err
	}
	return c.write(ctx, "set-"+word+"-mute", deviceName, muteArgument(mute))
}

func (c *Client) SetPort(ctx context.Context, d Direction, deviceName, port string) error {
	word, err := deviceWord(d)
	if err != nil {
		return err
	}
	return c.write(ctx, "set-"+word+"-port", deviceName, port)
}

func (c *Client) SetCardProfile(ctx context.Context, cardName, profile string) error {
	return c.write(ctx, "set-card-profile", cardName, profile)
}

func (c *Client) MoveStream(ctx context.Context, d Direction, streamIndex uint32, deviceName string) error {
	word, err := streamWord(d)
	if err != nil {
		return err
	}
	return c.write(ctx, "move-"+word, strconv.FormatUint(uint64(streamIndex), 10), deviceName)
}

func (c *Client) SetStreamVolume(ctx context.Context, d Direction, streamIndex uint32, pct int) error {
	word, err := streamWord(d)
	if err != nil {
		return err
	}
	return c.write(ctx, "set-"+word+"-volume", strconv.FormatUint(uint64(streamIndex), 10), volumeArgument(pct))
}

func (c *Client) SetStreamMute(ctx context.Context, d Direction, streamIndex uint32, mute bool) error {
	word, err := streamWord(d)
	if err != nil {
		return err
	}
	return c.write(ctx, "set-"+word+"-mute", strconv.FormatUint(uint64(streamIndex), 10), muteArgument(mute))
}
