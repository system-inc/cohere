package process_reading

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
)

// userTicksPerSecond is the clock /proc/<pid>/stat counts a start in, `USER_HZ`, which Linux fixes at 100 for every
// program on every architecture, whatever the kernel's own tick.
const userTicksPerSecond = 100

// Describe reads pid's parent and start from /proc/<pid>/stat, the start counted in userTicksPerSecond from the boot
// time /proc/stat names, and its command line with Command, wrapping ErrNoProcess when no process holds the id.
func Describe(pid int) (Description, error) {
	stat, readError := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if errors.Is(readError, fs.ErrNotExist) {
		return Description{}, fmt.Errorf("reading process %d's status: %w", pid, ErrNoProcess)
	}
	if readError != nil {
		return Description{}, fmt.Errorf("reading process %d's status: %w", pid, readError)
	}
	// The command name in parentheses may hold spaces and parentheses, so the fields are read after its last one:
	// the state is the third field of the line, the parent the fourth and the start the twenty-second.
	index := strings.LastIndex(string(stat), ") ")
	if index < 0 {
		return Description{}, fmt.Errorf("process %d's status %q has no parent and start to read", pid, stat)
	}
	fields := strings.Fields(string(stat)[index+2:])
	if len(fields) < 20 {
		return Description{}, fmt.Errorf("process %d's status %q has no parent and start to read", pid, stat)
	}
	parent, parentError := strconv.Atoi(fields[1])
	ticks, ticksError := strconv.ParseInt(fields[19], 10, 64)
	if parentError != nil || ticksError != nil {
		return Description{}, fmt.Errorf("process %d's status %q has no parent and start to read", pid, stat)
	}
	booted, bootError := bootTime()
	if bootError != nil {
		return Description{}, bootError
	}
	command, commandError := Command(pid)
	if commandError != nil {
		return Description{}, commandError
	}
	return Description{
		Parent:  parent,
		Started: booted.Add(time.Duration(ticks) * time.Second / userTicksPerSecond),
		Command: command,
	}, nil
}

// bootTime is when the machine booted, the btime line of /proc/stat.
func bootTime() (time.Time, error) {
	stat, readError := os.ReadFile("/proc/stat")
	if readError != nil {
		return time.Time{}, fmt.Errorf("reading the boot time: %w", readError)
	}
	for line := range strings.SplitSeq(string(stat), "\n") {
		if value, found := strings.CutPrefix(line, "btime "); found {
			seconds, parseError := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if parseError != nil {
				return time.Time{}, fmt.Errorf("reading the boot time from %q: %w", line, parseError)
			}
			return time.Unix(seconds, 0), nil
		}
	}
	return time.Time{}, errors.New("reading the boot time: /proc/stat has no btime line")
}
