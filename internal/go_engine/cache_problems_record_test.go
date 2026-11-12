package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// A store that refuses every write leaves its problems in the lint record, not on stderr alone (#27r8r24): a run whose
// store's directory cannot be made counts one problem of the findings cache or more, exactly as many as it notes on
// stderr, so a run read through `--json` still shows its cache was not written, and counts every write it refused, of
// which stderr notes only the first: one for each package that missed and was not unstorable, two at the least.
func TestAStoreThatRefusesAWriteCountsItsProblems(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "vet-finding"))
	// A file where the store's directory goes, so no directory can be made under it and every write fails, as the
	// store's own TestAPutThatCannotWriteIsAnError makes one.
	if writeError := os.WriteFile(filepath.Join(root, ".cache"), nil, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	arguments := []string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-fix"}
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	if exitCode := runOverlapping(arguments, nil, &standardOutput, &standardError, rules.All(), nil,
		embeddedStandardLibrary, true, nil); exitCode != exitClean && exitCode != exitFindings {
		test.Fatalf("exit %d, want %d or %d; stderr:\n%s", exitCode, exitClean, exitFindings, standardError.String())
	}
	var record contract.LintRecord
	found := false
	for line := range strings.Lines(standardOutput.String()) {
		if strings.HasPrefix(line, `{"kind":"lint"`) {
			if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
				test.Fatalf("reading the lint record %s: %v", line, decodeError)
			}
			found = true
		}
	}
	if !found {
		test.Fatalf("no lint record in\n%s", standardOutput.String())
	}
	noted := strings.Count(standardError.String(), "the findings cache: ")
	if noted == 0 {
		test.Fatalf("want the refused writes noted on stderr, got:\n%s", standardError.String())
	}
	if record.Cache.Problems != noted {
		test.Errorf("the lint record counts %d problems of the findings cache, want the %d noted on stderr:\n%s",
			record.Cache.Problems, noted, standardError.String())
	}
	refused := record.Cache.Missed - record.Cache.Unstorable
	if refused < 2 {
		test.Fatalf("want at least two writes refused, so the count tells every one from the first, got %+v",
			record.Cache)
	}
	if record.Cache.WriteFailed != refused || record.Cache.Written != 0 {
		test.Errorf("the lint record counts %d writes failed and %d written, want all %d refused",
			record.Cache.WriteFailed, record.Cache.Written, refused)
	}
}
