package pulse

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureSnapshot(t *testing.T) Snapshot {
	t.Helper()
	c := NewClient(func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "pactl" || len(args) < 3 || args[0] != "-f" || args[1] != "json" {
			t.Fatalf("unexpected snapshot command: %s %v", name, args)
		}
		return fixture(t, args[len(args)-1]), nil
	})
	s, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSnapshotRealFixtures(t *testing.T) {
	s := fixtureSnapshot(t)
	const jabraSink = "alsa_output.usb-_Jabra_Link_390_6CFBED5C5C34-00.analog-stereo"
	const jabraSource = "alsa_input.usb-_Jabra_Link_390_6CFBED5C5C34-00.mono-fallback"
	if s.Info.DefaultSinkName != jabraSink || s.Info.DefaultSourceName != jabraSource || s.Info.ServerName != "PulseAudio (on PipeWire 1.6.9)" {
		t.Fatalf("server info: %+v", s.Info)
	}
	if len(s.Sinks) != 3 || len(s.Sources) != 7 || len(s.Cards) != 4 || len(s.Outputs) != 1 || len(s.Inputs) != 1 {
		t.Fatalf("object counts: %d %d %d %d %d", len(s.Sinks), len(s.Sources), len(s.Cards), len(s.Outputs), len(s.Inputs))
	}
	foundJabra := false
	for _, d := range s.Sinks {
		if d.HasOwner || d.IsMonitor || d.Direction != Output {
			t.Fatalf("ALSA output flags: %+v", d)
		}
		if d.Name == jabraSink {
			foundJabra = true
			if d.VolumePct != 60 || d.ActivePort != "analog-output" || d.CardName != "alsa_card.usb-_Jabra_Link_390_6CFBED5C5C34-00" || len(d.Ports) != 1 || d.Ports[0].Availability != AvailabilityUnknown {
				t.Fatalf("Jabra output: %+v", d)
			}
		}
	}
	if !foundJabra {
		t.Fatal("missing Jabra output")
	}
	monitors := 0
	for _, d := range s.Sources {
		if d.Direction != Input || d.HasOwner {
			t.Fatalf("ALSA input flags: %+v", d)
		}
		if d.IsMonitor {
			monitors++
			if !strings.HasSuffix(d.Name, ".monitor") {
				t.Fatalf("unexpected monitor: %s", d.Name)
			}
		}
	}
	if monitors != 3 {
		t.Fatalf("monitors: %d", monitors)
	}
	for _, check := range []struct {
		stream       Stream
		direction    Direction
		binary, name string
	}{
		{s.Outputs[0], Output, "pacat", "Audio indicator fixture playback"},
		{s.Inputs[0], Input, "pacat", "Audio indicator fixture recording"},
	} {
		if check.stream.Direction != check.direction || check.stream.HasOwner || check.stream.VolumePct != 100 || check.stream.Corked || check.stream.Properties["application.process.binary"] != check.binary || check.stream.Properties["application.name"] != check.name || check.stream.ClientIndex == "" {
			t.Fatalf("stream properties: %+v", check.stream)
		}
	}
	if s.Outputs[0].DeviceIndex != 1550 || s.Inputs[0].DeviceIndex != 1551 {
		t.Fatalf("stream devices: %+v %+v", s.Outputs[0], s.Inputs[0])
	}
}

