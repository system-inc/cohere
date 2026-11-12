package go_engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// fieldWalksModule is the field-walks fixture's module: platform, whose types windows declares its own way, and a
// package per rule that walks another package's type, each reading platform the way its rule walks it.
const fieldWalksModule = "example.com/fieldwalks"

// fieldWalkRow is one rule that walks another package's type past what the type checker records (#mjdf7ah): the
// package of the fixture that hands it a type of platform, the rule's upstream name, the read that must cover the
// walk, which kind of use names, and the builds whose check of the package finds the rule's finding, since windows'
// type differs where the walk looks: host, windows, both, or none. The guard of every rule's walk of a struct's fields
// (type_predicates' guard_test.go) names each row by its name.
type fieldWalkRow struct {
	name    string
	user    string
	rule    string
	reads   []string
	foundOn string
}

// fieldWalkRows are every rule that walks another package's type, each judged by the read its walk starts from: a
// value handed to an empty interface, copied, compared, a parameter held by value, a struct literal and the value it
// copies fields from, an embedding, and a selection by name through embedded fields.
var fieldWalkRows = []fieldWalkRow{
	{name: "printf", user: "printed", rule: "printf", reads: []string{"Printed Whole"}, foundOn: "host"},
	{name: "copylocks", user: "locked", rule: "copylocks", reads: []string{"Locked Whole"}, foundOn: "windows"},
	{name: "SA1003", user: "encoded", rule: "SA1003", reads: []string{"Encoded Whole"}, foundOn: "windows"},
	{name: "SA4000", user: "measured", rule: "SA4000", reads: []string{"Measured Whole"}, foundOn: "host"},
	{name: "SA4017", user: "basic", rule: "SA4017", reads: []string{"Basic Whole"}, foundOn: "host"},
	{name: "SA9005", user: "hidden", rule: "SA9005", reads: []string{"Hidden Whole"}, foundOn: "host"},
	{name: "S1016", user: "converted", rule: "S1016", reads: []string{"Source Whole", "Target Whole"}, foundOn: "host"},
	{name: "composites", user: "paired", rule: "composites", reads: []string{"Pair Whole"}, foundOn: "both"},
	{name: "S1024", user: "stamped", rule: "S1024", reads: []string{"selection Stamped.Time"}, foundOn: "both"},
	{name: "SA1026", user: "channeled", rule: "SA1026", reads: []string{"Channeled Whole"}, foundOn: "windows"},
	{name: "structtag", user: "tagged", rule: "structtag", reads: []string{"Tagged Whole"}, foundOn: "host"},
	{name: "SA1029", user: "keyed", rule: "SA1029", reads: []string{"Key Whole"}, foundOn: "windows"},
}

// readsOfUser are the host's reads of the fixture's package user, not a test variant, failing the test when it has
// none.
func readsOfUser(test *testing.T, engine *pipeline, user string) *build_reads.Reads {
	test.Helper()

	for recorded, reads := range engine.buildRuns[0].reads {
		if recorded.PkgPath == fieldWalksModule+"/"+user && recorded.ForTest == "" {
			return reads
		}
	}
	test.Fatalf("the host recorded no reads of %s", user)
	return nil
}

// readLines are reads' uses of platform, each as its type's name and kind, and its selections through platform's types,
// each as `selection` and the type and name selected.
func readLines(reads *build_reads.Reads) []string {
	platform := fieldWalksModule + "/platform"
	var lines []string
	for _, use := range reads.Uses() {
		if use.Object.Pkg().Path() == platform {
			lines = append(lines, use.Object.Name()+" "+string(use.Kind))
		}
	}
	for _, selection := range reads.Selections() {
		if selection.Receiver.Pkg().Path() == platform {
			lines = append(lines, "selection "+selection.Receiver.Name()+"."+selection.Name)
		}
	}
	return lines
}

// foundBy reports whether run found anything by the rule of upstream name in file.
func foundBy(run buildRun, file string, upstream string) bool {
	return slices.ContainsFunc(run.result.Findings, func(finding check.Finding) bool {
		return filepath.Base(finding.File) == file && finding.Rule == rule.Namespace+upstream
	})
}

