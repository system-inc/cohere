package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Port is a target the toolchain can build for, as `go tool dist list -json` lists it.
type Port struct {
	OperatingSystem string
	Architecture    string
	CgoSupported    bool
}

func (port Port) String() string {
	return port.OperatingSystem + "/" + port.Architecture
}

// ReadPorts is every target the toolchain go runs in directory under the current environment plus environment can
// build for. It runs in the directory and environment load's go env reading does, so the toolchain a go.mod or
// go.work selects, through `GOTOOLCHAIN`, is the one that answers; go env itself has no list of targets, so this is
// the one more command a run asks.
func ReadPorts(directory string, environment []string) ([]Port, error) {
	command := exec.Command("go", "tool", "dist", "list", "-json")
	command.Dir = directory
	command.Env = append(os.Environ(), environment...)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("listing the toolchain's targets with go tool dist list in %s: %w: %s", directory,
			runError, strings.TrimSpace(standardError.String()))
	}
	var listed []struct {
		Goos         string `json:"GOOS"`
		Goarch       string `json:"GOARCH"`
		CgoSupported bool   `json:"CgoSupported"`
	}
	if decodeError := json.Unmarshal(output, &listed); decodeError != nil {
		return nil, fmt.Errorf("listing the toolchain's targets with go tool dist list in %s: %w", directory,
			decodeError)
	}
	ports := make([]Port, 0, len(listed))
	for _, entry := range listed {
		ports = append(ports, Port{OperatingSystem: entry.Goos, Architecture: entry.Goarch,
			CgoSupported: entry.CgoSupported})
	}
	return ports, nil
}
