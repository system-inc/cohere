package rule

import (
	"go/ast"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// plantedAnalyzer is an analyzer that finds nothing, for rules that need one to be valid.
var plantedAnalyzer = &analysis.Analyzer{
	Name: "planted",
	Doc:  "planted finds nothing; it exists so a rule under test has an analyzer",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, nil
	},
}

// optionedAnalyzer finds nothing and has one option, readsFile, for the rules that are configured by one.
var optionedAnalyzer = func() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name: "optioned",
		Doc:  "optioned finds nothing; it exists so a rule under test has an analyzer with an option",
		Run: func(pass *analysis.Pass) (any, error) {
			return nil, nil
		},
	}
	analyzer.Flags.String("readsFile", "", "the file a rule under test would read")
	return analyzer
}()

// The planted callbacks exist so a listener under test declares something, and are never called.
func plantedFunction(*Context, *intermediate_representation.Function) {
	// Never called: Validate reads only whether a callback is set.
}

func plantedInstruction(*Context, intermediate_representation.Instruction) {
	// Never called: Validate reads only whether a callback is set.
}

func plantedNode(*Context, ast.Node, []ast.Node) {
	// Never called: Validate reads only whether a callback is set.
}

func plantedFile(*Context, *ast.File) {
	// Never called: Validate reads only whether a callback is set.
}

func plantedPackageExit(*Context) {
	// Never called: Validate reads only whether a callback is set.
}

// listening is a well-formed rule but for listener, which is what a refusal of it names.
func listening(listener Listener) Rule {
	held := listener
	return Rule{
		Name:         "cohere-go/nilness",
		Origin:       OriginTools,
		UpstreamName: "nilness",
		Analyzer:     plantedAnalyzer,
		Listener:     &held,
	}
}

// TestValidateAcceptsAWellFormedRule is the control for the refusals below: a house rule and each kind of
// ported rule, written the way the registry will hold them.
func TestValidateAcceptsAWellFormedRule(test *testing.T) {
	test.Parallel()
	cases := []Rule{
		{
			Name:      "cohere-go/performance-require-parallel-test",
			Origin:    OriginHouse,
			TypeAware: true,
			Analyzer:  plantedAnalyzer,
		},
		{
			Name:         "cohere-go/printf",
			Origin:       OriginVet,
			UpstreamName: "printf",
			TypeAware:    true,
			Analyzer:     plantedAnalyzer,
		},
		{Name: "cohere-go/SA4006", Origin: OriginStaticcheck, UpstreamName: "SA4006", Analyzer: plantedAnalyzer},
		{
			Name:         "cohere-go/nilness",
			Origin:       OriginTools,
			UpstreamName: "nilness",
			TypeAware:    true,
			Analyzer:     plantedAnalyzer,
		},
		{
			Name:         "cohere-go/correctness-require-declared-walk",
			Origin:       OriginHouse,
			Analyzer:     optionedAnalyzer,
			ConfiguredBy: []string{"readsFile"},
		},
		{
			Name:         "cohere-go/nilness",
			Origin:       OriginTools,
			UpstreamName: "nilness",
			Analyzer:     plantedAnalyzer,
			Listener: &Listener{
				IntermediateRepresentation: &IntermediateRepresentationListener{Function: plantedFunction},
			},
		},
		{
			Name:         "cohere-go/unusedwrite",
			Origin:       OriginTools,
			UpstreamName: "unusedwrite",
			Analyzer:     plantedAnalyzer,
			Listener: &Listener{
				IntermediateRepresentation: &IntermediateRepresentationListener{
					Instructions: []intermediate_representation.Instruction{
						(*intermediate_representation.Store)(nil), (*intermediate_representation.Call)(nil),
					},
					Instruction: plantedInstruction,
				},
				PackageExit: plantedPackageExit,
			},
		},
		{
			Name:         "cohere-go/SA5000",
			Origin:       OriginStaticcheck,
			UpstreamName: "SA5000",
			Analyzer:     plantedAnalyzer,
			Listener: &Listener{
				IntermediateRepresentation: &IntermediateRepresentationListener{
					Instructions: []intermediate_representation.Instruction{
						(*intermediate_representation.MapUpdate)(nil),
					},
					Instruction: plantedInstruction,
				},
				PackageExit: plantedPackageExit,
			},
		},
		{
			Name:     "cohere-go/consistency-no-label",
			Origin:   OriginHouse,
			Analyzer: plantedAnalyzer,
			Listener: &Listener{Nodes: []ast.Node{(*ast.LabeledStmt)(nil)}, Node: plantedNode},
		},
		{
			Name:         "cohere-go/U1000",
			Origin:       OriginStaticcheck,
			UpstreamName: "U1000",
			Analyzer:     plantedAnalyzer,
			Listener:     &Listener{FileEnter: plantedFile, PackageExit: plantedPackageExit},
		},
	}
	for _, testCase := range cases {
		if validateError := testCase.Validate(); validateError != nil {
			test.Errorf("%s: refused a well-formed rule: %v", testCase.Name, validateError)
		}
	}
}

