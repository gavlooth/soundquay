package model

import "strings"

// AppKey finds the stable identity used to group playback and recording streams.
func AppKey(props map[string]string) (key string, routable bool) {
	if id := props["application.id"]; id != "" {
		return id, true
	}
	if binary := props["application.process.binary"]; binary != "" && !sharedRuntime(binary) {
		return binary, true
	}
	if name := props["application.name"]; name != "" {
		return name, true
	}
	return "node:" + props["node.name"], false
}

func sharedRuntime(binary string) bool {
	switch binary {
	case "electron", "python", "python3", "node", "java", "bwrap", "flatpak":
		return true
	default:
		return strings.HasPrefix(binary, "wine")
	}
}

func isApp(hasOwner bool, props map[string]string) bool {
	return !hasOwner && props["node.virtual"] != "true"
}
