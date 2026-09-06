package core

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/comments"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// PreferDestructuringKindOptions is the per-node-kind pair of switches.
type PreferDestructuringKindOptions struct {
	// Array enables the array-index arm, e.g. `var foo = array[0]`.
	Array *bool `json:"array"`
	// Object enables the property arm, e.g. `var foo = object.foo`.
	Object *bool `json:"object"`
}

// PreferDestructuringOptions is the decoded option surface.
//
// # Upstream's option surface is TWO schema elements and this tree can only deliver one
//
// `meta.schema` declares two entries: an enabling object, and a separate object carrying
// `enforceForRenamedProperties`. ESLint's `context.options` is every element after the severity, so
// upstream reads `context.options[1].enforceForRenamedProperties`.
//
// cohere's config layer keeps `setting.Options = tuple[1]` and DISCARDS everything after it, with no
// error (`internal/lint/configuration/configuration.go:391`). So a config written the upstream way:
//
//	["error", {"VariableDeclarator": {"object": true}}, {"enforceForRenamedProperties": true}]
//
// loads without complaint, runs, and silently ignores the second object. That is the `id-match`
// failure exactly: a rule that registers, passes a complete fixture pair, appears in `--rules`, and
// enforces something other than what the config says. Thirty of upstream's 103 corpus cases pass a
// second element, so this is the common spelling rather than an exotic one.
//
// The fix is to accept `enforceForRenamedProperties` INSIDE the single object this layer delivers.
//
// # The upstream spelling CANNOT be refused from here, and that is a measured limit rather than a
// # design choice
//
// The obvious guard is to refuse options that arrive as an array. It is written below and it can
// never fire through the real config layer, because the slice happens BEFORE the decoder is called:
// `["error", {enabling}, {enforceForRenamedProperties}]` reaches this function as the enabling
// object alone, indistinguishable from a config that named only that object. Measured on a seeded
// tree with the built binary -- the upstream spelling loads clean, runs, and reports zero on a
// source the option would flag, with the merged spelling reporting one on the same source.
//
// So the array branch below guards only a caller that hands the raw array directly, which the tests
// do. The real exposure is documented rather than defended, because there is nothing at this layer
// to defend it with: closing it means teaching `parseRuleSetting` to reject a tuple longer than two,
// which is a config-layer change affecting every rule and belongs in its own commit with its own
// probe rather than smuggled in beside a port.
type PreferDestructuringOptions struct {
	// VariableDeclarator gates `var foo = ...`.
	VariableDeclarator PreferDestructuringKindOptions
	// AssignmentExpression gates `foo = ...`.
	AssignmentExpression PreferDestructuringKindOptions
	// EnforceForRenamedProperties reports even when the binding's name differs from the property's.
	EnforceForRenamedProperties bool
}

// preferDestructuringWire is the object shape the config layer can deliver.
//
// It carries BOTH of upstream's spellings at once. Upstream's first schema element is a `oneOf`: an
// object with `VariableDeclarator` / `AssignmentExpression` keys, or a flat `{array, object}` pair
// that applies to both kinds. Which one was written is decided by whether `array` or `object` is
// present at the top level, which is upstream's own test.
type preferDestructuringWire struct {
	VariableDeclarator   *PreferDestructuringKindOptions `json:"VariableDeclarator"`
	AssignmentExpression *PreferDestructuringKindOptions `json:"AssignmentExpression"`
	Array                *bool                           `json:"array"`
	Object               *bool                           `json:"object"`
	// The second schema element's only key, folded in here because the config layer cannot deliver
	// a second element at all.
	EnforceForRenamedProperties *bool `json:"enforceForRenamedProperties"`
}

