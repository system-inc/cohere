package typescript

import (
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/imports"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// ConsistentTypeImportsPrefer selects which direction the rule enforces.
type ConsistentTypeImportsPrefer string

const (
	// ConsistentTypeImportsPreferTypeImports requires `import type` wherever every use is a type.
	ConsistentTypeImportsPreferTypeImports ConsistentTypeImportsPrefer = "type-imports"
	// ConsistentTypeImportsPreferNoTypeImports forbids the `type` keyword on imports entirely.
	ConsistentTypeImportsPreferNoTypeImports ConsistentTypeImportsPrefer = "no-type-imports"
)

// ConsistentTypeImportsFixStyle selects where a repair would place the `type` keyword.
//
// This port proposes no repair, so the option changes nothing about what reports. It is decoded and
// validated anyway rather than dropped, because dropping it would make a configuration that names
// it silently mean something else, and because the option becomes load-bearing the moment a fix
// lands here. See the fix section of the rule's doc comment for why there is none today.
type ConsistentTypeImportsFixStyle string

const (
	// ConsistentTypeImportsFixStyleSeparate writes `import type { A } from 'm'`.
	ConsistentTypeImportsFixStyleSeparate ConsistentTypeImportsFixStyle = "separate-type-imports"
	// ConsistentTypeImportsFixStyleInline writes `import { type A } from 'm'`.
	ConsistentTypeImportsFixStyleInline ConsistentTypeImportsFixStyle = "inline-type-imports"
)

// ConsistentTypeImportsOptions configures the rule.
//
// The authoritative surface is oxc's `ConsistentTypeImportsConfig`, which carries exactly these
// three fields under `serde(rename_all = "camelCase", deny_unknown_fields)`. Read from that struct
// rather than from an inventory column: the inventory names two options and there are three, and
// the third one is the only one whose default is `true`.
type ConsistentTypeImportsOptions struct {
	// DisallowTypeAnnotations forbids `import()` written inside a type, as in `type T = import('m')`.
	//
	// **Defaults to true**, which is the field most likely to be got wrong, because every other
	// boolean option in this tree defaults to false and a Go zero value would silently turn this
	// arm off. Three of the corpus's seventy-nine diagnostics come from this arm alone, and one
	// corpus pass case exists only to pin the `false` setting.
	DisallowTypeAnnotations bool

	// FixStyle selects where a repair would put the keyword. Decoded, defaulted, and unused; see
	// the type's own comment.
	FixStyle ConsistentTypeImportsFixStyle

	// Prefer selects the direction. `type-imports` is the default and is what the live config sets.
	Prefer ConsistentTypeImportsPrefer
}

// DefaultConsistentTypeImportsOptions is what the rule uses when the configuration names nothing.
//
// Every field here is non-zero, so this cannot be spelled as a zero-value struct. That matters more
// than it reads: a rule configured as a bare `"error"` receives nil options, and a decoder built
// from `rule.DecodeOptionsInto` would hand the rule three empty strings and a false, matching no arm
// and reporting nothing while looking correctly wired.
func DefaultConsistentTypeImportsOptions() ConsistentTypeImportsOptions {
	return ConsistentTypeImportsOptions{
		DisallowTypeAnnotations: true,
		FixStyle:                ConsistentTypeImportsFixStyleSeparate,
		Prefer:                  ConsistentTypeImportsPreferTypeImports,
	}
}

var messageConsistentTypeImportsTypeOverValue = rule.Message{
	Id: "typeOverValue",
	Description: "Every name this declaration imports is used only in a type position, so nothing " +
		"it brings in survives compilation. Written as `import type`, the statement is erased " +
		"outright rather than leaving a module request that a bundler has to keep, and a reader " +
		"can see at a glance that it cannot affect runtime behavior.",
}

var messageConsistentTypeImportsAvoidImportType = rule.Message{
	Id: "avoidImportType",
	Description: "This configuration asks for value imports throughout, and the `type` keyword " +
		"here opts one declaration out of that. Drop it so every import in the codebase reads " +
		"the same way.",
}

var messageConsistentTypeImportsNoImportTypeAnnotations = rule.Message{
	Id: "noImportTypeAnnotations",
	Description: "An `import()` written inside a type annotation hides a module dependency where " +
		"nothing looking at the import list will find it. Move it to a real `import type` " +
		"declaration at the top of the file and refer to the name.",
}

// consistentTypeImportsSomeAreOnlyTypes builds the per-declaration message naming the offenders.
//
// The names are interpolated, so a fixture asserting only the message id cannot see this line at
// all. The rendered text is asserted by equality in the fixtures for exactly that reason.
func consistentTypeImportsSomeAreOnlyTypes(names []string) rule.Message {
	return rule.Message{
		Id: "someImportsAreOnlyTypes",
		Description: fmt.Sprintf("Imports %s are only used as type.",
			consistentTypeImportsWordList(names)),
	}
}

// ConsistentTypeImports flags imports whose every use is a type, and `import type` under the
// inverted setting.
//
//	valid:   import type { Foo } from 'foo'; let foo: Foo;
//	valid:   import { A, B } from 'foo'; const foo: A = B();
//	valid:   import { type A, B } from 'foo'; type T = A; const b = B;
//	invalid: import { Foo } from 'foo'; let foo: Foo;
//	invalid: import { A, B } from 'foo'; const foo: A = B();   // A only
//	invalid: type T = import('foo');
//
// Ported from oxc's `consistent_type_imports`, which is what the gate runs.
//
// # The whole rule is one question asked per imported name
//
// For each name a declaration binds: does it have at least one reference, and is every one of those
// references a type reference? A name with no references at all answers no, deliberately, so an
// entirely unused import is silent here and left to the unused-import rules. That asymmetry is
// upstream's `is_only_has_type_references`, whose first act is to return false on an empty iterator,
// and it is pinned by the corpus's `import Foo from 'foo';` pass case.
//
// # What counts as a type reference is not `IsPartOfTypeNode`, and that was measured
//
// The obvious substrate for this is `ast.IsPartOfTypeNode`, and reaching for it alone loses three
// of the corpus's own failing shapes. Probed directly against the parser, `typeof Foo` inside a
// type alias gives a parent of `KindTypeQuery` with `IsPartOfTypeNode` answering **false**, and the
// left side of a qualified name such as `foo.Bar` gives `KindQualifiedName` with the same false. Both
// report upstream. So the predicate here is a walk that names those two kinds explicitly, and the
// corpus's `// TSTypeQuery` and `// TSQualifiedName` comments are upstream telling you the same
// thing.
//
// The opposite error is available too, and the parser hands us the discrimination for free. Probed
// on the release binary: `class C implements A {}` **reports** and `class C extends A {}` is
// **silent**. In this AST the `implements` name parses to `KindTypeReference` and the `extends` name
// to `KindExpressionWithTypeArguments`, so treating the latter as a type position would report a
// superclass as type-only and propose erasing an import the emitted code needs. There is no
// heritage-clause token to inspect; the parse shape already decided it.
//
// # Three sites resolve to the import through a different symbol
//
// `export { X }`, `export type { X }` and `export default X` put an identifier in the file that
// `GetSymbolAtLocation` resolves to the *export's* own symbol rather than the import's, so a rule
// matching on symbol identity sees no reference there at all and goes silent on six corpus cases.
// The recovery is `GetExportSpecifierLocalTargetSymbol`, which resolves an export specifier back to
// what it re-exports.
//
// Whether such a reference counts as a type is then decided by the export's own `type` keyword and
// not by position, which is the half a reading of the AST cannot supply. Measured on the release
// binary, all four with the same import:
//
//	export { Type };        SILENT   — a value export
//	export default Type;    SILENT   — a value export
//	export = Type;          SILENT   — a value export
//	export type { Type };   REPORTS  — a type-only export
//	export { type Type };   REPORTS  — likewise, per specifier
//
// Upstream reaches the same answers by a route worth recording, because it looks like it should
// give different ones. Its semantic builder marks all of these `Read | Type`, genuinely ambiguous,
// and then collapses the pair to a plain value reference during resolution whenever the symbol is
// value-capable, which an ordinary import alias always is. The ambiguity is real in the flags and
// resolved away before any rule sees it, so reproducing the flags rather than the outcome would be
// reproducing a mechanism instead of a decision.
//
// # `React` is exempted by its local name, spelled out
//
// A default or namespace specifier whose local name is exactly `React` is skipped, so
// `import React from 'react'; type T = React.FC;` is clean. This is a string comparison and not a
// resolution: probed on the binary, renaming the local to `Renamed` makes the identical file
// report. Upstream carries a `TODO` saying it should consult the tsconfig `jsx` field instead, and
// that gap is reproduced here rather than improved on, because improving it would change which
// files report and the differential harness compares against upstream. Named imports are not
// exempt, only the default and namespace forms.
//
// # This port reports and does not repair, which is a deliberate subset
//
// Upstream ships seventy-three fix vectors and this rule proposes no fix at all. That is not an
// omission and it is not laziness about a large fixer; it is the only subset that can be shown
// correct here, and the reason is in our own fix engine rather than in the rewrite.
//
// Upstream's repair for the ordinary `import Default, { TypeName } from 'm'` shape emits two
// insert-before edits at one offset, the declaration's start, splitting one statement into two new
// ones. Measured against `internal/fix`, a diagnostic carrying that pair produces **zero applied
// fixes and 1024 rejections**: `ProposalsFrom` flattens a diagnostic's fixes into independent
// proposals, `Resolve` refuses two insertions at one point as a mutual overlap, and the engine then
// re-proposes them once per pass until it hits the pass limit. That behavior is deliberate and
// documented at `ProposalsFrom`, which states that a rule needing two edits to land together needs
// grouping in the engine first and cannot get it by reporting them side by side, and notes that no
// shipped rule emits more than one fix per report.
//
// The rest of the fixer is no more portable. Its output bytes are arithmetic on character offsets
// rather than a rendering, which upstream's own vectors show: the same run writes
// `import type { A} from 'foo';` for one specifier and `import type { C } from 'foo';` for another,
// and merging into an existing declaration produces `import type { B , A} from 'foo';`. Reproducing
// those spaces exactly is a requirement of a faithful fix and buys nothing a reader wants.
//
// So the finding ships and the repair does not. A wrong fix is applied unattended; a missing one is
// visible in the report.
var ConsistentTypeImports = rule.Rule{
	Name: "@typescript-eslint/consistent-type-imports",

	// Every part of the rule resolves a name to a declaration. There is no syntactic route to
	// "which import does this identifier bind to", and the corpus turns on exactly that: several
	// failing cases are textually near-identical to passing ones and differ only in what a name
	// resolves to.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings, ok := options.(ConsistentTypeImportsOptions)
		if !ok {
			// A rule configured as a bare `"error"` is handed nil, and every default here is
			// non-zero. Without this line the rule registers, passes its fixtures through the
			// decoder, and reports nothing on the real tree.
			settings = DefaultConsistentTypeImportsOptions()
		}

		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				if ctx.TypeChecker == nil {
					return
				}
				sourceFile := node.AsSourceFile()
				if sourceFile == nil {
					return
				}

				if settings.DisallowTypeAnnotations {
					reportImportTypeAnnotations(ctx, sourceFile)
				}

				switch settings.Prefer {
				case ConsistentTypeImportsPreferNoTypeImports:
					reportTypeKeywordsToRemove(ctx, sourceFile)
				default:
					reportImportsUsedOnlyAsTypes(ctx, sourceFile)
				}
			},
		}
	},
}

