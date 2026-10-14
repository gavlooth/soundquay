package main

import "testing"

func TestPositionInsideKeepsMixerAboveBottomBar(t *testing.T) {
	// DP-1 with a 25 px bottom bar: usable area ends at y=480+1415.
	area := swayRect{X: 1080, Y: 480, Width: 2560, Height: 1415}
	cases := []struct {
		name         string
		window       swayRect
		wantX, wantY int
	}{
		{"opened at a click on the bar", swayRect{X: 3000, Y: 1650, Width: 780, Height: 520}, 2852, 1367},
		{"already inside stays put", swayRect{X: 1500, Y: 700, Width: 780, Height: 520}, 1500, 700},
		{"past the left and top edges", swayRect{X: 900, Y: 100, Width: 780, Height: 520}, 1088, 488},
		{"larger than the area keeps its top-left visible", swayRect{X: 0, Y: 0, Width: 3000, Height: 2000}, 1088, 488},
	}
	for _, c := range cases {
		x, y := positionInside(c.window, area)
		if x != c.wantX || y != c.wantY {
			t.Errorf("%s: got %d,%d want %d,%d", c.name, x, y, c.wantX, c.wantY)
		}
	}
}

func TestFindFloatingWindowReportsItsWorkspaceArea(t *testing.T) {
	root := swayNode{Type: "root", Nodes: []swayNode{{Type: "output", Nodes: []swayNode{
		{Type: "workspace", Rect: swayRect{X: 1, Y: 2, Width: 3, Height: 4}},
		{Type: "workspace", Rect: swayRect{X: 10, Y: 20, Width: 30, Height: 40}, FloatingNodes: []swayNode{{ID: 7, Name: mixerTitle}}},
	}}}}
	window, area, ok := findFloatingWindow(root, swayRect{}, mixerTitle)
	if !ok || window.ID != 7 || area != (swayRect{X: 10, Y: 20, Width: 30, Height: 40}) {
		t.Fatalf("got %v %v %v", window, area, ok)
	}
}
