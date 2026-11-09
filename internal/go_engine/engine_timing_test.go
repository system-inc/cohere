package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/timing_testing"
)

// spinningRuleName is the planted rule the timing record must put first.
const spinningRuleName = rule.Namespace + "consistency-spin-on-every-package"

// newSpinningRule is a planted house rule that spends a known amount of its own thread's CPU on every package and
// reports nothing, its own analyzer each time, since a test's rule is no other test's.
func newSpinningRule() rule.Rule {
	return rule.Rule{
		Name:   spinningRuleName,
		Origin: rule.OriginHouse,
		Analyzer: &analysis.Analyzer{
			Name: "spinoneverypackage",
			Doc:  "spinoneverypackage spends a known amount of CPU on every package, so a timed run has a rule to bill.",
			Run: func(*analysis.Pass) (any, error) {
				timing_testing.Spin(20 * time.Millisecond)
				return nil, nil
			},
		},
	}
}

// timedRun is the engine's stream over a fresh copy of the clean fixture, with the planted spinning rule and the
// rule that reports every file registered, under extra, and each record's kind in order.
func timedRun(test *testing.T, extra ...string) (stream string, kinds []string) {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", "clean"))
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root}, extra...)
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	exitCode := run(arguments, &standardOutput, &standardError, []rule.Rule{everyFileRule, newSpinningRule()}, nil)
	stream = standardOutput.String()
	checkStream(test, stream, exitCode)
	for line := range strings.Lines(stream) {
		var record struct {
			Kind string `json:"kind"`
		}
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatalf("decoding %q: %v", line, decodeError)
		}
		kinds = append(kinds, record.Kind)
	}
	return stream, kinds
}

// A run under --timing writes exactly one timing record, after the lint record and before the inputs, whose first rule
// row is the planted rule that spins, and reads no findings cache, whose restored packages would have run nothing. A
// run without the switch writes none.
func TestATimedRunWritesOneTimingRecordBetweenLintAndInputs(test *testing.T) {
	test.Parallel()
	timing_testing.RequireThreadClock(test)
	stream, kinds := timedRun(test, "--timing")
	if count := strings.Count(strings.Join(kinds, " "), string(contract.KindTiming)); count != 1 {
		test.Fatalf("the timed run wrote %d timing records, want 1; kinds %v", count, kinds)
	}
	timingIndex := slices.Index(kinds, string(contract.KindTiming))
	lintIndex := slices.Index(kinds, string(contract.KindLint))
	inputsIndex := slices.Index(kinds, string(contract.KindInputs))
	if lintIndex < 0 || inputsIndex < 0 || timingIndex < lintIndex || timingIndex > inputsIndex {
		test.Fatalf("the timing record is record %d, the lint record %d and the inputs %d, want it between them",
			timingIndex, lintIndex, inputsIndex)
	}
	lines := strings.Split(stream, "\n")
	var record contract.TimingRecord
	if decodeError := json.Unmarshal([]byte(lines[timingIndex]), &record); decodeError != nil {
		test.Fatalf("decoding the timing record %q: %v", lines[timingIndex], decodeError)
	}
	if record.Unavailable != "" || len(record.Rules) == 0 {
		test.Fatalf("the timing record measured nothing: %s", lines[timingIndex])
	}
	if top := record.Rules[0]; top.Name != spinningRuleName || top.Packages == 0 ||
		top.CpuMicroseconds < (20*time.Millisecond).Microseconds() {
		test.Errorf("the first rule row is %+v, want %s, billed at least one package's spin", top, spinningRuleName)
	}
	var lint struct {
		Cache contract.LintCache `json:"cache"`
	}
	if decodeError := json.Unmarshal([]byte(lines[lintIndex]), &lint); decodeError != nil {
		test.Fatalf("decoding the lint record %q: %v", lines[lintIndex], decodeError)
	}
	if lint.Cache.On {
		test.Errorf("the timed run's lint read the findings cache: %s", lines[lintIndex])
	}

	_, untimedKinds := timedRun(test)
	if slices.Contains(untimedKinds, string(contract.KindTiming)) {
		test.Errorf("a run without --timing wrote a timing record; kinds %v", untimedKinds)
	}
}
