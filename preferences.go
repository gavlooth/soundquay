package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type AppPreferences struct {
	PreferredRecordingSources map[string]string `json:"preferred_recording_sources"`
}

var preferencesPathFunc = defaultPreferencesPath

func defaultPreferencesPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "audio-indicator", "preferences.json"), nil
}

func loadAppPreferences() (AppPreferences, error) {
	path, err := preferencesPathFunc()
	if err != nil {
		return AppPreferences{PreferredRecordingSources: map[string]string{}}, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return AppPreferences{PreferredRecordingSources: map[string]string{}}, nil
	}
	if err != nil {
		return AppPreferences{PreferredRecordingSources: map[string]string{}}, err
	}

	var preferences AppPreferences
	if err := json.Unmarshal(data, &preferences); err != nil {
		return AppPreferences{PreferredRecordingSources: map[string]string{}}, err
	}
	if preferences.PreferredRecordingSources == nil {
		preferences.PreferredRecordingSources = map[string]string{}
	}

	return preferences, nil
}

func saveAppPreferences(preferences AppPreferences) error {
	path, err := preferencesPathFunc()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}
