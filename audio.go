package main

import (
	"bufio"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"slices"
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

type CommandRunner func(name string, args ...string) (string, error)

type LookPathFunc func(file string) (string, error)

type AudioBackendCapabilities struct {
	SelectedBackend      string `json:"selected_backend"`
	HasPactl             bool   `json:"has_pactl"`
	HasWpctl             bool   `json:"has_wpctl"`
	HasPWMetadata        bool   `json:"has_pw_metadata"`
	HasPWCLI             bool   `json:"has_pw_cli"`
	HasWirePlumberBinary bool   `json:"has_wireplumber_binary"`
	PipeWireRunning      bool   `json:"pipewire_running"`
	WirePlumberRunning   bool   `json:"wireplumber_running"`
	MetadataReadable     bool   `json:"metadata_readable"`
	MetadataWritable     bool   `json:"metadata_writable"`
	AdvancedRouting      bool   `json:"advanced_routing"`
}

type DiagnosticCommandResult struct {
	Command   string `json:"command"`
	Available bool   `json:"available"`
	Output    string `json:"output"`
	Err       error  `json:"-"`
}

type PipeWireNode struct {
	ID          string            `json:"id"`
	ClientID    string            `json:"client_id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Nick        string            `json:"nick"`
	MediaClass  string            `json:"media_class"`
	Properties  map[string]string `json:"properties"`
}

type PipeWireClient struct {
	ID              string            `json:"id"`
	ApplicationName string            `json:"application_name"`
	ProcessBinary   string            `json:"process_binary"`
	ProcessID       string            `json:"process_id"`
	Properties      map[string]string `json:"properties"`
}

type RecordingClientCandidate struct {
	ClientID      string `json:"client_id"`
	Application   string `json:"application"`
	ProcessBinary string `json:"process_binary"`
	ProcessID     string `json:"process_id"`
	PortalAppID   string `json:"portal_app_id"`
	Reason        string `json:"reason"`
}

type AdvancedRecordingInference struct {
	StreamNodeID string                     `json:"stream_node_id"`
	StreamLabel  string                     `json:"stream_label"`
	Candidates   []RecordingClientCandidate `json:"candidates"`
}

type PipeWireLink struct {
	ID         string `json:"id"`
	OutputNode string `json:"output_node"`
	InputNode  string `json:"input_node"`
}

type AdvancedRecordingStream struct {
	StreamNodeID    string `json:"stream_node_id"`
	StreamName      string `json:"stream_name"`
	Application     string `json:"application"`
	ClientID        string `json:"client_id"`
	CurrentSourceID string `json:"current_source_id"`
	CurrentSource   string `json:"current_source"`
}

type AudioBackend interface {
	Name() string
	Capabilities() AudioBackendCapabilities
	GetStatus() *AudioStatus
	GetAdvancedSources() []AudioSource
	GetAdvancedRecordingStreams() []AdvancedRecordingStream
	MoveAdvancedRecordingStream(streamNodeID string, sourceNodeName string) error
	SetVolume(volume int) error
	SetMute(muted bool) error
	SetMicVolume(volume int) error
	SetMicMute(muted bool) error
	SetSink(sinkName string) error
	SetSource(sourceName string) error
	SetStreamVolume(streamID string, volume int) error
	SetStreamMute(streamID string, muted bool) error
	SetSourceOutputVolume(streamID string, volume int) error
	SetSourceOutputMute(streamID string, muted bool) error
	MoveSinkInput(streamID string, sinkName string) error
	MoveSourceOutput(streamID string, sourceName string) error
}

type pactlBackend struct {
	runner       CommandRunner
	capabilities AudioBackendCapabilities
}

var (
	currentStatus = &AudioStatus{
		Streams:    []AudioStream{},
		MicStreams: []MicStream{},
		Sinks:      []AudioSink{},
		Sources:    []AudioSource{},
	}
	statusMutex sync.RWMutex
	backendOnce sync.Once
	backendInst AudioBackend
)

func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	return string(out), err
}

func commandAvailable(name string, lookPath LookPathFunc) bool {
	_, err := lookPath(name)
	return err == nil
}

func runDiagnosticCommand(run CommandRunner, lookPath LookPathFunc, name string, args ...string) DiagnosticCommandResult {
	result := DiagnosticCommandResult{
		Command:   strings.TrimSpace(name + " " + strings.Join(args, " ")),
		Available: commandAvailable(name, lookPath),
	}
	if !result.Available {
		return result
	}

	out, err := run(name, args...)
	result.Output = out
	result.Err = err
	return result
}

func parseWpctlRuntime(out string) (pipeWireRunning bool, wirePlumberRunning bool) {
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.Contains(line, "PipeWire 'pipewire-0'") {
			pipeWireRunning = true
		}
		if strings.Contains(line, "WirePlumber") {
			wirePlumberRunning = true
		}
	}
	return pipeWireRunning, wirePlumberRunning
}

func detectAudioBackendCapabilities(run CommandRunner, lookPath LookPathFunc) AudioBackendCapabilities {
	capabilities := AudioBackendCapabilities{
		SelectedBackend:      "pactl",
		HasPactl:             commandAvailable("pactl", lookPath),
		HasWpctl:             commandAvailable("wpctl", lookPath),
		HasPWMetadata:        commandAvailable("pw-metadata", lookPath),
		HasPWCLI:             commandAvailable("pw-cli", lookPath),
		HasWirePlumberBinary: commandAvailable("wireplumber", lookPath),
	}

	if capabilities.HasWpctl {
		if out, err := run("wpctl", "status"); err == nil {
			capabilities.PipeWireRunning, capabilities.WirePlumberRunning = parseWpctlRuntime(out)
		}
	}

	if capabilities.HasPWMetadata {
		if out, err := run("pw-metadata", "-n", "settings"); err == nil {
			capabilities.MetadataReadable = strings.Contains(out, `Found "settings" metadata`)
		}
	}

	capabilities.MetadataWritable = capabilities.HasPWMetadata &&
		capabilities.PipeWireRunning &&
		capabilities.WirePlumberRunning &&
		capabilities.MetadataReadable

	capabilities.AdvancedRouting = capabilities.HasWpctl &&
		capabilities.HasPWMetadata &&
		capabilities.PipeWireRunning &&
		capabilities.WirePlumberRunning &&
		capabilities.MetadataReadable &&
		capabilities.MetadataWritable

	if capabilities.AdvancedRouting {
		capabilities.SelectedBackend = "pipewire"
	}

	return capabilities
}

func newPactlBackend(run CommandRunner, capabilities AudioBackendCapabilities) AudioBackend {
	return &pactlBackend{
		runner:       run,
		capabilities: capabilities,
	}
}

type pipeWireBackend struct {
	*pactlBackend
}

func newPipeWireBackend(run CommandRunner, capabilities AudioBackendCapabilities) AudioBackend {
	return &pipeWireBackend{
		pactlBackend: &pactlBackend{
			runner:       run,
			capabilities: capabilities,
		},
	}
}

func newAudioBackend(run CommandRunner, capabilities AudioBackendCapabilities) AudioBackend {
	if capabilities.AdvancedRouting {
		return newPipeWireBackend(run, capabilities)
	}
	return newPactlBackend(run, capabilities)
}

func getAudioBackend() AudioBackend {
	backendOnce.Do(func() {
		capabilities := detectAudioBackendCapabilities(runCmd, exec.LookPath)
		backendInst = newAudioBackend(runCmd, capabilities)
	})
	return backendInst
}

func GetAudioBackendCapabilities() AudioBackendCapabilities {
	return getAudioBackend().Capabilities()
}

func GetAdvancedRecordingStreams() []AdvancedRecordingStream {
	return getAudioBackend().GetAdvancedRecordingStreams()
}

func GetAdvancedSources() []AudioSource {
	return getAudioBackend().GetAdvancedSources()
}

func moveAdvancedRecordingStream(streamNodeID string, sourceNodeName string) error {
	return getAudioBackend().MoveAdvancedRecordingStream(streamNodeID, sourceNodeName)
}

func GetAudioBackendDiagnostics() []DiagnosticCommandResult {
	commands := [][]string{
		{"pactl", "info"},
		{"wpctl", "status"},
		{"pw-metadata", "-n", "settings"},
		{"pw-cli", "info", "0"},
	}

	results := make([]DiagnosticCommandResult, 0, len(commands))
	for _, command := range commands {
		results = append(results, runDiagnosticCommand(runCmd, exec.LookPath, command[0], command[1:]...))
	}
	return results
}

func GetRecordingClientCandidates() []RecordingClientCandidate {
	return discoverRecordingClientCandidates(runCmd)
}

func InferAdvancedRecordingOwners(streams []AdvancedRecordingStream, candidates []RecordingClientCandidate) []AdvancedRecordingInference {
	inferences := make([]AdvancedRecordingInference, 0, len(streams))
	for _, stream := range streams {
		ranked := rankRecordingCandidatesForStream(stream, candidates)
		if len(ranked) > 3 {
			ranked = ranked[:3]
		}
		inferences = append(inferences, AdvancedRecordingInference{
			StreamNodeID: stream.StreamNodeID,
			StreamLabel:  firstNonEmpty(stream.Application, stream.StreamName),
			Candidates:   ranked,
		})
	}
	return inferences
}

func parsePWCLIClients(out string) map[string]PipeWireClient {
	clients := make(map[string]PipeWireClient)
	var current *PipeWireClient

	for _, line := range strings.Split(out, "\n") {
		if matches := regexp.MustCompile(`^id\s+(\d+),\s+type\s+PipeWire:Interface:Client/3`).FindStringSubmatch(strings.TrimSpace(line)); len(matches) > 1 {
			if current != nil {
				clients[current.ID] = *current
			}
			current = &PipeWireClient{
				ID:         matches[1],
				Properties: make(map[string]string),
			}
			continue
		}

		if current == nil {
			continue
		}

		if matches := regexp.MustCompile(`^\s*([A-Za-z0-9._-]+)\s+=\s+"?(.*?)"?\s*$`).FindStringSubmatch(line); len(matches) > 2 {
			key := matches[1]
			value := matches[2]
			current.Properties[key] = value
			if key == "application.name" {
				current.ApplicationName = value
			}
			if key == "application.process.binary" {
				current.ProcessBinary = value
			}
			if key == "application.process.id" {
				current.ProcessID = value
			}
		}
	}

	if current != nil {
		clients[current.ID] = *current
	}

	return clients
}

func parsePWCLINodes(out string) map[string]PipeWireNode {
	nodes := make(map[string]PipeWireNode)
	var current *PipeWireNode

	for _, line := range strings.Split(out, "\n") {
		if matches := regexp.MustCompile(`^id\s+(\d+),\s+type\s+PipeWire:Interface:Node/3`).FindStringSubmatch(strings.TrimSpace(line)); len(matches) > 1 {
			if current != nil {
				nodes[current.ID] = *current
			}
			current = &PipeWireNode{
				ID:         matches[1],
				Properties: make(map[string]string),
			}
			continue
		}

		if current == nil {
			continue
		}

		if matches := regexp.MustCompile(`^\s*([A-Za-z0-9._-]+)\s+=\s+"?(.*?)"?\s*$`).FindStringSubmatch(line); len(matches) > 2 {
			key := matches[1]
			value := matches[2]
			current.Properties[key] = value
			switch key {
			case "client.id":
				current.ClientID = value
			case "node.name":
				current.Name = value
			case "node.description":
				current.Description = value
			case "node.nick":
				current.Nick = value
			case "media.class":
				current.MediaClass = value
			}
		}
	}

	if current != nil {
		nodes[current.ID] = *current
	}

	return nodes
}

func parsePWCLILinks(out string) []PipeWireLink {
	var links []PipeWireLink
	var current *PipeWireLink

	for _, line := range strings.Split(out, "\n") {
		if matches := regexp.MustCompile(`^id\s+(\d+),\s+type\s+PipeWire:Interface:Link/3`).FindStringSubmatch(strings.TrimSpace(line)); len(matches) > 1 {
			if current != nil {
				links = append(links, *current)
			}
			current = &PipeWireLink{ID: matches[1]}
			continue
		}

		if current == nil {
			continue
		}

		if matches := regexp.MustCompile(`^\s*(link\.(?:output|input)\.node)\s+=\s+"?(.*?)"?\s*$`).FindStringSubmatch(line); len(matches) > 2 {
			switch matches[1] {
			case "link.output.node":
				current.OutputNode = matches[2]
			case "link.input.node":
				current.InputNode = matches[2]
			}
		}
	}

	if current != nil {
		links = append(links, *current)
	}

	return links
}

func discoverAdvancedRecordingStreams(run CommandRunner) []AdvancedRecordingStream {
	clientOut, err := run("pw-cli", "ls", "Client")
	if err != nil {
		return nil
	}
	nodeOut, err := run("pw-cli", "ls", "Node")
	if err != nil {
		return nil
	}
	linkOut, err := run("pw-cli", "ls", "Link")
	if err != nil {
		return nil
	}

	clients := parsePWCLIClients(clientOut)
	nodes := parsePWCLINodes(nodeOut)
	links := parsePWCLILinks(linkOut)

	// PipeWire exposes active capture streams as Stream/Input/Audio nodes.
	// We resolve a user-facing app name from the owning client application.name,
	// then follow Link objects from Audio/Source nodes into the stream node to
	// determine the stream's current input source.
	var streams []AdvancedRecordingStream
	for _, node := range nodes {
		if node.MediaClass != "Stream/Input/Audio" {
			continue
		}
		if node.Properties["node.virtual"] == "true" {
			continue
		}

		client := clients[node.ClientID]
		appName := client.ApplicationName
		if isGenericPipeWireApplication(appName) {
			appName = ""
		}
		if appName == "" {
			appName = node.Description
		}
		if appName == "" {
			appName = node.Name
		}

		stream := AdvancedRecordingStream{
			StreamNodeID: node.ID,
			StreamName:   firstNonEmpty(node.Description, node.Name),
			Application:  appName,
			ClientID:     node.ClientID,
		}

		for _, link := range links {
			if link.InputNode != node.ID {
				continue
			}
			sourceNode, ok := nodes[link.OutputNode]
			if !ok || sourceNode.MediaClass != "Audio/Source" {
				continue
			}
			stream.CurrentSourceID = sourceNode.ID
			stream.CurrentSource = firstNonEmpty(sourceNode.Description, sourceNode.Nick, sourceNode.Name)
			break
		}

		streams = append(streams, stream)
	}

	return streams
}

func discoverRecordingClientCandidates(run CommandRunner) []RecordingClientCandidate {
	clientOut, err := run("pw-cli", "ls", "Client")
	if err != nil {
		return nil
	}

	clients := parsePWCLIClients(clientOut)
	candidates := make([]RecordingClientCandidate, 0)
	for _, client := range clients {
		if isGenericPipeWireApplication(client.ApplicationName) {
			continue
		}

		client = inspectRecordingClientCandidate(run, client)
		reason := recordingCandidateReason(client)
		if reason == "" {
			continue
		}

		candidates = append(candidates, RecordingClientCandidate{
			ClientID:      client.ID,
			Application:   client.ApplicationName,
			ProcessBinary: client.ProcessBinary,
			ProcessID:     client.ProcessID,
			PortalAppID:   client.Properties["pipewire.access.portal.app_id"],
			Reason:        reason,
		})
	}

	slices.SortFunc(candidates, func(a, b RecordingClientCandidate) int {
		if a.Application == b.Application {
			return strings.Compare(a.ClientID, b.ClientID)
		}
		return strings.Compare(a.Application, b.Application)
	})

	return candidates
}

func inspectRecordingClientCandidate(run CommandRunner, client PipeWireClient) PipeWireClient {
	out, err := run("wpctl", "inspect", client.ID)
	if err != nil {
		return client
	}

	properties := parseWPCTLProperties(out)
	if client.Properties == nil {
		client.Properties = make(map[string]string)
	}
	for key, value := range properties {
		client.Properties[key] = value
	}
	if value := properties["application.name"]; value != "" {
		client.ApplicationName = value
	}
	if value := properties["application.process.binary"]; value != "" {
		client.ProcessBinary = value
	}
	if value := properties["application.process.id"]; value != "" {
		client.ProcessID = value
	}
	return client
}

func parseWPCTLProperties(out string) map[string]string {
	properties := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		matches := regexp.MustCompile(`^\s*\*?\s*([A-Za-z0-9._-]+)\s+=\s+"?(.*?)"?\s*$`).FindStringSubmatch(line)
		if len(matches) > 2 {
			properties[matches[1]] = matches[2]
		}
	}
	return properties
}

func recordingCandidateReason(client PipeWireClient) string {
	name := strings.ToLower(client.ApplicationName)
	binary := strings.ToLower(client.ProcessBinary)

	switch {
	case strings.Contains(name, "input"):
		return "application name suggests capture client"
	case strings.Contains(name, "voiceengine"):
		return "webrtc voice engine client"
	case binary == "chrome" || binary == "chromium":
		return "browser client present"
	case binary == "firefox" || binary == "thunderbird":
		return "communication/browser client present"
	case binary == "obs":
		return "recording client present"
	default:
		return ""
	}
}

func rankRecordingCandidatesForStream(stream AdvancedRecordingStream, candidates []RecordingClientCandidate) []RecordingClientCandidate {
	type scoredCandidate struct {
		candidate RecordingClientCandidate
		score     int
	}

	scored := make([]scoredCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		score := 0
		appName := strings.ToLower(candidate.Application)
		binary := strings.ToLower(candidate.ProcessBinary)
		streamLabel := strings.ToLower(firstNonEmpty(stream.Application, stream.StreamName))

		if strings.Contains(appName, "google chrome input") {
			score += 100
		}
		if strings.Contains(appName, "input") {
			score += 40
		}
		if strings.Contains(appName, "voiceengine") {
			score += 30
		}
		if binary == "chrome" || binary == "chromium" {
			score += 35
		}
		if binary == "firefox" || binary == "obs" {
			score += 20
		}
		if candidate.PortalAppID != "" {
			score += 10
		}
		if strings.Contains(streamLabel, "echo-cancel") && (strings.Contains(appName, "input") || strings.Contains(appName, "voiceengine")) {
			score += 15
		}

		if score > 0 {
			scored = append(scored, scoredCandidate{candidate: candidate, score: score})
		}
	}

	slices.SortFunc(scored, func(a, b scoredCandidate) int {
		if a.score == b.score {
			if a.candidate.Application == b.candidate.Application {
				return strings.Compare(a.candidate.ClientID, b.candidate.ClientID)
			}
			return strings.Compare(a.candidate.Application, b.candidate.Application)
		}
		return b.score - a.score
	})

	ranked := make([]RecordingClientCandidate, 0, len(scored))
	for _, item := range scored {
		ranked = append(ranked, item.candidate)
	}
	return ranked
}

func isGenericPipeWireApplication(application string) bool {
	switch strings.TrimSpace(application) {
	case "", "pipewire", "WirePlumber", "WirePlumber [export]":
		return true
	default:
		return false
	}
}

func discoverAdvancedSources(run CommandRunner) []AudioSource {
	nodeOut, err := run("pw-cli", "ls", "Node")
	if err != nil {
		return nil
	}

	nodes := parsePWCLINodes(nodeOut)
	sources := make([]AudioSource, 0)
	for _, node := range nodes {
		if node.MediaClass != "Audio/Source" {
			continue
		}
		if node.Properties["node.virtual"] == "true" {
			continue
		}

		// node.name is the stable routing target for WirePlumber target.object,
		// while description/nick are the user-facing labels shown in the UI.
		sources = append(sources, AudioSource{
			ID:          node.ID,
			Name:        node.Name,
			Description: firstNonEmpty(node.Description, node.Nick, node.Name),
			PipeWireID:  node.ID,
		})
	}

	return sources
}

func wirePlumberMoveInputStream(run CommandRunner, streamNodeID string, sourceNodeName string) error {
	if strings.TrimSpace(streamNodeID) == "" {
		return fmt.Errorf("stream node id is required")
	}
	if strings.TrimSpace(sourceNodeName) == "" {
		return fmt.Errorf("source node name is required")
	}
	_, err := run("pw-metadata", "-n", "default", streamNodeID, "target.object", sourceNodeName)
	return err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func getPipeWireDefaultIDForRunner(run CommandRunner) string {
	out, _ := run("wpctl", "status")
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

func pactlGetSinkInputs(run CommandRunner) []AudioStream {
	out, _ := run("pactl", "list", "sink-inputs")
	return parseSinkInputs(out)
}

func parseSinkInputs(out string) []AudioStream {
	var streams []AudioStream

	scanner := bufio.NewScanner(strings.NewReader(out))
	var stream *AudioStream
	reSink := regexp.MustCompile(`(?i)^sink:\s+(.+)$`)
	// Capture the percentage (e.g., "50" from "front-left: 32768 /  50% / -18.06 dB")
	reVolume := regexp.MustCompile(`(?i)volume:\s+front-left:\s+\d+\s*/\s*(\d+)%`)
	reMono := regexp.MustCompile(`(?i)volume:\s+mono:\s+\d+\s*/\s*(\d+)%`)
	reMuted := regexp.MustCompile(`(?i)^\s*mute(?:d)?:\s+(\w+)`)
	reName := regexp.MustCompile(`application.name\s+=\s+"([^"]+)"`)
	reBinary := regexp.MustCompile(`application.process.binary\s+=\s+"([^"]+)"`)
	reMediaName := regexp.MustCompile(`media.name\s+=\s+"([^"]+)"`)
	reNodeName := regexp.MustCompile(`node.name\s+=\s+"([^"]+)"`)
	reDeviceDescription := regexp.MustCompile(`device.description\s+=\s+"([^"]+)"`)

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
			if stream.Name == "" {
				if matches := reMediaName.FindStringSubmatch(line); len(matches) > 1 {
					stream.Name = matches[1]
				} else if matches := reDeviceDescription.FindStringSubmatch(line); len(matches) > 1 {
					stream.Name = matches[1]
				} else if matches := reNodeName.FindStringSubmatch(line); len(matches) > 1 {
					stream.Name = matches[1]
				}
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

func pactlGetSourceOutputs(run CommandRunner) []MicStream {
	out, _ := run("pactl", "list", "source-outputs")
	return parseSourceOutputs(out)
}

func parseSourceOutputs(out string) []MicStream {
	var streams []MicStream

	scanner := bufio.NewScanner(strings.NewReader(out))
	var stream *MicStream
	reSource := regexp.MustCompile(`(?i)^source:\s+(.+)$`)
	// Capture the percentage (e.g., "50" from "front-left: 32768 /  50% / -18.06 dB")
	reVolume := regexp.MustCompile(`(?i)volume:\s+front-left:\s+\d+\s*/\s*(\d+)%`)
	reMono := regexp.MustCompile(`(?i)volume:\s+mono:\s+\d+\s*/\s*(\d+)%`)
	reMuted := regexp.MustCompile(`(?i)^\s*mute(?:d)?:\s+(\w+)`)
	reName := regexp.MustCompile(`application.name\s+=\s+"([^"]+)"`)
	reBinary := regexp.MustCompile(`application.process.binary\s+=\s+"([^"]+)"`)
	reMediaName := regexp.MustCompile(`media.name\s+=\s+"([^"]+)"`)
	reNodeName := regexp.MustCompile(`node.name\s+=\s+"([^"]+)"`)
	reDeviceDescription := regexp.MustCompile(`device.description\s+=\s+"([^"]+)"`)

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
			if stream.Name == "" {
				if matches := reMediaName.FindStringSubmatch(line); len(matches) > 1 {
					stream.Name = matches[1]
				} else if matches := reDeviceDescription.FindStringSubmatch(line); len(matches) > 1 {
					stream.Name = matches[1]
				} else if matches := reNodeName.FindStringSubmatch(line); len(matches) > 1 {
					stream.Name = matches[1]
				}
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

func pactlGetSinks(run CommandRunner) []AudioSink {
	out, _ := run("pactl", "list", "sinks")
	defaultSink, _ := run("pactl", "get-default-sink")
	defaultSink = strings.TrimSpace(defaultSink)
	pipeWireID := getPipeWireDefaultIDForRunner(run)

	return parseSinks(out, defaultSink, pipeWireID)
}

func parseSinks(out, defaultSink, pipeWireID string) []AudioSink {
	var sinks []AudioSink

	scanner := bufio.NewScanner(strings.NewReader(out))
	var sink *AudioSink
	// Capture the percentage (e.g., "50" from "front-left: 32768 /  50% / -18.06 dB")
	reVolume := regexp.MustCompile(`(?i)volume:\s+front-left:\s+\d+\s*/\s*(\d+)%`)
	reMono := regexp.MustCompile(`(?i)volume:\s+mono:\s+\d+\s*/\s*(\d+)%`)
	reMuted := regexp.MustCompile(`(?i)^\s*mute(?:d)?:\s+(\w+)`)
	reDesc := regexp.MustCompile(`device.description\s+=\s+"([^"]+)"`)
	reDesc2 := regexp.MustCompile(`node.name\s+=\s+"([^"]+)"`)
	reSinkName := regexp.MustCompile(`^\s*Name:\s+(.+)$`)

	for scanner.Scan() {
		line := scanner.Text()

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
			// Parse the actual sink Name (e.g., "Name: alsa_output.usb-...")
			if matches := reSinkName.FindStringSubmatch(line); len(matches) > 1 {
				sink.Name = matches[1]
			}
			if matches := reDesc.FindStringSubmatch(line); len(matches) > 1 {
				sink.Description = matches[1]
			}
			if sink.Description == "" {
				if matches := reDesc2.FindStringSubmatch(line); len(matches) > 1 {
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

	for i := range sinks {
		sinks[i].Default = sinks[i].Name == defaultSink
		sinks[i].PipeWireID = pipeWireID
	}

	return sinks
}

func pactlGetSources(run CommandRunner) []AudioSource {
	out, _ := run("pactl", "list", "sources")
	defaultSource, _ := run("pactl", "get-default-source")
	defaultSource = strings.TrimSpace(defaultSource)
	pipeWireID := getPipeWireDefaultIDForRunner(run)

	return parseSources(out, defaultSource, pipeWireID)
}

func parseSources(out, defaultSource, pipeWireID string) []AudioSource {
	var sources []AudioSource

	scanner := bufio.NewScanner(strings.NewReader(out))
	var source *AudioSource
	// Capture the percentage (e.g., "50" from "front-left: 32768 /  50% / -18.06 dB")
	reVolume := regexp.MustCompile(`(?i)volume:\s+front-left:\s+\d+\s*/\s*(\d+)%`)
	reMono := regexp.MustCompile(`(?i)volume:\s+mono:\s+\d+\s*/\s*(\d+)%`)
	reMuted := regexp.MustCompile(`(?i)^\s*mute(?:d)?:\s+(\w+)`)
	reDesc := regexp.MustCompile(`device.description\s+=\s+"([^"]+)"`)
	reDesc2 := regexp.MustCompile(`node.name\s+=\s+"([^"]+)"`)
	reSourceName := regexp.MustCompile(`^\s*Name:\s+(.+)$`)

	for scanner.Scan() {
		line := scanner.Text()

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
			// Parse the actual source Name
			if matches := reSourceName.FindStringSubmatch(line); len(matches) > 1 {
				source.Name = matches[1]
			}
			if matches := reDesc.FindStringSubmatch(line); len(matches) > 1 {
				source.Description = matches[1]
			}
			if source.Description == "" {
				if matches := reDesc2.FindStringSubmatch(line); len(matches) > 1 {
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

	for i := range sources {
		sources[i].Default = sources[i].Name == defaultSource
		sources[i].PipeWireID = pipeWireID
	}

	return sources
}

func pactlGetDefaultSink(run CommandRunner) (string, int, bool) {
	out, _ := run("pactl", "get-default-sink")
	sinkName := strings.TrimSpace(out)

	sinks := pactlGetSinks(run)
	for _, sink := range sinks {
		if sink.Name == sinkName {
			return sinkName, sink.Volume, sink.Muted
		}
	}

	return sinkName, 50, false
}

func pactlGetDefaultSource(run CommandRunner) (string, int, bool) {
	out, _ := run("pactl", "get-default-source")
	sourceName := strings.TrimSpace(out)

	sources := pactlGetSources(run)
	for _, source := range sources {
		if source.Name == sourceName {
			return sourceName, source.Volume, source.Muted
		}
	}

	return sourceName, 50, false
}

func pactlGetStatus(run CommandRunner) *AudioStatus {
	status := &AudioStatus{
		Streams:    []AudioStream{},
		MicStreams: []MicStream{},
		Sinks:      []AudioSink{},
		Sources:    []AudioSource{},
	}

	defaultSink, volume, muted := pactlGetDefaultSink(run)
	status.DefaultSink = defaultSink
	status.Volume = volume
	status.Muted = muted

	status.Streams = pactlGetSinkInputs(run)
	status.StreamCount = len(status.Streams)
	status.Sinks = pactlGetSinks(run)

	defaultSource, micVol, micMuted := pactlGetDefaultSource(run)
	status.DefaultSource = defaultSource
	status.MicVolume = micVol
	status.MicMuted = micMuted

	status.MicStreams = pactlGetSourceOutputs(run)
	status.MicCount = len(status.MicStreams)
	status.Sources = pactlGetSources(run)

	statusMutex.Lock()
	currentStatus = status
	statusMutex.Unlock()

	return status
}

const debounceDelay = 50 * time.Millisecond

func (b *pactlBackend) Name() string {
	return "pactl"
}

func (b *pipeWireBackend) Name() string {
	return "pipewire"
}

func (b *pactlBackend) Capabilities() AudioBackendCapabilities {
	return b.capabilities
}

func (b *pactlBackend) GetStatus() *AudioStatus {
	return pactlGetStatus(b.runner)
}

func (b *pactlBackend) GetAdvancedSources() []AudioSource {
	if !b.capabilities.AdvancedRouting {
		return nil
	}
	return discoverAdvancedSources(b.runner)
}

func (b *pactlBackend) GetAdvancedRecordingStreams() []AdvancedRecordingStream {
	if !b.capabilities.AdvancedRouting {
		return nil
	}
	return discoverAdvancedRecordingStreams(b.runner)
}

func (b *pactlBackend) MoveAdvancedRecordingStream(streamNodeID string, sourceNodeName string) error {
	if !b.capabilities.AdvancedRouting {
		return fmt.Errorf("advanced routing is not available")
	}
	return wirePlumberMoveInputStream(b.runner, streamNodeID, sourceNodeName)
}

func (b *pactlBackend) SetVolume(volume int) error {
	return pactlSetVolume(b.runner, volume)
}

func (b *pactlBackend) SetMute(muted bool) error {
	return pactlSetMute(b.runner, muted)
}

func (b *pactlBackend) SetMicVolume(volume int) error {
	return pactlSetMicVolume(b.runner, volume)
}

func (b *pactlBackend) SetMicMute(muted bool) error {
	return pactlSetMicMute(b.runner, muted)
}

func (b *pactlBackend) SetSink(sinkName string) error {
	return pactlSetSink(b.runner, sinkName)
}

func (b *pactlBackend) SetSource(sourceName string) error {
	return pactlSetSource(b.runner, sourceName)
}

func (b *pactlBackend) SetStreamVolume(streamID string, volume int) error {
	return pactlSetStreamVolume(b.runner, streamID, volume)
}

func (b *pactlBackend) SetStreamMute(streamID string, muted bool) error {
	return pactlSetStreamMute(b.runner, streamID, muted)
}

func (b *pactlBackend) SetSourceOutputVolume(streamID string, volume int) error {
	return pactlSetSourceOutputVolume(b.runner, streamID, volume)
}

func (b *pactlBackend) SetSourceOutputMute(streamID string, muted bool) error {
	return pactlSetSourceOutputMute(b.runner, streamID, muted)
}

func (b *pactlBackend) MoveSinkInput(streamID string, sinkName string) error {
	return pactlMoveSinkInput(b.runner, streamID, sinkName)
}

func (b *pactlBackend) MoveSourceOutput(streamID string, sourceName string) error {
	return pactlMoveSourceOutput(b.runner, streamID, sourceName)
}

func getStatus() *AudioStatus {
	return getAudioBackend().GetStatus()
}

func setVolume(volume int) error {
	return getAudioBackend().SetVolume(volume)
}

func setMute(muted bool) error {
	return getAudioBackend().SetMute(muted)
}

func setMicVolume(volume int) error {
	return getAudioBackend().SetMicVolume(volume)
}

func setMicMute(muted bool) error {
	return getAudioBackend().SetMicMute(muted)
}

func setSink(sinkName string) error {
	return getAudioBackend().SetSink(sinkName)
}

func setSource(sourceName string) error {
	return getAudioBackend().SetSource(sourceName)
}

func setStreamVolume(streamID string, volume int) error {
	return getAudioBackend().SetStreamVolume(streamID, volume)
}

func setStreamMute(streamID string, muted bool) error {
	return getAudioBackend().SetStreamMute(streamID, muted)
}

func setSourceOutputVolume(streamID string, volume int) error {
	return getAudioBackend().SetSourceOutputVolume(streamID, volume)
}

func setSourceOutputMute(streamID string, muted bool) error {
	return getAudioBackend().SetSourceOutputMute(streamID, muted)
}

func moveSinkInput(streamID string, sinkName string) error {
	return getAudioBackend().MoveSinkInput(streamID, sinkName)
}

func moveSourceOutput(streamID string, sourceName string) error {
	return getAudioBackend().MoveSourceOutput(streamID, sourceName)
}

func pactlSetVolume(run CommandRunner, volume int) error {
	cmd := fmt.Sprintf("%d%%", volume)
	log.Printf("[PACTL] set-sink-volume @DEFAULT_SINK@ %s", cmd)
	out, err := run("pactl", "set-sink-volume", "@DEFAULT_SINK@", cmd)
	if err != nil {
		log.Printf("[PACTL] ERROR: %v, output: %s", err, out)
	}
	return err
}

func pactlSetMute(run CommandRunner, muted bool) error {
	if muted {
		_, err := run("pactl", "set-sink-mute", "@DEFAULT_SINK@", "true")
		return err
	}
	_, err := run("pactl", "set-sink-mute", "@DEFAULT_SINK@", "false")
	return err
}

func pactlSetMicVolume(run CommandRunner, volume int) error {
	_, err := run("pactl", "set-source-volume", "@DEFAULT_SOURCE@", fmt.Sprintf("%d%%", volume))
	return err
}

func pactlSetMicMute(run CommandRunner, muted bool) error {
	if muted {
		_, err := run("pactl", "set-source-mute", "@DEFAULT_SOURCE@", "true")
		return err
	}
	_, err := run("pactl", "set-source-mute", "@DEFAULT_SOURCE@", "false")
	return err
}

func pactlSetSink(run CommandRunner, sinkName string) error {
	_, err := run("pactl", "set-default-sink", sinkName)
	return err
}

func pactlSetSource(run CommandRunner, sourceName string) error {
	_, err := run("pactl", "set-default-source", sourceName)
	return err
}

func pactlSetStreamVolume(run CommandRunner, streamID string, volume int) error {
	_, err := run("pactl", "set-sink-input-volume", streamID, fmt.Sprintf("%d%%", volume))
	return err
}

func pactlSetStreamMute(run CommandRunner, streamID string, muted bool) error {
	if muted {
		_, err := run("pactl", "set-sink-input-mute", streamID, "true")
		return err
	}
	_, err := run("pactl", "set-sink-input-mute", streamID, "false")
	return err
}

func pactlSetSourceOutputVolume(run CommandRunner, streamID string, volume int) error {
	_, err := run("pactl", "set-source-output-volume", streamID, fmt.Sprintf("%d%%", volume))
	return err
}

func pactlSetSourceOutputMute(run CommandRunner, streamID string, muted bool) error {
	if muted {
		_, err := run("pactl", "set-source-output-mute", streamID, "true")
		return err
	}
	_, err := run("pactl", "set-source-output-mute", streamID, "false")
	return err
}

func pactlMoveSinkInput(run CommandRunner, streamID string, sinkName string) error {
	_, err := run("pactl", "move-sink-input", streamID, sinkName)
	return err
}

func pactlMoveSourceOutput(run CommandRunner, streamID string, sourceName string) error {
	_, err := run("pactl", "move-source-output", streamID, sourceName)
	return err
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
