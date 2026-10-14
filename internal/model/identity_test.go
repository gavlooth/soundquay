package model

import "testing"

func TestAppKey(t *testing.T) {
	tests := []struct {
		name     string
		props    map[string]string
		key      string
		routable bool
	}{
		{"id first", map[string]string{"application.id": "org.mozilla.firefox", "application.process.binary": "firefox", "application.name": "Firefox"}, "org.mozilla.firefox", true},
		{"binary before name", map[string]string{"application.process.binary": "chrome", "application.name": "Google Chrome"}, "chrome", true},
		{"name only", map[string]string{"application.name": "Recorder"}, "Recorder", true},
		{"node only", map[string]string{"node.name": "unidentified.playback"}, "node:unidentified.playback", false},
		{"no properties", nil, "node:", false},
		{"unrelated binary", map[string]string{"application.process.binary": "wineglass"}, "node:", false},
	}
	for _, binary := range []string{"electron", "python", "python3", "node", "java", "wine", "wine64", "wine-preloader", "bwrap", "flatpak"} {
		tests = append(tests, struct {
			name     string
			props    map[string]string
			key      string
			routable bool
		}{binary + " name fallback", map[string]string{"application.process.binary": binary, "application.name": "My App"}, "My App", true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, routable := AppKey(tt.props)
			if key != tt.key || routable != tt.routable {
				t.Fatalf("AppKey = (%q, %v), want (%q, %v)", key, routable, tt.key, tt.routable)
			}
		})
	}
}
