package main

import "fmt"

// engineMemory is the Go engine's account of its memory limit, the types record's `memory`.
type engineMemory struct {
	LimitBytes     int64  `json:"limitBytes"`
	CapBytes       int64  `json:"capBytes"`
	Source         string `json:"source"`
	AvailableBytes uint64 `json:"availableBytes"`
	Environment    string `json:"environment"`
}

// line says the limit and where it came from, so nobody wonders why a run collected when it did, or why the cap
// did not apply.
func (memory engineMemory) line() string {
	limit := gigabytes(uint64(max(memory.LimitBytes, 0)))
	capped := gigabytes(uint64(max(memory.CapBytes, 0)))
	switch memory.Source {
	case "Environment":
		return fmt.Sprintf("the environment sets the Go engine's collector (%s), so its %s cap does not apply",
			memory.Environment, capped)
	case "Cap":
		return fmt.Sprintf("the Go engine's limit is its %s cap, under half of the %s available",
			limit, gigabytes(memory.AvailableBytes))
	case "Available":
		return fmt.Sprintf("the Go engine's limit is %s, half of the %s available, under its %s cap",
			limit, gigabytes(memory.AvailableBytes), capped)
	case "Unreadable":
		return fmt.Sprintf("the Go engine's limit is its %s cap, with the collector at Go's default, since "+
			"available memory could not be read", limit)
	}
	return fmt.Sprintf("the Go engine's limit is %s, from a source this front door does not know (%q)",
		limit, memory.Source)
}
