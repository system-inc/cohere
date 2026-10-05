package text_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// goWhitespaceMarker opens the comment that keeps a Go-whitespace call: the call's own line ends with
// it, or the line above the call holds it, and the rest of the comment says why Go's set is the right
// one there, for instance a path or a key from cohere's own settings, which JavaScript never trims.
const goWhitespaceMarker = "Go whitespace:"

// goWhitespaceFunctions are the standard library's whitespace functions, by import path. Each uses
// unicode.IsSpace's set, which is not JavaScript's: see text.IsWhitespace.
var goWhitespaceFunctions = map[string][]string{
	"strings": {"TrimSpace", "Fields"},
	"bytes":   {"TrimSpace", "Fields"},
	"unicode": {"IsSpace"},
}

// goWhitespaceUnswept pins how many unmarked Go-whitespace calls each file held when the guard landed,
// pending #z4nssqs's sweep: lint's files by @system_cohere_lint_rules, format's by @system_cohere_format.
// Each site either moves to text.TrimWhitespace, text.WhitespaceFields or text.IsWhitespace, because it
// reads JavaScript source or answers as a JavaScript tool would, or keeps Go's set with a marker saying
// why. A file leaves this list when its count reaches zero, and the count only goes down: a new call in a
// pinned file fails just as it would anywhere else.
var goWhitespaceUnswept = map[string]int{
	// lint, @system_cohere_lint_rules.
	"internal/lint/configuration/configuration.go":                                   6,
	"internal/lint/configuration/version_pin.go":                                     2,
	"internal/lint/ecmascript/comments/comments.go":                                  3,
	"internal/lint/ecmascript/dotnotation/dotnotation.go":                            1,
	"internal/lint/optionschema/validate.go":                                         1,
	"internal/lint/rules/boundaries/dependencies.go":                                 1,
	"internal/lint/rules/core/array_callback_return.go":                              1,
	"internal/lint/rules/core/default_case.go":                                       1,
	"internal/lint/rules/core/max_lines.go":                                          1,
	"internal/lint/rules/core/no_extra_boolean_cast.go":                              1,
	"internal/lint/rules/core/no_fallthrough.go":                                     2,
	"internal/lint/rules/core/no_inline_comments.go":                                 3,
	"internal/lint/rules/core/no_invalid_this.go":                                    1,
	"internal/lint/rules/core/no_lonely_if.go":                                       3,
	"internal/lint/rules/core/no_restricted_imports.go":                              1,
	"internal/lint/rules/core/no_restricted_imports_matcher.go":                      1,
	"internal/lint/rules/core/no_unused_vars_fix.go":                                 1,
	"internal/lint/rules/core/object_shorthand.go":                                   2,
	"internal/lint/rules/core/prefer_destructuring.go":                               1,
	"internal/lint/rules/core/prefer_exponentiation_operator.go":                     1,
	"internal/lint/rules/core/prefer_regex_literals.go":                              1,
	"internal/lint/rules/core/radix.go":                                              1,
	"internal/lint/rules/core/require_await.go":                                      1,
	"internal/lint/rules/core/unicode_bom.go":                                        1,
	"internal/lint/rules/next/no_html_link_for_pages_options.go":                     2,
	"internal/lint/rules/nexus/consistency_no_long_line_comment.go":                  3,
	"internal/lint/rules/nexus/consistency_no_multiline_arrow_function.go":           2,
	"internal/lint/rules/nexus/correctness_no_caller_data_mutation.go":               4,
	"internal/lint/rules/nexus/localization_no_untranslated_value.go":                2,
	"internal/lint/rules/nexus/shouting.go":                                          1,
	"internal/lint/rules/react/conformance/fixture.go":                               3,
	"internal/lint/rules/react/conformance/pragma.go":                                1,
	"internal/lint/rules/react/iframe_missing_sandbox.go":                            1,
	"internal/lint/rules/react/jsx_fragments.go":                                     1,
	"internal/lint/rules/react/jsx_no_script_url.go":                                 1,
	"internal/lint/rules/react/jsx_no_useless_fragment.go":                           2,
	"internal/lint/rules/react/no_adjacent_inline_elements.go":                       2,
	"internal/lint/rules/react/no_invalid_html_attribute.go":                         3,
	"internal/lint/rules/react/no_object_type_as_default_prop.go":                    1,
	"internal/lint/rules/structure/consistency_require_organized_imports_section.go": 1,
	"internal/lint/rules/structure/react_hook_require_effect_comment.go":             3,
	"internal/lint/rules/tailwind/class_literals.go":                                 1,
	"internal/lint/rules/tailwind/class_templates.go":                                2,
	"internal/lint/rules/tailwind/collapse/classorder_differential.go":               1,
	"internal/lint/rules/tailwind/collapse/css_parser.go":                            7,
	"internal/lint/rules/tailwind/collapse/design_system.go":                         5,
	"internal/lint/rules/tailwind/collapse/theme_loader.go":                          3,
	"internal/lint/rules/tailwind/collapse/utility.go":                               2,
	"internal/lint/rules/tailwind/collapse/variant_printer.go":                       2,
	"internal/lint/rules/tailwind/enforce_consistent_variable_syntax.go":             1,
	"internal/lint/rules/tailwind/no_physical_direction.go":                          2,
	"internal/lint/rules/tailwind/tools/generate_candidate/main.go":                  2,
	"internal/lint/rules/tailwind/tools/generate_class_order/main.go":                2,
	"internal/lint/rules/tailwind/tools/generate_css_parser/main.go":                 2,
	"internal/lint/rules/tailwind/tools/generate_data_type/main.go":                  2,
	"internal/lint/rules/tailwind/tools/generate_syntax_tree/main.go":                2,
	"internal/lint/rules/tailwind/tools/generate_theme/main.go":                      2,
	"internal/lint/rules/tailwind/tools/generate_utility/main.go":                    2,
	"internal/lint/rules/tailwind/tools/generate_value_parser/main.go":               2,
	"internal/lint/rules/tailwind/tools/generate_variant/main.go":                    4,
	"internal/lint/rules/tailwind/tools/tooldirectory/tooldirectory.go":              1,
	"internal/lint/rules/typescript/array_type.go":                                   1,
	"internal/lint/rules/typescript/ban_ts_comment.go":                               1,
	"internal/lint/rules/typescript/ban_tslint_comment.go":                           1,
	"internal/lint/rules/typescript/class_literal_property_style.go":                 1,
	"internal/lint/rules/typescript/consistent_generic_constructors.go":              1,
	"internal/lint/rules/typescript/consistent_indexed_object_style.go":              1,
	"internal/lint/rules/typescript/consistent_type_imports.go":                      1,
	"internal/lint/rules/typescript/consistent_type_imports_fix.go":                  1,
	"internal/lint/rules/typescript/no_deprecated.go":                                1,
	"internal/lint/rules/typescript/no_invalid_this.go":                              1,
	"internal/lint/rules/typescript/no_misused_promises.go":                          2,
	"internal/lint/rules/typescript/no_restricted_types.go":                          2,
	"internal/lint/rules/typescript/no_unnecessary_template_expression.go":           1,
	"internal/lint/rules/typescript/switch_exhaustiveness_check.go":                  1,
	"internal/lint/rules/typescript/triple_slash_reference.go":                       2,

	// format, @system_cohere_format.
	"internal/format/prettier/bundles.go": 1,
	"internal/format/prettier/engine.go":  1,
}

