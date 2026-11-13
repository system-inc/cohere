package main

// downloadedModule is one module as `go mod download -json` reports it: its path and version, and why it could not be
// had, empty when it is in the module cache.
type downloadedModule struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
	Error   string `json:"Error"`
}
