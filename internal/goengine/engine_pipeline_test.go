package goengine

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/goengine/contract"
	"github.com/system-inc/cohere/internal/goengine/load"
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
