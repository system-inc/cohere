package canonical_function_hash

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
	"github.com/system-inc/cohere/static_single_assignment"
)

// loopPhi is a loop-carried phi, named provisionally by its header and index, with the back-edge operands that are
// hashed into its header once every block has been walked.
type loopPhi struct {
	provisional duplicate_functions.Digest
	backEdges   []high_level_intermediate_representation.PhiOperand
}

// effect appends one effect to a block's chain and returns its digest, which is also the value it produces, where it
// produces one: two calls of one function are two values.
//
// Mutates: memory the block's chain, which every effect after this one hashes with
func effect(builder *duplicate_functions.DigestBuilder, memory *duplicate_functions.Digest) duplicate_functions.Digest {
	result := builder.Digest(*memory).Sum()
	*memory = result
	return result
}

// singleSuccessor is the one real successor of a straight-line structural terminal, which this hash reads as a plain
// jump: a goto, a loop's entry, a label, a ternary's or a logical's test.
func singleSuccessor(
	terminal high_level_intermediate_representation.Terminal,
) (static_single_assignment.BlockId, bool) {
	switch terminal.(type) {
	case *high_level_intermediate_representation.Goto,
		*high_level_intermediate_representation.While,
		*high_level_intermediate_representation.DoWhile,
		*high_level_intermediate_representation.For,
		*high_level_intermediate_representation.ForOf,
		*high_level_intermediate_representation.ForIn,
		*high_level_intermediate_representation.Logical,
		*high_level_intermediate_representation.Ternary,
		*high_level_intermediate_representation.Optional,
		*high_level_intermediate_representation.Sequence,
		*high_level_intermediate_representation.Label,
		*high_level_intermediate_representation.Scope:
	default:
		return 0, false
	}
	var successors []static_single_assignment.BlockId
	high_level_intermediate_representation.EachSuccessor(terminal, func(block static_single_assignment.BlockId) {
		successors = append(successors, block)
	})
	if len(successors) != 1 {
		return 0, false
	}
	return successors[0], true
}

// parameterSignature adds to signature what lowering leaves out of a parameter: whether it is a rest parameter, the
// field a constructor's parameter property stores it in, with its modifiers and its type, and its default. Lowering
// drops every default and does not model a parameter property, so without this `f(a = 1)` and `f(a = 2)` hash alike,
// as do two constructors storing different fields. A literal default enters by value; any other is refused, since its
// computation is not in the IR. A parameter property is a field the class declares, so its type is part of what the
// constructor makes, where an ordinary parameter's is not.
func parameterSignature(
	signature *duplicate_functions.DigestBuilder,
	parameter *ast.Node,
	typeChecker *checker.Checker,
) error {
	declaration := parameter.AsParameterDeclaration()
	signature.Flag(declaration.DotDotDotToken != nil)
	if ast.HasSyntacticModifier(parameter, ast.ModifierFlagsParameterPropertyModifier) {
		signature.Text("property").Text(nameText(declaration.Name())).
			Number(int(ast.GetCombinedModifierFlags(parameter) & ast.ModifierFlagsParameterPropertyModifier))
		if typeChecker == nil {
			return refuse(duplicate_functions.RefusalNotHashable, "a parameter property without a checker")
		}
		signature.Text(typeChecker.TypeToString(typeChecker.GetTypeAtLocation(declaration.Name())))
	}
	initializer := declaration.Initializer
	if initializer == nil {
		signature.Text("required")
		return nil
	}
	signature.Text("default")
	for initializer.Kind == ast.KindParenthesizedExpression {
		initializer = initializer.AsParenthesizedExpression().Expression
	}
	switch initializer.Kind {
	case ast.KindNumericLiteral:
		signature.Digest(primitive(high_level_intermediate_representation.PrimitiveNumber(initializer.Text())))
	case ast.KindBigIntLiteral:
		signature.Digest(primitive(high_level_intermediate_representation.PrimitiveBigInt(initializer.Text())))
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		signature.Digest(primitive(high_level_intermediate_representation.PrimitiveString(initializer.Text())))
	case ast.KindTrueKeyword:
		signature.Digest(primitive(high_level_intermediate_representation.PrimitiveBoolean(true)))
	case ast.KindFalseKeyword:
		signature.Digest(primitive(high_level_intermediate_representation.PrimitiveBoolean(false)))
	case ast.KindNullKeyword:
		signature.Digest(primitive(high_level_intermediate_representation.PrimitiveNull{}))
	case ast.KindIdentifier:
		if initializer.Text() != "undefined" {
			return refuse(duplicate_functions.RefusalParameterDefault, "")
		}
		signature.Digest(primitive(high_level_intermediate_representation.PrimitiveUndefined{}))
	case ast.KindArrayLiteralExpression:
		if len(initializer.AsArrayLiteralExpression().Elements.Nodes) != 0 {
			return refuse(duplicate_functions.RefusalParameterDefault, "")
		}
		signature.Text("empty-array")
	case ast.KindObjectLiteralExpression:
		if len(initializer.AsObjectLiteralExpression().Properties.Nodes) != 0 {
			return refuse(duplicate_functions.RefusalParameterDefault, "")
		}
		signature.Text("empty-object")
	default:
		return refuse(duplicate_functions.RefusalParameterDefault, "")
	}
	return nil
}

