package type_predicates

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fieldWalks are the calls that reach into a struct's fields: go/types' Field, NumFields and Fields, and
// staticcheck's FlattenFields. strings.Fields and bytes.Fields split text, and are no walk.
var fieldWalks = []string{"Field", "NumFields", "Fields", "FlattenFields"}

// predicateWalkers are the packages whose functions walk a type's fields for a rule: this one and its locks and verbs,
// by the names a rule imports them under, and staticcheck's fake reflection, which reads a type the way
// encoding/json and encoding/xml do.
var predicateWalkers = []string{"type_predicates", "locks", "verbs"}

// fakeReflections are the import paths of staticcheck's fake reflection, each a walk of a type's fields.
var fakeReflections = []string{
	"honnef.co/go/tools/staticcheck/fakejson",
	"honnef.co/go/tools/staticcheck/fakexml",
	"honnef.co/go/tools/staticcheck/fakereflect",
}

// fieldWalker is a file under rules/ that reaches into a struct's fields, and either the row of go_engine's
// fieldWalkRows that proves the walk starts from a use the restore from the host judges by the whole type
// (#mjdf7ah), or why no importer's finding depends there on another package's type past what its reads record. Its
// path is from rules/.
type fieldWalker struct {
	file string
	row  string
	why  string
}

// fieldWalkers are every fieldWalker.
var fieldWalkers = []fieldWalker{
	{
		file: "concurrency_no_buffered_wait_delay/analyzer.go",
		why:  "finds which field of os/exec.Cmd a selector names, by identity",
	},
	{
		file: "consistency_no_any/flow_analysis.go",
		why: "pairs the fields of the package's own composite literals and struct declarations with their " +
			"values, which another package's unexported field never is",
	},
	{
		file: "consistency_no_boolean_outcome/analyzer.go",
		why:  "reads the package's own struct declarations",
	},
	{
		file: "consistency_no_shouting/analyzer.go",
		why:  "counts a function's parameters, an ast.FieldList",
	},
	{
		file: "correctness_no_identical_branches/comparison.go",
		why:  "compares syntax nodes by reflection",
	},
	{
		file: "guarded_delete/analyzer.go",
		why:  "compares syntax nodes by reflection",
	},
	{file: "inappropriate_context_key/analyzer.go", row: "SA1029"},

	{
		file: "ineffective_field_assignment/analyzer.go",
		why: "indexes the fields of a struct the function itself copies, by index and name, which a package can " +
			"write only where it may name them, and a copy of another package's struct is a read of it whole",
	},

	{
		file: "invalid_struct_tag/analyzer.go",
		why: "hands fakexml each field of the package's own struct, which reads its tag alone, and reads a string " +
			"option's field type through a pointer by its basic kind, which a read of the pointed type's name holds",
	},

	{
		file: "marker_type/marker_type.go",
		why:  "counts a method's parameters and results, ast.FieldLists",
	},
	{file: "marshaled_struct_without_exported_fields/analyzer.go", row: "SA9005"},

	{
		file: "misaligned_atomic_field/analyzer.go",
		why: "places a field of the package's own struct by Offsetsof over the fields before it, each of another " +
			"package's type held by value a read of that type whole, and judges nothing past a type parameter",
	},

	{
		file: "staticcheck_code/same_syntax.go",
		why:  "compares syntax nodes by reflection",
	},
	{file: "struct_conversion_by_field_copy/analyzer.go", row: "S1016"},

	{file: "time_until/analyzer.go", row: "S1024"},

	{
		file: "unused_code/graph_entry.go",
		why:  "marks the package's own fields used; it reports only its own objects",
	},
	{
		file: "unused_code/graph_types.go",
		why: "marks the package's own fields used, reading another package's struct only through the fields the " +
			"package's own structs embed, each embedding a read of the embedded type whole",
	},

	{
		file: "unused_code/graph_walk.go",
		why:  "marks fields used; it reports only the package's own objects",
	},
	{
		file: "unused_write/analyzer.go",
		why:  "indexes the fields of a struct the function itself writes, by index and name",
	},
	{
		file: "vet/asm_decl/argument_frame.go",
		why: "lays out every field of an assembly function's arguments, which no property stands for, so a " +
			"package with assembly files is judged by every type it reaches rendered whole (Restoring.Assembly)",
	},
	{file: "identical_operands/analyzer.go", row: "SA4000"},
	{file: "function_purity/judgment.go", row: "SA4017"},
	{file: "unencodable_binary_value/analyzer.go", row: "SA1003"},
	{file: "unsupported_marshal_type/analyzer.go", row: "SA1026"},
	{file: "vet/copy_lock/analyzer.go", row: "copylocks"},
	{file: "vet/copy_lock/lock_path.go", row: "copylocks"},
	{file: "vet/printf/argument_checks.go", row: "printf"},
	{file: "vet/printf/argument_matcher.go", row: "printf"},
	{file: "vet/printf/check_printf.go", row: "printf"},
	{file: "vet/printf/printf_call.go", row: "printf"},
	{
		file: "vet/cgo_call/analyzer.go",
		why: "walks an argument's type through pointers, but returns before judging anything unless the package's " +
			"types import runtime/cgo (run), and the load marks every such package never restored (recordReads)",
	},
	{file: "vet/composite/analyzer.go", row: "composites"},

	{
		file: "vet/iface_assert/free_type_parameters.go",
		why:  "looks for the function's own free type parameters, which another package's field can never hold",
	},
	{file: "vet/struct_tag/analyzer.go", row: "structtag"},

	{
		file: "vet/unsafe_ptr/analyzer.go",
		why: "follows a field selection's path through embedded fields, each read by its own line and the " +
			"selection's index path, which name every type on the way, and asks only whether the last is a " +
			"reflect header, by its name",
	},
}

