package type_predicates

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fieldWalks are the calls that reach into a struct's fields: go/types' Field, NumFields and Fields, and
// staticcheck's FlattenFields. strings.Fields and bytes.Fields split text, and are no walk.
var fieldWalks = []string{"Field", "NumFields", "Fields", "FlattenFields"}

// fieldWalker is a file under rules/ that reaches into a struct's fields without a property here, and why no
// importer's finding depends there on another package's unexported, non-embedded field. Its path is from rules/.
type fieldWalker struct {
	file string
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
	{
		file: "inappropriate_context_key/analyzer.go",
		why:  "counts a key's fields, which is part of its type as written; its comparability is a property",
	},
	{
		file: "ineffective_field_assignment/analyzer.go",
		why: "indexes the fields of a struct the function itself copies, by index and name, which a package can " +
			"write only where it may name them",
	},
	{
		file: "invalid_struct_tag/analyzer.go",
		why:  "reads the tags of the package's own struct declarations",
	},
	{
		file: "marker_type/marker_type.go",
		why:  "counts a method's parameters and results, ast.FieldLists",
	},
	{
		file: "marshaled_struct_without_exported_fields/analyzer.go",
		why: "reads whether each field is exported, flattening embedded structs alone, whose types keep their " +
			"whole rendering",
	},
	{
		file: "misaligned_atomic_field/analyzer.go",
		why: "places a field by Offsetsof over the fields before it, which needs each one's size and alignment, " +
			"a property, and judges nothing past a type parameter, whose fields keep their whole type",
	},
	{
		file: "staticcheck_code/same_syntax.go",
		why:  "compares syntax nodes by reflection",
	},
	{
		file: "struct_conversion_by_field_copy/analyzer.go",
		why:  "matches a literal's keyed fields by name and count, and names only fields the package may name",
	},
	{
		file: "time_until/analyzer.go",
		why:  "follows a selection's path through embedded fields, whose types keep their whole rendering",
	},
	{
		file: "unused_code/graph_entry.go",
		why:  "marks the package's own fields used; it reports only its own objects",
	},
	{
		file: "unused_code/graph_types.go",
		why: "marks the package's own fields used, reading another package's struct only through embedded " +
			"fields, whose types keep their whole rendering",
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
	{
		file: "vet/cgo_call/analyzer.go",
		why: "walks an argument's type through pointers, but returns before judging anything unless the package's " +
			"types import runtime/cgo (run), and the load marks every such package never restored (recordReads)",
	},
	{
		file: "vet/composite/analyzer.go",
		why: "counts a literal's fields and reads whether each is exported, names and count staying in the " +
			"rendering",
	},
	{
		file: "vet/iface_assert/free_type_parameters.go",
		why:  "looks for the function's own free type parameters, which another package's field can never hold",
	},
	{
		file: "vet/struct_tag/analyzer.go",
		why:  "reads the package's own struct declarations and the exported fields of the structs they embed",
	},
	{
		file: "vet/unsafe_ptr/analyzer.go",
		why:  "follows a selection's path through embedded fields, whose types keep their whole rendering",
	},
}

// unlistedFieldWalks are the Go files under root, tests and testdata left out, that reach into a struct's fields
// (fieldWalks) and that fieldWalkers does not list, each from root.
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
		walks := false
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return !walks
			}
			selector, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector || !slices.Contains(fieldWalks, selector.Sel.Name) {
				return true
			}
			if receiver, isIdentifier := selector.X.(*ast.Ident); isIdentifier &&
				(receiver.Name == "strings" || receiver.Name == "bytes") {
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

// Every file under rules/ that reaches into a struct's fields either asks this package's walks, which Observe renders
// of another package's unexported field, or is listed with why it reads nothing of such a field that Observe leaves
// out. A rule that reads a ninth thing of one would restore a finding the build should not, so it fails here until a
// property is added or the file is listed.
func TestEveryRuleWalkOfAStructsFieldsIsRendered(test *testing.T) {
	test.Parallel()

	unlisted := unlistedFieldWalks(test, filepath.Join("..", "rules"), fieldWalkers)
	if len(unlisted) > 0 {
		test.Errorf("these files reach into a struct's fields, which may be another package's unexported field the "+
			"restore from the host renders only by these properties:\n  %s\nMove the walk into type_predicates with "+
			"a property for what it reads, or list the file in fieldWalkers with why no property is needed:\n  %s",
			strings.Join(Properties(), "\n  "), strings.Join(unlisted, "\n  "))
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