// reportImportTypeAnnotations flags every `import('m')` written inside a type.
//
// A whole-file walk rather than a listener on the kind, because the rule already owns a
// `KindSourceFile` listener for the parts that must gather before they judge, and splitting one
// judgment across two anchors makes the ordering of findings depend on the walk rather than on the
// source. Findings come out in source order either way, which is what the corpus asserts.
func reportImportTypeAnnotations(ctx rule.Context, sourceFile *ast.SourceFile) {
	var visit func(*ast.Node)
	visit = func(current *ast.Node) {
		if current == nil {
			return
		}
		if current.Kind == ast.KindImportType {
			ctx.ReportNode(current, messageConsistentTypeImportsNoImportTypeAnnotations)
		}
		current.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile.AsNode())
}

// reportTypeKeywordsToRemove implements `prefer: no-type-imports`.
//
// Two anchors, and they are separate findings rather than one per declaration: a whole
// `import type { A } from 'm'` reports against the declaration, while an inline `import { type A }`
// reports against the specifier. The corpus pins both, and one three-declaration case produces
// exactly three findings.
//
// Nothing about references is consulted here. The setting says the keyword is unwanted wherever it
// appears, so a type-only import of a name used only as a type still reports.
func reportTypeKeywordsToRemove(ctx rule.Context, sourceFile *ast.SourceFile) {
	for _, statement := range sourceFile.Statements.Nodes {
		if statement.Kind != ast.KindImportDeclaration {
			continue
		}
		clause := statement.AsImportDeclaration().ImportClause
		if clause == nil {
			continue
		}
		if clause.AsImportClause().IsTypeOnly() {
			ctx.ReportNode(statement, messageConsistentTypeImportsAvoidImportType)
			continue
		}
		// An inline `type` on a specifier is only reachable when the declaration itself is not
		// type-only: `import type { type A }` is a syntax error, so the two forms never coexist.
		for _, named := range imports.BindingsOf(statement).Named {
			if named.Kind == ast.KindImportSpecifier && named.AsImportSpecifier().IsTypeOnly {
				ctx.ReportNode(named, messageConsistentTypeImportsAvoidImportType)
			}
		}
	}
}