func TestCardFixturePortDirectionsAndProfiles(t *testing.T) {
	cards, err := decodeCards(fixture(t, "cards"))
	if err != nil {
		t.Fatal(err)
	}
	pci := cards[0]
	if pci.Description != "Built-in Audio" || pci.ActiveProfile != "output:hdmi-stereo+input:analog-stereo" {
		t.Fatalf("PCI card: %+v", pci)
	}
	profile := slices.IndexFunc(pci.Profiles, func(p CardProfile) bool { return p.Name == pci.ActiveProfile })
	if profile < 0 || !pci.Profiles[profile].Available || pci.Profiles[profile].Priority != 5965 || pci.Profiles[profile].Sinks != 1 || pci.Profiles[profile].Sources != 1 {
		t.Fatalf("active profile: %+v", pci.Profiles)
	}
	for _, c := range cards {
		for _, p := range c.Ports {
			want := Output
			if strings.Contains(p.Name, "input") {
				want = Input
			}
			if p.Direction != want {
				t.Fatalf("%s/%s direction: %v", c.Name, p.Name, p.Direction)
			}
			if len(p.Profiles) == 0 {
				t.Fatalf("%s/%s has no profiles", c.Name, p.Name)
			}
		}
	}
	for _, check := range []struct {
		name      string
		available Availability
		priority  int
		profiles  []string
	}{
		{"hdmi-output-0", Available, 5900, []string{"output:hdmi-stereo", "output:hdmi-stereo+input:analog-stereo"}},
		{"analog-output-headphones", NotAvailable, 9900, []string{"output:analog-stereo", "output:analog-stereo+input:analog-stereo"}},
	} {
		i := slices.IndexFunc(pci.Ports, func(p CardPort) bool { return p.Name == check.name })
		if i < 0 || pci.Ports[i].Availability != check.available || pci.Ports[i].Priority != check.priority || !reflect.DeepEqual(pci.Ports[i].Profiles, check.profiles) {
			t.Fatalf("port %s: %+v", check.name, pci.Ports)
		}
	}
}

func TestDecodeVolumesAndMissingProperties(t *testing.T) {
	streams, err := decodeStreams([]byte(`[{"index":12,"sink":4,"owner_module":null,"properties":null,"volume":{"front-left":{"value_percent":"105%"},"front-right":{"value_percent":"106%"}}}]`), Output)
	if err != nil {
		t.Fatal(err)
	}
	if streams[0].VolumePct != 106 || streams[0].HasOwner || streams[0].Properties != nil {
		t.Fatalf("stream: %+v", streams[0])
	}
	devices, err := decodeDevices([]byte(`[{"owner_module":4294967295,"properties":null},{"owner_module":7},{"owner_module":null}]`), Input)
	if err != nil {
		t.Fatal(err)
	}
	if devices[0].HasOwner || !devices[1].HasOwner || devices[2].HasOwner || devices[0].VolumePct != 0 {
		t.Fatalf("module status: %+v", devices)
	}
	owned, err := decodeStreams([]byte(`[{"owner_module":9}]`), Input)
	if err != nil || !owned[0].HasOwner {
		t.Fatalf("module stream: %+v, %v", owned, err)
	}
	if _, err := decodeDevices([]byte(`[{"volume":{"mono":{"value_percent":"oops"}}}]`), Output); err == nil {
		t.Fatal("invalid volume succeeded")
	}
}

func TestCardPortExplicitDirection(t *testing.T) {
	profiles := map[string]jsonProfile{"duplex": {Sinks: 1, Sources: 1}}
	for _, check := range []struct {
		port jsonCardPort
		want Direction
	}{
		{jsonCardPort{Direction: "input", Type: "Analog", Profiles: []string{"duplex"}}, Input},
		{jsonCardPort{Direction: "output", Type: "Mic", Profiles: []string{"duplex"}}, Output},
		{jsonCardPort{Properties: map[string]string{"port.direction": "input"}, Profiles: []string{"duplex"}}, Input},
		{jsonCardPort{Type: "Mic", Profiles: []string{"duplex"}}, Input},
		{jsonCardPort{Type: "HDMI", Profiles: []string{"duplex"}}, Output},
	} {
		if got := cardPortDirection("port", check.port, profiles); got != check.want {
			t.Fatalf("port %+v direction %v, want %v", check.port, got, check.want)
		}
	}
}

func TestSnapshotReportsReadAndDecodeErrors(t *testing.T) {
	failure := errors.New("audio server unavailable")
	for _, malformed := range []bool{false, true} {
		c := NewClient(func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[len(args)-1] == "sources" {
				if malformed {
					return []byte("not JSON"), nil
				}
				return nil, failure
			}
			return fixture(t, args[len(args)-1]), nil
		})
		s, err := c.Snapshot(context.Background())
		if err == nil || !strings.Contains(err.Error(), "sources") || !reflect.DeepEqual(s, Snapshot{}) {
			t.Fatalf("snapshot failure: %+v, %v", s, err)
		}
		if !malformed && !errors.Is(err, failure) {
			t.Fatalf("read error lost: %v", err)
		}
	}
}
