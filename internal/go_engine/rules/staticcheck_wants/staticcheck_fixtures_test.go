package staticcheck_wants_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// The reader over staticcheck's own fixtures, read where the module cache keeps the release go.mod requires. These
// tests sit outside the package, since parity, which finds that directory, imports the package itself.

// staticcheckDiagnosticNotes is how many diag notes the Go files of staticcheck v0.8.1's check fixtures hold outside
// sa5012's, counted apart from this reader: every diag name after a comma, a space or the opening of a line's first
// //@ or /*@. A count is the one way a note the reader silently passes over shows, so it moves only with the pin.
const staticcheckDiagnosticNotes = 1204

// sa5012DiagnosticNotes is how many diag notes sa5012's fixture holds, counted apart from this reader.
const sa5012DiagnosticNotes = 16

// Every note in the fixtures of staticcheck v0.8.1's checks, the ones the house ports, reads, but sa5012's, whose fact
// notes are refused, and the reader finds every diag note there is.
func TestReadReadsStaticchecksFixtures(test *testing.T) {
	test.Parallel()
	fixtures := staticchecksFixtures(test)
	if len(fixtures) == 0 {
		test.Fatal("found no fixture of staticcheck's, so the test proves nothing")
	}
	read := 0
	refused := []string{}
	for _, fixture := range fixtures {
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			if strings.Contains(readError.Error(), "a fact note, where this reader reads diag notes alone") {
				refused = append(refused, fixture)
				continue
			}
			test.Errorf("reading %s: %v", fixture, readError)
			continue
		}
		read += len(wants)
	}
	test.Logf("read %d wants from %d fixtures, and refused %d for their fact notes: %v", read,
		len(fixtures)-len(refused), len(refused), refused)
	if read != staticcheckDiagnosticNotes {
		test.Errorf("read %d wants from staticcheck's fixtures, where they hold %d diag notes outside sa5012", read,
			staticcheckDiagnosticNotes)
	}
	if len(refused) != 1 || filepath.Base(filepath.Dir(refused[0])) != "sa5012" {
		test.Errorf("refused %v for their fact notes, where only sa5012's fixture holds any", refused)
	}
}

// Over every fixture of staticcheck v0.8.1's checks, ReadWithFacts reads every diag note Read reads and sa5012's
// besides, refusing none, and the fact notes, which sa5012's fixture alone holds: its four, one on each function that
// needs its slice of even length.
func TestReadWithFactsReadsStaticchecksFixtures(test *testing.T) {
	test.Parallel()
	fixtures := staticchecksFixtures(test)
	read := 0
	facts := []string{}
	for _, fixture := range fixtures {
		wants, fixtureFacts, readError := staticcheck_wants.ReadWithFacts(fixture)
		if readError != nil {
			test.Errorf("reading %s: %v", fixture, readError)
			continue
		}
		read += len(wants)
		for _, fact := range fixtureFacts {
			// The fact as its note writes it, after the base name of its file and its line.
			written := strings.SplitN(fact.String(), ":", 3)[2]
			facts = append(facts, fmt.Sprintf("%s:%d:%s", filepath.Base(fact.File), fact.Line, written))
		}
	}
	test.Logf("read %d wants and %d facts from %d fixtures", read, len(facts), len(fixtures))
	if expected := staticcheckDiagnosticNotes + sa5012DiagnosticNotes; read != expected {
		test.Errorf("read %d wants from staticcheck's fixtures, where they hold %d diag notes", read, expected)
	}
	expectedFacts := []string{
		`CheckEvenSliceLength.go:5: fact(args, "needs even elements")`,
		`CheckEvenSliceLength.go:11: fact(args, "needs even elements")`,
		`CheckEvenSliceLength.go:17: fact(args, "needs even elements")`,
		`CheckEvenSliceLength.go:21: fact(bleh, "needs even elements")`,
	}
	if !slices.Equal(facts, expectedFacts) {
		test.Errorf("read the facts\n%s\nwhere sa5012's fixture holds\n%s", strings.Join(facts, "\n"),
			strings.Join(expectedFacts, "\n"))
	}
}

// staticchecksFixtures is every testdata folder of staticcheck's checks the house ports, in the module go.mod
// requires.
func staticchecksFixtures(test *testing.T) []string {
	test.Helper()
	module := parity.StaticcheckDirectory(test)
	fixtures := []string{}
	for _, checks := range []string{"staticcheck", "simple", "stylecheck", "quickfix"} {
		found, globError := filepath.Glob(filepath.Join(module, checks, "*", "testdata"))
		if globError != nil {
			test.Fatal(globError)
		}
		fixtures = append(fixtures, found...)
	}
	return fixtures
}
