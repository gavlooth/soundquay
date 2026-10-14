package pulse

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestWriteCommandsAndVolumeLimits(t *testing.T) {
	ctx := context.Background()
	var got []string
	failure := errors.New("pactl write failed")
	c := NewClient(func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, failure
	})
	for _, check := range []struct {
		name string
		call func() error
		args []string
	}{
		{"default output", func() error { return c.SetDefault(ctx, Output, "device") }, []string{"set-default-sink", "device"}},
		{"default input", func() error { return c.SetDefault(ctx, Input, "device") }, []string{"set-default-source", "device"}},
		{"output volume above limit", func() error { return c.SetVolume(ctx, Output, "device", 151) }, []string{"set-sink-volume", "device", "150%"}},
		{"input volume below zero", func() error { return c.SetVolume(ctx, Input, "device", -1) }, []string{"set-source-volume", "device", "0%"}},
		{"output volume", func() error { return c.SetVolume(ctx, Output, "device", 105) }, []string{"set-sink-volume", "device", "105%"}},
		{"mute output", func() error { return c.SetMute(ctx, Output, "device", true) }, []string{"set-sink-mute", "device", "1"}},
		{"unmute input", func() error { return c.SetMute(ctx, Input, "device", false) }, []string{"set-source-mute", "device", "0"}},
		{"output port", func() error { return c.SetPort(ctx, Output, "device", "headphones") }, []string{"set-sink-port", "device", "headphones"}},
		{"input port", func() error { return c.SetPort(ctx, Input, "device", "mic") }, []string{"set-source-port", "device", "mic"}},
		{"card profile", func() error { return c.SetCardProfile(ctx, "card", "duplex") }, []string{"set-card-profile", "card", "duplex"}},
		{"move output", func() error { return c.MoveStream(ctx, Output, 12, "device") }, []string{"move-sink-input", "12", "device"}},
		{"move input", func() error { return c.MoveStream(ctx, Input, 13, "device") }, []string{"move-source-output", "13", "device"}},
		{"output stream volume above limit", func() error { return c.SetStreamVolume(ctx, Output, 12, 200) }, []string{"set-sink-input-volume", "12", "150%"}},
		{"input stream volume below zero", func() error { return c.SetStreamVolume(ctx, Input, 13, -10) }, []string{"set-source-output-volume", "13", "0%"}},
		{"input stream volume", func() error { return c.SetStreamVolume(ctx, Input, 13, 105) }, []string{"set-source-output-volume", "13", "105%"}},
		{"mute output stream", func() error { return c.SetStreamMute(ctx, Output, 12, true) }, []string{"set-sink-input-mute", "12", "1"}},
		{"unmute input stream", func() error { return c.SetStreamMute(ctx, Input, 13, false) }, []string{"set-source-output-mute", "13", "0"}},
	} {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, failure) {
				t.Fatalf("write error: %v", err)
			}
			if want := append([]string{"pactl"}, check.args...); !reflect.DeepEqual(got, want) {
				t.Fatalf("argv: %v, want %v", got, want)
			}
		})
	}
	for _, pct := range []int{0, 1, 149, 150} {
		if err := c.SetVolume(ctx, Output, "device", pct); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if got[len(got)-1] != volumeArgument(pct) {
			t.Fatalf("volume %d: %v", pct, got)
		}
	}
}

func TestWriteRejectsUnknownDirection(t *testing.T) {
	c := NewClient(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("invalid direction ran command")
		return nil, nil
	})
	ctx := context.Background()
	for _, call := range []func() error{
		func() error { return c.SetDefault(ctx, Direction(4), "device") },
		func() error { return c.SetVolume(ctx, Direction(4), "device", 60) },
		func() error { return c.SetMute(ctx, Direction(4), "device", true) },
		func() error { return c.SetPort(ctx, Direction(4), "device", "port") },
		func() error { return c.MoveStream(ctx, Direction(4), 1, "device") },
		func() error { return c.SetStreamVolume(ctx, Direction(4), 1, 60) },
		func() error { return c.SetStreamMute(ctx, Direction(4), 1, true) },
	} {
		if err := call(); err == nil {
			t.Fatal("invalid direction succeeded")
		}
	}
}

func TestExecRunnerSeparatesStdoutAndFailure(t *testing.T) {
	out, err := ExecRunner(context.Background(), "sh", "-c", "printf 'snapshot'; printf 'warning' >&2")
	if err != nil || string(out) != "snapshot" {
		t.Fatalf("success: %q, %v", out, err)
	}
	out, err = ExecRunner(context.Background(), "sh", "-c", "printf 'partial'; printf '  server disconnected \\n' >&2; exit 7")
	if string(out) != "partial" || err == nil || !strings.HasSuffix(err.Error(), ": server disconnected") {
		t.Fatalf("failure: %q, %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExecRunner(ctx, "sh", "-c", "sleep 10"); err == nil {
		t.Fatal("cancelled command succeeded")
	}
}
