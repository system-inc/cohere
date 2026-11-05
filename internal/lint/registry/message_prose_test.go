package registry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// proseWordPattern splits a description into the tokens a package name could occupy.
var proseWordPattern = regexp.MustCompile(`[A-Za-z0-9_]+`)

// TestMessagesDoNotNameInternalPackages refuses a package directory's name inside a rule's prose.
//
// Two package renames rewrote English along with the identifiers. `78bffa9` renamed
// `internal/unused` to `internal/unused_code_report` by substituting `unused.`, which matched the
// package qualifier in `unused.Run` and also every sentence that ended on the word. `8bf8a0b`
// carried the damage forward when it renamed the package again, so for five weeks
// nexus/consistency-no-ambiguous-identifier told every reader that a value was "deliberately
// unused_exports." Nothing failed: the compiler does not read prose, and no fixture asserted the
// sentence.
//
// The check is the one that survives the next rename without being edited. A package directory
// whose name carries an underscore is a token no person writes in a sentence, so a description
// containing one is either a substitution that leaked or a message telling a user about this
// tree's internals, and neither should reach the reader. The set is read from disk rather than
// listed here, so a package added or renamed tomorrow is covered the day it lands.
//
// What this cannot see is a rename to or from a plain word. The same commit turned "every
// settings-reachable fix." into "edit." when `fix/` became `edit/`, and `edit` is English, so no
// token test can tell the package from the verb. That one was found by reading the diff, and a
// rename whose new name is an ordinary word still needs that reading.
func TestMessagesDoNotNameInternalPackages(test *testing.T) {
	test.Parallel()
	packageNames := underscoredInternalPackageNames(test)
	if len(packageNames) == 0 {
		// An empty set matches nothing, and a guard with nothing to match passes for the wrong
		// reason, which is the defect it guards against.
		test.Fatal("found no underscored package directories under internal/, so this test checked nothing")
	}

	descriptions := ruleMessageDescriptionsOnDisk(test)
	if len(descriptions) == 0 {
		test.Fatal("found no message descriptions under internal/lint/rules, so this test checked nothing")
	}
	test.Logf("checked %d descriptions against %d underscored package names", len(descriptions), len(packageNames))

	for _, description := range descriptions {
		for _, word := range proseWordPattern.FindAllString(description.text, -1) {
			if !packageNames[word] {
				continue
			}
			test.Errorf("%s: a message description contains %q, which is the name of a package directory "+
				"under internal/ rather than a word a reader knows; a rename that substituted text "+
				"leaked into prose, or the message is telling a user about this tree's layout. "+
				"Description: %q", description.position, word, description.text)
		}
	}
}

// underscoredInternalPackageNames is every directory under internal/ that holds a Go package and
// whose name carries an underscore.
func underscoredInternalPackageNames(test *testing.T) map[string]bool {
	test.Helper()

	names := map[string]bool{}
	walkError := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() && entry.Name() == "testdata" {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		directory := filepath.Base(filepath.Dir(path))
		if strings.Contains(directory, "_") {
			names[directory] = true
		}
		return nil
	})
	if walkError != nil {
		test.Fatalf("walking internal/: %v", walkError)
	}
	return names
}

// messageDescription is the static text of one description and where it was written.
type messageDescription struct {
	position string
	text     string
}

// ruleMessageDescriptionsOnDisk reads every `Description` a rule package writes.
//
// Parsed rather than grepped, because a description is usually built from several literals around
// a name the rule only knows at report time, and the comments beside it discuss package names
// legitimately. Every string literal inside the description's expression is collected and joined,
// so `"Identifier \"" + name + "\" is..."` and a `fmt.Sprintf` format string are both read.
func ruleMessageDescriptionsOnDisk(test *testing.T) []messageDescription {
	test.Helper()

	var found []messageDescription
	fileSet := token.NewFileSet()
	walkError := filepath.WalkDir("../rules", func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() && entry.Name() == "testdata" {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		parsed, parseError := parser.ParseFile(fileSet, path, nil, 0)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", path, parseError)
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			keyed, isKeyed := node.(*ast.KeyValueExpr)
			if !isKeyed {
				return true
			}
			key, isIdentifier := keyed.Key.(*ast.Ident)
			if !isIdentifier || key.Name != "Description" {
				return true
			}
			found = append(found, messageDescription{
				position: fileSet.Position(keyed.Pos()).String(),
				text:     stringLiteralsIn(keyed.Value),
			})
			return false
		})
		return nil
	})
	if walkError != nil {
		test.Fatalf("walking internal/lint/rules: %v", walkError)
	}
	return found
}

// stringLiteralsIn joins the unquoted value of every string literal inside an expression.
func stringLiteralsIn(expression ast.Expr) string {
	var parts []string
	ast.Inspect(expression, func(node ast.Node) bool {
		literal, isLiteral := node.(*ast.BasicLit)
		if !isLiteral || literal.Kind != token.STRING {
			return true
		}
		unquoted, unquoteError := strconv.Unquote(literal.Value)
		if unquoteError != nil {
			unquoted = literal.Value
		}
		parts = append(parts, unquoted)
		return true
	})
	return strings.Join(parts, " ")
}
