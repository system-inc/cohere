//go:build unix && !linux

package process_reading

// niceFromPriority is the nice value getpriority answers with, which outside Linux is the nice value itself.
func niceFromPriority(priority int) int {
	return priority
}
