package go_engine

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// TestUnseenAloneIsAGap gives the types phase a file every package holds, so the engine's own held-check finds
// nothing, and that the load nonetheless names as Unseen. The file must be in filesWithoutRecord, and the run
// incomplete, on the load's word alone.
func TestUnseenAloneIsAGap(test *testing.T) {
	test.Parallel()
	file := "/project/seen.go"
	described := &describedProject{
		inScope:    []string{file},
		inScopeSet: map[string]bool{file: true},
		contents:   map[string][]byte{file: []byte("package seen\n")},
	}
	loaded := &load.Loaded{
		Packages: []*packages.Package{{GoFiles: []string{file}}},
		Unseen:   []string{file},
	}
	var output bytes.Buffer
	engine := &pipeline{
		request:       invocation{switches: map[string]bool{}},
		described:     described,
		writer:        contract.NewWriter(&output),
		standardError: io.Discard,
	}
	engine.write(provenanceRecord())

	engine.runTypes(loaded, nil)

	var types contract.TypesRecord
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, `{"kind":"types"`) {
			if decodeError := json.Unmarshal([]byte(line), &types); decodeError != nil {
				test.Fatalf("the types record does not decode: %v", decodeError)
			}
		}
	}
	if !slices.Equal(types.FilesWithoutRecord, []string{file}) {
		test.Errorf("filesWithoutRecord %q, want the unseen file alone; stream:\n%s", types.FilesWithoutRecord,
			output.String())
	}
	if !engine.incomplete {
		test.Error("a file the load saw nothing of left the run complete")
	}
}

// The type errors the load withheld in a dependency's cgo file are on the types record, file by file, and are no
// finding, and so is the count of the dependencies' declarations the rules were given without what reaches cgo: the
// record is how a run that withheld them reads differently from one with nothing to withhold.
func TestWithheldDependencyCgoErrorsAreOnTheTypesRecord(test *testing.T) {
	test.Parallel()
	withheld := load.Withheld{Package: "net", File: "/goroot/src/net/cgo_unix.go", Errors: 1}
	loaded := &load.Loaded{
		WithheldInDependencyCgo:           []load.Withheld{withheld},
		WithheldDependencyCgoDeclarations: 4,
	}
	var output bytes.Buffer
	engine := &pipeline{
		request:       invocation{switches: map[string]bool{}},
		described:     &describedProject{inScopeSet: map[string]bool{}, contents: map[string][]byte{}},
		writer:        contract.NewWriter(&output),
		standardError: io.Discard,
	}
	engine.write(provenanceRecord())

	engine.runTypes(loaded, nil)

	var types contract.TypesRecord
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, `{"kind":"types"`) {
			if decodeError := json.Unmarshal([]byte(line), &types); decodeError != nil {
				test.Fatalf("the types record does not decode: %v", decodeError)
			}
		}
	}
	want := []contract.WithheldFile{{Package: withheld.Package, File: withheld.File, Errors: withheld.Errors}}
	if !slices.Equal(types.WithheldInDependencyCgo, want) {
		test.Errorf("withheldInDependencyCgo %+v, want %+v; stream:\n%s", types.WithheldInDependencyCgo, want,
			output.String())
	}
	if types.WithheldDependencyCgoDeclarations != 4 {
		test.Errorf("withheldDependencyCgoDeclarations %d, want 4; stream:\n%s",
			types.WithheldDependencyCgoDeclarations, output.String())
	}
	if engine.findingsWritten > 0 || types.Diagnostics > 0 {
		test.Errorf("a withheld error was written as a finding; stream:\n%s", output.String())
	}
}

// A withheld file is written only with the errors it was withheld for: one counted with none is a load bug, and the
// types phase refuses to write it rather than name a file withheld for nothing.
func TestAWithheldDependencyCgoFileWithNoErrorsIsRefused(test *testing.T) {
	test.Parallel()
	loaded := &load.Loaded{
		WithheldInDependencyCgo: []load.Withheld{{Package: "net", File: "/goroot/src/net/cgo_unix.go", Errors: 0}},
	}
	var output bytes.Buffer
	engine := &pipeline{
		request:       invocation{switches: map[string]bool{}},
		described:     &describedProject{inScopeSet: map[string]bool{}, contents: map[string][]byte{}},
		writer:        contract.NewWriter(&output),
		standardError: io.Discard,
	}
	engine.write(provenanceRecord())
	defer func() {
		recovered := recover()
		message, isString := recovered.(string)
		if !isString || !strings.Contains(message, "cgo_unix.go is withheld with 0 type errors") {
			test.Errorf("runTypes recovered %#v, want a panic naming the file withheld with 0 type errors", recovered)
		}
		if strings.Contains(output.String(), `"kind":"types"`) {
			test.Errorf("a types record was written for a file withheld with no errors; stream:\n%s", output.String())
		}
	}()

	engine.runTypes(loaded, nil)
}
