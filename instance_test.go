package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstanceToggleRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.sock")
	commands := make(chan string, 2)
	server, err := listenInstance(path, func(command string) { commands <- command })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := sendInstanceCommand(path, "toggle"); err != nil {
		t.Fatal(err)
	}
	select {
	case command := <-commands:
		if command != "toggle" {
			t.Fatalf("command %q", command)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("toggle not delivered")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("socket mode %o", info.Mode().Perm())
	}
	other, err := listenInstance(path, func(string) { t.Error("second listener handled a command") })
	if !errors.Is(err, errInstanceRunning) || other != nil {
		t.Fatalf("second listener %v, error %v", other, err)
	}
	select {
	case command := <-commands:
		if command != "show" {
			t.Fatalf("command %q", command)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second invocation did not show the mixer")
	}
}
func TestInstanceReplacesStaleSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.sock")
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	commands := make(chan string, 1)
	server, err := listenInstance(path, func(command string) { commands <- command })
	if err != nil {
		t.Fatal(err)
	}
	if err := sendInstanceCommand(path, "toggle"); err != nil {
		t.Fatal(err)
	}
	select {
	case command := <-commands:
		if command != "toggle" {
			t.Fatalf("command %q", command)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("replacement socket did not answer")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remains after close: %v", err)
	}
}
func TestInstanceConcurrentStartsOverStaleSocketKeepOneListener(t *testing.T) {
	for round := range 20 {
		path := filepath.Join(t.TempDir(), "audio.sock")
		stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		stale.SetUnlinkOnClose(false)
		if err := stale.Close(); err != nil {
			t.Fatal(err)
		}
		type result struct {
			server *instance
			err    error
		}
		results := make(chan result, 2)
		for range 2 {
			go func() {
				server, err := listenInstance(path, func(string) {})
				results <- result{server, err}
			}()
		}
		running := 0
		for range 2 {
			r := <-results
			switch {
			case r.err == nil:
				running++
				defer r.server.Close()
			case errors.Is(r.err, errInstanceRunning):
			default:
				t.Fatalf("round %d: %v", round, r.err)
			}
		}
		if running != 1 {
			t.Fatalf("round %d: %d instances listening, want 1", round, running)
		}
	}
}
func TestInstanceDoesNotRemoveRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.sock")
	if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	server, err := listenInstance(path, func(string) { t.Error("unexpected command") })
	if err == nil || server != nil {
		t.Fatalf("server %v, error %v", server, err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "keep me" {
		t.Fatalf("file content %q", content)
	}
}
func TestInstanceCommandRequiresLiveSocket(t *testing.T) {
	if err := sendInstanceCommand(filepath.Join(t.TempDir(), "missing.sock"), "toggle"); err == nil {
		t.Fatal("missing instance returned success")
	}
}