// primitive is a constant's digest, by its value. A numeric literal's text in the IR is already its value, as the
// scanner gives it: `16`, `0x10`, `1_6` and `16.0` all lower as `16`, so the text is the value, and the fixture's
// literal pair holds the scanner to it.
func primitive(value high_level_intermediate_representation.PrimitiveValue) duplicate_functions.Digest {
	switch typed := value.(type) {
	case high_level_intermediate_representation.PrimitiveNull:
		return duplicate_functions.NewDigestBuilder("null").Sum()
	case high_level_intermediate_representation.PrimitiveUndefined:
		return duplicate_functions.NewDigestBuilder("undefined").Sum()
	case high_level_intermediate_representation.PrimitiveBoolean:
		return duplicate_functions.NewDigestBuilder("boolean").Flag(bool(typed)).Sum()
	case high_level_intermediate_representation.PrimitiveNumber:
		return duplicate_functions.NewDigestBuilder("number").Text(string(typed)).Sum()
	case high_level_intermediate_representation.PrimitiveString:
		return duplicate_functions.NewDigestBuilder("string").Text(string(typed)).Sum()
	case high_level_intermediate_representation.PrimitiveBigInt:
		return duplicate_functions.NewDigestBuilder("bigint").Text(string(typed)).Sum()
	}
	return duplicate_functions.NewDigestBuilder("primitive").Text(fmt.Sprintf("%T", value)).Sum()
}

// numericFamily is "number" or "bigint" for a type that is only one of them, a union of literals included, and empty
// for anything else.
func numericFamily(target *checker.Type) string {
	if target == nil {
		return ""
	}
	flags := target.Flags()
	switch {
	case flags&(checker.TypeFlagsNumber|checker.TypeFlagsNumberLiteral) != 0:
		return "number"
	case flags&(checker.TypeFlagsBigInt|checker.TypeFlagsBigIntLiteral) != 0:
		return "bigint"
	case flags&checker.TypeFlagsUnion != 0:
		family := ""
		for _, member := range target.Types() {
			memberFamily := numericFamily(member)
			if memberFamily == "" || (family != "" && memberFamily != family) {
				return ""
			}
			family = memberFamily
		}
		return family
	}
	return ""
}

// storeTarget is the node an assignment writes, for naming the global it stores to.
func storeTarget(node *ast.Node) *ast.Node {
	if node != nil && node.Kind == ast.KindBinaryExpression {
		return node.AsBinaryExpression().Left
	}
	return node
}
