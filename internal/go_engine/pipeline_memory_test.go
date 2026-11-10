package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// memoryRun is the engine's stream over a fresh copy of the clean fixture under memory, and the one memory record in
// it, which must sit just before the inputs (#5shepvt).
func memoryRun(test *testing.T, memory *engineMemory) (line string, record contract.MemoryRecord) {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", "clean"))
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	exitCode := run([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-fix", "--no-cache"},
		&standardOutput, &standardError, []rule.Rule{everyFileRule}, memory)
	stream := standardOutput.String()
	checkStream(test, stream, exitCode)
	lines := strings.Split(strings.TrimSuffix(stream, "\n"), "\n")
	kinds := make([]contract.Kind, 0, len(lines))
	for _, each := range lines {
		var envelope struct {
			Kind contract.Kind `json:"kind"`
		}
		if decodeError := json.Unmarshal([]byte(each), &envelope); decodeError != nil {
			test.Fatalf("decoding %q: %v", each, decodeError)
		}
		kinds = append(kinds, envelope.Kind)
	}
	memoryIndex := slices.Index(kinds, contract.KindMemory)
	if memoryIndex < 0 || slices.Index(kinds[memoryIndex+1:], contract.KindMemory) >= 0 {
		test.Fatalf("the run wrote %d memory records, want 1; kinds %v", strings.Count(stream, `"kind":"memory"`),
			kinds)
	}
	if inputsIndex := slices.Index(kinds, contract.KindInputs); inputsIndex != memoryIndex+1 {
		test.Fatalf("the memory record is record %d and the inputs %d, want it just before them; kinds %v",
			memoryIndex, inputsIndex, kinds)
	}
	line = lines[memoryIndex]
	if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
		test.Fatalf("decoding the memory record %q: %v", line, decodeError)
	}
	return line, record
}

// A run whose loads raised the limit writes each limit a raise set, in order, as the limit setter saw them, in one
// memory record just before the inputs.
func TestARunThatRaisedWritesItsRaisesBeforeTheInputs(test *testing.T) {
	test.Parallel()
	watch := &memoryWatch{current: engineMemoryCap}

	_, record := memoryRun(test, capMemoryWatched(watch, watch.collecting))

	var raised []int64
	for _, limit := range watch.setLimits() {
		if limit > engineMemoryCap {
			raised = append(raised, limit)
		}
	}
	if len(raised) == 0 {
		test.Fatalf("the run set the limit to %v and never raised it, so the record is read against nothing",
			watch.setLimits())
	}
	if !slices.Equal(record.RaisedToBytes, raised) {
		test.Errorf("the memory record says the run raised to %v, and the limit setter saw %v", record.RaisedToBytes,
			raised)
	}
}

// A run that raised nothing writes the record all the same, with an empty list, never null.
func TestARunThatRaisedNothingWritesAnEmptyList(test *testing.T) {
	test.Parallel()

	line, _ := memoryRun(test, nil)

	if want := `{"kind":"memory","raisedToBytes":[]}`; line != want {
		test.Errorf("a run that raised nothing wrote %s, want %s", line, want)
	}
}
