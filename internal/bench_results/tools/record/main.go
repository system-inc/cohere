// Command record writes one bench/quiet_machine.sh run to bench/results as a validated record
// (internal/bench_results). quiet_machine.sh --record runs it after the copy's tree is checked, from the
// module root:
//
//	go run ./internal/bench_results/tools/record -runs <runs.tsv> -binary <cohere> -project <name> \
//	  -project-commit <sha> -idle-floor <percent> -settle <seconds> -rounds <n> -modes <Cold,Warm,...> \
//	  -band <percent> [-stopped-early] [-edit <path>] [-project-kind GoModule] [-no-fix] [-every-build] \
//	  -gogc <value> -gomemlimit <value> -gomaxprocs <value>
//
// The Go settings are what the benchmark saw in its environment, each empty when unset, and -project-kind
// names the kind of the one project every run checked alone, absent for a run of the whole root. -no-fix says the
// cold and idle runs checked without fixing, and -every-build that every run asked to check every build the Go files
// name. The builds the cold and idle runs checked are read from the runs table.
//
// cohere's commit and whether its tree was modified come from the binary's own build info, not from
// its --version prose, so a binary that names no commit is refused rather than recorded as clean.
package main

import (
	"bufio"
	"debug/buildinfo"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/bench_results"
)

func main() {
	runsPath := flag.String("runs", "", "the run's runs.tsv")
	binary := flag.String("binary", "", "the pinned cohere binary every run used")
	project := flag.String("project", "", "the project's name")
	projectCommit := flag.String("project-commit", "", "the commit the copy was extracted at")
	idleFloor := flag.Float64("idle-floor", 0, "the share of all cores, in percent, idle before and after a quiet run")
	settle := flag.Float64("settle", 0, "the settle seconds")
	rounds := flag.Int("rounds", 0, "the rounds taken")
	modes := flag.String("modes", "", "the measured modes, comma-separated: Cold, Warm, Edit, Idle")
	band := flag.Float64("band", 0, "the early stop's agreement band, in percent")
	stoppedEarly := flag.Bool("stopped-early", false, "the run stopped early because its quiet runs agreed")
	edit := flag.String("edit", "", "the file the Edit runs changed, when Edit was measured")
	projectKind := flag.String("project-kind", "", "the kind of the one project every run checked alone, if one")
	noFix := flag.Bool("no-fix", false, "the cold and idle runs checked without fixing")
	everyBuild := flag.Bool("every-build", false, "every run asked to check every build the Go files name")
	goEnvironment := bench_results.GoEnvironment{}
	flag.StringVar(&goEnvironment.Gogc, "gogc", "", "GOGC in the benchmark's environment, empty when unset")
	flag.StringVar(&goEnvironment.Gomemlimit, "gomemlimit", "", "GOMEMLIMIT in the environment, empty when unset")
	flag.StringVar(&goEnvironment.Gomaxprocs, "gomaxprocs", "", "GOMAXPROCS in the environment, empty when unset")
	out := flag.String("out", bench_results.Directory, "the directory to write the record to")
	flag.Parse()

	measured := []bench_results.Mode{}
	for _, mode := range strings.Split(*modes, ",") {
		measured = append(measured, bench_results.Mode(mode))
	}
	path, recordError := record(
		*runsPath,
		*binary,
		*project,
		*projectCommit,
		*idleFloor,
		*settle,
		*rounds,
		measured,
		*band,
		*stoppedEarly,
		*edit,
		bench_results.ProjectKind(*projectKind),
		*noFix,
		*everyBuild,
		&goEnvironment,
		*out,
	)
	if recordError != nil {
		// Ignored: the exit code below says the run failed even when standard error cannot.
		fmt.Fprintf(os.Stderr, "record: %v\n", recordError)
		os.Exit(1)
	}
	// Ignored: the record is already on disk, and this line only names where.
	fmt.Printf("recorded %s\n", path)
}

