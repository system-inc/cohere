package structure

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// Typed fixtures resolve their imports against real source declarations.
const typeSuffixFile = "source/components/Button.tsx"

func TestReactComponentRequirePropertiesTypeSuffixFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		{
			"a component whose type is declared elsewhere",
			"import type { ButtonProps } from './Types';\n" +
				"export function Button(properties: ButtonProps) {\n    return <button>{properties.label}</button>;\n}\n",
			[]string{"useComponentPropertyTypeSuffix"},
		},
		{
			"an arrow component",
			"import type { ButtonProps } from './Types';\n" +
				"export const Button = (properties: ButtonProps) => <button>{properties.label}</button>;\n",
			[]string{"useComponentPropertyTypeSuffix"},
		},
		// Two findings for one wrong name, at the use and at the declaration. A reader fixing this
		// touches both, and seeing only one would leave the rename half done.
		{
			"an interface declared after the component",
			"export function Button(properties: ButtonInterface) {\n" +
				"    return <button>{properties.label}</button>;\n}\n" +
				"interface ButtonInterface { label: string }\n",
			[]string{"useComponentPropertyTypeSuffix", "useInterfaceSuffix"},
		},
		{
			"a type alias declared after the component",
			"export function Button(properties: ButtonProps) {\n" +
				"    return <button>{properties.label}</button>;\n}\n" +
				"type ButtonProps = { label: string };\n",
			[]string{"useComponentPropertyTypeSuffix", "useTypeAliasSuffix"},
		},
		// A name holding Properties as a substring rather than a suffix is still wrong.
		{
			"a type with Properties in the middle",
			"import type { ButtonPropertiesBag } from './Types';\n" +
				"export function Button(properties: ButtonPropertiesBag) {\n" +
				"    return <button>{properties.label}</button>;\n}\n",
			[]string{"useComponentPropertyTypeSuffix"},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectFindings(test, runTypeSuffix(test,
				typeSuffixFile, testCase.sourceText), testCase.wantIds...)
		})
	}
}

func TestReactComponentRequirePropertiesTypeSuffixStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		fileName   string
		sourceText string
	}{
		// The shape the rule asks for.
		{
			"a correctly suffixed type",
			typeSuffixFile,
			"interface ButtonProperties { label: string }\n" +
				"export function Button(properties: ButtonProperties) {\n" +
				"    return <button>{properties.label}</button>;\n}\n",
		},
		// The component-name gate. A lowercase function's parameter type is nobody's business.
		{
			"a lowercase function taking a badly named type",
			typeSuffixFile,
			"import type { ButtonProps } from './Types';\n" +
				"export function build(properties: ButtonProps) {\n    return <button />;\n}\n",
		},
		// An inline type has no name to judge, and a destructured parameter has no type reference.
		{
			"an inline object type",
			typeSuffixFile,
			"export function Button(properties: { label: string }) {\n" +
				"    return <button>{properties.label}</button>;\n}\n",
		},
		{
			"a destructured parameter",
			typeSuffixFile,
			"export function Button({ label }: { label: string }) {\n    return <button>{label}</button>;\n}\n",
		},
		// A destructured parameter with a badly named type reference. This is the shape the tree
		// actually has, in MenuItem.tsx, and the rule reported it until the identifier check was
		// added: a genuine violation of the convention that the gate does not report, which would
		// have become a second only-cohere divergence arrived at by accident rather than a ruling.
		//
		// Destructuring is `react-component-no-destructuring`'s business, and a destructured
		// parameter's type is often composed (`TypeA & { ... }`) rather than the single name this
		// rule renames.
		{
			"a destructured parameter with a badly named type",
			typeSuffixFile,
			"import type { MenuItemInterface } from './Types';\n" +
				"export function MenuItem({ label, ...buttonProperties }: MenuItemInterface) {\n" +
				"    return <button {...buttonProperties}>{label}</button>;\n}\n",
		},
		{
			"a component with no parameters",
			typeSuffixFile,
			"export function Button() {\n    return <button />;\n}\n",
		},
		// An interface not used as a component's property type is not this rule's business, even
		// when it is badly named.
		{
			"an unrelated interface",
			typeSuffixFile,
			"interface ButtonProps { label: string }\n" +
				"export function Button(properties: { label: string }) {\n" +
				"    return <button>{properties.label}</button>;\n}\n",
		},
		// Outside a React file the rule declines before looking.
		{
			"a component-shaped function in a ts file",
			"source/api/Thing.ts",
			"import React from 'react';\nimport type { ButtonProps } from './Types';\n" +
				"export function Button(properties: ButtonProps) {\n" +
				"    React.useEffect(() => {}, []);\n    return properties;\n}\n",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, runTypeSuffix(test,
				testCase.fileName, testCase.sourceText))
		})
	}
}

