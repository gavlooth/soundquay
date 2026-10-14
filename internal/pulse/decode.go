package pulse

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

type jsonVolume map[string]struct {
	Percent string `json:"value_percent"`
}

func (v jsonVolume) percent() (int, error) {
	if len(v) == 0 {
		return 0, nil
	}
	var sum float64
	for channel, volume := range v {
		pct, err := strconv.ParseFloat(strings.TrimSuffix(volume.Percent, "%"), 64)
		if err != nil {
			return 0, fmt.Errorf("channel %s volume %q: %w", channel, volume.Percent, err)
		}
		sum += pct
	}
	return int(math.Round(sum / float64(len(v)))), nil
}

func hasOwner(owner *uint32) bool {
	return owner != nil && *owner != math.MaxUint32
}

func availability(text string) Availability {
	switch text {
	case "available":
		return Available
	case "not available":
		return NotAvailable
	default:
		return AvailabilityUnknown
	}
}

func decodeInfo(data []byte) (ServerInfo, error) {
	var raw struct {
		ServerName        string `json:"server_name"`
		DefaultSinkName   string `json:"default_sink_name"`
		DefaultSourceName string `json:"default_source_name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ServerInfo{}, err
	}
	return ServerInfo{ServerName: raw.ServerName, DefaultSinkName: raw.DefaultSinkName, DefaultSourceName: raw.DefaultSourceName}, nil
}

type jsonDevicePort struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Type         string `json:"type"`
	Availability string `json:"availability"`
}

func decodeDevices(data []byte, direction Direction) ([]Device, error) {
	var raw []struct {
		Index       uint32            `json:"index"`
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Owner       *uint32           `json:"owner_module"`
		Volume      jsonVolume        `json:"volume"`
		Mute        bool              `json:"mute"`
		ActivePort  string            `json:"active_port"`
		Ports       []jsonDevicePort  `json:"ports"`
		Properties  map[string]string `json:"properties"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(raw))
	for _, r := range raw {
		pct, err := r.Volume.percent()
		if err != nil {
			return nil, fmt.Errorf("device %s: %w", r.Name, err)
		}
		d := Device{Direction: direction, Index: r.Index, Name: r.Name, Description: r.Description, CardName: r.Properties["device.name"], VolumePct: pct, Mute: r.Mute, ActivePort: r.ActivePort, IsMonitor: direction == Input && r.Properties["device.class"] == "monitor", HasOwner: hasOwner(r.Owner), Properties: r.Properties, Ports: make([]DevicePort, 0, len(r.Ports))}
		for _, p := range r.Ports {
			d.Ports = append(d.Ports, DevicePort{Name: p.Name, Description: p.Description, Type: p.Type, Availability: availability(p.Availability)})
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func decodeStreams(data []byte, direction Direction) ([]Stream, error) {
	var raw []struct {
		Index      uint32            `json:"index"`
		Sink       uint32            `json:"sink"`
		Source     uint32            `json:"source"`
		Client     string            `json:"client"`
		Owner      *uint32           `json:"owner_module"`
		Volume     jsonVolume        `json:"volume"`
		Mute       bool              `json:"mute"`
		Corked     bool              `json:"corked"`
		Properties map[string]string `json:"properties"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	streams := make([]Stream, 0, len(raw))
	for _, r := range raw {
		pct, err := r.Volume.percent()
		if err != nil {
			return nil, fmt.Errorf("stream %d: %w", r.Index, err)
		}
		device := r.Sink
		if direction == Input {
			device = r.Source
		}
		streams = append(streams, Stream{Direction: direction, Index: r.Index, DeviceIndex: device, ClientIndex: r.Client, VolumePct: pct, Mute: r.Mute, Corked: r.Corked, HasOwner: hasOwner(r.Owner), Properties: r.Properties})
	}
	return streams, nil
}

type jsonProfile struct {
	Description string `json:"description"`
	Available   bool   `json:"available"`
	Priority    int    `json:"priority"`
	Sinks       int    `json:"sinks"`
	Sources     int    `json:"sources"`
}

type jsonCardPort struct {
	Description  string            `json:"description"`
	Type         string            `json:"type"`
	Direction    string            `json:"direction"`
	Availability string            `json:"availability"`
	Priority     int               `json:"priority"`
	Profiles     []string          `json:"profiles"`
	Properties   map[string]string `json:"properties"`
}

// Combined profiles expose both directions; the port itself determines which one.
func cardPortDirection(name string, port jsonCardPort, profiles map[string]jsonProfile) Direction {
	for _, d := range []string{port.Direction, port.Properties["port.direction"], port.Properties["direction"]} {
		switch strings.ToLower(d) {
		case "input", "source", "in":
			return Input
		case "output", "sink", "out":
			return Output
		}
	}
	if port.Properties["device.icon_name"] == "audio-input-microphone" {
		return Input
	}
	kind := strings.ToLower(port.Type)
	propertyKind := strings.ToLower(port.Properties["port.type"])
	if strings.Contains(kind, "mic") || strings.Contains(kind, "input") || strings.Contains(propertyKind, "mic") || strings.Contains(propertyKind, "input") || strings.Contains(name, "input") {
		return Input
	}
	for _, profile := range port.Profiles {
		if profiles[profile].Sinks > 0 {
			return Output
		}
	}
	return Input
}

func decodeCards(data []byte) ([]Card, error) {
	var raw []struct {
		Index         uint32                  `json:"index"`
		Name          string                  `json:"name"`
		ActiveProfile string                  `json:"active_profile"`
		Properties    map[string]string       `json:"properties"`
		Profiles      map[string]jsonProfile  `json:"profiles"`
		Ports         map[string]jsonCardPort `json:"ports"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	cards := make([]Card, 0, len(raw))
	for _, r := range raw {
		c := Card{Index: r.Index, Name: r.Name, Description: r.Properties["device.description"], ActiveProfile: r.ActiveProfile, Properties: r.Properties, Profiles: make([]CardProfile, 0, len(r.Profiles)), Ports: make([]CardPort, 0, len(r.Ports))}
		for name, p := range r.Profiles {
			c.Profiles = append(c.Profiles, CardProfile{Name: name, Description: p.Description, Available: p.Available, Priority: p.Priority, Sinks: p.Sinks, Sources: p.Sources})
		}
		slices.SortFunc(c.Profiles, func(a, b CardProfile) int { return strings.Compare(a.Name, b.Name) })
		for name, p := range r.Ports {
			c.Ports = append(c.Ports, CardPort{Name: name, Description: p.Description, Type: p.Type, Direction: cardPortDirection(name, p, r.Profiles), Availability: availability(p.Availability), Priority: p.Priority, Profiles: p.Profiles})
		}
		slices.SortFunc(c.Ports, func(a, b CardPort) int { return strings.Compare(a.Name, b.Name) })
		cards = append(cards, c)
	}
	return cards, nil
}
