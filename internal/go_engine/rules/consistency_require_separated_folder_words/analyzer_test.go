package consistency_require_separated_folder_words_test

import (
	"go/ast"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_separated_folder_words"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_require_separated_folder_words.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-require-separated-folder-words")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// flaggedFixtures are the fixture packages the rule reports, shared by TestAnalyzer and the span test so a fixture
// added to one is held to both.
var flaggedFixtures = []string{
	"flagged/formatoptions",
	"flagged/goengine",
	"flagged/regexsyntax",
	"flagged/cgotest",
	"flagged/imessagebridge",
	"flagged/runoutput",
	"flagged/text_formatoptions",
	"flagged/clause",
	"flagged/jsonlines",
}

// The flagged fixtures carry a // want on the first line of each file the rule reports a folder at, since that
// finding sits at the file's start, and on the clause it reports a package clause at, and nowhere else. formatoptions
// is reported once for its package, at formatoptions.go, though an in-package test comes before it by name and a
// second file after it, and once for its external test package, at that package's first file, all its files being
// tests. jsonlines opens with a generated file and a test, so its finding moves to lines.go, the first file that is
// neither. goengine, regexsyntax and cgotest split on a two-letter word, a software term and the three-letter minimum,
// imessagebridge on a brand that is a software term too, runoutput by the tie rule, and text_formatoptions in one of
// its parts. clause is a word whose package clause differs from it and runs words together. The allowed fixtures are
// words a split would break, a name already separated, names that split no way, and a command whose clause is main.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_require_separated_folder_words.Analyzer,
		append(slices.Clone(flaggedFixtures),
			"allowed/workspace",
			"allowed/charset",
			"allowed/gitignore",
			"allowed/changelog",
			"allowed/lowercase",
			"allowed/format_options",
			"allowed/cachedvfs",
			"allowed/acheck",
			"allowed/withc",
			"allowed/command",
		)...,
	)
}

// Every folder finding is a point at its file's start, Pos and End both, and every package-name finding covers its
// clause from `package` to the end of the name. analysistest's wants match only the line a finding starts on and its
// message, never its column or its end, so the span is held here.
func TestEachFindingSitsWhereItsKindDoes(test *testing.T) {
	test.Parallel()
	results := analysistest.Run(errorsOnly{test}, analysistest.TestData(),
		consistency_require_separated_folder_words.Analyzer, flaggedFixtures...)
	folderId := policy.MessageOf(consistency_require_separated_folder_words.Name, "runTogetherFolder").Id
	packageNameId := policy.MessageOf(consistency_require_separated_folder_words.Name, "runTogetherPackageName").Id
	folderFindings := 0
	packageNameFindings := 0
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			position := result.Pass.Fset.Position(diagnostic.Pos)
			fileIndex := slices.IndexFunc(result.Pass.Files, func(file *ast.File) bool {
				return file.FileStart <= diagnostic.Pos && diagnostic.Pos <= file.FileEnd
			})
			if fileIndex < 0 {
				test.Errorf("%s: the finding is in no file of its package", position)
				continue
			}
			file := result.Pass.Files[fileIndex]
			switch diagnostic.Category {
			case folderId:
				folderFindings++
				if diagnostic.Pos != file.FileStart || diagnostic.End != file.FileStart {
					test.Errorf("%s: the folder finding spans %s to %s, want a point at the file's start", position,
						position, result.Pass.Fset.Position(diagnostic.End))
				}
			case packageNameId:
				packageNameFindings++
				if diagnostic.Pos != file.Package || diagnostic.End != file.Name.End() {
					test.Errorf("%s: the package-name finding spans %s to %s, want the clause %s to %s", position,
						position, result.Pass.Fset.Position(diagnostic.End), result.Pass.Fset.Position(file.Package),
						result.Pass.Fset.Position(file.Name.End()))
				}
			default:
				test.Errorf("%s: a finding of category %q, which is neither of the rule's", position,
					diagnostic.Category)
			}
		}
	}
	if folderFindings == 0 || packageNameFindings == 0 {
		test.Fatalf("the flagged fixtures gave %d folder and %d package-name findings, so a kind's span went unchecked",
			folderFindings, packageNameFindings)
	}
}

// The module fixture is a module named example.com/goengine, so its packages are judged below the module root: the
// root package is clean though the module's own path runs words together, inner is a word, and treebuilder inside it
// is reported.
func TestAnalyzerJudgesBelowTheModuleRoot(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		filepath.Join(analysistest.TestData(), "module"),
		consistency_require_separated_folder_words.Analyzer,
		"example.com/goengine",
		"example.com/goengine/inner",
		"example.com/goengine/inner/treebuilder",
	)
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