// DecodePreferDestructuringOptions reads the option object off the config.
func DecodePreferDestructuringOptions(raw []byte) (any, error) {
	enabled := true
	options := PreferDestructuringOptions{
		VariableDeclarator:   PreferDestructuringKindOptions{Array: &enabled, Object: &enabled},
		AssignmentExpression: PreferDestructuringKindOptions{Array: &enabled, Object: &enabled},
	}
	if len(raw) == 0 {
		return options, nil
	}

	// The upstream array spelling is refused rather than partially honoured. Decoding `tuple[0]` and
	// discarding the rest is exactly how `enforceForRenamedProperties` would go missing in silence.
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return options, fmt.Errorf(
			"prefer-destructuring: options arrived as an array. Upstream's schema has two elements " +
				"and this config layer delivers only the first, so `enforceForRenamedProperties` " +
				"would be silently dropped. Write one object instead, with the enabling keys and " +
				"enforceForRenamedProperties together")
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var wire preferDestructuringWire
	if err := decoder.Decode(&wire); err != nil {
		return options, fmt.Errorf("prefer-destructuring: %w", err)
	}

	if wire.EnforceForRenamedProperties != nil {
		options.EnforceForRenamedProperties = *wire.EnforceForRenamedProperties
	}

	// Upstream's normalisation, verbatim: a top-level `array` or `object` key means the flat
	// spelling, which applies to BOTH node kinds. Otherwise the per-kind keys are used as written,
	// and a kind that is absent is entirely disabled rather than defaulted on -- upstream replaces
	// `normalizedOptions` wholesale, so `{"VariableDeclarator": {...}}` leaves AssignmentExpression
	// undefined and `shouldCheck` answers false for it.
	if wire.Array != nil || wire.Object != nil {
		flat := PreferDestructuringKindOptions{Array: wire.Array, Object: wire.Object}
		options.VariableDeclarator = flat
		options.AssignmentExpression = flat
		return options, nil
	}
	if wire.VariableDeclarator != nil || wire.AssignmentExpression != nil {
		options.VariableDeclarator = PreferDestructuringKindOptions{}
		options.AssignmentExpression = PreferDestructuringKindOptions{}
		if wire.VariableDeclarator != nil {
			options.VariableDeclarator = *wire.VariableDeclarator
		}
		if wire.AssignmentExpression != nil {
			options.AssignmentExpression = *wire.AssignmentExpression
		}
	}
	return options, nil
}

// enabled reads one switch, where an absent switch means off.
//
// Upstream's `shouldCheck` is `normalizedOptions[nodeType][destructuringType]`, a plain truthiness
// test, so an absent key is falsy and the arm is off. Only the wholly-default configuration turns
// everything on, which the decoder builds explicitly.
func (o PreferDestructuringKindOptions) enabled(array bool) bool {
	switch {
	case array:
		return o.Array != nil && *o.Array
	default:
		return o.Object != nil && *o.Object
	}
}

var messagePreferDestructuring = rule.Message{
	Id: "preferDestructuring",
	Description: "Pulling one name out of an object or array by hand repeats the name on both " +
		"sides, so a rename touches two places and the two can drift. Destructuring writes it once.",
}

