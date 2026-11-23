package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// configuration is one build the census loads: a platform, whether cgo is on, and the build tags.
type configuration struct {
	operatingSystem string
	architecture    string
	// cgo is "0" or "1" as asked, or empty for the default: go's own for the host, off for an extra build.
	cgo    string
	tags   []string
	isHost bool
}

// parseConfiguration reads `goos/goarch[,cgo=0|1][,tags=a+b]`.
func parseConfiguration(spelled string) (configuration, error) {
	parts := strings.Split(spelled, ",")
	platform := strings.Split(parts[0], "/")
	if len(platform) != 2 || platform[0] == "" || platform[1] == "" {
		return configuration{}, fmt.Errorf("configuration %q: want goos/goarch first", spelled)
	}
	parsed := configuration{operatingSystem: platform[0], architecture: platform[1]}
	for _, part := range parts[1:] {
		key, value, hasValue := strings.Cut(part, "=")
		switch {
		case hasValue && key == "cgo" && (value == "0" || value == "1"):
			parsed.cgo = value
		case hasValue && key == "tags" && value != "":
			parsed.tags = strings.Split(value, "+")
		default:
			return configuration{}, fmt.Errorf("configuration %q: %q is neither cgo=0|1 nor tags=a+b", spelled, part)
		}
	}
	return parsed, nil
}

// target names the configuration as the engine's lint record names a build, given whether cgo is on.
func (build configuration) target(cgoOn bool) string {
	spelled := build.operatingSystem + "/" + build.architecture
	if cgoOn {
		spelled += " cgo"
	}
	if len(build.tags) != 0 {
		spelled += " tags " + strings.Join(build.tags, ",")
	}
	return spelled
}

// environment is the process's environment with the configuration's platform and cgo laid over it, and `GOWORK` at
// workFile. exec keeps the last value of a name, so these are the ones go reads.
func (build configuration) environment(workFile string) []string {
	environment := append(os.Environ(),
		"GOOS="+build.operatingSystem, "GOARCH="+build.architecture, "GOWORK="+workFile)
	switch {
	case build.cgo != "":
		environment = append(environment, "CGO_ENABLED="+build.cgo)
	case !build.isHost:
		// A cross platform's cgo would need a C compiler for it, so an extra build is cgo off unless it asks.
		environment = append(environment, "CGO_ENABLED=0")
	}
	return environment
}

// cgoChoice says where the configuration's cgo setting came from, for the report.
func (build configuration) cgoChoice() string {
	switch {
	case build.cgo != "":
		return "asked"
	case build.isHost:
		return "go's default for the host"
	default:
		return "off, the default for an extra build"
	}
}

// cgoEnabled asks go whether cgo is on under environment, in root.
func cgoEnabled(root string, environment []string) (bool, error) {
	command := exec.Command("go", "env", "CGO_ENABLED")
	command.Dir, command.Env = root, environment
	answer, runError := command.Output()
	if runError != nil {
		return false, fmt.Errorf("go env CGO_ENABLED: %w", runError)
	}
	return strings.TrimSpace(string(answer)) == "1", nil
}