// reportImportsUsedOnlyAsTypes implements `prefer: type-imports`, which is the default and the
// setting the live config uses.
//
// One finding per declaration at most. Which of the two messages it carries depends on whether
// *every* specifier was found type-only, and the count is against the specifier list rather than
// against the named specifiers: `import A, {} from 'foo'` has one specifier and one type-only name,
// so it takes the whole-declaration message, which is what the corpus's `import A, {} from 'foo'`
// case asserts.
func reportImportsUsedOnlyAsTypes(ctx rule.Context, sourceFile *ast.SourceFile) {
	// Built once and only when an import that could report actually exists, so a file with no
	// value imports at all pays nothing for the index.
	var byText map[string][]*ast.Node

	for _, statement := range sourceFile.Statements.Nodes {
		if statement.Kind != ast.KindImportDeclaration {
			continue
		}
		declaration := statement.AsImportDeclaration()
		clause := declaration.ImportClause
		if clause == nil {
			// `import 'm'` binds nothing. The corpus writes one as the first statement of a
			// two-import case precisely to check it is stepped over rather than tripped on.
			continue
		}
		if clause.AsImportClause().IsTypeOnly() {
			// Already what the rule is asking for.
			continue
		}

		if byText == nil {
			byText = consistentTypeImportsIdentifiers(sourceFile)
		}

		bindings := imports.BindingsOf(statement)
		specifierCount := 0
		typeOnlyNames := []string{}

		consider := func(local *ast.Node, exemptReact bool, alreadyTypeOnly bool) {
			if local == nil {
				return
			}
			specifierCount++
			if exemptReact && local.Text() == "React" {
				// Skipped rather than counted as a value use. Upstream `continue`s before the
				// check, so the name is absent from both sides of the all-are-types comparison,
				// which is why `import React from 'react'` alone is silent rather than reporting
				// as a wholly type-only import.
				specifierCount--
				return
			}
			if alreadyTypeOnly {
				return
			}
			if isReferencedOnlyAsType(ctx, byText, local) {
				typeOnlyNames = append(typeOnlyNames, local.Text())
			}
		}

		consider(bindings.Default, true, false)
		if bindings.Namespace != nil {
			consider(bindings.Namespace.Name(), true, false)
		}
		for _, named := range bindings.Named {
			if named.Kind != ast.KindImportSpecifier {
				continue
			}
			consider(named.Name(), false, named.AsImportSpecifier().IsTypeOnly)
		}

		if len(typeOnlyNames) == 0 {
			continue
		}

		if len(typeOnlyNames) == specifierCount {
			// `import type {} from 'm' with { type: 'json' }` is not valid TypeScript, so upstream
			// withholds the whole-declaration finding when an attributes clause is present rather
			// than proposing something that cannot be written. Probed: an import with an `assert`
			// clause is silent even when every name is type-only.
			if declaration.Attributes == nil {
				ctx.ReportNode(statement, messageConsistentTypeImportsTypeOverValue)
			}
			continue
		}

		ctx.ReportNode(statement, consistentTypeImportsSomeAreOnlyTypes(typeOnlyNames))
	}
}

