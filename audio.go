package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AudioStream struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Binary   string `json:"binary"`
	Volume   int    `json:"volume"`
	Muted    bool   `json:"muted"`
	SinkName string `json:"sink_name"`
}

type AudioSink struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Volume      int    `json:"volume"`
	Muted       bool   `json:"muted"`
	Default     bool   `json:"default"`
	PipeWireID  string `json:"pipewire_id"`
}

type AudioSource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Volume      int    `json:"volume"`
	Muted       bool   `json:"muted"`
	Default     bool   `json:"default"`
	PipeWireID  string `json:"pipewire_id"`
}

type MicStream struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Binary     string `json:"binary"`
	Volume     int    `json:"volume"`
	Muted      bool   `json:"muted"`
	SourceName string `json:"source_name"`
}

type AudioStatus struct {
	DefaultSink   string        `json:"default_sink"`
	Volume        int           `json:"volume"`
	Muted         bool          `json:"muted"`
	Streams       []AudioStream `json:"streams"`
	StreamCount   int           `json:"stream_count"`
	Sinks         []AudioSink   `json:"sinks"`
	DefaultSource string        `json:"default_source"`
	MicVolume     int           `json:"mic_volume"`
	MicMuted      bool          `json:"mic_muted"`
	MicStreams    []MicStream   `json:"mic_streams"`
	MicCount      int           `json:"mic_count"`
	Sources       []AudioSource `json:"sources"`
}

var (
	currentStatus = &AudioStatus{
		Streams:    []AudioStream{},
		MicStreams: []MicStream{},
		Sinks:      []AudioSink{},
		Sources:    []AudioSource{},
	}
	statusMutex sync.RWMutex
)

func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	return string(out), err
}

func getPipeWireDefaultID() string {
	out, _ := runCmd("wpctl", "status")
	lines := strings.Split(out, "\n")
	inSinks := false
	for _, line := range lines {
		if strings.Contains(line, "Sinks:") {
			inSinks = true
			continue
		}
		if inSinks && strings.Contains(line, "Sources:") {
			break
		}
		if inSinks && strings.Contains(line, "*") {
			re := regexp.MustCompile(`\*\s+(\d+)\.`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				return matches[1]
			}
		}
	}
	return ""
}

func getSinkInputs() []AudioStream {
	out, _ := runCmd("pactl", "list", "sink-inputs")
	var streams []AudioStream

	scanner := bufio.NewScanner(strings.NewReader(out))
	var stream *AudioStream
	reSink := regexp.MustCompile(`sink:\s+(.+?)\s+`)
	reVolume := regexp.MustCompile(`volume:\s+front-left:\s+(\d+)/\s+`)
	reMono := regexp.MustCompile(`volume:\s+mono:\s+(\d+)/\s+`)
	reMuted := regexp.MustCompile(`muted:\s+(\w+)`)
	reName := regexp.MustCompile(`application.name\s+=\s+"([^"]+)"`)
	reBinary := regexp.MustCompile(`application.process.binary\s+=\s+"([^"]+)"`)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if strings.Contains(line, "Sink Input #") {
			if stream != nil {
				streams = append(streams, *stream)
			}
			stream = &AudioStream{}
			re := regexp.MustCompile(`Sink Input #(\d+)`)
			if matches := re.FindStringSubmatch(line); len(matches) > 1 {
				stream.ID = matches[1]
			}
		} else if stream != nil {
			if matches := reName.FindStringSubmatch(line); len(matches) > 1 {
				stream.Name = matches[1]
			}
			if matches := reBinary.FindStringSubmatch(line); len(matches) > 1 {
				stream.Binary = matches[1]
			}
			if matches := reSink.FindStringSubmatch(line); len(matches) > 1 {
				stream.SinkName = matches[1]
			}
			if matches := reMuted.FindStringSubmatch(line); len(matches) > 1 {
				stream.Muted = matches[1] == "yes"
			}
			if matches := reMono.FindStringSubmatch(line); len(matches) > 1 {
				if vol, err := strconv.Atoi(matches[1]); err == nil {
					stream.Volume = vol
				}
			} else if matches := reVolume.FindStringSubmatch(line); len(matches) > 1 {
				if vol, err := strconv.Atoi(matches[1]); err == nil {
					stream.Volume = vol
				}
			}
		}
	}

	if stream != nil {
		streams = append(streams, *stream)
	}

	return streams
}

