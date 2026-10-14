package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}
	return filepath.Join(dir, "soundquay", "config.json"), nil
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{Version: ConfigVersion}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if c.Version != ConfigVersion {
		return Config{}, fmt.Errorf("unsupported config version %d (expected %d)", c.Version, ConfigVersion)
	}
	return c, nil
}

// SaveConfig replaces the config only after the complete new file reaches disk.
func SaveConfig(path string, c Config) (err error) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	defer func() {
		if removeErr := os.Remove(file.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary config: %w", removeErr))
		}
	}()
	_, writeErr := file.Write(append(data, '\n'))
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func (c Config) copy() Config {
	c.HiddenDevices = slices.Clone(c.HiddenDevices)
	c.Nicknames = maps.Clone(c.Nicknames)
	c.Routes = slices.Clone(c.Routes)
	return c
}

func (c Config) WithRoute(app, name, direction, device string) Config {
	result := c.WithoutRoute(app, direction)
	result.Routes = append(result.Routes, Route{App: app, Name: name, Direction: direction, Device: device})
	return result
}

func (c Config) WithoutRoute(app, direction string) Config {
	result := c.copy()
	result.Routes = slices.DeleteFunc(result.Routes, func(r Route) bool {
		return r.App == app && r.Direction == direction
	})
	return result
}

func (c Config) RouteFor(app, direction string) (Route, bool) {
	for _, route := range c.Routes {
		if route.App == app && route.Direction == direction {
			return route, true
		}
	}
	return Route{}, false
}