// Declaration order used to decide whether the declaration itself was reported, and that was wrong.
//
// The previous version of this test pinned the order dependence deliberately, reasoning that the
// use site is reported either way and that fixing it costs a second walk of the file for a second
// finding about the same rename. Both halves of that were true. The conclusion was still wrong,
// and the reason is the fix rather than the finding.
//
// On the declaration-first ordering the rule emitted one finding carrying one fix, and that fix
// renamed the *usage*. So `CardProps` became `CardProperties` at the call site while the
// declaration kept its old name, and the result parses and does not compile:
//
//	dangling.ts(2,34): error TS2304: Cannot find name 'CardProperties'
//
// A fix engine's parse guard structurally cannot refuse that, because the output is syntactically
// valid. And declaration-first is the conventional ordering, so it was the common case.
//
// The trade was priced as "a second finding about the same rename" when what it actually bought was
// "the fix does not break the file". Recorded at length because the reasoning was careful and still
// reached the wrong answer: it weighed the cost of the walk against the value of the finding, and
// the finding was not the thing at stake.
//
// The fix derives the corrected name rather than appending to the wrong one.
func TestReactComponentRequirePropertiesTypeSuffixFixDerivesTheName(test *testing.T) {
	test.Parallel()

	rule_testing.ExpectFixedSource(test,
		runTypeSuffix(test, typeSuffixFile,
			"import type { ButtonProps } from './Types';\n"+
				"export function Button(properties: ButtonProps) {\n    return <button />;\n}\n"),
		"import type { ButtonProps } from './Types';\n"+
			"export function Button(properties: ButtonProperties) {\n    return <button />;\n}\n")

	// An Interface suffix is replaced rather than appended to, which is the case that separates a
	// derived rename from a blind one: appending gives ButtonInterfaceProperties.
	rule_testing.ExpectFixedSource(test,
		runTypeSuffix(test, typeSuffixFile,
			"import type { ButtonInterface } from './Types';\n"+
				"export function Button(properties: ButtonInterface) {\n    return <button />;\n}\n"),
		"import type { ButtonInterface } from './Types';\n"+
			"export function Button(properties: ButtonProperties) {\n    return <button />;\n}\n")

	// A name with no known suffix gets one appended.
	rule_testing.ExpectFixedSource(test,
		runTypeSuffix(test, typeSuffixFile,
			"import type { ButtonBag } from './Types';\n"+
				"export function Button(properties: ButtonBag) {\n    return <button />;\n}\n"),
		"import type { ButtonBag } from './Types';\n"+
			"export function Button(properties: ButtonBagProperties) {\n    return <button />;\n}\n")
}

// Both orderings, asserted separately, because either alone passes a broken rule.
//
// This is the fixture the original pair could not supply. The rule filled its map of
// component property types from the component listener and read it from the declaration listener,
// one walk in source order, so an interface declared above its component was visited first, saw an
// empty map, and was skipped.
//
// The failure was silent and it produced uncompilable source. The usage still got its fix, so
// `CardProps` became `CardProperties` at the call site while the declaration kept its old name, and
// the result parses. A fix engine's parse guard structurally cannot refuse that.
//
// Declaration-first is the conventional ordering, so this was the common case rather than an edge,
// and the existing fixtures passed because on that ordering the rule emits one finding rather than
// two. `ExpectFindings` asserts a count and cannot tell a missing second finding from correct
// behavior.
func TestReactComponentRequirePropertiesTypeSuffixIsOrderIndependent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{
			name: "the declaration comes first",
			sourceText: "interface CardProps { a: string }\n" +
				"export function Card(properties: CardProps) { return null; }\n",
		},
		{
			name: "the component comes first",
			sourceText: "export function Card(properties: CardProps) { return null; }\n" +
				"interface CardProps { a: string }\n",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runTypeSuffix(test,
				typeSuffixFile, testCase.sourceText)

			// Both halves have to be renamed or the file stops compiling, so the count is the
			// assertion rather than a detail of it.
			if len(result.Diagnostics) != 2 {
				test.Fatalf("wanted two findings, one for the usage and one for the declaration, got %d",
					len(result.Diagnostics))
			}

			// And both have to carry a fix. A finding with no fix leaves the same broken half
			// behind while looking like the rule saw it.
			for index, diagnostic := range result.Diagnostics {
				if len(diagnostic.Fixes) != 1 {
					test.Fatalf("finding %d carries %d fixes, wanted one", index, len(diagnostic.Fixes))
				}
			}
		})
	}
}