func getSourceOutputs() []MicStream {
	out, _ := runCmd("pactl", "list", "source-outputs")
	var streams []MicStream

	scanner := bufio.NewScanner(strings.NewReader(out))
	var stream *MicStream
	reSource := regexp.MustCompile(`source:\s+(.+?)\s+`)
	reVolume := regexp.MustCompile(`volume:\s+front-left:\s+(\d+)/\s+`)
	reMono := regexp.MustCompile(`volume:\s+mono:\s+(\d+)/\s+`)
	reMuted := regexp.MustCompile(`muted:\s+(\w+)`)
	reName := regexp.MustCompile(`application.name\s+=\s+"([^"]+)"`)
	reBinary := regexp.MustCompile(`application.process.binary\s+=\s+"([^"]+)"`)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if strings.Contains(line, "Source Output #") {
			if stream != nil {
				streams = append(streams, *stream)
			}
			stream = &MicStream{}
			re := regexp.MustCompile(`Source Output #(\d+)`)
			if matches := re.FindStringSubmatch(line); len(matches) > 1 {
				stream.ID = matches[1]
			}
		} else if stream != nil {
			if matches := reName.FindStringSubmatch(line); len(matches) > 1 {
				stream.Name = matches[1]
			}
			if matches := reBinary.FindStringSubmatch(line); len(matches) > 1 {
				stream.Binary = matches[1]
			}
			if matches := reSource.FindStringSubmatch(line); len(matches) > 1 {
				stream.SourceName = matches[1]
			}
			if matches := reMuted.FindStringSubmatch(line); len(matches) > 1 {
				stream.Muted = matches[1] == "yes"
			}
			if matches := reMono.FindStringSubmatch(line); len(matches) > 1 {
				if vol, err := strconv.Atoi(matches[1]); err == nil {
					stream.Volume = vol
				}
			} else if matches := reVolume.FindStringSubmatch(line); len(matches) > 1 {
				if vol, err := strconv.Atoi(matches[1]); err == nil {
					stream.Volume = vol
				}
			}
		}
	}

	if stream != nil {
		streams = append(streams, *stream)
	}

	return streams
}

func getSinks() []AudioSink {
	out, _ := runCmd("pactl", "list", "sinks")
	var sinks []AudioSink

	scanner := bufio.NewScanner(strings.NewReader(out))
	var sink *AudioSink
	reVolume := regexp.MustCompile(`volume:\s+front-left:\s+(\d+)/\s+`)
	reMono := regexp.MustCompile(`volume:\s+mono:\s+(\d+)/\s+`)
	reMuted := regexp.MustCompile(`muted:\s+(\w+)`)
	reName := regexp.MustCompile(`device.description\s+=\s+"([^"]+)"`)
	reName2 := regexp.MustCompile(`node.name\s+=\s+"([^"]+)"`)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if strings.Contains(line, "Sink #") {
			if sink != nil {
				sinks = append(sinks, *sink)
			}
			sink = &AudioSink{}
			re := regexp.MustCompile(`Sink #(\d+)`)
			if matches := re.FindStringSubmatch(line); len(matches) > 1 {
				sink.ID = matches[1]
			}
		} else if sink != nil {
			if matches := reName.FindStringSubmatch(line); len(matches) > 1 {
				sink.Description = matches[1]
			}
			if sink.Description == "" {
				if matches := reName2.FindStringSubmatch(line); len(matches) > 1 {
					sink.Description = matches[1]
				}
			}
			if matches := reMuted.FindStringSubmatch(line); len(matches) > 1 {
				sink.Muted = matches[1] == "yes"
			}
			if matches := reMono.FindStringSubmatch(line); len(matches) > 1 {
				if vol, err := strconv.Atoi(matches[1]); err == nil {
					sink.Volume = vol
				}
			} else if matches := reVolume.FindStringSubmatch(line); len(matches) > 1 {
				if vol, err := strconv.Atoi(matches[1]); err == nil {
					sink.Volume = vol
				}
			}
		}
	}

	if sink != nil {
		sinks = append(sinks, *sink)
	}

	defaultSink, _ := runCmd("pactl", "get-default-sink")
	for i := range sinks {
		sinks[i].Default = sinks[i].Name == defaultSink
		sinks[i].PipeWireID = getPipeWireDefaultID()
		sinks[i].Name = "alsa_output." + sinks[i].ID
	}

	return sinks
}

func getSources() []AudioSource {
	out, _ := runCmd("pactl", "list", "sources")
	var sources []AudioSource

	scanner := bufio.NewScanner(strings.NewReader(out))
	var source *AudioSource
	reVolume := regexp.MustCompile(`volume:\s+front-left:\s+(\d+)/\s+`)
	reMono := regexp.MustCompile(`volume:\s+mono:\s+(\d+)/\s+`)
	reMuted := regexp.MustCompile(`muted:\s+(\w+)`)
	reName := regexp.MustCompile(`device.description\s+=\s+"([^"]+)"`)
	reName2 := regexp.MustCompile(`node.name\s+=\s+"([^"]+)"`)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if strings.Contains(line, "Source #") {
			if source != nil {
				sources = append(sources, *source)
			}
			source = &AudioSource{}
			re := regexp.MustCompile(`Source #(\d+)`)
			if matches := re.FindStringSubmatch(line); len(matches) > 1 {
				source.ID = matches[1]
			}
		} else if source != nil {
			if matches := reName.FindStringSubmatch(line); len(matches) > 1 {
				source.Description = matches[1]
			}
			if source.Description == "" {
				if matches := reName2.FindStringSubmatch(line); len(matches) > 1 {
					source.Description = matches[1]
				}
			}
			if matches := reMuted.FindStringSubmatch(line); len(matches) > 1 {
				source.Muted = matches[1] == "yes"
			}
			if matches := reMono.FindStringSubmatch(line); len(matches) > 1 {
				if vol, err := strconv.Atoi(matches[1]); err == nil {
					source.Volume = vol
				}
			} else if matches := reVolume.FindStringSubmatch(line); len(matches) > 1 {
				if vol, err := strconv.Atoi(matches[1]); err == nil {
					source.Volume = vol
				}
			}
		}
	}

	if source != nil {
		sources = append(sources, *source)
	}

	defaultSource, _ := runCmd("pactl", "get-default-source")
	for i := range sources {
		sources[i].Default = sources[i].Name == defaultSource
		sources[i].PipeWireID = getPipeWireDefaultID()
		sources[i].Name = "alsa_input." + sources[i].ID
	}

	return sources
}

