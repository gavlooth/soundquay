package model

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func testConfig() Config {
	return Config{Version: ConfigVersion, HiddenDevices: []string{loopSink}, Nicknames: map[string]string{jabraSink: "Headset"}, Routes: []Route{{App: "chrome", Name: "Google Chrome", Direction: RouteOutput, Device: jabraSink}, {App: "chrome", Name: "Google Chrome", Direction: RouteInput, Device: ankerSource}}}
}

func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "soundquay", "config.json")
	want := testConfig()
	if err := SaveConfig(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config = %+v, want %+v", got, want)
	}
	want = want.WithRoute("firefox", "Firefox", RouteOutput, hdmiSink)
	if err := SaveConfig(path, want); err != nil {
		t.Fatal(err)
	}
	got, err = LoadConfig(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement config = %+v, error = %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("temporary files remain: %v", entries)
	}
}

func TestLoadConfigMissingAndInvalidFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	got, err := LoadConfig(path)
	if err != nil || !reflect.DeepEqual(got, Config{Version: ConfigVersion}) {
		t.Fatalf("missing config = %+v, error = %v", got, err)
	}
	for _, data := range []string{`{"version":2}`, `{"version":0}`, `{"version":1`, `{"version":1} trailing`} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Fatalf("accepted invalid config %q", data)
		}
	}
	if _, err := LoadConfig(filepath.Dir(path)); err == nil {
		t.Fatal("reading a directory returned no error")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	got, err := DefaultConfigPath()
	if err != nil || got != filepath.Join(dir, "soundquay", "config.json") {
		t.Fatalf("path = %q, error = %v", got, err)
	}
}

func TestWithRouteReplacesOnlyMatchingDirectionWithoutSharing(t *testing.T) {
	original := testConfig()
	before := testConfig()
	changed := original.WithRoute("chrome", "New Chrome", RouteOutput, hdmiSink)
	if len(changed.Routes) != 2 {
		t.Fatalf("routes: %+v", changed.Routes)
	}
	out, ok := changed.RouteFor("chrome", RouteOutput)
	if !ok || out.Name != "New Chrome" || out.Device != hdmiSink {
		t.Fatalf("output route: %+v, found %v", out, ok)
	}
	in, ok := changed.RouteFor("chrome", RouteInput)
	if !ok || in.Device != ankerSource {
		t.Fatalf("input route changed: %+v", in)
	}
	changed.HiddenDevices[0] = "different"
	changed.Nicknames[jabraSink] = "Different"
	changed.Routes[0].Device = "different"
	if !reflect.DeepEqual(original, before) {
		t.Fatalf("receiver changed: %+v", original)
	}
	original.HiddenDevices[0] = "original mutation"
	original.Nicknames[jabraSink] = "Original mutation"
	original.Routes[0].Device = "original mutation"
	if changed.HiddenDevices[0] != "different" || changed.Nicknames[jabraSink] != "Different" || changed.Routes[0].Device != "different" {
		t.Fatalf("result shares receiver storage: %+v", changed)
	}
}

func TestWithoutRouteRemovesDuplicatesAndCopiesConfig(t *testing.T) {
	original := testConfig()
	original.Routes = append(original.Routes, original.Routes[0])
	changed := original.WithoutRoute("chrome", RouteOutput)
	if len(changed.Routes) != 1 || changed.Routes[0].Direction != RouteInput || len(original.Routes) != 3 {
		t.Fatalf("routes after delete: %+v; receiver: %+v", changed.Routes, original.Routes)
	}
	if route, ok := changed.RouteFor("chrome", RouteOutput); ok || route != (Route{}) {
		t.Fatalf("deleted route found: %+v", route)
	}
	changed.HiddenDevices[0] = "different"
	changed.Nicknames[jabraSink] = "Different"
	changed.Routes[0].Device = "different"
	if original.HiddenDevices[0] != loopSink || original.Nicknames[jabraSink] != "Headset" || original.Routes[1].Device != ankerSource {
		t.Fatalf("receiver shares delete result storage: %+v", original)
	}
	if got := (Config{}).WithRoute("app", "App", RouteOutput, "device"); len(got.Routes) != 1 {
		t.Fatalf("empty config add: %+v", got)
	}
}

func TestSaveConfigKeepsPreviousFileWhenTemporaryFileCannotBeCreated(t *testing.T) {
	// The destination fits the filesystem name limit, but its temporary name does not.
	// This gives a deterministic write error even when tests run as root.
	path := filepath.Join(t.TempDir(), strings.Repeat("c", 250))
	before := []byte(`{"version":1,"routes":[]}`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(path, testConfig()); err == nil {
		t.Fatal("temporary-file creation should fail")
	}
	after, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("previous config changed: %q, error = %v", after, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file remains: %v, error = %v", entries, err)
	}
}

func TestSaveConfigReturnsDirectoryAndRenameErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(filepath.Join(file, "config.json"), testConfig()); err == nil {
		t.Fatal("creating directory over a file should fail")
	}
	destination := filepath.Join(dir, "destination")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(destination, testConfig()); err == nil {
		t.Fatal("replacing directory with file should fail")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("failed rename left temporary file: %v, error = %v", entries, err)
	}
}

func TestSaveConfigKeepsPreviousFileWhenTemporaryWriteFails(t *testing.T) {
	if path := os.Getenv("AUDIO_INDICATOR_CONFIG_WRITE_FAILURE"); path != "" {
		signal.Ignore(syscall.SIGXFSZ)
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 1, Max: 1}); err != nil {
			t.Fatal(err)
		}
		if err := SaveConfig(path, testConfig()); err == nil {
			t.Fatal("writing past the file size limit should fail")
		}
		return
	}
	path := filepath.Join(t.TempDir(), "config.json")
	before := []byte(`{"version":1,"routes":[]}`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(binary, "-test.run=^TestSaveConfigKeepsPreviousFileWhenTemporaryWriteFails$")
	child.Env = append(os.Environ(), "AUDIO_INDICATOR_CONFIG_WRITE_FAILURE="+path)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("write failure test: %v\n%s", err, output)
	}
	after, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("previous config changed after failed write: %q, error = %v", after, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed write left temporary file: %v, error = %v", entries, err)
	}
}
