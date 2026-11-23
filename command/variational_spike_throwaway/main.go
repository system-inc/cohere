// Command variational_spike_throwaway prices a variability-aware Go type checker (#yraxcp9): it checks a tree's own
// packages once for every build with the variational fork of go/types (internal/variational_types/types), proves each
// expression's answer projected to each build equal to a plain go/types check of that build, and times the check
// against plain checks of the same packages. It is a throwaway measurement, never to land.
//
//	go run ./command/cohere-dev build -o <folder>/variational-spike ./command/variational_spike_throwaway
//	<folder>/variational-spike --root <tree> --host darwin/arm64,cgo=1 \
//	    --build linux/arm64,cgo=0 --build windows/arm64,cgo=0 --json <folder>/spike.json
//
// A configuration is spelled as the platform census spells one, `goos/goarch[,cgo=0|1][,tags=a+b]`, and every build
// must share the host's architecture. Bit b of a mask is build b: the host is bit 0, each --build the next in order.
//
// The flags:
//
//	--root <directory>       the tree, holding a go.mod or a go.work; required
//	--host <configuration>   the first build, bit 0; required
//	--build <configuration>  another build, repeatable; at most seven
//	--patterns <patterns>    our packages, space separated, `./...` by default
//	--tests                  test variants; not built yet, so only false is taken
//	--json <file>            every number, written when given
//	--plant-mismatch         change one projected answer of the host before the proof, which must then show red
//	--cpu-profile <file>     after the timed phases, profile the variational check into file and the host's plain
//	                         check into file.host-plain, each run --profile-repeat times
//	--profile-repeat <n>     how many times each profiled check runs, 1 by default
//	--deadline <duration>    stop the run once it has taken this long, 0 for none
//	--stop-above-gigabytes   stop the run once the process holds more than this much memory, 0 for none
//
// The exit code is 0 when every build's projection matches its oracle exactly, 4 when any doesn't or a check reported
// an error, 1 when the run failed, 2 when the command line was refused and 3 when a bound stopped it.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/metrics"
	"strings"
	"time"
)

// The exit codes.
const (
	exitGreen   = 0
	exitFailed  = 1
	exitRefused = 2
	exitStopped = 3
	exitRed     = 4
)

// bytesInGigabyte is what --stop-above-gigabytes counts in, and memoryMetric the runtime's whole mapped memory.
const (
	bytesInGigabyte = 1 << 30
	memoryMetric    = "/memory/classes/total:bytes"
)

// maximumBuilds is how many builds a types.Mask holds.
const maximumBuilds = 8