// goWhitespaceSite is one unmarked reference to a Go whitespace function.
type goWhitespaceSite struct {
	file     string
	line     int
	function string
}

// goWhitespaceSitesIn finds every unmarked reference to a Go whitespace function in a file, called or
// passed as a value (`strings.TrimFunc(value, unicode.IsSpace)`), through whatever name the file imports
// the package under.
func goWhitespaceSitesIn(fileSet *token.FileSet, file *ast.File, relative string) []goWhitespaceSite {
	localNames := map[string]string{}
	for _, specification := range file.Imports {
		path, err := strconv.Unquote(specification.Path.Value)
		if err != nil {
			continue
		}
		if _, watched := goWhitespaceFunctions[path]; !watched {
			continue
		}
		local := path
		if specification.Name != nil {
			local = specification.Name.Name
		}
		localNames[local] = path
	}
	if len(localNames) == 0 {
		return nil
	}

	markedLines := map[int]bool{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			body := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(comment.Text, "//"), "/*")) // Go whitespace: a Go comment's text
			if strings.HasPrefix(body, goWhitespaceMarker) {
				// The comment's own line keeps a trailing call, and the line below its group keeps the call
				// there.
				markedLines[fileSet.Position(comment.Pos()).Line] = true
				markedLines[fileSet.Position(group.End()).Line+1] = true
			}
		}
	}

	var found []goWhitespaceSite
	ast.Inspect(file, func(node ast.Node) bool {
		selector, isSelector := node.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		identifier, isIdentifier := selector.X.(*ast.Ident)
		if !isIdentifier || identifier.Obj != nil {
			// A local variable named `strings` is not the package.
			return true
		}
		path, isWatched := localNames[identifier.Name]
		if !isWatched || !slices.Contains(goWhitespaceFunctions[path], selector.Sel.Name) {
			return true
		}
		line := fileSet.Position(selector.Pos()).Line
		if !markedLines[line] {
			found = append(found, goWhitespaceSite{file: relative, line: line, function: path + "." + selector.Sel.Name})
		}
		return true
	})
	return found
}