// PreferDestructuring asks for destructuring where a member access only copies a name.
//
//	valid:   var { foo } = object;
//	valid:   var foo = object.bar;                 the names differ, so nothing is repeated
//	valid:   var foo = object[bar];                a computed key is not a name
//	invalid: var foo = object.foo;                 fixed to `var {foo} = object;`
//	invalid: var foo = array[0];                   reported, never fixed
//
// # The fixer is deliberately narrow, and its declines are the specification
//
// Upstream's `shouldFix` repairs ONE shape: a variable declarator whose binding is a plain
// identifier, whose initializer is a non-computed member access, and whose binding name equals the
// property name. Everything else reports without a fix: an assignment expression, an array index, a
// renamed property, a computed key. Those declines are reproduced rather than improved on, and
// upstream's corpus asserts them as `output: null` on thirty of its forty-eight reporting cases.
//
// # Comments are half the fixer's corpus and the whole of its subtlety
//
// Twenty-four of upstream's forty-eight reporting cases exist to pin what happens to comments, and
// the rule is one comparison: the repair is DECLINED when the declarator holds more comments than
// the member access's object does, because only comments inside that object survive into the
// replacement. So:
//
//	var foo = bar(/* c */).foo;        FIXED, the comment is inside the object
//	var foo = object./* c */foo;       declined, the comment sits in the part being deleted
//	var foo /* c */ = object.foo;      declined, the comment sits outside the object
//	var foo = object.foo/* c */;       FIXED, the comment is after the replaced range entirely
//
// The last row is why the comparison counts comments inside the DECLARATOR rather than inside the
// whole statement: a trailing comment is outside the declarator and never at risk.
//
// # A parenthesised receiver is re-parenthesised only below assignment precedence
//
// Upstream wraps the object text when its precedence is lower than an assignment expression's, which
// is the sequence operator and nothing else in practice. Its corpus pins both directions in adjacent
// cases: `(a, b).foo` keeps its parentheses and `(a = b).foo` loses them, becoming
// `var {foo} = a = b;`. Reproduced by testing for a comma expression, with both cases as fixtures.
//
// # The span is the declarator or the whole assignment
//
// Measured against the installed 10.8.1 build: `var foo = object.foo;` reports columns 5 to 21,
// which is the declarator without `var`, and `foo = array[0];` reports columns 1 to 15.
var PreferDestructuring = rule.Rule{
	Name: "prefer-destructuring",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings, ok := options.(PreferDestructuringOptions)
		if !ok {
			enabled := true
			settings = PreferDestructuringOptions{
				VariableDeclarator:   PreferDestructuringKindOptions{Array: &enabled, Object: &enabled},
				AssignmentExpression: PreferDestructuringKindOptions{Array: &enabled, Object: &enabled},
			}
		}

		report := func(node *ast.Node, kind string, fixes []rule.Fix) {
			message := rule.Message{
				Id: messagePreferDestructuring.Id,
				Description: fmt.Sprintf("Use %s destructuring. %s",
					kind, messagePreferDestructuring.Description),
			}
			if len(fixes) == 0 {
				ctx.ReportNode(node, message)
				return
			}
			ctx.ReportNodeWithFixes(node, message, fixes...)
		}

		performCheck := func(left *ast.Node, right *ast.Node, reportNode *ast.Node,
			kindOptions PreferDestructuringKindOptions, isDeclarator bool) {

			property, computed := preferDestructuringMemberParts(right)
			if property == nil {
				return
			}
			// `super.foo` and `this.#x` are excluded by upstream before anything else.
			if preferDestructuringReceiver(right).Kind == ast.KindSuperKeyword ||
				property.Kind == ast.KindPrivateIdentifier {
				return
			}

			// An array INDEX access, which upstream detects by the property being an integer.
			if computed && preferDestructuringIsArrayIndex(property) {
				if kindOptions.enabled(true) {
					report(reportNode, "array", nil)
				}
				return
			}

			if !kindOptions.enabled(false) {
				return
			}

			var fixes []rule.Fix
			if isDeclarator {
				if fix, repairable := preferDestructuringFix(ctx, reportNode, right); repairable {
					fixes = []rule.Fix{fix}
				}
			}

			if settings.EnforceForRenamedProperties {
				report(reportNode, "object", fixes)
				return
			}

			// Without that option the names must match, which is the whole point: a member access
			// whose property name differs from the binding is not repeating anything.
			if left == nil || left.Kind != ast.KindIdentifier {
				return
			}
			switch {
			case computed && property.Kind == ast.KindStringLiteral:
				// Upstream's `property.type === "Literal" && leftNode.name === property.value`,
				// which has no `!computed` clause, so `object['foo']` DOES match when the names
				// agree. Its corpus asserts that as a reporting case.
				if property.Text() == left.Text() {
					report(reportNode, "object", fixes)
				}
			case !computed && property.Kind == ast.KindIdentifier:
				if property.Text() == left.Text() {
					report(reportNode, "object", fixes)
				}
			}
		}

		return rule.Listeners{
			ast.KindVariableDeclaration: func(node *ast.Node) {
				declaration := node.AsVariableDeclaration()
				if declaration.Initializer == nil {
					return
				}
				// `using x = ...` cannot be destructured at all: the destructured form is a parse
				// error, so a finding there would be unactionable. Upstream skips both `using` and
				// `await using`.
				if preferDestructuringIsUsingDeclaration(node) {
					return
				}
				// The initializer is unwrapped because upstream's parser folds parentheses away, so
				// `var foo = (object.foo);` arrives there as a plain member access and REPORTS.
				// Without this the whole check is skipped and two of upstream's reporting cases go
				// silent.
				performCheck(declaration.Name(), preferDestructuringUnwrap(declaration.Initializer),
					node, settings.VariableDeclarator, true)
			},
			ast.KindBinaryExpression: func(node *ast.Node) {
				expression := node.AsBinaryExpression()
				// Plain `=` only. A compound assignment is not a copy of a name.
				if expression.OperatorToken.Kind != ast.KindEqualsToken {
					return
				}
				performCheck(expression.Left, preferDestructuringUnwrap(expression.Right), node,
					settings.AssignmentExpression, false)
			},
		}
	},
}