// Every rule that walks another package's type past what the type checker records starts its walk from a use the
// reads judge by the whole type, or, for a walk by name through embedded fields, by the selection of that name: so a
// package whose rule's finding hangs on a type windows declares its own way is declined on windows and checked from
// source there, and each finding is the one its own build's check finds, never the host's restored (#mjdf7ah). Each
// row's finding differs between the builds where windows' type differs where the rule looks, which is what restoring
// it would have got wrong. A value handed to a non-empty interface is read for the interface's method set alone: no
// rule that walks a type's fields walks one that flows into a non-empty interface, so sized, which hands every type of
// the rows to one, finds nothing in either build by any of the rows' rules, reads each type's method set and none
// whole, and holds on windows, though every type it hands over differs there.
func TestEveryWalkOfAnotherPackagesTypeStartsFromAWholeRead(test *testing.T) {
	test.Parallel()

	engine, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "field-walks")), exitFindings,
		"--other-builds")
	host, windows := engine.buildRuns[0], windowsRun(test, engine)
	for _, row := range fieldWalkRows {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()

			lines := readLines(readsOfUser(test, engine, row.user))
			for _, want := range row.reads {
				if !slices.Contains(lines, want) {
					test.Errorf("want %s's reads to hold %q, which %s's walk starts from, got %q", row.user, want,
						row.rule, lines)
				}
			}
			identifier := fieldWalksModule + "/" + row.user
			if slices.Contains(windows.result.FromHost.Restored, identifier) {
				test.Errorf("windows restored %s from the host, whose %s finding hangs on a type windows declares "+
					"its own way", row.user, row.rule)
			}
			file := row.user + ".go"
			onHost, onWindows := foundBy(host, file, row.rule), foundBy(windows, file, row.rule)
			want := map[string][2]bool{"host": {true, false}, "windows": {false, true}, "both": {true, true},
				"none": {false, false}}[row.foundOn]
			if onHost != want[0] || onWindows != want[1] {
				test.Errorf("want %s found in %s %s, got the host %t and windows %t", row.rule, file, row.foundOn,
					onHost, onWindows)
			}
		})
	}
	test.Run("a non-empty interface", func(test *testing.T) {
		test.Parallel()

		lines := readLines(readsOfUser(test, engine, "sized"))
		for _, typeName := range []string{"Printed", "Locked", "Encoded", "Hidden", "Channeled"} {
			if !slices.Contains(lines, typeName+" MethodSet") || slices.Contains(lines, typeName+" Whole") {
				test.Errorf("want sized to read %s's method set and not the whole of it, got %q", typeName, lines)
			}
		}
		for _, row := range fieldWalkRows {
			if foundBy(host, "sized.go", row.rule) || foundBy(windows, "sized.go", row.rule) {
				test.Errorf("%s walked a value sized hands to a non-empty interface", row.rule)
			}
		}
		// A rule's Method lookup still judges each type name of the closure with a method of the name by its whole
		// rendering, which kinds of use leave alone, so sized may be declined on windows by a lookup, never by a read
		// of a type.
		if why := windows.result.FromHost.Declined[fieldWalksModule+"/sized"]; strings.HasPrefix(why, "T ") {
			test.Errorf("windows declined sized by a read of a type, %q, though its reads are the method sets alone, "+
				"the same there", why)
		}
	})
}

// The guard of every rule's walk of a struct's fields (type_predicates' guard_test.go) names, for each walk it lists
// without a why, a row of fieldWalkRows that covers it: every name it gives is a row here, so a row renamed or dropped
// leaves no walk named by a row that does not exist.
func TestTheFieldWalkGuardNamesOnlyRowsThatExist(test *testing.T) {
	test.Parallel()

	guard, parseError := parser.ParseFile(token.NewFileSet(), filepath.Join("type_predicates", "guard_test.go"), nil,
		parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	named := 0
	ast.Inspect(guard, func(node ast.Node) bool {
		pair, isPair := node.(*ast.KeyValueExpr)
		if !isPair {
			return true
		}
		key, isIdentifier := pair.Key.(*ast.Ident)
		literal, isLiteral := pair.Value.(*ast.BasicLit)
		if !isIdentifier || key.Name != "row" || !isLiteral {
			return true
		}
		name, unquoteError := strconv.Unquote(literal.Value)
		if unquoteError != nil {
			test.Fatal(unquoteError)
		}
		named++
		if !slices.ContainsFunc(fieldWalkRows, func(row fieldWalkRow) bool { return row.name == name }) {
			test.Errorf("the guard names the row %q, which fieldWalkRows does not hold", name)
		}
		return true
	})
	if named == 0 {
		test.Error("the guard names no row, so this test tells nothing")
	}
}