// consistentTypeImportsIdentifiers indexes a file's identifiers by their text, once.
//
// Written as an index rather than as a walk per imported name because the naive shape re-walks the
// whole tree for every specifier in every import statement, and a file with twenty imports then
// visits every node twenty times. Measured on the ahra tree at 3,407 files: the per-name walk cost
// 667ms and 2.9% of all rule time, which is the same shape as the rule that was 64.5% of a morning
// because it rebuilt a map per file.
//
// Keyed by text because an identifier spelled differently cannot resolve to this import, so the text
// is an exact pre-filter on a question that would otherwise cost a checker call per identifier. The
// filter is not a discrimination: symbol identity below already implies it.
func consistentTypeImportsIdentifiers(sourceFile *ast.SourceFile) map[string][]*ast.Node {
	byText := map[string][]*ast.Node{}
	var visit func(*ast.Node)
	visit = func(current *ast.Node) {
		if current == nil {
			return
		}
		if current.Kind == ast.KindIdentifier {
			text := current.Text()
			byText[text] = append(byText[text], current)
		}
		current.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile.AsNode())
	return byText
}

// isReferencedOnlyAsType answers the rule's single question for one imported name.
//
// False when the name has no references at all, which is upstream's own first line and not an
// accident of the loop: an unused import is a different rule's finding.
func isReferencedOnlyAsType(ctx rule.Context, byText map[string][]*ast.Node, local *ast.Node) bool {
	target := ctx.TypeChecker.GetSymbolAtLocation(local)
	if target == nil {
		return false
	}

	found := false
	for _, candidate := range byText[local.Text()] {
		if candidate == local || !resolvesToImport(ctx, candidate, target) {
			continue
		}
		found = true
		if !isTypeReference(candidate) {
			// One value use settles it, and the remaining candidates cannot change the answer.
			return false
		}
	}
	return found
}