func record(
	runsPath, binary, project, projectCommit string,
	idleFloor, settle float64,
	rounds int,
	measured []bench_results.Mode,
	band float64,
	stoppedEarly bool,
	edit string,
	projectKind bench_results.ProjectKind,
	noFix bool,
	everyBuild bool,
	goEnvironment *bench_results.GoEnvironment,
	out string,
) (string, error) {
	cohere, binaryProvenanceError := binaryProvenance(binary)
	if binaryProvenanceError != nil {
		return "", binaryProvenanceError
	}
	machine, thisMachineError := thisMachine()
	if thisMachineError != nil {
		return "", thisMachineError
	}
	runs, builds, readRunsError := readRuns(runsPath)
	if readRunsError != nil {
		return "", readRunsError
	}
	result := bench_results.Record{
		Schema:        bench_results.SchemaVersion,
		RecordedAt:    time.Now().UTC().Format(time.RFC3339),
		Cohere:        cohere,
		Project:       bench_results.Project{Name: project, Commit: projectCommit},
		Machine:       machine,
		IdleFloor:     idleFloor,
		SettleSeconds: settle,
		Rounds:        rounds,
		MeasuredModes: measured,
		Band:          band,
		StoppedEarly:  stoppedEarly,
		Edit:          edit,
		ProjectKind:   projectKind,
		NoFix:         noFix,
		EveryBuild:    everyBuild,
		Builds:        builds,
		GoEnvironment: goEnvironment,
		Runs:          runs,
		Modes:         bench_results.Summarize(measured, runs),
	}
	encoded, encodeError := bench_results.Encode(result)
	if encodeError != nil {
		return "", encodeError
	}
	if createDirectoryError := os.MkdirAll(out, 0o755); createDirectoryError != nil {
		return "", createDirectoryError
	}
	path := filepath.Join(out, bench_results.FileName(result))
	if _, statError := os.Stat(path); statError == nil {
		return "", fmt.Errorf(
			"%s exists; a second run of this commit on this machine today needs a record of its own",
			path,
		)
	}
	return path, os.WriteFile(path, encoded, 0o644)
}

// binaryProvenance is cohere's commit, its first --version line and whether it was built from a modified tree, as
// cohere states them (internal/release/packaging's Provenance). The launcher builds from a committed snapshot with
// VCS stamping off and stamps the commit itself, so the commit comes from the `commit:` line, lengthened to the full
// commit by this repository, and the tree was modified when the `source:` line is printed or the binary's own
// vcs.modified stamp says so.
func binaryProvenance(binary string) (bench_results.Cohere, error) {
	output, outputError := exec.Command(binary, "--version").Output()
	if outputError != nil {
		return bench_results.Cohere{}, fmt.Errorf("%s --version: %w", binary, outputError)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	cohere := bench_results.Cohere{Version: strings.TrimSpace(lines[0])}
	short := ""
	for _, line := range lines[1:] {
		key, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found {
			continue
		}
		switch key {
		case "commit":
			short = strings.TrimSpace(value)
		case "source":
			cohere.Dirty = true
		}
	}
	if short == "" {
		return cohere, fmt.Errorf("%s names no commit, so nothing reproduces its numbers", binary)
	}
	full, outputError := exec.Command("git", "rev-parse", "--verify", "--quiet", short+"^{commit}").Output()
	if outputError != nil {
		return cohere, fmt.Errorf("%s was built from %s, which this repository does not have", binary, short)
	}
	cohere.Commit = strings.TrimSpace(string(full))
	if info, readFileError := buildinfo.ReadFile(binary); readFileError == nil {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.modified" && setting.Value == "true" {
				cohere.Dirty = true
			}
		}
	}
	return cohere, nil
}

