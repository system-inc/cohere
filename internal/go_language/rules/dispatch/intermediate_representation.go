package dispatch

import (
	"go/token"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/build_intermediate_representation"
)

// IntermediateRepresentation is the walk over cohere's own intermediate representation, which every reader that
// walked staticcheck's has moved onto (#g0rwdw1, #kk4864m). It calls the listeners registered on it that a package's
// pass starts (see start) over one walk of the package's source functions, in the order buildir's SrcFuncs lists
// them: for each function every listener's Function, then each instruction, in block order, to every listener whose
// Instructions list its kind, then every FunctionExit; and once the functions are walked, every PackageExit.
// Listeners are called in the order they were registered.
//
// The package is built whole by build_intermediate_representation, an action the walk requires, before it starts. In
// an extra build's package where a function stands (#e4rd5dw), that action leaves the building to its readers, a
// listener whose rule is confined on a file is called for no function standing there, and the walk builds the package
// less the functions that stand for every listener it started (see run).
type IntermediateRepresentation struct {
	*listeners
}

// NewIntermediateRepresentation is a dispatcher over cohere's own intermediate representation with no rule
// registered on it. Its analyzer requires build_intermediate_representation, and runs only where that does, on a
// package without errors.
func NewIntermediateRepresentation() *IntermediateRepresentation {
	dispatcher := &IntermediateRepresentation{}
	dispatcher.listeners = newListeners(
		"dispatch_intermediate_representation",
		"walks each package's functions in cohere's own intermediate representation once, calling every "+
			"listener registered on it, for the rule analyzers that report what each found",
		[]*analysis.Analyzer{build_intermediate_representation.Analyzer},
		false,
		dispatcher.run,
	)
	return dispatcher
}

// RuleAnalyzer registers listener, which must hear cohere's own intermediate representation's walk, as the rule
// named ruleName, and returns the rule's own analyzer, named analyzerName and documented by doc, which requires this
// dispatcher. A listener that reads Function.ValueForExpression has its rule's analyzer require the representation
// and its ExpressionReader mark as well, so the package records the expression map wherever the rule runs (see
// build_intermediate_representation.ExpressionReader).
func (dispatcher *IntermediateRepresentation) RuleAnalyzer(
	ruleName string,
	analyzerName string,
	doc string,
	listener *rule.Listener,
) *analysis.Analyzer {
	if !listener.IsIntermediateRepresentation() {
		// Invariant: the shared RuleAnalyzer hands every listener that is not syntax here, and a listener of neither
		// walk is one Validate refuses, so any here is a mistake in building the registry, never in a package.
		panic("rule " + ruleName + " registers a listener that does not hear cohere-go's own intermediate " +
			"representation on the dispatcher over it, which would never call it")
	}
	kinds := []reflect.Type{}
	for _, kind := range listener.IntermediateRepresentation.Instructions {
		kinds = append(kinds, reflect.TypeOf(kind))
	}
	analyzer := dispatcher.ruleAnalyzer(ruleName, analyzerName, doc, listener, kinds)
	if listener.IntermediateRepresentation.ReadsValueForExpression {
		analyzer.Requires = append(analyzer.Requires, build_intermediate_representation.Analyzer,
			build_intermediate_representation.ExpressionReader)
	}
	return analyzer
}

// run walks the package's source functions once, calling every started listener, each with its own context and its
// own recover, and returns each started rule's outcome. A package large enough to split (shardsOf) is walked by
// several runs at once: the listeners marked FileLocal by one run per shard of its files, each hearing the functions
// declared in its files, every other listener by one run over all of them (#0jnq91n). The package is built whole
// before any run starts, so a file-local listener reading another file's function finds it built.
//
// A run confined on a file hears no function standing there, whose findings are the host's (rule.StandingFiles), and
// a function standing for every started run is one nobody is called for, so the package is built without it, still
// before any run starts (#e4rd5dw). A listener that reads the blocks of a function it was not handed, a callee's, may
// then find one that was never built, so it must call that function's Build first, which builds a function once
// whoever asks: a rule is confined only when its findings in a function depend on that function alone. With a started
// run that is confined on no file, and in every pass nothing stands in, the package is built whole.
func (dispatcher *IntermediateRepresentation) run(pass *analysis.Pass) (any, error) {
	// Started first, which refuses a pass whose representation failed, so that failure is named as one.
	whole, parts, startError := dispatcher.start(pass, dispatcher.shardsOf(pass))
	if startError != nil {
		return nil, startError
	}
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	// With no started listener there is nothing to walk.
	if len(whole.runs) == 0 && (len(parts) == 0 || len(parts[0].runs) == 0) {
		return finishShards(whole, parts), nil
	}
	// Nil in a pass where no started run is confined, every host's, which is built and walked as it always was.
	standing := standingDeclarationsOf(whole, parts)
	var functions []*intermediate_representation.Function
	if standing == nil || len(standing.unheard) == 0 {
		functions = slices.Collect(built.SourceFunctions())
	} else {
		functions = slices.Collect(built.SourceFunctionsBut(standing.isUnheard))
	}
	if len(parts) == 0 {
		walkFunctions(whole, functions, standing)
		return finish(whole), nil
	}
	shardOf := shardOfFile(pass, len(parts))
	shardFunctions := make([][]*intermediate_representation.Function, len(parts))
	for _, function := range functions {
		// A function with no position, or one in a file the pass does not hold, such as the synthetic package
		// initializer, goes to the first shard, so every function is one shard's.
		shard := 0
		if function.Pos().IsValid() {
			shard = shardOf[pass.Fset.File(function.Pos())]
		}
		shardFunctions[shard] = append(shardFunctions[shard], function)
	}
	walks := []shardWalk{{started: whole, walk: func() { walkFunctions(whole, functions, standing) }}}
	for index, part := range parts {
		walks = append(walks, shardWalk{
			started: part,
			walk:    func() { walkFunctions(part, shardFunctions[index], standing) },
		})
	}
	walkConcurrently(walks)
	return finishShards(whole, parts), nil
}

// walkFunctions calls every one of started's listeners over functions: Function, each instruction in block order to
// the listeners whose Instructions list its kind, FunctionExit. A function that stands in a file (standing.fileOf)
// is heard only by the runs that are not confined on that file, which are found once for the file and called as
// started's own are for any other function. With standing nil no function is asked where it stands.
func walkFunctions(
	started *startedRuns,
	functions []*intermediate_representation.Function,
	standing *standingDeclarations,
) {
	if len(started.runs) == 0 {
		return
	}
	// Made by the first function found standing, so a walk that meets none allocates nothing for them.
	var hearingStanding map[*token.File]*startedRuns
	for _, function := range functions {
		hearing := started
		if standing != nil {
			if tokenFile := standing.fileOf(function); tokenFile != nil {
				if hearingStanding == nil {
					hearingStanding = map[*token.File]*startedRuns{}
				}
				if hearingStanding[tokenFile] == nil {
					hearingStanding[tokenFile] = started.notConfinedOn(tokenFile)
				}
				hearing = hearingStanding[tokenFile]
			}
		}
		for _, run := range hearing.runs {
			run.function(function)
		}
		// With no started listener hearing an instruction, the blocks are not walked at all.
		if len(hearing.byKind) > 0 {
			for _, block := range function.Blocks {
				for _, instruction := range block.Instructions {
					for _, run := range hearing.byKind[reflect.TypeOf(instruction)] {
						run.instruction(instruction)
					}
				}
			}
		}
		for _, run := range hearing.runs {
			run.functionExit(function)
		}
	}
}
