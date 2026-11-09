package timing

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// sharedKinds reads every analyzer as a shared one, no rule's.
func sharedKinds(string) (string, contract.SharedKind) {
	return "", contract.SharedAnalyzer
}

// newFixedCollector is a collector whose measuring costs perCall, fixed, where New would learn it from the clock, so
// what a row takes out of it is known.
func newFixedCollector(perCall time.Duration) *Collector {
	return &Collector{
		perCall:   perCall,
		analyzers: costs{},
		listeners: costs{},
		nested:    map[string]time.Duration{},
	}
}

// A nil collector is a run that did not ask: Measure runs the work and bills nothing, every add is a no-op, and the
// record is the zero record.
func TestANilCollectorDoesNothing(test *testing.T) {
	test.Parallel()
	var collector *Collector
	ran := false
	if cpu := collector.Measure(func() { ran = true }); cpu != 0 || !ran {
		test.Fatalf("a nil collector's Measure returned %v and ran the work %t, want 0 and true", cpu, ran)
	}
	collector.Analyzer("inspect", time.Second)
	collector.Listener("cohere-go/a-rule", time.Second, 3)
	collector.Nested("dispatch_syntax", time.Second, 3)
	collector.List(time.Second, time.Second)
	collector.Parse(time.Second, 1)
	collector.TypeCheck(time.Second)
	collector.ExportData(time.Second)
	record := collector.Record(sharedKinds)
	if record.Rules != nil || record.Shared != nil || record.Load != (contract.LoadTiming{}) ||
		record.MeasuredCalls != 0 || record.ProcessCpuMicroseconds != 0 || record.Unavailable != "" {
		test.Fatalf("a nil collector's record is %+v, want the zero record", record)
	}
}

// What a dispatcher's action nested is taken out of its shared row, the clock reads of each nested call with it,
// and a row never goes below 0. Another shared row is untouched.
func TestNestedIsTakenOutOfASharedRow(test *testing.T) {
	test.Parallel()
	collector := newFixedCollector(time.Microsecond)
	collector.Analyzer("dispatch_syntax", 10*time.Millisecond)
	collector.Analyzer("dispatch_syntax", 2*time.Millisecond)
	collector.Nested("dispatch_syntax", 4*time.Millisecond, 1000)
	collector.Analyzer("inspect", 3*time.Millisecond)
	collector.Analyzer("dispatch_ir", time.Millisecond)
	collector.Nested("dispatch_ir", 5*time.Millisecond, 0)
	record := collector.Record(sharedKinds)
	expected := []contract.SharedTiming{
		// 12ms, less the 4ms nested and the 1ms its 1000 calls' reads cost.
		{Name: "dispatch_syntax", Kind: contract.SharedAnalyzer, CpuMicroseconds: 7000, Packages: 2},
		{Name: "inspect", Kind: contract.SharedAnalyzer, CpuMicroseconds: 3000, Packages: 1},
		{Name: "dispatch_ir", Kind: contract.SharedAnalyzer, CpuMicroseconds: 0, Packages: 1},
	}
	if !slices.Equal(record.Shared, expected) {
		test.Fatalf("the shared rows are %+v, want %+v", record.Shared, expected)
	}
}

// Rows are most expensive first, and of two that cost the same, by name, so a table reads the same every run.
func TestRowsSortByCpuThenByName(test *testing.T) {
	test.Parallel()
	collector := newFixedCollector(0)
	for _, name := range []string{"zeta", "alpha", "middle"} {
		collector.Listener(name, 2*time.Millisecond, 1)
		collector.Analyzer(name, 2*time.Millisecond)
	}
	collector.Listener("costly", 9*time.Millisecond, 1)
	collector.Analyzer("costly", 9*time.Millisecond)
	record := collector.Record(func(analyzer string) (string, contract.SharedKind) {
		// Each analyzer is a shared row under its own name, and each listener a rule row.
		return "", contract.SharedDispatcher
	})
	var rules []string
	var shared []string
	for _, row := range record.Rules {
		rules = append(rules, row.Name)
	}
	for _, row := range record.Shared {
		shared = append(shared, row.Name)
	}
	expected := []string{"costly", "alpha", "middle", "zeta"}
	if !slices.Equal(rules, expected) || !slices.Equal(shared, expected) {
		test.Fatalf("the rule rows are %v and the shared rows %v, want each %v", rules, shared, expected)
	}
}

// A rule's own analyzer and its listener are one row under the rule's name: the analyzer's CPU and packages, the
// listener's CPU and calls. An analyzer of no rule stays a shared row of its own.
func TestAnAnalyzerThatIsARulesJoinsItsListenersCallsInOneRow(test *testing.T) {
	test.Parallel()
	collector := newFixedCollector(0)
	collector.Analyzer("consistency_no_label", time.Millisecond)
	collector.Analyzer("consistency_no_label", time.Millisecond)
	collector.Listener("cohere-go/consistency-no-label", 5*time.Millisecond, 40)
	collector.Listener("cohere-go/consistency-no-label", 3*time.Millisecond, 2)
	collector.Analyzer("inspect", 4*time.Millisecond)
	record := collector.Record(func(analyzer string) (string, contract.SharedKind) {
		if analyzer == "consistency_no_label" {
			return "cohere-go/consistency-no-label", ""
		}
		return "", contract.SharedAnalyzer
	})
	expectedRules := []contract.RuleTiming{
		{Name: "cohere-go/consistency-no-label", CpuMicroseconds: 10_000, Packages: 2, Calls: 42},
	}
	expectedShared := []contract.SharedTiming{
		{Name: "inspect", Kind: contract.SharedAnalyzer, CpuMicroseconds: 4000, Packages: 1},
	}
	if !slices.Equal(record.Rules, expectedRules) || !slices.Equal(record.Shared, expectedShared) {
		test.Fatalf("the rows are %+v and %+v, want %+v and %+v", record.Rules, record.Shared, expectedRules,
			expectedShared)
	}
}

// A run that measured no rule and no analyzer writes both lists as `[]`, never `null`.
func TestRulesAndSharedEncodeAsEmptyListsWhenEmpty(test *testing.T) {
	test.Parallel()
	encoded, encodeError := json.Marshal(newFixedCollector(0).Record(sharedKinds))
	if encodeError != nil {
		test.Fatalf("encoding the record: %v", encodeError)
	}
	for _, expected := range []string{`"rules":[]`, `"shared":[]`} {
		if !strings.Contains(string(encoded), expected) {
			test.Errorf("the record %s does not hold %s", encoded, expected)
		}
	}
}
