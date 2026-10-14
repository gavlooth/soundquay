// Package model turns a pulse.Snapshot plus the user's config into what the UI shows,
// and decides which streams to move to their saved per-app devices.
package model

import "soundquay/internal/pulse"

// SwitchEntry is one line under "Output" or "Input" in the switcher menu and mixer
// device picker: a device, or a specific port of a card.
type SwitchEntry struct {
	Direction pulse.Direction
	// ID is stable across snapshots: the device name, or "port:<card>:<port>" for card ports.
	ID    string
	Label string // nickname, else description; "<card description>: <port description>" when a card offers several ports in this direction
	// DeviceName is the sink/source to make default. Empty when the entry needs
	// Profile activated first (the device appears only after the profile switch).
	DeviceName string
	CardName   string // "" for devices without a card (virtual/filter devices)
	PortName   string // "" when the device has no selectable port
	Profile    string // card profile to activate before selecting; "" when no switch is needed
	Active     bool   // this is the default device and (when PortName != "") its active port
}

// DeviceChoice is a target in a per-app output/input picker.
type DeviceChoice struct {
	Name  string // sink/source name
	Label string // nickname, else description
}

// AppStream is one live stream of an app.
type AppStream struct {
	Direction  pulse.Direction
	Index      uint32
	DeviceName string // name of the sink/source it is connected to; "" if unknown
	VolumePct  int
	Mute       bool
	MediaName  string // properties["media.name"]
}

// App groups the live streams that share an app key, plus saved routes.
type App struct {
	Key      string
	Name     string // display name
	Routable bool   // false for keys without application identity ("node:<name>"); routes cannot be saved
	Running  bool   // has at least one live stream
	Outputs  []AppStream
	Inputs   []AppStream
	// OutputRoute / InputRoute are the saved device names; "" means follow the default device.
	OutputRoute string
	InputRoute  string
}

// View is everything the tray menu and mixer render.
type View struct {
	DefaultOutput   string // default sink name
	DefaultInput    string // default source name
	OutputVolumePct int    // of the default sink
	OutputMute      bool
	InputVolumePct  int // of the default source
	InputMute       bool
	Outputs         []SwitchEntry  // visible output entries, sorted by Label
	Inputs          []SwitchEntry  // visible input entries (monitors excluded), sorted by Label
	OutputDevices   []DeviceChoice // visible sinks for per-app pickers, sorted by Label
	InputDevices    []DeviceChoice // visible non-monitor sources for per-app pickers, sorted by Label
	Apps            []App          // running apps and apps with saved routes, sorted by lower-case Name, then Key
	AllDevices      []DeviceChoice // every sink and non-monitor source including hidden ones, for Preferences
}

// RouteDirection values used in config.json.
const (
	RouteOutput = "output"
	RouteInput  = "input"
)

// Route is a saved per-app device choice.
type Route struct {
	App       string `json:"app"`       // app key
	Name      string `json:"name"`      // display name at save time, shown while the app is not running
	Direction string `json:"direction"` // RouteOutput or RouteInput
	Device    string `json:"device"`    // sink/source name
}

// ConfigVersion is the only config.json version this build reads.
const ConfigVersion = 1

// Config is ~/.config/soundquay/config.json.
type Config struct {
	Version       int               `json:"version"`
	HiddenDevices []string          `json:"hidden_devices"`
	Nicknames     map[string]string `json:"nicknames"`
	Routes        []Route           `json:"routes"`
}

// Move asks the coordinator to move a stream to a device.
type Move struct {
	Direction   pulse.Direction
	StreamIndex uint32
	DeviceName  string
}
