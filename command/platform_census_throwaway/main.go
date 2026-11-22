// Command platform_census_throwaway counts how much a Go tree's programs truly differ by platform (#7p95dnk): it loads
// the tree once for the host and once per extra build, every package type-checked from source, and compares each build
// against the host. It is a throwaway measurement, never to land, and depends on nothing of the engine: it measures
// the programs, not the engine.
//
//	go run ./command/cohere-dev build -o <folder>/platform-census ./command/platform_census_throwaway
//	<folder>/platform-census --root <tree> --host darwin/arm64 \
//	    --build windows/arm64 --build linux/arm64 --build freebsd/arm64 --json <folder>/census.json
//
// A configuration is spelled `goos/goarch[,cgo=0|1][,tags=a+b]`. The host's cgo is go's own default for it unless
// given; an extra build's is off unless given, since a cross platform's cgo needs a C compiler for it. The engine's
// lint record names each build as `windows/arm64`, with ` cgo` when cgo is on and ` tags a,b` after, so the record's
// `linux/arm64 cgo tags purego` is this command's `--build linux/arm64,cgo=1,tags=purego`.
//
// The flags:
//
//	--root <directory>       the tree, holding a go.mod or a go.work; required
//	--host <configuration>   the configuration every build is compared against; required
//	--build <configuration>  an extra build, repeatable; at least one
//	--patterns <patterns>    the packages, space separated, `./...` by default
//	--tests                  load each package's test variants too, on by default (`--tests=false` turns it off)
//	--json <file>            the full detail, written when given
//	--deadline <duration>    stop the run once it has taken this long, 0 for none
//	--stop-above-gigabytes   stop the run once the process holds more than this much memory, 0 for none
//
// The tree is never written. A tree with a go.work is loaded under a side copy of it (sideWorkFile), so a go list that
// rewrites go.work.sum rewrites the copy's; a tree with only a go.mod is loaded with `GOWORK=off`, so no go.work above
// it is read. `-mod=mod`, which can rewrite a go.mod, is never passed.
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

// The exit codes: the census was printed; the load or the json failed; the command line was refused; a bound stopped
// the run.
const (
	exitCounted = 0
	exitFailed  = 1
	exitRefused = 2
	exitStopped = 3
)

// bytesInGigabyte is what --stop-above-gigabytes counts in, and memoryMetric the runtime's whole mapped memory.
const (
	bytesInGigabyte = 1 << 30
	memoryMetric    = "/memory/classes/total:bytes"
)

// commandLine is what the flags asked for.
type commandLine struct {
	root               string
	host               configuration
	builds             []configuration
	patterns           []string
	tests              bool
	jsonFile           string
	deadline           time.Duration
	stopAboveGigabytes float64
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run takes the census the command line asks for, printing the table to standardOutput and progress and what stopped
// it to standardError, and returns the exit code.
func run(arguments []string, standardOutput io.Writer, standardError io.Writer) int {
	asked, parseError := parseCommandLine(arguments, standardError)
	if parseError != nil {
		say(standardError, "platform-census: %v\n", parseError)
		return exitRefused
	}
	stopWatching := watchBounds(asked, standardError)
	defer stopWatching()
	report, censusError := takeCensus(asked, standardError)
	if censusError != nil {
		say(standardError, "platform-census: %v\n", censusError)
		return exitFailed
	}
	if _, writeError := io.WriteString(standardOutput, tableText(report)); writeError != nil {
		say(standardError, "platform-census: printing the table: %v\n", writeError)
		return exitFailed
	}
	if asked.jsonFile != "" {
		encoded, encodeError := json.MarshalIndent(report, "", "  ")
		if encodeError != nil {
			say(standardError, "platform-census: encoding the json: %v\n", encodeError)
			return exitFailed
		}
		if writeError := os.WriteFile(asked.jsonFile, append(encoded, '\n'), 0o644); writeError != nil {
			say(standardError, "platform-census: %v\n", writeError)
			return exitFailed
		}
	}
	return exitCounted
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
	flags := flag.NewFlagSet("platform-census", flag.ContinueOnError)
	flags.SetOutput(standardError)
	flags.StringVar(&asked.root, "root", "", "the tree, holding a go.mod or a go.work")
	hostSpelling := flags.String("host", "", "the host configuration, goos/goarch[,cgo=0|1][,tags=a+b]")
	flags.Var(configurationList{configurations: &asked.builds}, "build", "an extra build, repeatable")
	patterns := flags.String("patterns", "./...", "the packages, space separated")
	flags.BoolVar(&asked.tests, "tests", true, "load test variants too")
	flags.StringVar(&asked.jsonFile, "json", "", "the file the full detail is written to")
	flags.DurationVar(&asked.deadline, "deadline", 0, "stop once the run has taken this long")
	flags.Float64Var(&asked.stopAboveGigabytes, "stop-above-gigabytes", 0, "stop once the process holds more")
	if parseError := flags.Parse(arguments); parseError != nil {
		return asked, parseError
	}
	if flags.NArg() != 0 {
		return asked, fmt.Errorf("unexpected arguments %q", flags.Args())
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
	if len(asked.builds) == 0 {
		return asked, errors.New("at least one --build is required")
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
				say(standardError, "platform-census: stopped at the deadline, %s\n", asked.deadline)
				os.Exit(exitStopped)
			}
			metrics.Read(sample)
			// The runtime's whole mapped memory, heap and stacks and its own, is what the machine gives the process.
			held := float64(sample[0].Value.Uint64()) / bytesInGigabyte
			if asked.stopAboveGigabytes != 0 && held > asked.stopAboveGigabytes {
				say(standardError, "platform-census: stopped holding %.1f GB, over %.1f, after %s\n",
					held, asked.stopAboveGigabytes, time.Since(started).Round(time.Second))
				os.Exit(exitStopped)
			}
		}
	}()
	return func() { close(done) }
}