// thisMachine is the hardware, read from sysctl and sw_vers. The host's name is never read.
func thisMachine() (bench_results.Machine, error) {
	read := func(name string, arguments ...string) (string, error) {
		output, outputError := exec.Command(name, arguments...).Output()
		if outputError != nil {
			return "", fmt.Errorf("%s %s: %w", name, strings.Join(arguments, " "), outputError)
		}
		return strings.TrimSpace(string(output)), nil
	}
	machine := bench_results.Machine{}
	var readError error
	if machine.Model, readError = read("sysctl", "-n", "hw.model"); readError != nil {
		return machine, readError
	}
	if machine.Processor, readError = read("sysctl", "-n", "machdep.cpu.brand_string"); readError != nil {
		return machine, readError
	}
	cores, readError := read("sysctl", "-n", "hw.ncpu")
	if readError != nil {
		return machine, readError
	}
	var parseError error
	if machine.Cores, parseError = strconv.Atoi(cores); parseError != nil {
		return machine, parseError
	}
	memory, readError := read("sysctl", "-n", "hw.memsize")
	if readError != nil {
		return machine, readError
	}
	if machine.MemoryBytes, parseError = strconv.ParseInt(memory, 10, 64); parseError != nil {
		return machine, parseError
	}
	version, readError := read("sw_vers", "-productVersion")
	if readError != nil {
		return machine, readError
	}
	machine.Os = "macOS " + version
	return machine, nil
}

// readRuns reads quiet_machine.sh's runs table: mode, round, verdict_s, settled_s, summary_s, findings,
// cache, load_before, load_after, idle_before, idle_after, quiet, exit, primes, builds, tab-separated, with a header
// line the script prints apart. It also gives the builds the cold and idle runs checked, which must be the same in
// each: a record names one set of builds for the number Guard compares.
func readRuns(path string) ([]bench_results.Run, []string, error) {
	file, openError := os.Open(path)
	if openError != nil {
		return nil, nil, openError
	}
	defer file.Close() // Ignored: the file was only read, so a failed close loses nothing.
	modes := map[string]bench_results.Mode{
		"cold": bench_results.ModeCold,
		"warm": bench_results.ModeWarm,
		"edit": bench_results.ModeEdit,
		"idle": bench_results.ModeIdle,
	}
	runs := []bench_results.Run{}
	var builds []string
	buildsRead := false
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 15 {
			return nil, nil, fmt.Errorf("%s:%d: %d fields, want 15", path, line, len(fields))
		}
		mode, found := modes[fields[0]]
		if !found {
			return nil, nil, fmt.Errorf("%s:%d: no mode %q", path, line, fields[0])
		}
		if fields[11] != "quiet" && fields[11] != "loaded" {
			return nil, nil, fmt.Errorf("%s:%d: quiet is %q, want quiet or loaded", path, line, fields[11])
		}
		numbers := map[int]float64{}
		for _, index := range []int{1, 2, 3, 4, 5, 7, 8, 9, 10, 12, 13} {
			var parseError error
			if numbers[index], parseError = strconv.ParseFloat(fields[index], 64); parseError != nil {
				return nil, nil, fmt.Errorf("%s:%d: field %d: %w", path, line, index+1, parseError)
			}
		}
		if mode == bench_results.ModeCold || mode == bench_results.ModeIdle {
			var checked []string
			if fields[14] != "" {
				checked = strings.Split(fields[14], bench_results.BuildsSeparator)
			}
			if buildsRead && !slices.Equal(checked, builds) {
				return nil, nil, fmt.Errorf("%s:%d: the run checked the builds %q, and an earlier one %q", path, line,
					checked, builds)
			}
			builds, buildsRead = checked, true
		}
		runs = append(runs, bench_results.Run{
			Mode:           mode,
			Round:          int(numbers[1]),
			VerdictSeconds: numbers[2],
			SettledSeconds: numbers[3],
			SummarySeconds: numbers[4],
			LoadBefore:     numbers[7],
			LoadAfter:      numbers[8],
			IdleBefore:     numbers[9],
			IdleAfter:      numbers[10],
			Quiet:          fields[11] == "quiet",
			Findings:       int(numbers[5]),
			Cache:          fields[6],
			Exit:           int(numbers[12]),
			Primes:         int(numbers[13]),
		})
	}
	return runs, builds, scanner.Err()
}