func getDefaultSink() (string, int, bool) {
	out, _ := runCmd("pactl", "get-default-sink")
	sinkName := strings.TrimSpace(out)

	sinks := getSinks()
	for _, sink := range sinks {
		if sink.Name == sinkName {
			return sinkName, sink.Volume, sink.Muted
		}
	}

	return sinkName, 50, false
}

func getDefaultSource() (string, int, bool) {
	out, _ := runCmd("pactl", "get-default-source")
	sourceName := strings.TrimSpace(out)

	sources := getSources()
	for _, source := range sources {
		if source.Name == sourceName {
			return sourceName, source.Volume, source.Muted
		}
	}

	return sourceName, 50, false
}

func getStatus() *AudioStatus {
	status := &AudioStatus{
		Streams:    []AudioStream{},
		MicStreams: []MicStream{},
		Sinks:      []AudioSink{},
		Sources:    []AudioSource{},
	}

	defaultSink, volume, muted := getDefaultSink()
	status.DefaultSink = defaultSink
	status.Volume = volume
	status.Muted = muted

	status.Streams = getSinkInputs()
	status.StreamCount = len(status.Streams)
	status.Sinks = getSinks()

	defaultSource, micVol, micMuted := getDefaultSource()
	status.DefaultSource = defaultSource
	status.MicVolume = micVol
	status.MicMuted = micMuted

	status.MicStreams = getSourceOutputs()
	status.MicCount = len(status.MicStreams)
	status.Sources = getSources()

	statusMutex.Lock()
	currentStatus = status
	statusMutex.Unlock()

	return status
}

func setVolume(volume int) {
	_, _ = runCmd("pactl", "set-sink-volume", "@DEFAULT_SINK@", fmt.Sprintf("%d%%", volume))
}

func setMute(muted bool) {
	if muted {
		_, _ = runCmd("pactl", "set-sink-mute", "@DEFAULT_SINK@", "true")
	} else {
		_, _ = runCmd("pactl", "set-sink-mute", "@DEFAULT_SINK@", "false")
	}
}

func setMicVolume(volume int) {
	_, _ = runCmd("pactl", "set-source-volume", "@DEFAULT_SOURCE@", fmt.Sprintf("%d%%", volume))
}

func setMicMute(muted bool) {
	if muted {
		_, _ = runCmd("pactl", "set-source-mute", "@DEFAULT_SOURCE@", "true")
	} else {
		_, _ = runCmd("pactl", "set-source-mute", "@DEFAULT_SOURCE@", "false")
	}
}

func setSink(sinkName string) {
	_, _ = runCmd("pactl", "set-default-sink", sinkName)
}

func setSource(sourceName string) {
	_, _ = runCmd("pactl", "set-default-source", sourceName)
}

func setStreamVolume(streamID string, volume int) {
	_, _ = runCmd("pactl", "set-sink-input-volume", streamID, fmt.Sprintf("%d%%", volume))
}

func setStreamMute(streamID string, muted bool) {
	if muted {
		_, _ = runCmd("pactl", "set-sink-input-mute", streamID, "true")
	} else {
		_, _ = runCmd("pactl", "set-sink-input-mute", streamID, "false")
	}
}

func setSourceOutputVolume(streamID string, volume int) {
	_, _ = runCmd("pactl", "set-source-output-volume", streamID, fmt.Sprintf("%d%%", volume))
}

func setSourceOutputMute(streamID string, muted bool) {
	if muted {
		_, _ = runCmd("pactl", "set-source-output-mute", streamID, "true")
	} else {
		_, _ = runCmd("pactl", "set-source-output-mute", streamID, "false")
	}
}

func moveSinkInput(streamID string, sinkName string) {
	_, _ = runCmd("pactl", "move-sink-input", streamID, sinkName)
}

func subscribe() {
	cmd := exec.Command("pactl", "subscribe")
	stdout, _ := cmd.StdoutPipe()
	_ = cmd.Start()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "sink") || strings.Contains(line, "source") || strings.Contains(line, "stream") {
			getStatus()
		}
	}
}

func GetCurrentStatus() *AudioStatus {
	statusMutex.RLock()
	defer statusMutex.RUnlock()
	return currentStatus
}

func UpdateStatus() {
	go getStatus()
}

func StartBackgroundUpdate() {
	ticker := time.NewTicker(2 * time.Second)
	go func() {
		for range ticker.C {
			getStatus()
		}
	}()
}
