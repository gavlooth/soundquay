package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

const mixerTitle = "Audio Mixer"

// mixerMargin is the gap in pixels kept between the mixer and the edge of the usable area.
const mixerMargin = 8

type swayRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type swayNode struct {
	ID            int64      `json:"id"`
	Type          string     `json:"type"`
	Name          string     `json:"name"`
	Rect          swayRect   `json:"rect"`
	Nodes         []swayNode `json:"nodes"`
	FloatingNodes []swayNode `json:"floating_nodes"`
}

// findFloatingWindow returns the floating window with the given title and the rect of
// its workspace. A workspace rect excludes space reserved by bars such as Waybar.
func findFloatingWindow(node swayNode, workspace swayRect, title string) (swayNode, swayRect, bool) {
	if node.Type == "workspace" {
		workspace = node.Rect
	}
	for _, child := range node.FloatingNodes {
		if child.Name == title {
			return child, workspace, true
		}
	}
	for _, list := range [][]swayNode{node.Nodes, node.FloatingNodes} {
		for _, child := range list {
			if found, ws, ok := findFloatingWindow(child, workspace, title); ok {
				return found, ws, true
			}
		}
	}
	return swayNode{}, swayRect{}, false
}

// positionInside moves a window rect inside the usable area, keeping mixerMargin to every edge.
func positionInside(window, area swayRect) (x, y int) {
	clamp := func(v, low, high int) int {
		if high < low {
			return low
		}
		return max(low, min(v, high))
	}
	x = clamp(window.X, area.X+mixerMargin, area.X+area.Width-window.Width-mixerMargin)
	y = clamp(window.Y, area.Y+mixerMargin, area.Y+area.Height-window.Height-mixerMargin)
	return x, y
}

// keepMixerInsideUsableArea runs after the mixer is shown. Sway's rule places it at the
// mouse, which is on Waybar when the click came from the bar; this moves it fully above
// the bar and inside the output. It does nothing outside Sway.
func keepMixerInsideUsableArea() {
	if os.Getenv("SWAYSOCK") == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// The window needs a moment to be mapped and placed by the Sway rule.
	for range 10 {
		time.Sleep(50 * time.Millisecond)
		out, err := exec.CommandContext(ctx, "swaymsg", "-t", "get_tree").Output()
		if err != nil {
			log.Printf("Read Sway tree: %v", err)
			return
		}
		var root swayNode
		if err := json.Unmarshal(out, &root); err != nil {
			log.Printf("Decode Sway tree: %v", err)
			return
		}
		window, area, ok := findFloatingWindow(root, swayRect{}, mixerTitle)
		if !ok {
			continue
		}
		x, y := positionInside(window.Rect, area)
		if x == window.Rect.X && y == window.Rect.Y {
			return
		}
		command := fmt.Sprintf("[con_id=%d] move absolute position %d %d", window.ID, x, y)
		if out, err := exec.CommandContext(ctx, "swaymsg", command).CombinedOutput(); err != nil {
			log.Printf("Move mixer: %v: %s", err, out)
		}
		return
	}
}