// A rename onto a name the file already declares is refused, and the finding stays.
//
// Found on Kirk's real tree rather than in a corpus. `MenuItem.tsx` declares both spellings, which
// is what a codebase mid-migration looks like by definition, and the fix rewrote
//
//	export type MenuItemInterface = MenuItemProperties & { ... }
//
// into a type alias whose name is its own right-hand side:
//
//	TS2300: Duplicate identifier 'MenuItemProperties'
//	TS2456: Type alias 'MenuItemProperties' circularly references itself
//
// Both parse. Same class as the ordering defect this rule shipped an hour earlier: valid syntax,
// broken program, and a fix engine's refusal guard has nothing to catch.
//
// The rule renames toward a fixed convention, so a collision is not an exotic case. Any file
// holding both spellings has one, and holding both spellings is exactly what migrating looks like.
//
// The finding is kept and only the fix is withheld. The name still violates the convention, and
// which of the two declarations survives is a decision only the author can make.
func TestReactComponentRequirePropertiesTypeSuffixRefusesATakenName(test *testing.T) {
	test.Parallel()

	source := "export interface MenuItemProperties { a: string }\n" +
		"export type MenuItemInterface = MenuItemProperties;\n" +
		"export function MenuItem(properties: MenuItemInterface) { return null; }\n"

	result := runTypeSuffix(test, typeSuffixFile, source)
	if len(result.Diagnostics) == 0 {
		test.Fatal("wanted the convention violation still reported, got no findings")
	}
	for index, diagnostic := range result.Diagnostics {
		if len(diagnostic.Fixes) != 0 {
			test.Fatalf("finding %d proposes %d fixes onto a name the file already declares, wanted none",
				index, len(diagnostic.Fixes))
		}
	}
}

// The ordinary case still carries its fix, which is what keeps the guard from being a silencer.
//
// Without this, disabling every fix in the rule passes the test above, and a rule that proposes
// nothing is indistinguishable from one that correctly refuses one collision.
func TestReactComponentRequirePropertiesTypeSuffixStillFixesWhenTheNameIsFree(test *testing.T) {
	test.Parallel()

	source := "export interface MenuItemInterface { a: string }\n" +
		"export function MenuItem(properties: MenuItemInterface) { return null; }\n"

	result := runTypeSuffix(test, typeSuffixFile, source)
	if len(result.Diagnostics) != 2 {
		test.Fatalf("wanted two findings, got %d", len(result.Diagnostics))
	}
	for index, diagnostic := range result.Diagnostics {
		if len(diagnostic.Fixes) != 1 {
			test.Fatalf("finding %d carries %d fixes, wanted one", index, len(diagnostic.Fixes))
		}
	}
}

// nextRouteTypes stands in for the declaration file Next generates and next-env.d.ts pulls in:
// `PageProps` and `LayoutProps` are globals carrying the framework's names.
const nextRouteTypes = "declare global {\n" +
	"    interface PageProps<Route extends string> { params: Promise<{ route: Route }> }\n" +
	"    interface LayoutProps<Route extends string> { params: Promise<{ route: Route }>; children: unknown }\n" +
	"}\nexport {};\n"

