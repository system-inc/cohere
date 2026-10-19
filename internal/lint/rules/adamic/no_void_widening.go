package adamic

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	type_checking "github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/checking/flow"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

var voidWideningText = policy.MessageOf("adamic/no-void-widening", "voidWidening")

// NoVoidWidening refuses reading a hidden return value through a type that promises
// something about it (#cs0p22v). A void-typed function may return any value: assigning
// () => 'x' to () => void is legal. Relating that function to () => number | void
// lets a truthiness check expose the string as a number. The fixtures compile under
// strict tsc and throw in Node. The same hole survives containers and Promise.resolve.
//
// unknown exposes no unchecked value and is deliberately left to Adamic's compiler.
// Direct void-to-{} is already rejected by tsc. Value-to-void remains legal.
var NoVoidWidening = rule.Rule{
	Name:             "adamic/no-void-widening",
	NeedsTypeChecker: true,
	ProgramReads:     rule.ReadsCompilerOptions | rule.ReadsDefaultLibrary,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.TypeChecker == nil || ctx.SourceFile == nil {
			return nil
		}
		tc := ctx.TypeChecker
		walker := flow.WalkerFor(ctx)
		proof := voidBodyProof{checker: tc, empty: make(map[*ast.Node]bool)}
		var sourceNode *ast.Node
		var sourceRoot *checker.Type
		judge := func(pair flow.Pair) (bool, bool) {
			// Judge the whole callable union before the walker chooses a safe member.
			// Its call result includes the value-returning member even when the value
			// itself entered through the void-returning member.
			source := checker.Checker_getSignaturesOfType(tc, pair.Source, checker.SignatureKindCall)
			target := checker.Checker_getSignaturesOfType(tc, pair.Target, checker.SignatureKindCall)
			if len(source) == 1 && len(target) == 1 &&
				checker.Checker_getReturnTypeOfSignature(tc, source[0]).Flags()&checker.TypeFlagsVoid != 0 &&
				readsVoidValue(tc, checker.Checker_getReturnTypeOfSignature(tc, target[0])) &&
				!proof.signatureEmpty(source[0]) {
				return true, false
			}
			// Pair the promise's value directly, rather than descending into then's
			// generic callback signatures. Promise<void> may resolve to the hidden value.
			if isVoidPromise(pair.Source) && isVoidPromise(pair.Target) && pair.Source.Target() == pair.Target.Target() {
				from := checker.Checker_getTypeArguments(tc, pair.Source)
				to := checker.Checker_getTypeArguments(tc, pair.Target)
				return len(from) == 1 && len(to) == 1 && from[0].Flags()&checker.TypeFlagsVoid != 0 && readsVoidValue(tc, to[0]) &&
					!proof.promiseOriginEmpty(sourceNode, sourceRoot, pair.Path), false
			}
			return false, true
		}
		return walker.Listeners(func(site flow.Site) {
			sourceNode, sourceRoot = site.Node, site.Source
			found, wrong := walker.Walk(site, judge)
			if !wrong {
				return
			}
			ctx.ReportNode(site.Node, rule.Message{Id: "voidWidening", Description: voidWideningText.Render(map[string]string{
				"slot":   slotText(ctx.SourceFile, site.Node, found.Path),
				"source": type_checking.StableTypeText(tc, found.Source),
				"target": type_checking.StableTypeText(tc, found.Target),
			})})
		})
	},
}

// Only any or a union exposing a readable value is this hole. In particular,
// unknown requires a check, and {} | void promises nothing about a truthy value.
func readsVoidValue(tc *checker.Checker, target *checker.Type) bool {
	if target.Flags()&checker.TypeFlagsAny != 0 {
		return true
	}
	if target.Flags()&checker.TypeFlagsUnion == 0 {
		return false
	}
	for _, member := range target.Types() {
		if member.Flags()&(checker.TypeFlagsStringLike|checker.TypeFlagsNumberLike|checker.TypeFlagsBooleanLike|checker.TypeFlagsBigIntLike|checker.TypeFlagsESSymbolLike|checker.TypeFlagsNonPrimitive) != 0 {
			return true
		}
		if member.Flags()&checker.TypeFlagsObject != 0 &&
			(len(checker.Checker_getPropertiesOfType(tc, member)) != 0 || len(checker.Checker_getSignaturesOfType(tc, member, checker.SignatureKindCall)) != 0) {
			return true
		}
	}
	return false
}

