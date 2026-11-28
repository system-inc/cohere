package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/system-inc/cohere/internal/testing/concurrency_tests"
)

// raceGatedFile is the file holding the land gate's -race list, raceGated, from this package's directory.
var raceGatedFile = filepath.Join("..", "..", "..", "command", "cohere-dev", "race_gated.go")

// Every package holding a test whose name or file says it runs code at once is on the -race list in
// command/cohere-dev's race_gated.go, so a new concurrency test is caught by the race detector at the gate from its
// first landing (#bnabymg). It walks the whole module, so it lives here, with the guards that read the whole tree by
// design, and command/cohere-dev reruns only for what it reads (#0zdyk51). The list is read from its file, parsed, so
// this package needs nothing of the command.
func TestEveryConcurrencyTestIsRaceGated(test *testing.T) {
	test.Parallel()
	gated := raceGatedDirectories(test)
	if len(gated) == 0 {
		test.Fatalf("%s's raceGated names no directory, so the module was checked against nothing", raceGatedFile)
	}
	found, walkError := concurrency_tests.Directories(filepath.Join("..", "..", ".."))
	if walkError != nil {
		test.Fatal(walkError)
	}
	if len(found) == 0 {
		test.Fatal("no concurrency test was found in the module, so the list was checked against nothing")
	}
	for directory, why := range found {
		if !gated[directory] {
			test.Errorf("%s holds %s, and is not on the -race list in race_gated.go: add it, with why", directory, why)
		}
	}
}

// raceGatedDirectories are the directories raceGated names in raceGatedFile, each entry's `directory` field, which is
// always a string literal there.
func raceGatedDirectories(test *testing.T) map[string]bool {
	test.Helper()
	parsed, parseError := parser.ParseFile(token.NewFileSet(), raceGatedFile, nil, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	directories := map[string]bool{}
	for _, declaration := range parsed.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value, isValue := specification.(*ast.ValueSpec)
			if !isValue || len(value.Names) != 1 || value.Names[0].Name != "raceGated" || len(value.Values) != 1 {
				continue
			}
			list, isList := value.Values[0].(*ast.CompositeLit)
			if !isList {
				test.Fatalf("%s's raceGated is no list literal", raceGatedFile)
			}
			for _, element := range list.Elts {
				entry, isEntry := element.(*ast.CompositeLit)
				if !isEntry {
					test.Fatalf("%s's raceGated holds an entry that is no literal", raceGatedFile)
				}
				directories[entryDirectory(test, entry)] = true
			}
		}
	}
	return directories
}

// entryDirectory is the string literal entry's `directory` field holds, failing the test when it holds none.
func entryDirectory(test *testing.T, entry *ast.CompositeLit) string {
	test.Helper()
	for _, field := range entry.Elts {
		keyed, isKeyed := field.(*ast.KeyValueExpr)
		if !isKeyed {
			continue
		}
		key, isIdentifier := keyed.Key.(*ast.Ident)
		literal, isLiteral := keyed.Value.(*ast.BasicLit)
		if !isIdentifier || key.Name != "directory" || !isLiteral || literal.Kind != token.STRING {
			continue
		}
		directory, unquoteError := strconv.Unquote(literal.Value)
		if unquoteError != nil {
			test.Fatal(unquoteError)
		}
		return directory
	}
	test.Fatalf("%s's raceGated holds an entry with no directory written as a string literal", raceGatedFile)
	return ""
}