// unlistedFieldWalks are the Go files under root, tests and testdata left out, that reach into a struct's fields
// (fieldWalks), call a walk of this package's (predicateWalkers) or import staticcheck's fake reflection
// (fakeReflections), and that fieldWalkers does not list, each from root.
func unlistedFieldWalks(test *testing.T, root string, listed []fieldWalker) []string {
	test.Helper()

	var unlisted []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		isListed := slices.ContainsFunc(listed, func(walker fieldWalker) bool {
			return walker.file == filepath.ToSlash(relative)
		})
		if isListed {
			return nil
		}
		parsed, parseError := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if parseError != nil {
			return parseError
		}
		walks := slices.ContainsFunc(parsed.Imports, func(imported *ast.ImportSpec) bool {
			importPath, unquoteError := strconv.Unquote(imported.Path.Value)
			return unquoteError == nil && slices.Contains(fakeReflections, importPath)
		})
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return !walks
			}
			selector, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			receiver, isIdentifier := selector.X.(*ast.Ident)
			if isIdentifier && slices.Contains(predicateWalkers, receiver.Name) {
				walks = true
				return false
			}
			if !slices.Contains(fieldWalks, selector.Sel.Name) {
				return true
			}
			if isIdentifier && (receiver.Name == "strings" || receiver.Name == "bytes") {
				return true
			}
			walks = true
			return !walks
		})
		if walks {
			unlisted = append(unlisted, filepath.ToSlash(relative))
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return unlisted
}

// Every file under rules/ that reaches into a struct's fields, itself, through this package's walks or through
// staticcheck's fake reflection, is listed with the row of go_engine's fieldWalkRows that proves its walk starts from
// a use the restore from the host judges by the whole type, or with why its finding depends on nothing of another
// package's type past what the package's reads record (#mjdf7ah). Another package's unexported field is rendered
// there only by these properties, so a walk that reads anything else of one needs a property too. A new walk fails here
// until it is listed.
func TestEveryRuleWalkOfAStructsFieldsIsRendered(test *testing.T) {
	test.Parallel()

	unlisted := unlistedFieldWalks(test, filepath.Join("..", "rules"), fieldWalkers)
	if len(unlisted) > 0 {
		test.Errorf("these files reach into a struct's fields, which may be another package's type a reader holds "+
			"only by member or by name, and whose unexported fields the restore from the host renders only by "+
			"these properties:\n  %s\nList the file in fieldWalkers with a row of go_engine's fieldWalkRows that "+
			"proves its walk starts from a whole read, or with why it needs none:\n  %s",
			strings.Join(Properties(), "\n  "), strings.Join(unlisted, "\n  "))
	}
	for _, walker := range fieldWalkers {
		if (walker.row == "") == (walker.why == "") {
			test.Errorf("%s gives a row %q and a why %q, want exactly one", walker.file, walker.row, walker.why)
		}
		if _, statError := os.Stat(filepath.Join("..", "rules", filepath.FromSlash(walker.file))); statError != nil {
			test.Errorf("fieldWalkers lists %s, which is not there: %v", walker.file, statError)
		}
	}
}

// The guard finds a walk of a struct's fields that is not listed: one planted in a rule of its own goes red.
func TestThePlantedFieldWalkIsFound(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	rule := filepath.Join(root, "planted", "analyzer.go")
	if makeError := os.MkdirAll(filepath.Dir(rule), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	source := "package planted\n\nimport \"go/types\"\n\n" +
		"func sized(structure *types.Struct) int { return structure.NumFields() }\n"
	if writeError := os.WriteFile(rule, []byte(source), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	unlisted := unlistedFieldWalks(test, root, fieldWalkers)
	if !slices.Equal(unlisted, []string{"planted/analyzer.go"}) {
		test.Errorf("want the planted walk found, got %v", unlisted)
	}
}
