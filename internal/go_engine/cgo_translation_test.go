package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cgo_testing"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// typesRecordOf runs the engine over a copy of fixture, in process and under `--no-cache`, so the load is the
// fixture's whole, and returns its types record as written.
func typesRecordOf(test *testing.T, fixture string) contract.TypesRecord {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", fixture))
	arguments := []string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-fix", "--no-cache"}
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	if exitCode := run(arguments, &standardOutput, &standardError, rules.All()); exitCode > exitFindings {
		test.Fatalf("exit %d; stderr:\n%s", exitCode, standardError.String())
	}
	for line := range strings.Lines(standardOutput.String()) {
		if !strings.HasPrefix(line, `{"kind":"types"`) {
			continue
		}
		var record contract.TypesRecord
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatalf("reading the types record %s: %v", line, decodeError)
		}
		if !strings.Contains(line, `"cgoTranslation":{"packages":`) {
			test.Fatalf("want cgoTranslation written, got %s", line)
		}
		return record
	}
	test.Fatalf("no types record in\n%s", standardOutput.String())
	return contract.TypesRecord{}
}

// The load from source runs no cgo tool, so its types record says so as 0 packages in 0 milliseconds, written
// rather than left out.
func TestTheSourceLoadCountsNoCgoTranslation(test *testing.T) {
	test.Parallel()

	record := typesRecordOf(test, "vet-finding")
	if record.CgoTranslation != (contract.CgoTranslation{}) {
		test.Fatalf("want 0 packages in 0 milliseconds, got %+v", record.CgoTranslation)
	}
}

// A project with cgo of its own goes through go/packages, whose cgo tool translates its package and every
// dependency with cgo files, so its types record counts at least the project's package and the time that load took.
func TestTheCgoLoadCountsItsTranslation(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)

	record := typesRecordOf(test, "cgo")
	if record.CgoTranslation.Packages < 1 || record.CgoTranslation.Milliseconds < 1 {
		test.Fatalf("want the project's package and the load's time counted, got %+v", record.CgoTranslation)
	}
}
