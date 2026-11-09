package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/timing_testing"
)

// timingMarginChildVariable marks a test binary the margin test started as a timed engine run of its own, which
// TestMain runs as the engine over its command line rather than as the tests, so the process's CPU is that one run's
// and no parallel test's.
const timingMarginChildVariable = "COHERE_TEST_TIMING_MARGIN_CHILD"

// marginSpin is the planted rule's known CPU, enough that the work the rows measure dominates the run.
const marginSpin = 400 * time.Millisecond

// marginSpinCeiling bounds the planted rule's row from above at half again its spin. The row is the rule's Run on a
// thread held for it, and Run does nothing but spin, so it bills the spin and a few clock reads; a row billed twice
// holds twice the spin and is caught here even where the rows' sum, which moves with what the runtime's own threads
// cost on a busy machine, would still fit under the process's CPU.
const marginSpinCeiling = marginSpin * 3 / 2

// runTimingMarginChild is the child's whole run: the engine over arguments, under --timing, with only the rule that
// spins marginSpin once registered.
func runTimingMarginChild(arguments []string, standardOutput io.Writer, standardError io.Writer) int {
	var spun atomic.Bool
	spinning := rule.Rule{
		Name:   spinningRuleName,
		Origin: rule.OriginHouse,
		Analyzer: &analysis.Analyzer{
			Name: "spinonce",
			Doc:  "spinonce spends a known amount of CPU on the first package it runs on, so the run has work to bill.",
			Run: func(*analysis.Pass) (any, error) {
				if spun.CompareAndSwap(false, true) {
					timing_testing.Spin(marginSpin)
				}
				return nil, nil
			},
		},
	}
	return run(arguments, standardOutput, standardError, []rule.Rule{spinning}, nil)
}

// The timing table bills a planted rule's known CPU to that rule, once, and its rows never sum past the process's
// CPU: the planted rule is the first row, billed at least its spin and less than half again more, and the rows,
// every rule, every shared analyzer, the load's phases this process ran and the measuring's own cost, are no more
// than the process's user and system CPU. The run is a child process, this test binary started as the engine
// (TestMain), so the process's CPU is that run's alone, where in this process every parallel test would add to it.
// The go commands the listing ran are its children's CPU, never this process's, so their row is left out of the sum.
//
// What share of the process the rows cover is logged, never asserted: what no row measures (start, the census, the
// fix phase, the runtime's collector on its own threads) grows with the machine's load, so a floor on the share is a
// performance number, and land's gate refused one at 78.5% on a busy machine where an idle one read 84 to 88%.
func TestTheTimingTableBillsAPlantedRuleOnceWithinTheRunsCpu(test *testing.T) {
	test.Parallel()
	timing_testing.RequireThreadClock(test)
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatalf("finding the test binary: %v", executableError)
	}
	root := copyFixture(test, filepath.Join("testdata", "clean"))
	command := exec.Command(executable, "--contract", fmt.Sprint(contract.Version), "--root", root, "--timing")
	command.Env = append(os.Environ(), timingMarginChildVariable+"=1")
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	command.Stdout, command.Stderr = &standardOutput, &standardError
	// Ignored: the planted rule reports nothing, so the run exits 0, and any other exit leaves no timing record,
	// which fails below with the child's stderr.
	_ = command.Run()
	var record *contract.TimingRecord
	for line := range strings.Lines(standardOutput.String()) {
		var kind struct {
			Kind string `json:"kind"`
		}
		if decodeError := json.Unmarshal([]byte(line), &kind); decodeError != nil || kind.Kind != "timing" {
			continue
		}
		record = &contract.TimingRecord{}
		if decodeError := json.Unmarshal([]byte(line), record); decodeError != nil {
			test.Fatalf("decoding the timing record %q: %v", line, decodeError)
		}
	}
	if record == nil || record.Unavailable != "" || len(record.Rules) == 0 {
		test.Fatalf("the child wrote no measured timing record; stdout:\n%s\nstderr:\n%s", standardOutput.String(),
			standardError.String())
	}

	rows := record.MeasuringCpuMicroseconds + record.Load.ListCpuMicroseconds + record.Load.ParseCpuMicroseconds +
		record.Load.TypeCheckCpuMicroseconds + record.Load.ExportDataCpuMicroseconds
	for _, ruleRow := range record.Rules {
		rows += ruleRow.CpuMicroseconds
	}
	for _, sharedRow := range record.Shared {
		rows += sharedRow.CpuMicroseconds
	}
	process := record.ProcessCpuMicroseconds
	test.Logf("the rows hold %dµs of the process's %dµs, %.1f%%", rows, process, 100*float64(rows)/float64(process))
	if rows > process {
		test.Errorf("the rows hold %dµs, more than the process's %dµs, so something was billed twice", rows, process)
	}
	// Less a millisecond for the calibrated reads taken out and what the record rounds away.
	if top := record.Rules[0]; top.Name != spinningRuleName ||
		top.CpuMicroseconds < (marginSpin-time.Millisecond).Microseconds() ||
		top.CpuMicroseconds >= marginSpinCeiling.Microseconds() {
		test.Errorf("the first rule row is %+v, want %s, billed its %v once, under %v", top, spinningRuleName,
			marginSpin, marginSpinCeiling)
	}
}
