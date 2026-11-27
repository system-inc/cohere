package process_reading

import "time"

// Describe reads pid's parent and start from the process table (processEntry) and its command line with Command,
// wrapping ErrNoProcess when no process holds the id.
func Describe(pid int) (Description, error) {
	process, entryError := processEntry(pid)
	if entryError != nil {
		return Description{}, entryError
	}
	command, commandError := Command(pid)
	if commandError != nil {
		return Description{}, commandError
	}
	started := process.Proc.P_starttime
	return Description{
		Parent:  int(process.Eproc.Ppid),
		Started: time.Unix(started.Sec, int64(started.Usec)*int64(time.Microsecond)),
		Command: command,
	}, nil
}