// preferDestructuringUnwrap strips parentheses, which upstream's parser folds away before the rule
// ever runs.
//
// A loop rather than one step, because `((x))` nests. `ast.SkipParentheses` is not used: it
// dereferences its argument and every caller here holds an optional node.
func preferDestructuringUnwrap(node *ast.Node) *ast.Node {
	for node != nil && node.Kind == ast.KindParenthesizedExpression {
		node = node.AsParenthesizedExpression().Expression
	}
	return node
}

// preferDestructuringMemberParts returns the accessed property and whether the access is computed.
//
// Our parser splits ESTree's one MemberExpression into two node kinds, which is why this exists at
// all: `a.b` is a PropertyAccessExpression and `a[b]` is an ElementAccessExpression, where upstream
// has one node with a `computed` flag.
func preferDestructuringMemberParts(node *ast.Node) (*ast.Node, bool) {
	if node == nil {
		return nil, false
	}
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		access := node.AsPropertyAccessExpression()
		// `object?.foo` is a different node in ESTree (ChainExpression wrapping the access), so
		// upstream's `rightNode.type !== "MemberExpression"` guard declines it and both of its
		// optional-chaining cases are VALID. Our parser keeps one node kind and marks the token, so
		// the exclusion has to be explicit or this rule reports two of upstream's passing cases.
		if access.QuestionDotToken != nil {
			return nil, false
		}
		return access.Name(), false
	case ast.KindElementAccessExpression:
		access := node.AsElementAccessExpression()
		if access.QuestionDotToken != nil {
			return nil, false
		}
		return access.ArgumentExpression, true
	}
	return nil, false
}

// preferDestructuringReceiver returns the object an access is made on, or the node itself.
func preferDestructuringReceiver(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		return node.AsPropertyAccessExpression().Expression
	case ast.KindElementAccessExpression:
		return node.AsElementAccessExpression().Expression
	}
	return node
}

// preferDestructuringIsArrayIndex is upstream's `Number.isInteger(node.property.value)`.
//
// A numeric literal whose canonical text has no `.`, no exponent and no sign. Our parser has already
// normalised the text -- `0x0` arrives as `0` and `1e2` as `100` -- which is the same equivalence
// class `Number.isInteger` produces, so the test is on the canonical form rather than on the source
// spelling. A negative index cannot appear here: `array[-1]` parses the minus as a unary operator
// rather than as part of the literal, and upstream's `.value` is likewise undefined for it.
func preferDestructuringIsArrayIndex(property *ast.Node) bool {
	if property == nil || property.Kind != ast.KindNumericLiteral {
		return false
	}
	text := property.Text()
	if text == "" {
		return false
	}
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}
	return true
}

// preferDestructuringIsUsingDeclaration reports an explicit-resource-management declaration.
//
// # `NodeFlagsAwaitUsing` is a COMPOSITE and testing it disabled this rule on every `const`
//
// A first draft wrote the obvious thing, `flags&NodeFlagsUsing != 0 || flags&NodeFlagsAwaitUsing !=
// 0`, matching upstream's two-kind test. The second half is wrong here, and silently:
//
//	NodeFlagsAwaitUsing = NodeFlagsConst | NodeFlagsUsing        nodeflags.go:51
//
// It is a composite rather than a bit of its own, so `flags & NodeFlagsAwaitUsing` is non-zero for
// EVERY `const` declaration, and the guard skipped all of them. The rule reported nothing on any
// modern source file while passing all 103 imported fixtures, because upstream's corpus is written
// in `var`.
//
// No fixture could have caught it and no mutation of this rule would either: the defect is a true
// statement about a constant in another package. The instrument that found it was a dry run through
// the real config layer against a seeded tree, where the rule reported zero with a control that
// reported three.
//
// `NodeFlagsUsing` alone is correct for both spellings, since `await using` carries it too.
func preferDestructuringIsUsingDeclaration(node *ast.Node) bool {
	list := node.Parent
	if list == nil || list.Kind != ast.KindVariableDeclarationList {
		return false
	}
	return list.Flags&ast.NodeFlagsUsing != 0
}