// resolvesToImport reports whether an identifier binds to the import's symbol.
//
// Two accessors rather than one, and the second is not a fallback for a failure: an identifier
// inside an export specifier resolves through `GetSymbolAtLocation` to the export's own symbol,
// which is a perfectly good symbol that simply is not this one. Asking only the first accessor
// answers "no reference here" for every `export { X }`, `export type { X }` and `export default X`
// in the file, which is silence on six corpus cases rather than a crash.
func resolvesToImport(ctx rule.Context, identifier *ast.Node, target *ast.Symbol) bool {
	if ctx.TypeChecker.GetSymbolAtLocation(identifier) == target {
		return true
	}
	return ctx.TypeChecker.GetExportSpecifierLocalTargetSymbol(identifier) == target
}

// isTypeReference reports whether one identifier's occurrence is a type reference.
//
// See the rule's doc comment for what was measured here. The short form: `IsPartOfTypeNode` alone
// misses `typeof X` and the left of a qualified name, an export specifier's type-ness comes from
// the export's keyword rather than from its position, and a class `extends` clause must stay a
// value reference.
func isTypeReference(identifier *ast.Node) bool {
	if ast.IsPartOfTypeNode(identifier) {
		return true
	}

	for current := identifier; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindTypeQuery, ast.KindQualifiedName:
			// `type T = typeof X` and the `foo` of `type T = foo.Bar`. Both answer false to
			// IsPartOfTypeNode and both report upstream.
			return true

		case ast.KindExportSpecifier:
			// `export { type X }` marks the specifier; `export type { X }` marks the declaration
			// two levels up. Either one makes this a type reference and neither is visible from
			// the identifier's own position.
			if current.AsExportSpecifier().IsTypeOnly {
				return true
			}
			return isTypeOnlyExportDeclaration(current)

		case ast.KindExportAssignment:
			// `export = X` and `export default X`. Both are value exports, measured.
			return false

		case ast.KindExpressionWithTypeArguments:
			// A class `extends` clause. The emitted code needs the binding, so this is a value use
			// even though it sits inside a heritage clause.
			return false
		}

		if ast.IsStatement(current) {
			// Nothing above a statement can turn an expression into a type.
			return false
		}
	}
	return false
}