// dependencyFiles is a program holding every kind of dependency the rule leaves alone: Next's globals in
// a declaration file, a stand-in React whose types are a declaration file under node_modules, and a
// package that ships plain TypeScript under node_modules, which is a dependency's although it is not a
// declaration file.
var dependencyFiles = map[string]string{
	".next/types/routes.d.ts":         nextRouteTypes,
	"next-env.d.ts":                   "/// <reference path=\"./.next/types/routes.d.ts\" />\n",
	"node_modules/react/package.json": `{"name": "react", "types": "index.d.ts"}`,
	"node_modules/react/index.d.ts":   "export type ComponentProps<Tag extends string> = { tag: Tag };\n",
	"node_modules/kit/package.json":   `{"name": "kit", "types": "index.ts"}`,
	"node_modules/kit/index.ts":       "export interface KitProps { label: string }\n",
}

// runWithDependencies runs the rule on subject, written at fileName beside dependencyFiles.
func runWithDependencies(test *testing.T, fileName string, subject string) rule_testing.Result {
	test.Helper()
	files := map[string]string{fileName: subject}
	for name, contents := range dependencyFiles {
		files[name] = contents
	}
	return rule_testing.RunTypedFiles(test, ReactComponentRequirePropertiesTypeSuffix, files, fileName)
}

// A type a dependency declares keeps the dependency's name (#kk5kk04). Ownership is decided by where the
// type is declared, so Next's globals, React's types and a package's own TypeScript all pass, each
// through the path that can tell it apart: a declaration file, a node_modules path, and an import
// followed to what it names.
func TestReactComponentRequirePropertiesTypeSuffixLeavesADependencysTypeAlone(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		fileName   string
		sourceText string
	}{
		{
			"Next's global PageProps",
			"app/x/page.tsx",
			"export default function Page(properties: PageProps<'/x'>) {\n    return <main>{String(properties)}</main>;\n}\n",
		},
		{
			"Next's global LayoutProps",
			"app/x/layout.tsx",
			"export default function Layout(properties: LayoutProps<'/x'>) {\n    return <main>{String(properties.children)}</main>;\n}\n",
		},
		{
			"React's ComponentProps, imported by name",
			typeSuffixFile,
			"import type { ComponentProps } from 'react';\n" +
				"export function Button(properties: ComponentProps<'button'>) {\n    return <button>{properties.tag}</button>;\n}\n",
		},
		{
			"React's ComponentProps, through the namespace",
			typeSuffixFile,
			"import * as React from 'react';\n" +
				"export function Button(properties: React.ComponentProps<'button'>) {\n    return <button>{properties.tag}</button>;\n}\n",
		},
		{
			"a package's type shipped as plain TypeScript under node_modules",
			typeSuffixFile,
			"import type { KitProps } from 'kit';\n" +
				"export const Button = (properties: KitProps) => <button>{properties.label}</button>;\n",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, runWithDependencies(test, testCase.fileName, testCase.sourceText))
		})
	}
}

// The fix used to rename Next's global at the parameter, and the program stopped type checking.
//
// `PageProps<'/x'>` became `PageProperties<'/x'>`, which parses and names nothing, so the fix engine's
// parse guard had nothing to refuse. The rule now reports nothing there, so there is no fix to apply,
// while a component of ours in the same file keeps both its findings and both its fixes: skipping the
// dependency's type is not a silencer for the file.
func TestReactComponentRequirePropertiesTypeSuffixNeverRenamesADependencysType(test *testing.T) {
	test.Parallel()

	result := runWithDependencies(test, "app/x/page.tsx",
		"function Card(properties: CardProps) {\n    return <p>{properties.label}</p>;\n}\n"+
			"export default function Page(properties: PageProps<'/x'>) {\n    return <Card label={String(properties)} />;\n}\n"+
			"interface CardProps { label: string }\n")
	rule_testing.ExpectFixedSource(test, result,
		"function Card(properties: CardProperties) {\n    return <p>{properties.label}</p>;\n}\n"+
			"export default function Page(properties: PageProps<'/x'>) {\n    return <Card label={String(properties)} />;\n}\n"+
			"interface CardProperties { label: string }\n")
	if len(result.Diagnostics) != 2 {
		test.Fatalf("wanted the two findings for CardProps and none for PageProps, got %d", len(result.Diagnostics))
	}
}

