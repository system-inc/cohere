package go_engine

// The engine side of cohere-go/consistency-require-separated-folder-words's mirrors. The rule knows no mirror: a
// folder that keeps another project's name is excused by a //cohere:ignore directly above the package clause the
// finding is at, which only the engine's suppression reads, and a directive there that withholds nothing is the
// engine's own consistency-no-unused-ignore finding. analysistest runs the analyzer alone, so only a run through the
// engine can show either.

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
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_separated_folder_words"
	"github.com/system-inc/cohere/internal/go_engine/suppression"
)

// separatedFolderWordsDirective is the directive a mirror carries, on its own line directly above the clause.
const separatedFolderWordsDirective = "//cohere:ignore " + consistency_require_separated_folder_words.Name +
	" typescript-go's package name\n"

// separatedFolderWordsRun runs the engine's lint phase over the module in directory with the rule alone registered,
// as the engine runs a project with no settings file, every house rule and the suppression rules at error, and returns
// the stream's finding records and its lint record.
func separatedFolderWordsRun(test *testing.T, directory string) ([]contract.FindingRecord, contract.LintRecord) {
	test.Helper()
	registered := []rule.Rule{consistency_require_separated_folder_words.Rule}
	arguments := []string{"--contract", fmt.Sprint(contract.Version), "--root", directory, "--lint"}
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	exitCode := run(arguments, &standardOutput, &standardError, registered)
	if exitCode != 0 && exitCode != 1 {
		test.Fatalf("the engine exited %d, so the run measured nothing; stderr:\n%s", exitCode, standardError.String())
	}
	var findings []contract.FindingRecord
	var lint contract.LintRecord
	linted := false
	for line := range strings.SplitSeq(strings.TrimSuffix(standardOutput.String(), "\n"), "\n") {
		var record struct {
			Kind string `json:"kind"`
		}
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatalf("a line of the stream is not a record: %v: %s", decodeError, line)
		}
		switch record.Kind {
		case "finding":
			var finding contract.FindingRecord
			if decodeError := json.Unmarshal([]byte(line), &finding); decodeError != nil {
				test.Fatalf("a finding record does not decode: %v: %s", decodeError, line)
			}
			findings = append(findings, finding)
		case "lint":
			if decodeError := json.Unmarshal([]byte(line), &lint); decodeError != nil {
				test.Fatalf("the lint record does not decode: %v: %s", decodeError, line)
			}
			linted = true
		}
	}
	if !linted {
		test.Fatalf("the stream has no lint record, so the rule never ran:\n%s", standardOutput.String())
	}
	return findings, lint
}

// A module with four packages: its root, whose folder is the module's own path, example.com/formatoptions, and so is
// nobody's to rename from inside it, clean with no directive, as typescript-go's shims are; tsoptions, a mirror
// carrying the directive, whose finding is withheld and counted; treebuilder, which runs words together and carries
// none, so its finding shows the rule ran; and inner, a word, whose directive withholds nothing and is reported unused
// at the directive's own line.
func TestSeparatedFolderWordsMirrorIsExcusedByTheEngineSuppression(test *testing.T) {
	test.Parallel()
	// Resolved, so the file names the engine reports are the ones written here.
	directory, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	writeTextRulesFile(test, directory, "go.mod", fmt.Appendf(nil, "module example.com/formatoptions\n\ngo %s\n",
		textRulesGoVersion(test)))
	writeTextRulesFile(test, directory, "root.go", []byte("package formatoptions\n\nconst Root = 1\n"))
	writeTextRulesFile(test, directory, "tsoptions/options.go",
		[]byte(separatedFolderWordsDirective+"package tsoptions\n\nconst Options = 1\n"))
	tree := writeTextRulesFile(test, directory, "treebuilder/tree.go",
		[]byte("package treebuilder\n\nconst Tree = 1\n"))
	inner := writeTextRulesFile(test, directory, "inner/inner.go",
		[]byte("// The package's name is a word.\n"+separatedFolderWordsDirective+"package inner\n\nconst Inner = 1\n"))

	findings, lint := separatedFolderWordsRun(test, directory)

	type located struct {
		rule string
		file string
		line int
	}
	reported := []located{}
	for _, finding := range findings {
		reported = append(reported, located{finding.Rule, finding.File, finding.Line})
	}
	want := []located{
		{suppression.RuleNoUnusedIgnore, inner, 2},
		{consistency_require_separated_folder_words.Name, tree, 1},
	}
	if !slices.Equal(reported, want) {
		test.Errorf("the engine reported %v, want %v: the module root clean, the mirror withheld, the run-together "+
			"folder reported at its clause, and the directive on a word reported unused", reported, want)
	}
	if withheld := lint.WithheldBySuppression[consistency_require_separated_folder_words.Name]; withheld != 1 {
		test.Errorf("the lint record counts %d findings withheld by suppression, want the mirror's 1", withheld)
	}
}
