package checker

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/timing"
	"github.com/system-inc/cohere/internal/go_engine/timing_testing"
)

// timedKey stands in for the engine's key, rule.Timed, an analyzer that is never run.
var timedKey = &analysis.Analyzer{
	Name: "timed_key",
	Doc:  "keys the run's collector and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("the key ran on %s", pass.Pkg.Path())
	},
}

// timedPackages is how many packages the timing tests analyze, each its own action.
const timedPackages = 4

// timedModule is timedPackages packages, each a root.
func timedModule(test *testing.T) []*packages.Package {
	test.Helper()
	files := map[string]string{}
	for index := range timedPackages {
		name := fmt.Sprintf("timed%d", index)
		files[name+"/"+name+".go"] = packageOf(name, 0)
	}
	return writeModule(test, "timed", files, "./...")
}

// sharedRows is each analyzer's shared row in collector's record, by name, every analyzer read as no rule's.
func sharedRows(collector *timing.Collector) map[string]contract.SharedTiming {
	rows := map[string]contract.SharedTiming{}
	for _, row := range collector.Record(func(string) (string, contract.SharedKind) {
		return "", contract.SharedAnalyzer
	}).Shared {
		rows[row.Name] = row
	}
	return rows
}

// Under Options.Timing an analyzer's run is billed its own thread's CPU, never its wall time: one that spins a known
// amount of CPU on each package, read by its own thread's clock inside its run, is billed at least that, less the
// calibrated reads each measurement takes out and the microseconds the record rounds away; one that sleeps on each
// package is billed a small part of its sleep, where a wall clock would bill all of it. Every pass holds the
// collector under Options.Timed.
func TestAnAnalyzersRunIsBilledItsOwnCpuNotItsWaiting(test *testing.T) {
	test.Parallel()
	timing_testing.RequireThreadClock(test)
	const spinPerPackage = 5 * time.Millisecond
	const sleepPerPackage = 20 * time.Millisecond
	collector := timing.New()
	var spun atomic.Int64
	var slept atomic.Int64
	var sawCollector sync.Map
	spinner := &analysis.Analyzer{
		Name: "spinner",
		Doc:  "spinner spends a known amount of its own thread's CPU on each package",
		Run: func(pass *analysis.Pass) (any, error) {
			held, _ := pass.ResultOf[timedKey].(*timing.Collector)
			sawCollector.Store(pass.Pkg.Path(), held == collector)
			spun.Add(int64(timing_testing.Spin(spinPerPackage)))
			return nil, nil
		},
	}
	sleeper := &analysis.Analyzer{
		Name: "sleeper",
		Doc:  "sleeper waits on each package and spends almost nothing",
		Run: func(*analysis.Pass) (any, error) {
			started := time.Now()
			time.Sleep(sleepPerPackage)
			slept.Add(int64(time.Since(started)))
			return nil, nil
		},
	}
	graph, analyzeError := Analyze([]*analysis.Analyzer{spinner, sleeper}, timedModule(test),
		&Options{Timing: collector, Timed: timedKey})
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	for action := range graph.All() {
		if action.Err != nil {
			test.Fatalf("%s failed: %v", action, action.Err)
		}
	}
	seen := 0
	sawCollector.Range(func(path any, held any) bool {
		seen++
		if held != true {
			test.Errorf("the pass on %v did not hold the run's collector under Timed", path)
		}
		return true
	})
	if seen != timedPackages {
		test.Errorf("the spinner ran on %d packages, want %d", seen, timedPackages)
	}

	rows := sharedRows(collector)
	spinnerRow, sleeperRow := rows["spinner"], rows["sleeper"]
	if spinnerRow.Packages != timedPackages || sleeperRow.Packages != timedPackages {
		test.Fatalf("the rows are %+v and %+v, want each on %d packages", spinnerRow, sleeperRow, timedPackages)
	}
	spunSelf := time.Duration(spun.Load())
	billed := time.Duration(spinnerRow.CpuMicroseconds) * time.Microsecond
	if allowance := timedPackages*time.Microsecond + time.Microsecond; billed < spunSelf-allowance {
		test.Errorf("an analyzer that measured %v of its own CPU was billed %v", spunSelf, billed)
	}
	sleptWall := time.Duration(slept.Load())
	if sleeperBilled := time.Duration(sleeperRow.CpuMicroseconds) * time.Microsecond; sleeperBilled > sleptWall/20 {
		test.Errorf("an analyzer that only sleeps was billed %v of CPU, having slept %v", sleeperBilled, sleptWall)
	}
	test.Logf("spinner billed %v against %v it measured itself; sleeper billed %dµs after %v asleep", billed,
		spunSelf, sleeperRow.CpuMicroseconds, sleptWall)
}

// An action on a restored package runs nothing, so nothing is billed for it; and without Options.Timing nothing is
// measured, and no pass holds anything under Timed.
func TestARestoredActionAndAnUntimedRunAreNotMeasured(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	collector := timing.New()
	var held atomic.Int64
	counted := &analysis.Analyzer{
		Name: "counted",
		Doc:  "counted records whether its pass holds anything under the timed key",
		Run: func(pass *analysis.Pass) (any, error) {
			if _, present := pass.ResultOf[timedKey]; present {
				held.Add(1)
			}
			return nil, nil
		},
	}
	_, restoredError := Analyze([]*analysis.Analyzer{counted}, roots, &Options{
		Timing:   collector,
		Timed:    timedKey,
		Restored: func(*packages.Package) bool { return true },
		Restore:  func(*Action) error { return nil },
	})
	if restoredError != nil {
		test.Fatalf("analyzing restored packages: %v", restoredError)
	}
	if rows := sharedRows(collector); len(rows) != 0 {
		test.Errorf("restored actions were billed %+v", rows)
	}
	_, untimedError := Analyze([]*analysis.Analyzer{counted}, roots, &Options{Timed: timedKey})
	if untimedError != nil {
		test.Fatalf("analyzing without timing: %v", untimedError)
	}
	if held.Load() != 0 {
		test.Errorf("%d passes held something under Timed with no collector", held.Load())
	}
}