// A dependency's name is not collected for the declaration findings, so a declaration of ours that
// happens to share it, in a scope of its own, is not renamed on its account. Collecting it reported the
// helper's interface and renamed it to `KitProperties`, a rename nothing asked for.
func TestReactComponentRequirePropertiesTypeSuffixDoesNotCollectADependencysName(test *testing.T) {
	test.Parallel()

	rule_testing.ExpectClean(test, runWithDependencies(test, typeSuffixFile,
		"import type { KitProps } from 'kit';\n"+
			"export function Button(properties: KitProps) {\n    return <button>{properties.label}</button>;\n}\n"+
			"export function describe() {\n    interface KitProps { label: string }\n"+
			"    const sample: KitProps = { label: 'a' };\n    return sample.label;\n}\n"))
}

// Our own interface without the suffix still fails, at the use and at the declaration, each with its fix,
// in a program that also holds the dependencies: the predicate keeps what our source declares.
func TestReactComponentRequirePropertiesTypeSuffixStillFixesOurOwnTypeBesideDependencies(test *testing.T) {
	test.Parallel()

	rule_testing.ExpectFixedSource(test,
		runWithDependencies(test, typeSuffixFile,
			"export function Button(properties: ButtonProps) {\n    return <button>{properties.label}</button>;\n}\n"+
				"export interface ButtonProps { label: string }\n"),
		"export function Button(properties: ButtonProperties) {\n    return <button>{properties.label}</button>;\n}\n"+
			"export interface ButtonProperties { label: string }\n")
}

// Without a checker no declaration can be established, so no rename is offered.
func TestReactComponentRequirePropertiesTypeSuffixWithoutACheckerIsSilent(test *testing.T) {
	test.Parallel()
	rule_testing.ExpectClean(
		test,
		rule_testing.Run(test, ReactComponentRequirePropertiesTypeSuffix, "/repository/app/x/page.tsx",
			"export default function Page(properties: PageProps<'/x'>) { return null; }"),
	)
}

// Missing generated types are compiler errors, not names this rule may invent.
func TestReactComponentRequirePropertiesTypeSuffixRequiresADeclaration(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name, source, dependency, fixed string
		findings                        int
	}{
		{name: "unresolved global", source: "export function Page(properties: PageProps) { return null; }"},
		{
			name:   "unresolved import",
			source: "import type { PageProps } from './missing'; export function Page(properties: PageProps) { return null; }",
		},
		{
			name:   "unresolved qualified name",
			source: "export function Page(properties: Missing.PageProps) { return null; }",
		},
		{
			name:   "unresolved name must not collect a different declaration",
			source: "export function Page(properties: PageProps) { return null; } function helper() { interface PageProps { label: string } }",
		},
		{
			name:     "our declaration",
			source:   "interface PageProps { label: string } export function Page(properties: PageProps) { return null; }",
			fixed:    "interface PageProperties { label: string } export function Page(properties: PageProperties) { return null; }",
			findings: 2,
		},
		{
			name:       "dependency declaration",
			source:     "export function Page(properties: PageProps) { return null; }",
			dependency: "interface PageProps { label: string }",
		},
	}
	for _, tc := range cases {
		test.Run(tc.name, func(test *testing.T) {
			test.Parallel()
			files := map[string]string{"app/page.tsx": tc.source}
			if tc.dependency != "" {
				files["routes.d.ts"] = tc.dependency
			}
			result := rule_testing.RunTypedFiles(test, ReactComponentRequirePropertiesTypeSuffix, files, "app/page.tsx")
			if len(result.Diagnostics) != tc.findings {
				test.Fatalf("got %d findings, want %d: %v", len(result.Diagnostics), tc.findings, result.MessageIds())
			}
			if tc.fixed != "" {
				rule_testing.ExpectFixedSource(test, result, tc.fixed+"\n")
			} else {
				for _, diagnostic := range result.Diagnostics {
					if len(diagnostic.Fixes) != 0 {
						test.Fatal("unexpected fix")
					}
				}
			}
		})
	}
}

// Imports used by the positive fixtures must resolve, not merely look like imports.
func runTypeSuffix(test *testing.T, fileName, source string) rule_testing.Result {
	test.Helper()
	return rule_testing.RunTypedFiles(test, ReactComponentRequirePropertiesTypeSuffix, map[string]string{
		fileName:                     source,
		"source/components/Types.ts": "export interface ButtonProps { label: string } export interface ButtonInterface { label: string } export interface ButtonPropertiesBag { label: string } export interface ButtonBag { label: string }",
	}, fileName)
}
