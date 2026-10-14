// Package pulse reads and changes PulseAudio/pipewire-pulse state through `pactl`.
package pulse

import "context"

// Runner executes a command and returns its standard output.
// Production code uses ExecRunner; tests pass a fake.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Direction says whether a device or stream plays sound or records it.
type Direction int

const (
	Output Direction = iota // sinks and sink-inputs
	Input                   // sources and source-outputs
)

// Availability of a port as reported by pactl.
type Availability int

const (
	AvailabilityUnknown Availability = iota // "availability unknown"
	Available                               // "available"
	NotAvailable                            // "not available"
)

// DevicePort is a port of a sink or source.
type DevicePort struct {
	Name         string
	Description  string
	Type         string // pactl "type", e.g. "HDMI", "Headphones", "Analog", "Mic"
	Availability Availability
}

// Device is a sink (Direction == Output) or source (Direction == Input).
type Device struct {
	Direction   Direction
	Index       uint32
	Name        string // node.name; stable identity
	Description string
	CardName    string // properties["device.name"], e.g. "alsa_card.pci-0000_00_1f.3"; "" when not backed by a card
	VolumePct   int    // average of channel value_percent, rounded
	Mute        bool
	ActivePort  string
	Ports       []DevicePort
	IsMonitor   bool // sources only: properties["device.class"] == "monitor"
	HasOwner    bool // owner_module set (not null and not 4294967295): created by a module (loopback, echo-cancel, filter)
	Properties  map[string]string
}

// CardProfile is one profile of a card.
type CardProfile struct {
	Name        string
	Description string
	Available   bool
	Priority    int
	Sinks       int
	Sources     int
}

// CardPort is a port of a card. Profiles lists the profile names that expose it.
type CardPort struct {
	Name         string
	Description  string
	Type         string
	Direction    Direction // derived: port is used by sinks (Output) or sources (Input)
	Availability Availability
	Priority     int
	Profiles     []string
}

// Card is a sound card.
type Card struct {
	Index         uint32
	Name          string
	Description   string // properties["device.description"]
	ActiveProfile string
	Profiles      []CardProfile
	Ports         []CardPort
	Properties    map[string]string
}

// Stream is a sink-input (Direction == Output) or source-output (Direction == Input).
type Stream struct {
	Direction   Direction
	Index       uint32
	DeviceIndex uint32 // sink index for sink-inputs, source index for source-outputs
	ClientIndex string
	VolumePct   int
	Mute        bool
	Corked      bool
	HasOwner    bool // owner_module set: created by a module, not an application
	Properties  map[string]string
}

// ServerInfo carries the default devices.
type ServerInfo struct {
	ServerName        string
	DefaultSinkName   string
	DefaultSourceName string
}

// Snapshot is the complete audio state at one moment.
type Snapshot struct {
	Info    ServerInfo
	Sinks   []Device
	Sources []Device
	Cards   []Card
	Outputs []Stream // sink-inputs
	Inputs  []Stream // source-outputs
}

// EventKind is the verb of a `pactl subscribe` line.
type EventKind int

const (
	EventNew EventKind = iota
	EventChange
	EventRemove
	// EventResync is emitted after `pactl subscribe` (re)starts; consumers take a full snapshot.
	EventResync
)

// Event is one parsed `pactl subscribe` line, e.g. "Event 'new' on sink-input #12".
type Event struct {
	Kind     EventKind
	Facility string // "sink", "source", "sink-input", "source-output", "card", "server", "client", "module", ...
	Index    uint32
}

// Subscriber is implemented by Client.Subscribe; declared for consumers that fake it.
type Subscriber interface {
	Subscribe(ctx context.Context) <-chan Event
}
