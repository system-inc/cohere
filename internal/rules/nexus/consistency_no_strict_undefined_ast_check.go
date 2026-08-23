package nexus

import (
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

// lintRuleDirectorySegments are where a lint rule's AST reading actually happens.
//
// The concern is specific to code that walks parser output, so the rule scopes itself to where that
// code lives rather than trusting a naming convention to hold across the tree.
var lintRuleDirectorySegments = []string{
	"code-quality/lint/rules/",
	"code-quality/lint/utilities/",
}

// optionalAstPropertyNames are properties that ESTree and TypeScript-ESTree declare optional on some
// node type.
//
// Every entry can be absent, which is the only condition under which the two parsers disagree: a
// property that is always present reads the same under both. The list is deliberately conservative,
// holding the TypeScript-side annotations where the divergence was measured plus the core ESTree
// optionals that carry the same absent-or-node shape.
var optionalAstPropertyNames = map[string]bool{
	// TypeScript-ESTree surface, where the divergence was first measured.
	"accessibility": true, "returnType": true, "superClass": true, "superTypeArguments": true,
	"typeAnnotation": true, "typeArguments": true, "typeParameters": true,

	// Core ESTree optionals with the same shape.
	"alternate": true, "argument": true, "body": true, "declaration": true, "finalizer": true,
	"handler": true, "id": true, "init": true, "label": true, "parent": true, "source": true,
	"test": true, "update": true, "value": true,
}

func messageStrictUndefinedAstCheck(property string, operator string, suggested string) rule.Message {
	return rule.Message{
		Id: "strictUndefinedAstCheck",
		Description: "`" + property + " " + operator + " undefined` asks one parser a question the other " +
			"answers differently. The typescript-eslint parser omits an absent optional property, so it " +
			"reads as undefined; oxlint materializes it as null, so this comparison is false under oxlint " +
			"for a property that is genuinely absent. Write `" + property + " " + suggested + " null`, " +
			"which is true for exactly null and undefined and is the question you meant to ask. This fails " +
			"silently: the rule keeps working under one linter and quietly stops under the other.",
	}
}

// ConsistencyNoStrictUndefinedAstCheck requires `== null` rather than `=== undefined` when a lint
// rule tests an optional AST property.
//
//	valid:   if (annotation.typeArguments != null) { }
//	valid:   if (englishValue === undefined) { }        // not an AST property
//	invalid: if (annotation.typeArguments !== undefined) { }
//	invalid: if (node.returnType === undefined) { }
//
// Our rules run under two parsers and the two spell "this optional property is absent" differently,
// so a strict comparison asks about one parser's spelling and silently answers no under the other.
//
// This already cost us once. A class-instance exemption read `annotation.typeArguments !== undefined`.
// Under one parser the exemption fired correctly; under the other the same annotation carried a null,
// the guard returned early, and `export const discordClient: Client = ...` was told to rename itself.
// The repair was one character.
//
// What makes it worth a rule rather than an audit is how it fails. Nothing throws, nothing is
// reported, and the rule keeps passing its tests under whichever linter the author happened to run.
//
// Absence is detected without type information, so two independent signals must agree before it
// reports: the file lives where lint rules live, and the property being compared is one the node
// types declare optional. Requiring the property name is what keeps the rule quiet, because a lint
// rule is full of legitimate strict comparisons against things that are not AST nodes at all. The
// trade is deliberate: a comparison against an optional property this list has not learned yet goes
// unreported, which costs a missed warning, while reporting `value === undefined` inside a traversal
// helper would cost the rule's credibility. A rule people switch off protects nothing.
//
// No fix. Rewriting `!== undefined` to `!= null` widens what the comparison accepts, and a fix that
// silently changes a condition's meaning is the wrong thing to apply without a reader.
var ConsistencyNoStrictUndefinedAstCheck = rule.Rule{
	Name: "consistency-no-strict-undefined-ast-check",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		filePath := normalizedFileName(ctx.SourceFile)
		isLintRuleFile := false
		for _, segment := range lintRuleDirectorySegments {
			if strings.Contains(filePath, segment) {
				isLintRuleFile = true
				break
			}
		}
		// Outside the directories that read parser output there is nothing here to protect.
		if !isLintRuleFile {
			return nil
		}

		sourceText := ctx.SourceFile.Text()

		return rule.Listeners{
			ast.KindBinaryExpression: func(node *ast.Node) {
				binary := node.AsBinaryExpression()
				if binary == nil || binary.OperatorToken == nil {
					return
				}

				var operator, suggested string
				switch binary.OperatorToken.Kind {
				case ast.KindEqualsEqualsEqualsToken:
					operator, suggested = "===", "=="
				case ast.KindExclamationEqualsEqualsToken:
					operator, suggested = "!==", "!="
				default:
					return
				}

				// Either side may hold the literal, since `undefined === node.returnType` is the
				// same comparison written the other way round. The other side has to name an
				// optional property.
				var compared *ast.Node
				if isUndefinedIdentifier(binary.Right) {
					compared = binary.Left
				} else if isUndefinedIdentifier(binary.Left) {
					compared = binary.Right
				}
				if compared == nil {
					return
				}

				propertyName, isStatic := staticPropertyName(compared)
				if !isStatic || !optionalAstPropertyNames[propertyName] {
					return
				}

				ctx.ReportNode(node, messageStrictUndefinedAstCheck(
					nodeSourceText(sourceText, compared), operator, suggested))
			},
		}
	},
}

// isUndefinedIdentifier reports whether an expression is the bare `undefined` identifier.
//
// A member read named undefined, or the string 'undefined', is a different thing and is left alone.
// The identifier is the only form that participates in the parser divergence.
func isUndefinedIdentifier(node *ast.Node) bool {
	return node != nil && node.Kind == ast.KindIdentifier && node.Text() == "undefined"
}

// staticPropertyName returns the property a member expression reads, when it reads one statically.
//
// Computed access carries no name to match against the optional list, and a chain ending in a call
// is reading a return value rather than a node property, so both decline.
func staticPropertyName(node *ast.Node) (string, bool) {
	if node == nil || node.Kind != ast.KindPropertyAccessExpression {
		return "", false
	}
	access := node.AsPropertyAccessExpression()
	if access == nil {
		return "", false
	}
	name := access.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return "", false
	}
	return name.Text(), true
}

// nodeSourceText returns the source text a node spans, so the message can quote what the author
// actually wrote rather than a reconstruction of it.
func nodeSourceText(sourceText string, node *ast.Node) string {
	start, end := node.Pos(), node.End()
	if start < 0 || end > len(sourceText) || start >= end {
		return ""
	}
	return strings.TrimSpace(sourceText[start:end])
}