// goWhitespaceSites walks the ported code, lint and format, and finds every unmarked site. Test files
// are left out: a test trimming its own expected output never reaches a verdict a user sees.
func goWhitespaceSites(t *testing.T) []goWhitespaceSite {
	t.Helper()
	moduleRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var found []goWhitespaceSite
	files := 0
	for _, directory := range []string{"internal/lint", "internal/format"} {
		walkError := filepath.WalkDir(filepath.Join(moduleRoot, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			files++
			relative, err := filepath.Rel(moduleRoot, path)
			if err != nil {
				return err
			}
			found = append(found, goWhitespaceSitesIn(fileSet, file, filepath.ToSlash(relative))...)
			return nil
		})
		if walkError != nil {
			t.Fatalf("walking %s: %v", directory, walkError)
		}
	}
	// A walk that read nothing would pass everything, so it has to have read the tree.
	if files < 1000 {
		t.Fatalf("the walk read %d files under internal/lint and internal/format, which is not the tree", files)
	}
	sort.Slice(found, func(left, right int) bool {
		return found[left].file < found[right].file || found[left].file == found[right].file && found[left].line < found[right].line
	})
	return found
}

// Ported code reads whitespace as JavaScript does (#z4nssqs). A Go whitespace call in lint or format
// fails here by file and line unless it carries a `// Go whitespace:` comment saying why Go's set is
// right there, or its file is pinned in goWhitespaceUnswept at its exact count. A pin above the count
// fails too, so the list can only shrink.
func TestPortedCodeReadsWhitespaceAsJavaScriptDoes(t *testing.T) {
	t.Parallel()
	byFile := map[string][]goWhitespaceSite{}
	for _, site := range goWhitespaceSites(t) {
		byFile[site.file] = append(byFile[site.file], site)
	}
	for file, sites := range byFile {
		pinned := goWhitespaceUnswept[file]
		if len(sites) == pinned {
			continue
		}
		if len(sites) < pinned {
			t.Errorf("%s holds %d unmarked Go-whitespace calls, below its pin of %d: lower the pin in goWhitespaceUnswept", file, len(sites), pinned)
			continue
		}
		for _, site := range sites {
			t.Errorf("%s:%d: %s uses Go's whitespace, which is not JavaScript's: use text.TrimWhitespace, text.WhitespaceFields or text.IsWhitespace, or say why Go's set is right with a `// %s` comment",
				site.file, site.line, site.function, goWhitespaceMarker)
		}
	}
	for file, pinned := range goWhitespaceUnswept {
		if len(byFile[file]) == 0 {
			t.Errorf("goWhitespaceUnswept pins %s at %d, and it holds none: remove it", file, pinned)
		}
	}
}

// The guard sees each kind of site it exists for, through an aliased import and as a value, and keeps
// each marked one: the control that the tree-wide pass above is a walk that could have failed.
func TestTheWhitespaceGuardSeesEachKindOfSite(t *testing.T) {
	t.Parallel()
	source := `package probe

import (
	"bytes"
	"strings"
	text "unicode"
)

func probe(value string, raw []byte) {
	_ = strings.TrimSpace(value)
	_ = strings.Fields(value)
	_ = bytes.TrimSpace(raw)
	_ = strings.TrimFunc(value, text.IsSpace)
	_ = strings.TrimSpace(value) // Go whitespace: a settings key
	// Go whitespace: a path from the command line, which
	// no JavaScript tool reads.
	_ = strings.Fields(value)
	_ = strings.TrimLeft(value, " ")
	strings := []string{}
	_ = strings
}
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "probe.go", source, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, site := range goWhitespaceSitesIn(fileSet, file, "probe.go") {
		lines = append(lines, fmt.Sprintf("%d %s", site.line, site.function))
	}
	want := []string{"10 strings.TrimSpace", "11 strings.Fields", "12 bytes.TrimSpace", "13 unicode.IsSpace"}
	if !slices.Equal(lines, want) {
		t.Fatalf("the guard found %v, want %v", lines, want)
	}
}
