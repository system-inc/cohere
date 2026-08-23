package nexus

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const constantCasingFile = "/repository/source/Thing.ts"
const constantCasingComponentFile = "/repository/source/Thing.tsx"

func TestConsistencyRequireConstantCasingFires(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		sourceText string
		wantId     string
	}{
		// The three clauses, each firing on its own failure.
		{
			"camelCase export", constantCasingFile,
			"export const numberAbsentPlaceholder = '-';\n",
			"requirePascalCaseExported",
		},
		{
			"PascalCase local", constantCasingFile,
			"const OrderColumns = [1, 2];\n",
			"requireCamelCaseLocal",
		},
		{
			"PascalCase exported function", constantCasingFile,
			"export const FormatNumber = (value: number) => String(value);\n",
			"requireCamelCaseFunction",
		},
		{
			"PascalCase exported instance", constantCasingFile,
			"class NetworkService {}\nexport const NetworkServiceSingleton = new NetworkService();\n",
			"requireCamelCaseInstance",
		},

		// Kind beats reach: a function stays camelCase even where PascalCase would be right for data.
		{
			"PascalCase local function", constantCasingFile,
			"const FormatNumber = function (value: number) { return String(value); };\n",
			"requireCamelCaseFunction",
		},
		// The shape with no local evidence at all: every call site is in another module, so the
		// factory's own name is what says a function comes back.
		{
			"factory-produced function", constantCasingFile,
			"import { RuleCreator } from 'x';\nexport const CreateEsLintRule = RuleCreator('a');\n",
			"requireCamelCaseFunction",
		},
		// Usage is the evidence: an ordinary call on the right, proven a function by being invoked.
		{
			"called like a function", constantCasingFile,
			"import { build } from 'x';\nconst Handler = build();\nHandler();\n",
			"requireCamelCaseFunction",
		},
		// The declared return type separates a function factory from an instance factory.
		{
			"local factory returning a function", constantCasingFile,
			"function makeMiddleware(): (input: string) => string {\n" +
				"    return function (input: string) { return input; };\n}\n" +
				"export const LocaleMiddleware = makeMiddleware();\n",
			"requireCamelCaseFunction",
		},
		{
			"local resolver returning a class", constantCasingFile,
			"class AgentManager {}\nfunction resolveAgentManager(): AgentManager {\n" +
				"    return new AgentManager();\n}\n" +
				"export const AgentManagerSingleton = resolveAgentManager();\n",
			"requireCamelCaseInstance",
		},
		// The cached-singleton guard, in both operand orders.
		{
			"cached singleton, construction on the right", constantCasingFile,
			"declare const globalForThing: { thing?: Thing };\nclass Thing {}\n" +
				"export const CachedThing = globalForThing.thing ?? new Thing();\n",
			"requireCamelCaseInstance",
		},
		{
			"cached singleton, construction on the left", constantCasingFile,
			"declare const globalForThing: { thing?: Thing };\nclass Thing {}\n" +
				"export const CachedThing = new Thing() || globalForThing.thing;\n",
			"requireCamelCaseInstance",
		},
		// A bare class annotation says what the value holds even when the initializer cannot.
		{
			"class type annotation", constantCasingFile,
			"import { Client } from 'x';\nimport { lookup } from 'y';\n" +
				"export const DiscordClient: Client = lookup();\n",
			"requireCamelCaseInstance",
		},

		// Separator folding: the suggestion has to be a name the rule would accept.
		{
			"snake_case local", constantCasingFile,
			"const user_id = 'x';\n",
			"requireCamelCaseLocal",
		},
		{
			"snake_case export", constantCasingFile,
			"export const apple_root_ca = 'x';\n",
			"requirePascalCaseExported",
		},

		// A PascalCase local in a .tsx file that is never rendered is still a PascalCase local. This
		// is what makes the JSX exemption resolve against usage rather than the file extension.
		{
			"unrendered PascalCase local in tsx", constantCasingComponentFile,
			"const OrderColumns = [1, 2];\n",
			"requireCamelCaseLocal",
		},
		// A name merely ending in Context is judged on its casing; only the call shape exempts.
		{
			"value named Context without the call", constantCasingFile,
			"const ThemeContext = { value: 1 };\n",
			"requireCamelCaseLocal",
		},
		// A lookup table is structurally an as-const object too, but its keys are labels rather than
		// members, so it is data and takes the exported/local split.
		{
			"as const lookup table is not an enum", constantCasingFile,
			"const classNames = { small: 'a', large: 'b' } as const;\nexport const table = classNames;\n",
			"requirePascalCaseExported",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ConsistencyRequireConstantCasing, testCase.fileName, testCase.sourceText)
			ruletest.ExpectFindings(t, result, testCase.wantId)
		})
	}
}

func TestConsistencyRequireConstantCasingStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		sourceText string
	}{
		// The three clauses, satisfied.
		{"PascalCase export", constantCasingFile, "export const OrderColumns = [1, 2];\n"},
		{"camelCase local", constantCasingFile, "const orderColumns = [1, 2];\n"},
		{
			"camelCase exported function", constantCasingFile,
			"export const formatNumber = (value: number) => String(value);\n",
		},
		{
			"camelCase exported instance", constantCasingFile,
			"class NetworkService {}\nexport const networkService = new NetworkService();\n",
		},

		// Only const carries the immutability the convention rests on.
		{"let is not const", constantCasingFile, "let OrderColumns = [1, 2];\n"},
		{"var is not const", constantCasingFile, "var OrderColumns = [1, 2];\n"},

		// Destructuring takes its names from the shape of the value rather than from a choice.
		{"destructured local", constantCasingFile, "const { OrderColumns } = source;\ndeclare const source: any;\n"},
		{"array destructuring", constantCasingFile, "const [First] = list;\ndeclare const list: string[];\n"},

		// A declaration describes something this file does not control, so the upstream spelling is
		// the contract. Lowercasing it would make the script throw while the type-checker stayed
		// happy, because the declaration is all TypeScript ever sees.
		{"declare const", constantCasingFile, "declare const DocumentApp: unknown;\n"},

		// Owned by consistency-no-screaming-snake-case, which has the better message for it.
		{"screaming snake local", constantCasingFile, "const MAX_RETRY_COUNT = 3;\n"},
		{"screaming snake export", constantCasingFile, "export const HTTP_TIMEOUT = 5000;\n"},

		// The trailing underscore escapes a keyword, so recasing produces a name the parser rejects.
		{"reserved word escape", constantCasingFile, "const do_ = 1;\n"},
		{"reserved word escape, exported", constantCasingFile, "export const class_ = 1;\n"},

		// A leading underscore marks a knowingly-unused binding, which is orthogonal to casing.
		{"leading underscore local", constantCasingFile, "const _results = [1];\n"},
		{"leading underscore export", constantCasingFile, "export const _Results = [1];\n"},

		// Built-in containers hold rather than act, so an exported one is exported data.
		{"exported Map keeps PascalCase", constantCasingFile, "export const Conversations = new Map();\n"},
		{"exported Set keeps PascalCase", constantCasingFile, "export const Seen = new Set();\n"},
		{"exported URL keeps PascalCase", constantCasingFile, "export const Endpoint = new URL('https://x');\n"},

		// React shapes, each of which needs its capital for a different reason.
		{"createContext", constantCasingFile, "import { createContext } from 'react';\nconst ThemeContext = createContext(null);\n"},
		{
			"React.createContext", constantCasingFile,
			"import * as React from 'react';\nconst ThemeContext = React.createContext(null);\n",
		},
		{
			"memo-wrapped component", constantCasingComponentFile,
			"import { memo } from 'react';\nexport const MenuSearch = memo(function MenuSearch() { return null; });\n",
		},
		{
			"FC-annotated component", constantCasingComponentFile,
			"import type { FC } from 'react';\nexport const ShareBlock: FC = function () { return null; };\n",
		},
		{
			"React.FC-annotated component", constantCasingComponentFile,
			"import * as React from 'react';\nexport const ShareBlock: React.FC = function () { return null; };\n",
		},
		// JSX owns the capital letter, so a rendered local keeps it.
		{
			"local rendered as an element", constantCasingComponentFile,
			"declare function getIcon(): any;\nexport function Row() {\n" +
				"    const Icon = getIcon();\n    return <Icon />;\n}\n",
		},
		// Handed to something else to render: the receiver will put it in element position.
		{
			"local handed to a JSX attribute", constantCasingComponentFile,
			"declare function getIcon(): any;\ndeclare function Panel(properties: { icon: any }): any;\n" +
				"export function Row() {\n    const Icon = getIcon();\n    return <Panel icon={Icon} />;\n}\n",
		},
		// The capital is what keeps the declaration from referencing itself.
		{
			"shadows a camelCase source", constantCasingComponentFile,
			"declare const processingIcon: any;\ndeclare const BrokenCircleIcon: any;\n" +
				"export function Row() {\n    const ProcessingIcon = processingIcon ?? BrokenCircleIcon;\n" +
				"    return ProcessingIcon;\n}\n",
		},

		// Mirrors an upstream constructor's own spelling, including through a fallback chain where
		// the constructor sits two levels down.
		{"re-binds a global constructor", constantCasingFile, "const NativeMap = globalThis.Map;\n"},
		{
			"constructor picked out of a fallback chain", constantCasingFile,
			"declare const polyfill: any;\n" +
				"const ResizeObserverConstructor = polyfill ?? (typeof window !== 'undefined' ? window.ResizeObserver : undefined);\n",
		},
		// A lowercase property read is not a constructor re-binding.
		{"lowercase property read is unaffected", constantCasingFile, "declare const counts: any;\nconst total = counts.sum;\n"},

		// An import wearing a const; import-require-node-namespace owns this spelling.
		{
			"dynamic import binding", constantCasingFile,
			"export async function run() {\n    const NodeFileSystem = await import('node:fs');\n" +
				"    return NodeFileSystem;\n}\n",
		},
		{
			"dynamic import reaching a default export", constantCasingFile,
			"export async function run() {\n    const Database = (await import('better-sqlite3')).default;\n" +
				"    return Database;\n}\n",
		},

		// consistency-require-type-suffix requires these to end in Kind, which forces PascalCase.
		{
			"const enum shape, string members", constantCasingFile,
			"const ButtonVariantKind = { Default: 'Default', Destructive: 'Destructive' } as const;\n" +
				"export const Used = ButtonVariantKind;\n",
		},
		{
			"const enum shape, numeric members", constantCasingFile,
			"const DirectionKind = { Positive: 1, Negative: -1 } as const;\nexport const Used = DirectionKind;\n",
		},

		// The suffixes our conventions give to shapes rather than to classes, and the generic
		// container that turned 129 exported lookup tables into false reports.
		{
			"Record annotation is data", constantCasingFile,
			"export const OrderColumns: Record<string, number> = { a: 1 };\n",
		},
		// The suffixes our conventions give to shapes rather than to classes. The factory is
		// imported on purpose: a locally declared one whose return type is PascalCase reaches the
		// resolver clause first and is judged an instance, which is correct and a different path.
		{
			"Properties-suffixed annotation is data", constantCasingFile,
			"import type { RowProperties } from 'x';\nimport { read } from 'y';\n" +
				"export const DefaultRow: RowProperties = read();\n",
		},
		{
			"Options-suffixed annotation is data", constantCasingFile,
			"import type { RowOptions } from 'x';\nimport { read } from 'y';\n" +
				"export const DefaultRow: RowOptions = read();\n",
		},
		{
			"object literal outranks a class-shaped annotation", constantCasingFile,
			"import type { Transition } from 'framer-motion';\n" +
				"export const CollapsibleTransition: Transition = { type: 'spring' };\n",
		},

		// A function alias, resolved through the file rather than through a type.
		{
			"alias for a local function", constantCasingFile,
			"function numberCompact(value: number) { return String(value); }\n" +
				"export const formatNumber = numberCompact;\n",
		},
		{
			"alias for a camelCase import is read as a function", constantCasingFile,
			"import { numberCompact } from 'x';\nexport const formatNumber = numberCompact;\n",
		},
		// The inverse: a PascalCase import is not read as a function, so the export keeps PascalCase.
		{
			"alias for a PascalCase import is data", constantCasingFile,
			"import { ProjectRoot } from 'x';\nexport const AhraProjectRoot = ProjectRoot;\n",
		},

		// A construction cast to something else. This is the shape of every generated GraphQL
		// document constant, and unwrapping the cast to find the "new" underneath produced 95
		// findings on a tree whose baseline is clean. A value cast away from its constructor is not
		// being handed to the reader as that class, and this rule has no type information to
		// overrule the author with, so the instance clause reads the initializer raw here.
		{
			"construction cast away from its class", constantCasingFile,
			"export class TypedDocumentString<R, V> { constructor(public value: string) {} }\n" +
				"export const AccountDocument = new TypedDocumentString('query {}') as unknown as " +
				"TypedDocumentString<number, number>;\n",
		},

		{"no declarations", constantCasingFile, "export function run() {\n    return 1;\n}\n"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ConsistencyRequireConstantCasing, testCase.fileName, testCase.sourceText)
			ruletest.ExpectClean(t, result)
		})
	}
}