// Like flow's library containers, Promise is recognized in declaration files;
// a local class with an unused type argument merely wearing the name is not one.
func isVoidPromise(t *checker.Type) bool {
	if t.ObjectFlags()&checker.ObjectFlagsReference == 0 || t.Target() == nil ||
		t.Target().Symbol() == nil || t.Target().Symbol().Name != "Promise" {
		return false
	}
	for _, declaration := range t.Target().Symbol().Declarations {
		if sourceFile := ast.GetSourceFileOfNode(declaration); sourceFile != nil && sourceFile.IsDeclarationFile {
			return true
		}
	}
	return false
}

// A signature from a type can have hidden a value in a value-to-void relation.
// A body proves its own result, provided every return is provably empty. Merely
// having a body is insufficient: `return hidden()` may forward a hidden value
// from a void-typed function. Cache by declaration, including an in-progress false
// answer so recursive forwarding stays conservative.
type voidBodyProof struct {
	checker *checker.Checker
	empty   map[*ast.Node]bool
}

func (p *voidBodyProof) signatureEmpty(signature *checker.Signature) bool {
	if signature == nil {
		return false
	}
	declaration := signature.Declaration()
	if declaration == nil || !ast.IsFunctionLike(declaration) || declaration.Body() == nil {
		return false
	}
	if empty, seen := p.empty[declaration]; seen {
		return empty
	}
	p.empty[declaration] = false
	body := declaration.Body()
	empty := true
	if !ast.IsBlock(body) {
		empty = p.expressionEmpty(body, 0)
	} else {
		ast.ForEachReturnStatement(body, func(statement *ast.Node) bool {
			expression := statement.AsReturnStatement().Expression
			if expression != nil && !p.expressionEmpty(expression, 0) {
				empty = false
			}
			return !empty
		})
	}
	p.empty[declaration] = empty
	return empty
}

func (p *voidBodyProof) expressionEmpty(expression *ast.Node, depth int) bool {
	if expression == nil || depth > 8 {
		return false
	}
	expression = ast.SkipParentheses(expression)
	if expression.Kind == ast.KindVoidExpression {
		return true
	}
	own := p.checker.GetTypeAtLocation(expression)
	if own != nil && own.Flags()&(checker.TypeFlagsUndefined|checker.TypeFlagsNever) != 0 {
		return true
	}
	switch expression.Kind {
	case ast.KindCallExpression:
		return p.signatureEmpty(p.checker.GetResolvedSignature(expression))
	case ast.KindAwaitExpression:
		return p.expressionEmpty(expression.AsAwaitExpression().Expression, depth+1)
	case ast.KindIdentifier:
		// Only an inferred const retains its initializer's provenance. An annotation
		// could already have erased a value, and a mutable binding could be replaced.
		symbol := p.checker.GetSymbolAtLocation(expression)
		if symbol == nil || symbol.ValueDeclaration == nil || symbol.ValueDeclaration.Kind != ast.KindVariableDeclaration {
			return false
		}
		declaration := symbol.ValueDeclaration
		variable := declaration.AsVariableDeclaration()
		return declaration.Parent != nil && declaration.Parent.Flags&ast.NodeFlagsConst != 0 && variable.Type == nil &&
			p.expressionEmpty(variable.Initializer, depth+1)
	}
	return false
}

// A Promise value's type alone does not remember its producer. At the top use
// the expression's resolved signature; for a function's return use the source
// signature at that path. Container steps preserve that origin. Contravariant
// parameter paths and unresolved signatures remain conservative.
func (p *voidBodyProof) promiseOriginEmpty(node *ast.Node, source *checker.Type, path []flow.Step) bool {
	if len(path) == 0 {
		return p.expressionEmpty(node, 0)
	}
	for index, step := range path {
		if source == nil {
			return false
		}
		switch step.Kind {
		case flow.StepReturn:
			signatures := checker.Checker_getSignaturesOfType(p.checker, source, checker.SignatureKindCall)
			if len(signatures) != 1 {
				return false
			}
			if index == len(path)-1 {
				return p.signatureEmpty(signatures[0])
			}
			source = checker.Checker_getReturnTypeOfSignature(p.checker, signatures[0])
		case flow.StepProperty:
			property := checker.Checker_getPropertyOfType(p.checker, source, step.Name)
			if property == nil {
				return false
			}
			source = checker.Checker_getTypeOfSymbol(p.checker, property)
		case flow.StepElement, flow.StepTypeArgument:
			// The walker may have selected a union member or expanded a constraint.
			// Its original root then has no reference arguments we can safely inspect.
			if source.Flags()&checker.TypeFlagsObject == 0 || source.ObjectFlags()&checker.ObjectFlagsReference == 0 {
				return false
			}
			arguments := checker.Checker_getTypeArguments(p.checker, source)
			argument := step.Index
			if argument < 0 {
				argument = 0
			}
			if argument >= len(arguments) {
				return false
			}
			source = arguments[argument]
		default:
			return false
		}
	}
	return false
}
