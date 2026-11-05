package tailwind

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// TestClassLiteralsAgreeWithUpstream reads every case in testdata/class_literals/cases.json through
// the Go reader, with its options and settings decoded by a rule's real decoder, and compares the
// strings it reads with what better-tailwindcss 4.7.0's own listener handed a rule for the same case
// (upstream.json, written by tools/generate_class_literals), range for range (#btxd64n).
//
// A template with holes is a literal per run upstream, from its opening delimiter to its closing one,
// so the Go reader's template is compared as its head, middles and tail. Upstream's concatenation
// flags are compared with the reader's, since two fixers trim by them.
//
// A case marked pending names the unit that will read it; until then it must disagree, so a unit that
// lands without unmarking its cases fails here.
func TestClassLiteralsAgreeWithUpstream(test *testing.T) {
	test.Parallel()
	cases := readClassLiteralCases(test)
	oracle := readClassLiteralOracle(test)
	if len(oracle.Cases) != len(cases) {
		test.Fatalf("upstream.json holds %d cases and cases.json %d: run tools/generate_class_literals", len(oracle.Cases), len(cases))
	}
	decode := tailwindDecoders(test)[NoDuplicateClasses.Name]

	for index, testCase := range cases {
		expected := oracle.Cases[index]
		if expected.Name != testCase.Name {
			test.Fatalf("case %d is %q in cases.json and %q in upstream.json: run tools/generate_class_literals", index, testCase.Name, expected.Name)
		}
		test.Run(testCase.Name, func(test *testing.T) {
			test.Parallel()
			var raw []byte
			if testCase.Options != nil {
				raw = testCase.Options
			}
			base := rule.OptionsBase{ConfigDirectory: "/repo"}
			if testCase.Settings != nil {
				base = settingsBase(string(testCase.Settings))
			}
			decoded, err := decode(raw, base)
			if err != nil {
				test.Fatalf("decoding the case's options: %v", err)
			}
			reader := surfacesOf(test, decoded).compiled

			code := strings.Join(testCase.Code, "\n")
			got := readEveryClassLiteral(test, reader, code)
			want := make([]string, 0, len(expected.Literals))
			for _, literal := range expected.Literals {
				want = append(want, upstreamLiteral{literal.Start, literal.End, literal.ConcatenatedLeft, literal.ConcatenatedRight}.describe(code))
			}

			agrees := slices.Equal(got, want)
			if testCase.Pending != "" {
				if agrees {
					test.Fatalf("pending on %s, yet the reader now agrees with upstream: unmark the case", testCase.Pending)
				}
				return
			}
			if !agrees {
				test.Errorf("the reader disagrees with better-tailwindcss %s\n%s", oracle.Plugin, diffLines(want, got))
			}
		})
	}
}

// classLiteralCase is one case of cases.json.
type classLiteralCase struct {
	Name     string          `json:"name"`
	Options  json.RawMessage `json:"options"`
	Settings json.RawMessage `json:"settings"`
	Code     []string        `json:"code"`
	Pending  string          `json:"pending"`
}

// classLiteralOracle is upstream.json.
type classLiteralOracle struct {
	Plugin string `json:"plugin"`
	Cases  []struct {
		Name     string `json:"name"`
		Literals []struct {
			Start             int  `json:"start"`
			End               int  `json:"end"`
			ConcatenatedLeft  bool `json:"concatenatedLeft"`
			ConcatenatedRight bool `json:"concatenatedRight"`
		} `json:"literals"`
	} `json:"cases"`
}

func readClassLiteralCases(test *testing.T) []classLiteralCase {
	test.Helper()
	raw, readError := os.ReadFile(filepath.Join("testdata", "class_literals", "cases.json"))
	if readError != nil {
		test.Fatal(readError)
	}
	var cases []classLiteralCase
	if decodeError := json.Unmarshal(raw, &cases); decodeError != nil {
		test.Fatal(decodeError)
	}
	return cases
}

func readClassLiteralOracle(test *testing.T) classLiteralOracle {
	test.Helper()
	raw, readError := os.ReadFile(filepath.Join("testdata", "class_literals", "upstream.json"))
	if readError != nil {
		test.Fatal(readError)
	}
	var oracle classLiteralOracle
	if decodeError := json.Unmarshal(raw, &oracle); decodeError != nil {
		test.Fatal(decodeError)
	}
	return oracle
}

// upstreamLiteral is one literal as upstream reports it: its range, delimiters included, and its
// concatenation flags.
type upstreamLiteral struct {
	start, end                          int
	concatenatedLeft, concatenatedRight bool
}

// describe is one line naming the literal by its source text, so a disagreement reads as the strings
// that differ.
func (literal upstreamLiteral) describe(code string) string {
	flags := ""
	if literal.concatenatedLeft {
		flags += " concatenated-left"
	}
	if literal.concatenatedRight {
		flags += " concatenated-right"
	}
	return fmt.Sprintf("%d-%d %s%s", literal.start, literal.end, code[literal.start:literal.end], flags)
}

// readEveryClassLiteral parses the code and asks the reader about every node, returning what it read
// as upstream would report it, in source order.
func readEveryClassLiteral(test *testing.T, reader *ClassLiteralReader, code string) []string {
	test.Helper()
	fileName := tspath.NormalizePath("/Case.tsx")
	sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: tspath.RootedFilePath(fileName), PathKey: tspath.PathKey(fileName),
	}, code, core.ScriptKindTSX)
	if sourceFile == nil {
		test.Fatal("could not parse the case")
	}

	seen := map[[2]int]upstreamLiteral{}
	record := func(node *ast.Node, concatenated classValueEdges) {
		tokenRange := rule.TokenRange(sourceFile, node)
		key := [2]int{tokenRange.Pos(), tokenRange.End()}
		seen[key] = upstreamLiteral{key[0], key[1], concatenated.Leading, concatenated.Trailing}
	}
	var walk func(node *ast.Node)
	walk = func(node *ast.Node) {
		values := reader.readClassValues(node)
		for _, literal := range values.literals {
			record(literal.Node, literal.Concatenated)
		}
		for _, template := range values.templates {
			expression := template.node.AsTemplateExpression()
			record(expression.Head, template.concatenated)
			for _, span := range expression.TemplateSpans.Nodes {
				record(span.AsTemplateSpan().Literal, template.concatenated)
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return false
		})
	}
	walk(sourceFile.AsNode())

	literals := make([]upstreamLiteral, 0, len(seen))
	for _, literal := range seen {
		literals = append(literals, literal)
	}
	slices.SortFunc(literals, func(left, right upstreamLiteral) int {
		if left.start != right.start {
			return left.start - right.start
		}
		return left.end - right.end
	})
	described := make([]string, 0, len(literals))
	for _, literal := range literals {
		described = append(described, literal.describe(code))
	}
	return described
}

// diffLines lists what only upstream read and what only the reader read.
func diffLines(want []string, got []string) string {
	var builder strings.Builder
	for _, line := range want {
		if !slices.Contains(got, line) {
			builder.WriteString("  only upstream: " + line + "\n")
		}
	}
	for _, line := range got {
		if !slices.Contains(want, line) {
			builder.WriteString("  only cohere:   " + line + "\n")
		}
	}
	return builder.String()
}
