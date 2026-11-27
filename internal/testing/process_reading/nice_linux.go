package process_reading

// niceFromPriority is the nice value the raw getpriority system call answers with on Linux, which is 20 minus it, so
// that no answer is negative (getpriority(2)); glibc's wrapper turns it back, and Go's syscall package does not.
func niceFromPriority(priority int) int {
	return 20 - priority
}
