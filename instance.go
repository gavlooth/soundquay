package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var errInstanceRunning = errors.New("another soundquay is running")

func instancePath() (string, error) {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return "", fmt.Errorf("XDG_RUNTIME_DIR is not set")
	}
	return filepath.Join(runtime, "soundquay.sock"), nil
}
func sendInstanceCommand(path, command string) error {
	if command != "toggle" && command != "show" && command != "quit" {
		return fmt.Errorf("unknown command %q", command)
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	if _, err = io.WriteString(conn, command+"\n"); err != nil {
		return err
	}
	response, err := bufio.NewReader(io.LimitReader(conn, 64)).ReadString('\n')
	if err != nil {
		return err
	}
	if response != "ok\n" {
		return fmt.Errorf("instance replied %q", strings.TrimSpace(response))
	}
	return nil
}

type instance struct {
	listener *net.UnixListener
	done     chan struct{}
	once     sync.Once
}

func listenInstance(path string, handle func(string)) (*instance, error) {
	// Two processes starting together must not both remove a stale socket and bind;
	// the lock makes the second one wait and then find the first one's socket.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("lock %s: %w", lock.Name(), err)
	}
	address := &net.UnixAddr{Name: path, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		// Only a failed connection means a socket can be stale; a slow reply does not.
		conn, connectErr := net.DialTimeout("unix", path, time.Second)
		if connectErr == nil {
			conn.Close()
			if err := sendInstanceCommand(path, "show"); err != nil {
				return nil, err
			}
			return nil, errInstanceRunning
		}
		if !errors.Is(connectErr, syscall.ECONNREFUSED) && !errors.Is(connectErr, os.ErrNotExist) {
			return nil, fmt.Errorf("connect to existing socket: %w", connectErr)
		}
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return nil, fmt.Errorf("listen on %s: %w", path, err)
		}
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to remove non-socket %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		listener, err = net.ListenUnix("unix", address)
		if err != nil {
			return nil, err
		}
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	server := &instance{listener: listener, done: make(chan struct{})}
	go server.serve(handle)
	return server, nil
}
func (s *instance) serve(handle func(string)) {
	defer close(s.done)
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("Accept instance command: %v", err)
			}
			return
		}
		func() {
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				log.Printf("Read instance command: %v", err)
				return
			}
			command, err := bufio.NewReader(io.LimitReader(conn, 64)).ReadString('\n')
			if err != nil {
				log.Printf("Read instance command: %v", err)
				return
			}
			command = strings.TrimSuffix(command, "\n")
			if command != "toggle" && command != "show" && command != "quit" {
				if _, err := io.WriteString(conn, "error: unknown command\n"); err != nil {
					log.Printf("Reply to instance command: %v", err)
				}
				return
			}
			if _, err := io.WriteString(conn, "ok\n"); err != nil {
				log.Printf("Reply to instance command: %v", err)
				return
			}
			handle(command)
		}()
	}
}
func (s *instance) Close() error {
	var err error
	s.once.Do(func() { err = s.listener.Close(); <-s.done })
	return err
}
