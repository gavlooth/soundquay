package main

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

func TestSnapshotsPreserveOnlyTheEditedSlider(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	edited := newGuardedSlider(func(int) {})
	other := newGuardedSlider(func(int) {})
	now := time.Now()
	edited.SetValue(80)
	edited.changedAt = now
	edited.dragging = true
	edited.update(20, now.Add(2*time.Second))
	other.update(40, now)
	if edited.Value != 80 || other.Value != 40 {
		t.Fatalf("during drag: edited %v, other %v", edited.Value, other.Value)
	}
	edited.dragging = false
	edited.changedAt = now
	edited.update(20, now.Add(999*time.Millisecond))
	if edited.Value != 80 {
		t.Fatalf("recent user value replaced: %v", edited.Value)
	}
	edited.update(20, now.Add(time.Second))
	if edited.Value != 20 {
		t.Fatalf("snapshot after edit delay ignored: %v", edited.Value)
	}
}