// TestValidateRefusesEachMalformedRule plants one defect per case and expects the refusal that names it.
func TestValidateRefusesEachMalformedRule(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name   string
		rule   Rule
		saying string
	}{
		{
			name:   "an empty name",
			rule:   Rule{Origin: OriginHouse, Analyzer: plantedAnalyzer},
			saying: "has no name",
		},
		{
			name:   "a name outside the namespace",
			rule:   Rule{Name: "cohere-swift/force-unwrapping", Origin: OriginHouse, Analyzer: plantedAnalyzer},
			saying: "does not start with",
		},
		{
			name:   "a bare upstream name",
			rule:   Rule{Name: "printf", Origin: OriginVet, UpstreamName: "printf", Analyzer: plantedAnalyzer},
			saying: "does not start with",
		},
		{
			name:   "the namespace alone",
			rule:   Rule{Name: "cohere-go/", Origin: OriginHouse, Analyzer: plantedAnalyzer},
			saying: "the namespace alone",
		},
		{
			name:   "no analyzer",
			rule:   Rule{Name: "cohere-go/consistency-no-long-line", Origin: OriginHouse},
			saying: "has no analyzer",
		},
		{
			name:   "no origin",
			rule:   Rule{Name: "cohere-go/consistency-no-long-line", Analyzer: plantedAnalyzer},
			saying: "which is none of",
		},
		{
			name: "an unknown origin",
			rule: Rule{
				Name:         "cohere-go/force-unwrapping",
				Origin:       "swiftlint",
				UpstreamName: "force_unwrapping",
				Analyzer:     plantedAnalyzer,
			},
			saying: "which is none of",
		},
		{
			name: "an upstream name on a house rule",
			rule: Rule{
				Name:         "cohere-go/consistency-no-long-line",
				Origin:       OriginHouse,
				UpstreamName: "lll",
				Analyzer:     plantedAnalyzer,
			},
			saying: "a house rule has no upstream",
		},
		{
			name:   "a vet rule with no upstream name",
			rule:   Rule{Name: "cohere-go/printf", Origin: OriginVet, Analyzer: plantedAnalyzer},
			saying: "names no upstream rule",
		},
		{
			name:   "a staticcheck rule with no upstream name",
			rule:   Rule{Name: "cohere-go/SA4006", Origin: OriginStaticcheck, Analyzer: plantedAnalyzer},
			saying: "names no upstream rule",
		},
		{
			name:   "a tools rule with no upstream name",
			rule:   Rule{Name: "cohere-go/nilness", Origin: OriginTools, Analyzer: plantedAnalyzer},
			saying: "names no upstream rule",
		},
		{
			name:   "a listener that declares nothing",
			rule:   listening(Listener{Requires: []*analysis.Analyzer{plantedAnalyzer}}),
			saying: "declares no callback",
		},
		{
			name: "a listener that declares the engine's own walk and the syntax walk",
			rule: listening(Listener{FileEnter: plantedFile,
				IntermediateRepresentation: &IntermediateRepresentationListener{Function: plantedFunction}}),
			saying: "declares both the engine's own intermediate representation's walk and the syntax walk",
		},
		{
			name: "the engine's own walk with none of its callbacks",
			rule: listening(Listener{IntermediateRepresentation: &IntermediateRepresentationListener{},
				PackageExit: plantedPackageExit}),
			saying: "with none of its callbacks",
		},
		{
			name: "the engine's own instruction kinds with no callback",
			rule: listening(Listener{IntermediateRepresentation: &IntermediateRepresentationListener{
				Function:     plantedFunction,
				Instructions: []intermediate_representation.Instruction{(*intermediate_representation.Call)(nil)},
			}}),
			saying: "has no IntermediateRepresentation.Instruction to call for them",
		},
		{
			name: "the engine's own instruction kind listed twice",
			rule: listening(Listener{IntermediateRepresentation: &IntermediateRepresentationListener{
				Instruction: plantedInstruction,
				Instructions: []intermediate_representation.Instruction{
					(*intermediate_representation.Call)(nil), (*intermediate_representation.Call)(nil),
				},
			}}),
			saying: "twice in IntermediateRepresentation.Instructions",
		},
		{
			name: "a file-local listener with PackageExit",
			rule: listening(Listener{
				FileLocal:   true,
				FileEnter:   func(*Context, *ast.File) {},
				PackageExit: plantedPackageExit,
			}),
			saying: "is marked FileLocal and declares PackageExit",
		},
		{
			name:   "a listener with PackageExit alone",
			rule:   listening(Listener{PackageExit: plantedPackageExit}),
			saying: "declares PackageExit alone",
		},
		{
			name: "an instruction callback with no kinds",
			rule: listening(Listener{
				IntermediateRepresentation: &IntermediateRepresentationListener{Instruction: plantedInstruction},
			}),
			saying: "sets IntermediateRepresentation.Instruction and lists no instruction kinds",
		},
		{
			name:   "node kinds with no callback",
			rule:   listening(Listener{FileEnter: plantedFile, Nodes: []ast.Node{(*ast.CallExpr)(nil)}}),
			saying: "has no Node to call for them",
		},
		{
			name:   "a node callback with no kinds",
			rule:   listening(Listener{Node: plantedNode}),
			saying: "sets Node and lists no node kinds",
		},
		{
			name: "a bare nil kind",
			rule: listening(Listener{IntermediateRepresentation: &IntermediateRepresentationListener{
				Instruction: plantedInstruction, Instructions: []intermediate_representation.Instruction{nil},
			}}),
			saying: "lists a bare nil in IntermediateRepresentation.Instructions",
		},
		{
			name: "a node kind listed twice",
			rule: listening(
				Listener{Node: plantedNode, Nodes: []ast.Node{(*ast.CallExpr)(nil), (*ast.CallExpr)(nil)}},
			),
			saying: "twice in Nodes",
		},
		{
			name: "an option its analyzer has no flag for",
			rule: Rule{
				Name:         "cohere-go/correctness-require-declared-walk",
				Origin:       OriginHouse,
				Analyzer:     optionedAnalyzer,
				ConfiguredBy: []string{"readsFiles"},
			},
			saying: "has no flag for",
		},
	}
	for _, testCase := range cases {
		validateError := testCase.rule.Validate()
		if validateError == nil {
			test.Errorf("%s: accepted", testCase.name)
			continue
		}
		if !strings.Contains(validateError.Error(), testCase.saying) {
			test.Errorf("%s: refused with %q, expected it to say %q", testCase.name, validateError, testCase.saying)
		}
	}
}
