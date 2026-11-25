package main

import "fmt"

// goMemory is cohere-go's account of its memory limit, the types record's `memory`.
type goMemory struct {
	LimitBytes     int64  `json:"limitBytes"`
	Source         string `json:"source"`
	AvailableBytes uint64 `json:"availableBytes"`
	Environment    string `json:"environment"`
	SharedBy       uint64 `json:"sharedBy"`
}

// line says the limit and where it came from, and with buildsAtOnce over 0 how many builds beside the host's ran side
// by side under it, as `memory limit 5.4 GiB, half of the 11.5 GB available: 3 builds at once`, so nobody wonders why a
// run collected when it did, or why its builds ran as wide as they did (#jrr6a5x). The limit is in GiB, as Go's
// runtime takes it, and the memory available in GB to a tenth, as the front door's policy read it, so the half reads
// at a glance even on a machine with 11.5 GB.
func (memory goMemory) line(buildsAtOnce int) string {
	limit := fmt.Sprintf("%.1f GiB", float64(max(memory.LimitBytes, 0))/(1<<30))
	var said string
	switch memory.Source {
	case "Available":
		said = fmt.Sprintf("memory limit %s, half of the %.1f GB available", limit, float64(memory.AvailableBytes)/1e9)
		if memory.SharedBy > 1 {
			said += fmt.Sprintf(" shared by %d projects at once", memory.SharedBy)
		}
	case "Environment":
		if memory.LimitBytes == 0 {
			said = fmt.Sprintf("memory limit none, since the environment sets cohere-go's collector (%s)",
				memory.Environment)
		} else {
			said = fmt.Sprintf("memory limit %s, set by the environment (%s)", limit, memory.Environment)
		}
	case "Unreadable":
		said = fmt.Sprintf("memory limit %s as a bound alone, with the collector at Go's default, since available "+
			"memory could not be read", limit)
	default:
		said = fmt.Sprintf("memory limit %s, from a source this front door does not know (%q)", limit, memory.Source)
	}
	if buildsAtOnce > 0 {
		said += ": " + counted(buildsAtOnce, "build at once", "builds at once")
	}
	return said
}
