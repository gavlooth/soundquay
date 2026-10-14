package main

import (
	"path/filepath"
	"testing"
)

func TestLoadAndSaveAppPreferences(t *testing.T) {
	tempDir := t.TempDir()
	originalPathFunc := preferencesPathFunc
	preferencesPathFunc = func() (string, error) {
		return filepath.Join(tempDir, "preferences.json"), nil
	}
	defer func() {
		preferencesPathFunc = originalPathFunc
	}()

	initial, err := loadAppPreferences()
	if err != nil {
		t.Fatalf("expected missing preferences file to load cleanly, got %v", err)
	}
	if len(initial.PreferredRecordingSources) != 0 {
		t.Fatalf("expected empty preferences on first load, got %+v", initial)
	}

	expected := AppPreferences{
		PreferredRecordingSources: map[string]string{
			"Google Chrome input": "alsa_input.usb-jabra",
			"OBS Studio":          "alsa_input.pci-built-in",
		},
	}
	if err := saveAppPreferences(expected); err != nil {
		t.Fatalf("expected preferences save to succeed, got %v", err)
	}

	loaded, err := loadAppPreferences()
	if err != nil {
		t.Fatalf("expected saved preferences to load, got %v", err)
	}
	if len(loaded.PreferredRecordingSources) != len(expected.PreferredRecordingSources) {
		t.Fatalf("unexpected loaded preferences size: %+v", loaded)
	}
	for application, source := range expected.PreferredRecordingSources {
		if loaded.PreferredRecordingSources[application] != source {
			t.Fatalf("expected %s -> %s, got %+v", application, source, loaded.PreferredRecordingSources)
		}
	}
}