// isTypeOnlyExportDeclaration reports whether the export declaration containing a specifier carries
// the `type` keyword, as `export type { X }` does.
func isTypeOnlyExportDeclaration(specifier *ast.Node) bool {
	for current := specifier.Parent; current != nil; current = current.Parent {
		if current.Kind == ast.KindExportDeclaration {
			return current.AsExportDeclaration().IsTypeOnly
		}
		if ast.IsStatement(current) && current.Kind != ast.KindExportDeclaration {
			return false
		}
	}
	return false
}

// consistentTypeImportsWordList renders names the way upstream's message does.
//
// One name is bare, two are joined with `and` and no comma, and three or more take an Oxford comma
// before the final `and`. The three-name form is what a reimplementation gets wrong, and the corpus
// asserts it directly with `Imports A, C, and D are only used as type.`
func consistentTypeImportsWordList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
	}
}

// DecodeConsistentTypeImportsOptions maps the wire keys onto the settings the rule reads.
//
// Hand written rather than `rule.DecodeOptionsInto` because every default is non-zero, so a
// zero-value struct is not a valid configuration of this rule. An unrecognized enum value falls
// back to the default rather than disabling the arm: upstream refuses such a configuration outright
// and there is no error channel that reaches a user here, so the choice is between the documented
// behavior and silence, and silence would look like the rule working.
func DecodeConsistentTypeImportsOptions(raw []byte) (any, error) {
	options := DefaultConsistentTypeImportsOptions()

	decoded, err := rule.DecodeOptionsInto[consistentTypeImportsRawOptions]()(raw)
	if err != nil {
		return options, err
	}
	wire, _ := decoded.(consistentTypeImportsRawOptions)

	if wire.DisallowTypeAnnotations != nil {
		options.DisallowTypeAnnotations = *wire.DisallowTypeAnnotations
	}
	if wire.Prefer != nil {
		switch ConsistentTypeImportsPrefer(*wire.Prefer) {
		case ConsistentTypeImportsPreferTypeImports:
			options.Prefer = ConsistentTypeImportsPreferTypeImports
		case ConsistentTypeImportsPreferNoTypeImports:
			options.Prefer = ConsistentTypeImportsPreferNoTypeImports
		}
	}
	if wire.FixStyle != nil {
		switch ConsistentTypeImportsFixStyle(*wire.FixStyle) {
		case ConsistentTypeImportsFixStyleSeparate:
			options.FixStyle = ConsistentTypeImportsFixStyleSeparate
		case ConsistentTypeImportsFixStyleInline:
			options.FixStyle = ConsistentTypeImportsFixStyleInline
		}
	}

	return options, nil
}

// consistentTypeImportsRawOptions is the wire shape, with pointers so an absent key is
// distinguishable from a key set to the zero value. That distinction is the whole reason this
// struct exists separately: `disallowTypeAnnotations: false` and an absent key mean opposite things.
type consistentTypeImportsRawOptions struct {
	DisallowTypeAnnotations *bool   `json:"disallowTypeAnnotations"`
	FixStyle                *string `json:"fixStyle"`
	Prefer                  *string `json:"prefer"`
}
