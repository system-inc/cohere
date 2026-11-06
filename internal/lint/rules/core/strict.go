package core

import (
	"encoding/json"
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// StrictMode selects which spelling of strict mode the rule asks for.
//
// The values are upstream's own spellings, because the option is upstream's bare string and the
// decoder reads it straight into this type. A config written for ESLint reads the same here.
type StrictMode string

const (
	// StrictSafe picks Global or Function by what the file is. The default.
	//
	// Upstream resolves it to Global for a CommonJS file or one allowing a top-level return, and to
	// Function otherwise. In this tree it resolves to Function for every script, because nothing
	// here is parsed as CommonJS.
	StrictSafe StrictMode = "safe"

	// StrictGlobal wants exactly one directive at the top of the file and none inside a function.
	StrictGlobal StrictMode = "global"

	// StrictFunction wants a directive at the top of each top-level function and none at the file
	// level. This is the only mode that tracks nesting, because a directive is redundant inside a
	// function whose parent is already strict.
	StrictFunction StrictMode = "function"

	// StrictNever wants no directive anywhere.
	StrictNever StrictMode = "never"
)

// DecodeStrictOptions reads this rule's one option element, upstream's bare mode string.
//
// Upstream's schema is `[{ enum: ["never", "global", "function", "safe"] }]`, so `["error", "never"]`
// is the whole configuration and the element is a string, never an object. An earlier decoder here
// read it from a `{"mode": ...}` object no ESLint version accepts, which refused upstream's own form
// and stopped a run on a config copied from a real repository. The object is refused now, with the
// error naming the shape that works.
//
// Hand-rolled so an unrecognized mode fails loudly. Every arm here is selected by string equality,
// so an unknown string left in the mode would pick a silent fifth behaviour of reporting nothing at
// all. No element at all is upstream's default, Safe.
func DecodeStrictOptions(raw []byte) (StrictMode, error) {
	if len(raw) == 0 {
		return StrictSafe, nil
	}
	var mode StrictMode
	if decodeError := json.Unmarshal(raw, &mode); decodeError != nil {
		return StrictSafe, fmt.Errorf(
			"strict: the option is a bare string, one of \"never\", \"global\", \"function\" or "+
				"\"safe\", got %s", raw)
	}
	switch mode {
	case StrictSafe, StrictGlobal, StrictFunction, StrictNever:
	default:
		return StrictSafe, fmt.Errorf(
			"strict: unknown mode %q, wanted one of \"never\", \"global\", \"function\" or \"safe\"", mode)
	}
	return mode, nil
}

// The messages. Upstream carries ten ids for what reads like one judgment, and the split is not
// decorative: each names a different reason the directive is wrong, and three of them are conditions
// the author cannot fix by moving the directive.
var (
	messageStrictModule = rule.Message{
		Id: "module",
		Description: "This file is a module, and every module is already strict. The directive " +
			"does nothing here, so a reader has to check the file's imports to learn that it is " +
			"redundant rather than load-bearing. Delete it.",
	}
	messageStrictImplied = rule.Message{
		Id: "implied",
		Description: "The compiler already makes this file strict: `alwaysStrict`, which `strict` " +
			"turns on, has TypeScript check it as strict code and emit the directive itself. This " +
			"one does nothing, so a reader has to check the tsconfig to learn that it is " +
			"redundant rather than load-bearing. Delete it.",
	}
	messageStrictNever = rule.Message{
		Id: "never",
		Description: "This directive asks for strict mode, which the configuration does not " +
			"permit. Delete it, or change the configuration if strict mode is what this file " +
			"wants.",
	}
	messageStrictGlobal = rule.Message{
		Id: "global",
		Description: "This file has no `use strict` directive at the top, and the configuration " +
			"asks for the global form. Without it the file runs in sloppy mode, where an " +
			"assignment to an undeclared name silently creates a global. Add the directive as " +
			"the first statement.",
	}
	messageStrictFunction = rule.Message{
		Id: "function",
		Description: "This function has no `use strict` directive, and the configuration asks " +
			"for the function form. Without it the body runs in sloppy mode, where `this` falls " +
			"back to the global object and an assignment to an undeclared name creates a global. " +
			"Add the directive as the function's first statement.",
	}
	messageStrictMultiple = rule.Message{
		Id: "multiple",
		Description: "This is a second `use strict` directive in the same prologue. Only the " +
			"first has any effect, so the rest read as though they do something and do not. " +
			"Delete the duplicates.",
	}
	messageStrictUnnecessary = rule.Message{
		Id: "unnecessary",
		Description: "This function is already inside strict-mode code, so the directive changes " +
			"nothing. A redundant directive suggests the author was unsure which parts of the " +
			"file are strict. Delete it.",
	}
	messageStrictUnnecessaryInClasses = rule.Message{
		Id: "unnecessaryInClasses",
		Description: "A class body is always strict, so a directive inside a method changes " +
			"nothing and suggests the author did not know that. Delete it.",
	}
	messageStrictNonSimpleParameterList = rule.Message{
		Id: "nonSimpleParameterList",
		Description: "A `use strict` directive is a syntax error since ES2016 in a function whose " +
			"parameter list uses a default, a rest element or destructuring. This does not run at " +
			"all. Delete the directive, or make every parameter a plain name.",
	}
)

// messageStrictWrap is built per finding, because it names the function.
func messageStrictWrapFor(description string) rule.Message {
	return rule.Message{
		Id: "wrap",
		Description: fmt.Sprintf("The configuration asks for the function form of `use strict`, "+
			"and %s cannot carry a directive because its parameter list uses a default, a rest "+
			"element or destructuring, which makes the directive a syntax error since ES2016. "+
			"Wrap it in a function that does carry one.", description),
	}
}

// Strict enforces one spelling of the `use strict` directive.
//
//	valid:   export const a = 1;                       // a module is already strict
//	valid:   'use strict'; function f() {}             // under Global, in a script
//	invalid: 'use strict'; export const a = 1;         // a module, so the directive is redundant
//	invalid: const a = 1;                              // under Global, in a script
//	invalid: 'use strict'; 'use strict';               // only the first does anything
//
// # A module collapses every mode into one
//
// Upstream's first act is `if (node.sourceType === "module") mode = "module"`, which overrides
// whatever was configured. So in a module the rule has exactly one thing to say: the directive is
// redundant, delete it. Every other mode, and the whole nesting apparatus below, applies only to a
// script.
//
// That matters more here than upstream, because this tree is essentially all modules: measured,
// 2,917 of 2,919 TypeScript files carry a top-level import or export, and the two that do not are
// still compiled as modules. So the mode option is nearly inert in practice and the rule is, in
// effect, a guard against somebody pasting a directive into a module.
//
// The script arms are ported in full anyway. They are not dead code -- a `.js` or `.cjs` file reaches
// them, and so does a TypeScript file under a tsconfig that turns `alwaysStrict` off -- and
// reproducing only the arm this tree exercises would be a port of the configuration rather than of
// the rule.
//
// # A TypeScript file is strict whatever its shape
//
// Upstream's other override is `ecmaFeatures.impliedStrict`, a parser flag saying the code will run
// strict however it is written, which selects the `implied` mode: every directive is redundant and
// nothing is asked for. The TypeScript compiler's `alwaysStrict` is that flag. Under it the checker
// binds every TypeScript file as strict code, and the emit writes the directive into any output that
// is not already ESM. `strict` turns it on, and TypeScript 6 turns `strict` on by default, so an
// unset tsconfig gets it. The transpilers that read a tsconfig (esbuild, ts-jest) honor it too.
//
// Without this, a test file with no import or export, which is a script by TypeScript's module
// detection, was asked for a directive in every top-level function. That is api's Base
// ProjectSecrets.test.ts, where ESLint was silent because its flat config parses every `.ts` file as
// a module (#hks3djf). ESLint's silence was right for a different reason. The file is strict because
// the compiler says so, not because it is a module, so here it reads `implied` rather than `module`.
//
// A JavaScript file is left on the script arms. `alwaysStrict` governs what the compiler emits, and a
// `.js` or `.cjs` file in these repositories is run as written rather than emitted, so a sloppy one
// really does run sloppy.
//
// # Three findings the author cannot fix by moving the directive
//
// `nonSimpleParameterList` is the sharpest: since ES2016 a directive inside a function with a
// default, rest or destructured parameter is a syntax error, so that code does not run at all.
// Upstream reports it and deliberately offers no repair, because the fix is a judgment about which
// half to change. `wrap` is its counterpart under the function form, where the function cannot be
// made strict without restructuring it.
//
// # The repair only ever deletes a whole statement
//
// Every fix this rule offers removes one directive statement, and never rebuilds a span. That is
// what makes it safe on TypeScript: a parameter annotation, a return type and a generic list all sit
// outside the statement being deleted and cannot be caught in it. Measured against the installed
// rule on annotated functions, which its own JavaScript corpus cannot contain.
//
// Five of the ten findings carry that repair and five do not, and the split is upstream's
// `shouldFix`: a directive that is merely redundant is removed, while one whose presence is a
// symptom of something else is reported and left alone.
var Strict = rule.Rule[StrictMode]{
	Name: "strict",
	// `alwaysStrict` decides whether a TypeScript script is strict, so the cached verdict has to
	// follow the tsconfig.
	ProgramReads: rule.ReadsCompilerOptions,
	Run: func(ruleContext rule.Context, options rule.Given[StrictMode]) rule.Listeners {
		mode := StrictSafe
		if decoded, configured := options.Read(); configured {
			mode = decoded
		}
		return rule.Listeners{
			ast.KindSourceFile: func(file *ast.Node) {
				checkStrict(ruleContext, file, mode)
			},
		}
	},
}

// strictModeModule is the mode a module collapses to. Not one of the configurable four, which is
// why it is a package constant rather than a StrictMode the decoder accepts.
const strictModeModule StrictMode = "module"

// strictModeImplied is the mode a TypeScript file collapses to when the compiler makes it strict. Like
// strictModeModule it is selected by the file rather than configured, which is upstream's shape too:
// `impliedStrict` is a parser feature, and `implied` is not one of the option's four values.
const strictModeImplied StrictMode = "implied"

// checkStrict resolves the mode for this file and walks it.
func checkStrict(ruleContext rule.Context, file *ast.Node, mode StrictMode) {
	source := file.AsSourceFile()
	resolvedMode := mode

	if type_checking.CompilerImpliesStrict(ruleContext, source) {
		resolvedMode = strictModeImplied
	} else if resolvedMode == StrictSafe {
		// Upstream picks Global when the file is CommonJS or the parser allows a top-level
		// return, and Function otherwise.
		//
		// Only the first half is expressible. `ecmaFeatures.globalReturn` is a parser feature
		// with no counterpart in cohere, so the second half can never fire here -- and it is not
		// a detail: seventeen of upstream's cases turn on it, and the same source under the same
		// options is clean with it and reports twice without it. Those cases are recorded as
		// unimportable in the test rather than being approximated.
		//
		// `CommonJSModuleIndicator` is what our parser sets for a file using `require` or
		// `module.exports`. Measured: it stays nil for `module.exports = 1` in a `.ts` file, so
		// even this half is narrower here than upstream, and Safe resolves to Function for
		// effectively every script this tree can produce.
		if source.CommonJSModuleIndicator != nil {
			resolvedMode = StrictGlobal
		} else {
			resolvedMode = StrictFunction
		}
	}
	// A module overrides everything, including an explicitly configured mode. This is upstream's
	// first act inside its Program listener and it is why the option is nearly inert in this tree.
	if ast.IsExternalModule(source) {
		resolvedMode = strictModeModule
	}

	walker := strictWalker{ruleContext: ruleContext, mode: resolvedMode}
	statements := source.Statements.Nodes
	directives := strictDirectivesIn(ruleContext, statements)

	if resolvedMode == StrictGlobal {
		// The global form wants exactly one directive at the top. A file with statements and no
		// directive is reported once, on the span covering every statement, which is upstream's
		// `loc` spanning the first through the last.
		if len(statements) > 0 && len(directives) == 0 {
			span := rule.TokenRange(ruleContext.SourceFile, statements[0])
			last := statements[len(statements)-1]
			ruleContext.ReportRange(core.NewTextRange(span.Pos(), last.End()), messageStrictGlobal)
		}
		walker.reportAllExceptFirst(directives, messageStrictMultiple, true)
	} else {
		walker.reportAll(directives, walker.fileLevelMessage(), strictShouldFix(resolvedMode))
	}

	// The function arms. Under the function form the walk carries whether the enclosing scope is
	// already strict and whether we are inside a class body; every other mode ignores both.
	walker.walkStatements(statements, false, false, false)
}

// strictShouldFix is upstream's `shouldFix`: which findings carry a repair.
//
// A directive that is merely redundant is removed. One whose presence is a symptom of a different
// problem -- the wrong form for the configuration -- is reported and left alone, because the repair
// is a judgment about what the author meant rather than a deletion.
func strictShouldFix(mode StrictMode) bool {
	return mode == strictModeModule || mode == strictModeImplied
}

// strictRemoveDirective builds the repair, which deletes one whole directive statement.
//
// The span is the statement's own token range, so nothing outside it can be caught in the deletion.
// That is the whole reason this fixer is safe on TypeScript: a parameter annotation, a return type
// and a generic list all live in the function's signature, which is a different node entirely.
func strictRemoveDirective(ruleContext rule.Context, directive *ast.Node) rule.Fix {
	return rule.RemoveRange(rule.TokenRange(ruleContext.SourceFile, directive))
}

// strictDirectivesIn returns the `use strict` directives at the head of a statement list.
//
// Upstream reads the directive prologue, which is the run of expression statements whose expression
// is a string literal, starting at the first statement. The scan stops at the first statement that
// is not one, so a directive after any real code is not a directive at all -- it is a string
// expression, and the engine ignores it.
//
// Only `use strict` counts. Another directive such as `use asm` occupies a prologue slot and is not
// this rule's business, and upstream's value comparison skips it the same way.
func strictDirectivesIn(ruleContext rule.Context, statements []*ast.Node) []*ast.Node {
	var directives []*ast.Node
	for _, statement := range statements {
		if statement.Kind != ast.KindExpressionStatement {
			break
		}
		expression := statement.AsExpressionStatement().Expression
		if expression == nil || expression.Kind != ast.KindStringLiteral {
			break
		}
		if expression.Text() != "use strict" {
			// A different prologue directive, such as `use asm`. Upstream writes into a sparse
			// array here -- `directives[i] = statement`, indexed by the statement's own position
			// -- and skips the assignment for anything that is not `use strict`. Its later
			// `forEach` and `slice` then skip the holes, so a `use strict` sitting after another
			// directive is never reported at all.
			//
			// Measured against the installed rule: `'use asm'; 'use strict';` is clean and
			// `'use strict'; 'use asm';` reports. That is an upstream defect rather than a
			// judgment -- the directive is equally redundant either way round -- and it is
			// reproduced rather than corrected, because the corpus is the specification and a
			// port that quietly reported the case would disagree with the tool it replaces.
			//
			// Reproduced by stopping here rather than by continuing, which is what makes the
			// dense slice below equivalent to upstream's sparse one: once a hole exists, nothing
			// after it can be reported.
			break
		}
		directives = append(directives, statement)
	}
	return directives
}

// strictHasSimpleParameterList answers whether every parameter is a plain name.
//
// Since ES2016 a `use strict` directive is a syntax error in a function whose parameter list uses a
// default, a rest element or destructuring, so this is what separates a redundant directive from
// one that stops the file running. A TypeScript annotation does not make a parameter non-simple:
// `function f(a: string)` is still a plain name with a type on it, which is a distinction upstream's
// JavaScript corpus cannot express and which is measured here rather than assumed.
func strictHasSimpleParameterList(node *ast.Node) bool {
	for _, parameter := range strictParametersOf(node) {
		declaration := parameter.AsParameterDeclaration()
		if declaration.Initializer != nil || declaration.DotDotDotToken != nil {
			return false
		}
		if name := declaration.Name(); name == nil || name.Kind != ast.KindIdentifier {
			return false
		}
	}
	return true
}

// strictParametersOf returns a function-like node's parameters.
func strictParametersOf(node *ast.Node) []*ast.Node {
	if parameters := node.Parameters(); parameters != nil {
		return parameters
	}
	return nil
}

// strictFunctionBody returns a function-like node's body, or nil.
func strictFunctionBody(node *ast.Node) *ast.Node {
	return node.Body()
}

// strictDescribeFunction names a function for the `wrap` message.
//
// Upstream uses `getFunctionNameWithKind`, which renders `function 'foo'` or `arrow function`. The
// same shape is produced here from the node's own name, and an anonymous function is described by
// its kind alone.
func strictDescribeFunction(ruleContext rule.Context, node *ast.Node) string {
	kind := "function"
	if node.Kind == ast.KindArrowFunction {
		kind = "arrow function"
	}
	if name := node.Name(); name != nil && name.Kind == ast.KindIdentifier {
		return fmt.Sprintf("%s `%s`", kind, name.Text())
	}
	return "this " + kind
}
