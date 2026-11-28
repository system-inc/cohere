package estree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/json_value"
	"github.com/system-inc/cohere/internal/testing/typescript_fixtures"
)

// parseFromCorpusVariable names more roots for the proof below, separated by the path list separator: a real
// tree, such as ahra, read the way the fixtures are. Unset, the proof reads the repository's fixtures alone.
const parseFromCorpusVariable = "COHERE_PARSE_FROM_CORPUS"

// The formatter takes the fix pass's guard's tree rather than parsing every TypeScript file a second time
// (#dk2502g). This proves the two are one tree, on every TypeScript file of the fixtures, read by the
// directories that hold them (typescript_fixtures), and of any tree the variable names: the guard's tree,
// captured from the transform the formatter really calls, converts to an ESTree equal, property for property
// and comment for comment, to the one a fresh parse converts to, and it is the tree ParseTypeScriptFrom takes
// rather than one it refuses and parses again. The mutant is a tree of other bytes: converted, it must differ,
// so the comparison could have failed, and ParseTypeScriptFrom must refuse it.
func TestTheGuardsTreeIsTheTreeTheFormatterWouldParse(test *testing.T) {
	test.Parallel()
	extensions := []string{".ts", ".tsx", ".mts", ".cts"}
	files := typescript_fixtures.Files(test, extensions...)
	if extra := os.Getenv(parseFromCorpusVariable); extra != "" {
		for _, root := range filepath.SplitList(extra) {
			files = append(files, typescript_fixtures.Under(root, extensions...)...)
		}
	}
	if len(files) < 50 {
		test.Fatalf("the corpus holds %d files, too few for agreement to mean anything", len(files))
	}

	compared, hashbangs, unparseable, mutantsCaught := 0, 0, 0, 0
	for _, fileName := range files {
		contents, readError := os.ReadFile(fileName)
		if readError != nil {
			test.Fatal(readError)
		}
		text := string(contents)
		if strings.HasPrefix(text, "#!") {
			hashbangs++
			continue
		}
		if parses, _ := edit.Parses(fileName, text); !parses {
			// The formatter refuses a file that does not parse before the transform is reached.
			unparseable++
			continue
		}

		var guards *ast.SourceFile
		capture := func(_ string, given string, parsed *ast.SourceFile) (string, error) {
			guards = parsed
			return given, nil
		}
		noProposals := func(string, string) ([]edit.Proposal, error) { return nil, nil }
		if _, fixAndTransformTextError := edit.FixAndTransformText(
			fileName,
			text,
			noProposals,
			capture,
			1,
		); fixAndTransformTextError != nil {
			test.Fatalf("%s: %v", fileName, fixAndTransformTextError)
		}
		if guards == nil {
			test.Errorf("%s: the formatter handed the transform no tree", fileName)
			continue
		}
		if !treeIsFor(fileName, text, guards) {
			test.Errorf("%s: ParseTypeScriptFrom refuses the guard's tree, so the file is still parsed twice", fileName)
			continue
		}

		fresh := convertedDump(test, fileName, ParseSourceFile(fileName, text))
		if taken := convertedDump(test, fileName, guards); taken != fresh {
			test.Errorf("%s: the guard's tree converts to another ESTree than a fresh parse does", fileName)
		}
		compared++

		// The mutant: the tree of the text with one more space in front, every offset one past the file's own.
		mutant := ParseSourceFile(fileName, " "+text)
		if treeIsFor(fileName, text, mutant) {
			test.Errorf("%s: ParseTypeScriptFrom would take a tree of other bytes", fileName)
		}
		if convertedDump(test, fileName, mutant) != fresh {
			mutantsCaught++
		}
	}
	test.Logf(
		"%d TypeScript files compared, %d with a hashbang left to their own parse, %d that do not parse, %d mutants caught",
		compared,
		hashbangs,
		unparseable,
		mutantsCaught,
	)
	if compared < 50 {
		test.Fatalf("only %d files were compared", compared)
	}
	if mutantsCaught != compared {
		test.Errorf(
			"a tree of other bytes converted to the same ESTree on %d of %d files, so the comparison could not fail there",
			compared-mutantsCaught,
			compared,
		)
	}
}

// convertedDump is the ESTree a tree converts to, with its comments, as JSON, postprocessed against the tree's
// own text. A file that fails to convert dumps its error, so two trees that fail alike compare equal and one that
// fails alone does not.
func convertedDump(test *testing.T, fileName string, sourceFile *ast.SourceFile) string {
	test.Helper()
	text := sourceFile.Text()
	program, comments, _, convertTypeScriptError := convertTypeScript(sourceFile, text, nil)
	if convertTypeScriptError != nil {
		return "error: " + convertTypeScriptError.Error()
	}
	offsets := make([]int, len(text)+1)
	for index := range offsets {
		offsets[index] = index
	}
	dumped := make(json_value.Array, len(comments))
	for index, comment := range comments {
		dumped[index] = json_value.Object{
			"type":  jsonString(comment.Type()),
			"range": json_value.Array{jsonInteger(comment.Range[0]), jsonInteger(comment.Range[1])},
			"value": jsonString(comment.String("value")),
		}
	}
	encoded, encodeError := json.Marshal(json_value.Object{"ast": dumpValue(program, offsets), "comments": dumped})
	if encodeError != nil {
		test.Fatalf("%s: %v", fileName, encodeError)
	}
	return string(encoded)
}