// The framework option exists because a name a library reads verbatim is a contract rather than a
// choice, and the rule cannot know which names those are.
func TestConsistencyRequireConstantCasingFrameworkNames(t *testing.T) {
	source := "export const runtime = 'edge';\n"

	reported := ruletest.Run(t, ConsistencyRequireConstantCasing, constantCasingFile, source)
	ruletest.ExpectFindings(t, reported, "requirePascalCaseExported")

	exempt := ruletest.RunWithOptions(t, ConsistencyRequireConstantCasing, constantCasingFile, source,
		ConsistencyRequireConstantCasingOptions{FrameworkConstantNames: []string{"runtime"}})
	ruletest.ExpectClean(t, exempt)
}

// The suggestion has to be a name the rule's own predicates accept, or the reader who takes it gets
// the same report again. This is the check that catches a converter handing back its own input.
func TestConsistencyRequireConstantCasingSuggestionsAreAccepted(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		wantAccept func(string) bool
	}{
		{"snake_case to camelCase", "const user_id = 'x';\n", isCamelCase},
		{"snake_case to PascalCase", "export const apple_root_ca = 'x';\n", isPascalCase},
		{"screaming part folded", "const utm_SOURCE = 'x';\n", isCamelCase},
		{"screaming part folded, exported", "export const utm_SOURCE = 'x';\n", isPascalCase},
		{"camelCase to PascalCase", "export const numberAbsentPlaceholder = '-';\n", isPascalCase},
		{"PascalCase to camelCase", "const OrderColumns = [1];\n", isCamelCase},
		{"leading underscore kept", "const _order_columns = [1];\n", isCamelCase},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ConsistencyRequireConstantCasing, constantCasingFile, testCase.sourceText)
			if len(result.Diagnostics) != 1 {
				t.Fatalf("want exactly one finding, got %d", len(result.Diagnostics))
			}

			// The message quotes the suggestion in parentheses, which is where the reader takes it
			// from, so that is the string this reads back.
			description := result.Diagnostics[0].Message.Description
			opening := strings.Index(description, `("`)
			if opening < 0 {
				t.Fatalf("no suggestion in message: %s", description)
			}
			rest := description[opening+2:]
			closing := strings.Index(rest, `"`)
			if closing < 0 {
				t.Fatalf("unterminated suggestion in message: %s", description)
			}
			suggestion := rest[:closing]

			if !testCase.wantAccept(suggestion) {
				t.Fatalf("suggestion %q is not a casing the rule accepts", suggestion)
			}
		})
	}
}

