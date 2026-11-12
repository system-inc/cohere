package check

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// methodLookupRules are the packages that record a Method or Interface lookup, each a rule that reads one bit of it,
// whether a method of another package fixes its subject's name, which is what methodLookupHolds judges it by. That
// judgment also holds an unanswered lookup when the build's new candidates all have another shape than the subject,
// so each rule's own fixtures pin that a method of another shape never fixes a name, red if its match loosens to the
// name alone: abbreviated's flagged.go, commanding's Cmd and wideStack's Len; no_any's flagged.go, memoryCache's
// Save; initialism's flagged.go, record's MarshalJSON. A rule joining this list brings such a fixture.
//
// Neither side of the judgment counts a concrete type whose method's name is unexported when the subject is a function
// or a concrete type's method (countedCandidates), so a rule joining this list must show it never lets a concrete
// foreign method with an unexported name fix such a subject's name, or that narrowing is wrong for it. Each rule pins
// it in its own package, red if it ever lets one: TestAnUnexportedConcreteForeignMethodNeverFixesAName.
//
// The judgment's candidates come from the verdict's method index (indexPackage), which indexes as the rules do: an
// interface by its whole method set, an alias's too, and any other named type by the methods it declares, never one
// promoted to it, nor an alias of it. So a rule joining this list must read a concrete type's declared methods alone,
// and an alias only of an interface, or the index loses a candidate it reads. Each rule pins its part, red if it ever
// reads more: TestOnlyDeclaredMethodsFixAName and TestAnAliasOfAConcreteTypeNeverFixesAName. An alias of an interface
// stays in the index because initialism reads it: its foreignInterfaces adds every type name of an imported scope
// with no alias check, and `type Closer = interface{ Close() error }` is the one name its interface has, so a build
// that gains it can fix a name the host left unfixed (TestAMethodLookupHoldsUnlessItsRulesBitCouldFlip's alias of an
// interface). abbreviated and no-any skip every alias, so for them it is a candidate they never read, which costs a
// restore and never a stale one.
var methodLookupRules = []string{
	"rules/consistency_no_abbreviated_identifier",
	"rules/consistency_no_any",
	"rules/consistency_no_uppercase_initialism",
}

// methodLookupRecorders are the packages under root, the go_engine directory, outside build_reads, which declares the
// queries, and check, which judges them, whose non-test files build a build_reads.Lookup of the Method or Interface
// query, or of a query the source does not spell out, which could be either, each with where it does.
func methodLookupRecorders(test *testing.T, root string) map[string][]string {
	test.Helper()

	recorders := map[string][]string{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative == "build_reads" || relative == "check" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		parsed, parseError := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseError != nil {
			return parseError
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, isLiteral := node.(*ast.CompositeLit)
			if !isLiteral || !isBuildReadsSelector(literal.Type, "Lookup") {
				return true
			}
			// A lookup written by position names no field, so its query cannot be told apart from the rest.
			if len(literal.Elts) > 0 {
				if _, isKeyed := literal.Elts[0].(*ast.KeyValueExpr); !isKeyed {
					recorders[filepath.ToSlash(filepath.Dir(relative))] = append(
						recorders[filepath.ToSlash(filepath.Dir(relative))], fileSet.Position(literal.Pos()).String())
					return true
				}
			}
			for _, element := range literal.Elts {
				field, isField := element.(*ast.KeyValueExpr)
				if key, isIdentifier := field.Key.(*ast.Ident); !isField || !isIdentifier || key.Name != "Query" {
					continue
				}
				if recordsMethodOrInterface(field.Value) {
					recorders[filepath.ToSlash(filepath.Dir(relative))] = append(
						recorders[filepath.ToSlash(filepath.Dir(relative))], fileSet.Position(field.Pos()).String())
				}
			}
			return true
		})
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return recorders
}

// isBuildReadsSelector reports whether expression is build_reads.name.
func isBuildReadsSelector(expression ast.Expr, name string) bool {
	selector, isSelector := expression.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	qualifier, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && qualifier.Name == "build_reads" && selector.Sel.Name == name
}

// recordsMethodOrInterface reports whether a lookup's Query may be Method or Interface: their constants or their
// spelled strings, or anything else but another query's constant or string, which the source does not settle.
func recordsMethodOrInterface(query ast.Expr) bool {
	switch typed := query.(type) {
	case *ast.SelectorExpr:
		if isBuildReadsSelector(typed, "QueryMethod") || isBuildReadsSelector(typed, "QueryInterface") {
			return true
		}
		return !isBuildReadsSelector(typed, typed.Sel.Name) || !strings.HasPrefix(typed.Sel.Name, "Query")
	case *ast.BasicLit:
		spelled, unquoteError := strconv.Unquote(typed.Value)
		return unquoteError != nil || spelled == "Method" || spelled == "Interface"
	}
	return true
}

// Only the rules read for one bit of a Method or Interface lookup record one: the verdict judges such a lookup by
// whether that bit could flip in another build (methodLookupHolds), so a rule that reads more of the answer, which
// method answered or how many could, would be judged too narrowly. A new recorder fails here until its use of the
// answer is read and it joins methodLookupRules.
func TestOnlyTheRulesReadForOneBitRecordMethodOrInterfaceLookups(test *testing.T) {
	test.Parallel()

	recorders := methodLookupRecorders(test, "..")
	for _, recorder := range slices.Sorted(maps.Keys(recorders)) {
		if !slices.Contains(methodLookupRules, recorder) {
			test.Errorf("%s records a Method or Interface lookup (%s): read how it uses the answer before adding it "+
				"to methodLookupRules, since the verdict judges such a lookup by whether one method fixes the "+
				"subject's name, and a rule that reads more of the answer needs its own judgment", recorder,
				strings.Join(recorders[recorder], ", "))
		}
	}
	for _, listed := range methodLookupRules {
		if _, records := recorders[listed]; !records {
			test.Errorf("%s is in methodLookupRules but records no Method or Interface lookup: take it out", listed)
		}
	}
}