// preferDestructuringFix builds upstream's `fixIntoObjectDestructuring`, or declines.
//
// Two gates, and both are upstream's. `shouldFix` restricts the repair to the one simple shape, and
// the comment comparison declines whenever the rewrite would delete a comment.
func preferDestructuringFix(ctx rule.Context, declarator *ast.Node, right *ast.Node) (rule.Fix, bool) {
	if right.Kind != ast.KindPropertyAccessExpression {
		return rule.Fix{}, false
	}
	access := right.AsPropertyAccessExpression()
	if access.QuestionDotToken != nil {
		return rule.Fix{}, false
	}
	property := access.Name()
	name := declarator.AsVariableDeclaration().Name()
	if property == nil || property.Kind != ast.KindIdentifier ||
		name == nil || name.Kind != ast.KindIdentifier || name.Text() != property.Text() {
		return rule.Fix{}, false
	}

	// The OBJECT is unwrapped for the same reason, and it changes two things at once. Upstream's
	// `getText(rightNode.object)` sees no parentheses, so `(f()).foo` becomes `var {foo} = f();`
	// rather than `= (f());`, and its `getCommentsInside(rightNode.object)` likewise counts only
	// what is inside the expression. A comment sitting BETWEEN the parenthesis and the expression --
	// `(/* c */ object).foo` -- is therefore inside the declarator and outside the object, which is
	// what makes upstream decline that repair. Counting against the parenthesized range instead
	// includes the comment on both sides of the comparison and wrongly repairs it.
	object := preferDestructuringUnwrap(access.Expression)
	if object == nil {
		return rule.Fix{}, false
	}

	// Upstream: decline when the declarator holds more comments than the object does, because only
	// the object's own comments survive into the replacement text.
	declaratorRange := rule.TokenRange(ctx.SourceFile, declarator)
	objectRange := rule.TokenRange(ctx.SourceFile, object)
	if preferDestructuringCommentsInside(ctx, declaratorRange) >
		preferDestructuringCommentsInside(ctx, objectRange) {
		return rule.Fix{}, false
	}

	objectText := ctx.SourceFile.Text()[objectRange.Pos():objectRange.End()]
	// Upstream re-parenthesises when the object's precedence is below an assignment expression's.
	// A comma expression is the only thing that qualifies, which its corpus pins in adjacent cases:
	// `(a, b).foo` keeps the parens and `(a = b).foo` loses them.
	if preferDestructuringIsCommaExpression(object) {
		objectText = "(" + objectText + ")"
	}

	return rule.ReplaceRange(declaratorRange,
		"{"+property.Text()+"} = "+objectText), true
}

// preferDestructuringCommentsInside counts comments falling wholly inside a range.
//
// `comments.ForFile` is the shelf's cached per-file scan, which is upstream's `getCommentsInside`.
func preferDestructuringCommentsInside(ctx rule.Context, textRange core.TextRange) int {
	count := 0
	for _, comment := range comments.ForFile(ctx) {
		if comment.Range.Pos() >= textRange.Pos() && comment.Range.End() <= textRange.End() {
			count++
		}
	}
	return count
}

// preferDestructuringIsCommaExpression reports a sequence expression, the one thing that binds
// looser than an assignment.
func preferDestructuringIsCommaExpression(node *ast.Node) bool {
	return node.Kind == ast.KindBinaryExpression &&
		node.AsBinaryExpression().OperatorToken.Kind == ast.KindCommaToken
}