// commandLine is what the flags asked for.
type commandLine struct {
	root     string
	host     configuration
	builds   []configuration
	patterns []string
	jsonFile string
	plant    bool
	// cpuProfile, when given, is where the profiled repeats of the variational check go, profileRepeat how many.
	cpuProfile    string
	profileRepeat int
	// deadline and stopAboveGigabytes bound the run, zero for no bound.
	deadline           time.Duration
	stopAboveGigabytes float64
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run runs the spike the command line asks for, printing the table to standardOutput and progress to standardError,
// and returns the exit code.
func run(arguments []string, standardOutput io.Writer, standardError io.Writer) int {
	asked, parseError := parseCommandLine(arguments, standardError)
	if parseError != nil {
		say(standardError, "variational-spike: %v\n", parseError)
		return exitRefused
	}
	stopWatching := watchBounds(asked, standardError)
	defer stopWatching()
	report, spikeError := runSpike(asked, standardError)
	if spikeError != nil {
		say(standardError, "variational-spike: %v\n", spikeError)
		return exitFailed
	}
	if _, writeError := io.WriteString(standardOutput, tableText(report)); writeError != nil {
		say(standardError, "variational-spike: printing the table: %v\n", writeError)
		return exitFailed
	}
	if asked.jsonFile != "" {
		encoded, encodeError := json.MarshalIndent(report, "", "  ")
		if encodeError != nil {
			say(standardError, "variational-spike: encoding the json: %v\n", encodeError)
			return exitFailed
		}
		if writeError := os.WriteFile(asked.jsonFile, append(encoded, '\n'), 0o644); writeError != nil {
			say(standardError, "variational-spike: %v\n", writeError)
			return exitFailed
		}
	}
	if !report.green() {
		return exitRed
	}
	return exitGreen
}

// configurationList is the repeatable --build flag.
type configurationList struct {
	configurations *[]configuration
}

func (list configurationList) String() string {
	if list.configurations == nil {
		return ""
	}
	spelled := make([]string, 0, len(*list.configurations))
	for _, each := range *list.configurations {
		spelled = append(spelled, each.target(each.cgo == "1"))
	}
	return strings.Join(spelled, " ")
}

func (list configurationList) Set(value string) error {
	parsed, parseError := parseConfiguration(value)
	if parseError != nil {
		return parseError
	}
	*list.configurations = append(*list.configurations, parsed)
	return nil
}

// parseCommandLine reads the flags, writing flag's own complaints to standardError.
func parseCommandLine(arguments []string, standardError io.Writer) (commandLine, error) {
	asked := commandLine{}
	flags := flag.NewFlagSet("variational-spike", flag.ContinueOnError)
	flags.SetOutput(standardError)
	flags.StringVar(&asked.root, "root", "", "the tree, holding a go.mod or a go.work")
	hostSpelling := flags.String("host", "", "the host configuration, goos/goarch[,cgo=0|1][,tags=a+b]")
	flags.Var(configurationList{configurations: &asked.builds}, "build", "another build, repeatable")
	patterns := flags.String("patterns", "./...", "our packages, space separated")
	tests := flags.Bool("tests", false, "test variants, not built yet")
	flags.StringVar(&asked.jsonFile, "json", "", "the file every number is written to")
	flags.BoolVar(&asked.plant, "plant-mismatch", false, "change one projected answer before the proof")
	flags.StringVar(&asked.cpuProfile, "cpu-profile", "", "profile repeats of each check into this file")
	flags.IntVar(&asked.profileRepeat, "profile-repeat", 1, "how many times each profiled check runs")
	flags.DurationVar(&asked.deadline, "deadline", 0, "stop once the run has taken this long")
	flags.Float64Var(&asked.stopAboveGigabytes, "stop-above-gigabytes", 0, "stop once the process holds more")
	if parseError := flags.Parse(arguments); parseError != nil {
		return asked, parseError
	}
	if flags.NArg() != 0 {
		return asked, fmt.Errorf("unexpected arguments %q", flags.Args())
	}
	if asked.profileRepeat < 1 {
		return asked, errors.New("--profile-repeat must be at least 1")
	}
	if *tests {
		return asked, errors.New("--tests: test variants are not built yet; the spike checks packages only")
	}
	if asked.root == "" {
		return asked, errors.New("--root is required")
	}
	if *hostSpelling == "" {
		return asked, errors.New("--host is required")
	}
	host, hostError := parseConfiguration(*hostSpelling)
	if hostError != nil {
		return asked, hostError
	}
	asked.host = host
	asked.host.isHost = true
	if 1+len(asked.builds) > maximumBuilds {
		return asked, fmt.Errorf("%d builds, but a mask holds at most %d", 1+len(asked.builds), maximumBuilds)
	}
	every := append([]configuration{asked.host}, asked.builds...)
	if _, architectureError := sameArchitecture(every); architectureError != nil {
		return asked, architectureError
	}
	asked.patterns = strings.Fields(*patterns)
	if len(asked.patterns) == 0 {
		return asked, errors.New("--patterns names no package")
	}
	return asked, nil
}

// watchBounds stops the process once it has run past asked's deadline or holds more memory than asked allows, so a
// run too heavy for the machine it is on ends by itself rather than by a signal from outside. It returns the function
// that stops the watch.
func watchBounds(asked commandLine, standardError io.Writer) func() {
	if asked.deadline == 0 && asked.stopAboveGigabytes == 0 {
		return func() { /* nothing is watched. */ }
	}
	done := make(chan struct{})
	started := time.Now()
	go func() {
		ticker := time.NewTicker(time.Second)
		sample := []metrics.Sample{{Name: memoryMetric}}
		for {
			select {
			case <-done:
				ticker.Stop()
				return
			case <-ticker.C:
			}
			// The process ends here, from inside, so the side copy of a go.work stays in the temporary directory.
			if asked.deadline != 0 && time.Since(started) > asked.deadline {
				say(standardError, "variational-spike: stopped at the deadline, %s\n", asked.deadline)
				os.Exit(exitStopped)
			}
			metrics.Read(sample)
			// The runtime's whole mapped memory, heap and stacks and its own, is what the machine gives the process.
			held := float64(sample[0].Value.Uint64()) / bytesInGigabyte
			if asked.stopAboveGigabytes != 0 && held > asked.stopAboveGigabytes {
				say(standardError, "variational-spike: stopped holding %.1f GB, over %.1f, after %s\n",
					held, asked.stopAboveGigabytes, time.Since(started).Round(time.Second))
				os.Exit(exitStopped)
			}
		}
	}()
	return func() { close(done) }
}

// say writes one progress message to progress.
func say(progress io.Writer, format string, arguments ...any) {
	// Ignored: the run is in its table, its json and its exit code, which a closed terminal does not change.
	_, _ = fmt.Fprintf(progress, format, arguments...)
}
