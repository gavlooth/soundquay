package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"soundquay/internal/model"
	"soundquay/internal/pulse"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "soundquay:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: soundquay [toggle|show|quit|snapshot]")
	}
	if len(args) == 1 && args[0] != "snapshot" {
		path, err := instancePath()
		if err != nil {
			return err
		}
		switch args[0] {
		case "toggle", "show", "quit":
			if err := sendInstanceCommand(path, args[0]); err != nil {
				return fmt.Errorf("no running instance answered: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("unknown command %q", args[0])
		}
	}
	var path string
	if len(args) == 0 {
		var err error
		path, err = instancePath()
		if err != nil {
			return err
		}
		// A second invocation does not initialize the GUI driver or load config.
		info, err := os.Stat(path)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			if err := sendInstanceCommand(path, "show"); err == nil {
				return nil
			}
		}
	}
	client := pulse.NewClient(pulse.ExecRunner)
	configPath, err := model.DefaultConfigPath()
	if err != nil {
		return err
	}
	cfg, err := model.LoadConfig(configPath)
	if err != nil {
		return err
	}
	if len(args) == 1 {
		snapshot, err := client.Snapshot(context.Background())
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			Snapshot pulse.Snapshot `json:"snapshot"`
			View     model.View     `json:"view"`
		}{snapshot, model.Build(snapshot, cfg)})
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	application := app.NewWithID("soundquay")
	application.Settings().SetTheme(&DarkTheme{})
	var coord *coordinator
	dispatch := func(msg Msg) { coord.Dispatch(msg) }
	mixer := newMixer(application, dispatch)
	prefs := newPreferences(application, dispatch)
	show := mixer.show
	var stopOnce sync.Once
	quit := func() { stopOnce.Do(func() { cancel(); application.Quit() }) }
	tray := newTray(application, dispatch, mixer.toggle, show, func() { prefs.window.Show(); prefs.window.RequestFocus() }, quit)
	coord = newCoordinator(client, configPath, cfg, func(state renderedState) {
		fyne.Do(func() { mixer.update(state); prefs.update(state); tray.update(state.View) })
	})
	server, err := listenInstance(path, func(command string) {
		fyne.Do(func() {
			switch command {
			case "toggle":
				mixer.toggle()
			case "show":
				show()
			case "quit":
				quit()
			}
		})
	})
	if errors.Is(err, errInstanceRunning) {
		application.Quit()
		return nil
	}
	if err != nil {
		application.Quit()
		return err
	}
	defer func() {
		if err := server.Close(); err != nil {
			log.Printf("Close instance socket: %v", err)
		}
	}()
	tray.update(model.Build(pulse.Snapshot{}, cfg))
	go coord.Run(ctx)
	go func() { <-ctx.Done(); fyne.Do(quit) }()
	application.Run()
	cancel()
	return nil
}