// Acceptance is necessary and not sufficient, which is the whole reason this second test exists.
//
// isCamelCase accepts any letters after the first, so the un-folded "utmSOURCE" passes it while
// being exactly the shape the folding exists to prevent. A test that only asked "is the suggestion
// acceptable" stayed green with the fold removed, so it was measuring the wrong property. These
// assert the exact string, which is the only thing that pins the fold down.
func TestConsistencyRequireConstantCasingSuggestionText(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		want       string
	}{
		// An all-uppercase part has no internal boundaries to preserve, so it is lowered whole.
		// Leaving it produces "utmSOURCE", which is neither casing.
		{"all-uppercase part is folded whole", "const utm_SOURCE = 'x';\n", "utmSource"},
		{"all-uppercase part is folded whole, exported", "export const utm_SOURCE = 'x';\n", "UtmSource"},
		{"underscores become word boundaries", "const user_id = 'x';\n", "userId"},
		{"underscores become word boundaries, exported", "export const apple_root_ca = 'x';\n", "AppleRootCa"},
		// The leading underscore is the unused-binding convention and is not this rule's to remove.
		{"leading underscore survives", "const _order_columns = [1];\n", "_orderColumns"},
		{"internal boundaries survive", "export const numberAbsentPlaceholder = '-';\n", "NumberAbsentPlaceholder"},
		{"internal boundaries survive, lowered", "const OrderColumns = [1];\n", "orderColumns"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ConsistencyRequireConstantCasing, constantCasingFile, testCase.sourceText)
			if len(result.Diagnostics) != 1 {
				t.Fatalf("want exactly one finding, got %d", len(result.Diagnostics))
			}
			if suggestion := suggestionFrom(t, result.Diagnostics[0].Message.Description); suggestion != testCase.want {
				t.Fatalf("suggestion = %q, want %q", suggestion, testCase.want)
			}
		})
	}
}

// suggestionFrom reads the name a message offers, which is quoted in parentheses and is where the
// reader takes it from.
func suggestionFrom(t *testing.T, description string) string {
	t.Helper()

	opening := strings.Index(description, `("`)
	if opening < 0 {
		t.Fatalf("no suggestion in message: %s", description)
	}
	rest := description[opening+2:]
	closing := strings.Index(rest, `"`)
	if closing < 0 {
		t.Fatalf("unterminated suggestion in message: %s", description)
	}
	return rest[:closing]
}
