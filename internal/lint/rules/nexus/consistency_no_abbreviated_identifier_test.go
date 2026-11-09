package nexus

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/lint/testing"
)

const abbreviatedFile = "/repository/source/Thing.tsx"

func TestConsistencyNoAbbreviatedIdentifierFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		// Whole-word abbreviations.
		{"prop", "const prop = 1;\n", []string{"abbreviatedIdentifier"}},
		{"props", "const props = 1;\n", []string{"abbreviatedIdentifier"}},
		{"param", "const param = 1;\n", []string{"abbreviatedIdentifier"}},
		{"params", "const params = 1;\n", []string{"abbreviatedIdentifier"}},
		{"ref", "const ref = 1;\n", []string{"abbreviatedIdentifier"}},
		{"config", "const config = 1;\n", []string{"abbreviatedIdentifier"}},
		{"idx", "const idx = 0;\n", []string{"abbreviatedIdentifier"}},
		{"arg", "const arg = 1;\n", []string{"abbreviatedIdentifier"}},
		{"args", "const args = 1;\n", []string{"abbreviatedIdentifier"}},
		{"acc", "const acc = 0;\n", []string{"abbreviatedIdentifier"}},
		{"char", "const char = 'a';\n", []string{"abbreviatedIdentifier"}},
		{"fn", "const fn = () => 1;\n", []string{"abbreviatedIdentifier"}},
		{"str", "const str = 'a';\n", []string{"abbreviatedIdentifier"}},
		{"val", "const val = 1;\n", []string{"abbreviatedIdentifier"}},
		{"arr", "const arr = [];\n", []string{"abbreviatedIdentifier"}},
		{"obj", "const obj = {};\n", []string{"abbreviatedIdentifier"}},
		{"num", "const num = 1;\n", []string{"abbreviatedIdentifier"}},
		{"res", "const res = 1;\n", []string{"abbreviatedIdentifier"}},
		{"err", "const err = 1;\n", []string{"abbreviatedIdentifier"}},
		{"req", "const req = 1;\n", []string{"abbreviatedIdentifier"}},
		{"msg", "const msg = 1;\n", []string{"abbreviatedIdentifier"}},
		{"min", "const min = 1;\n", []string{"abbreviatedIdentifier"}},
		{"max", "const max = 1;\n", []string{"abbreviatedIdentifier"}},
		{"ctx", "const ctx = 1;\n", []string{"abbreviatedIdentifier"}},
		{"db", "const db = 1;\n", []string{"abbreviatedIdentifier"}},
		{"tx", "const tx = 1;\n", []string{"abbreviatedIdentifier"}},
		{"opts", "const opts = {};\n", []string{"abbreviatedIdentifier"}},
		{"cur", "const cur = 1;\n", []string{"abbreviatedIdentifier"}},
		{"pct", "const pct = 1;\n", []string{"abbreviatedIdentifier"}},
		{"prev", "const prev = 1;\n", []string{"abbreviatedIdentifier"}},

		// camelCase prefixes.
		{"ctx prefix", "const ctxValue = 1;\n", []string{"abbreviatedIdentifier"}},
		{"db prefix", "const dbCode = 1;\n", []string{"abbreviatedIdentifier"}},
		{"tx prefix", "const txHash = 1;\n", []string{"abbreviatedIdentifier"}},
		{"opts prefix", "const optsForRun = 1;\n", []string{"abbreviatedIdentifier"}},
		{"cur prefix", "const curStep = 1;\n", []string{"abbreviatedIdentifier"}},
		{"pct prefix", "const pctComplete = 1;\n", []string{"abbreviatedIdentifier"}},
		{"prev prefix", "const prevStep = 1;\n", []string{"abbreviatedIdentifier"}},
		{"idx prefix", "const idxStart = 0;\n", []string{"abbreviatedIdentifier"}},
		{"config prefix", "const configValue = 1;\n", []string{"abbreviatedIdentifier"}},
		{"prop prefix", "const propName = 1;\n", []string{"abbreviatedIdentifier"}},
		{"params prefix", "const paramsText = 1;\n", []string{"abbreviatedIdentifier"}},
		{"ref prefix", "const refCount = 1;\n", []string{"abbreviatedIdentifier"}},
		{"arg prefix", "const argCount = 1;\n", []string{"abbreviatedIdentifier"}},
		{"args prefix", "const argsList = 1;\n", []string{"abbreviatedIdentifier"}},
		{"char prefix", "const charSet = 1;\n", []string{"abbreviatedIdentifier"}},
		{"fn prefix", "const fnCache = 1;\n", []string{"abbreviatedIdentifier"}},
		{"str prefix", "const strValue = 1;\n", []string{"abbreviatedIdentifier"}},
		{"max prefix", "const maxAge = 1;\n", []string{"abbreviatedIdentifier"}},
		{"err prefix", "const errCount = 1;\n", []string{"abbreviatedIdentifier"}},

		// Suffixes.
		{"Prop suffix", "const rowProp = 1;\n", []string{"abbreviatedSuffix"}},
		{"Props suffix", "const rowProps = 1;\n", []string{"abbreviatedSuffix"}},
		{"Param suffix", "const queryParam = 1;\n", []string{"abbreviatedSuffix"}},
		{"Params suffix", "const searchParams = 1;\n", []string{"abbreviatedSuffix"}},
		{"Ref suffix", "const nodeRef = 1;\n", []string{"abbreviatedSuffix"}},
		{"Config suffix", "const buildConfig = 1;\n", []string{"abbreviatedSuffix"}},
		{"Idx suffix", "const rowIdx = 1;\n", []string{"abbreviatedSuffix"}},
		{"Arg suffix", "const firstArg = 1;\n", []string{"abbreviatedSuffix"}},
		{"Args suffix", "const esBuildArgs = 1;\n", []string{"abbreviatedSuffix"}},
		{"Char suffix", "const lastChar = 1;\n", []string{"abbreviatedSuffix"}},
		{"Fn suffix", "const compareFn = 1;\n", []string{"abbreviatedSuffix"}},
		{"Str suffix", "const queryStr = 1;\n", []string{"abbreviatedSuffix"}},
		{"Val suffix", "const inputVal = 1;\n", []string{"abbreviatedSuffix"}},
		{"Arr suffix", "const itemArr = 1;\n", []string{"abbreviatedSuffix"}},
		{"Obj suffix", "const targetObj = 1;\n", []string{"abbreviatedSuffix"}},
		{"Num suffix", "const pageNum = 1;\n", []string{"abbreviatedSuffix"}},
		{"Res suffix", "const fetchRes = 1;\n", []string{"abbreviatedSuffix"}},
		{"Err suffix", "const parseErr = 1;\n", []string{"abbreviatedSuffix"}},
		{"Req suffix", "const httpReq = 1;\n", []string{"abbreviatedSuffix"}},
		{"Msg suffix", "const errorMsg = 1;\n", []string{"abbreviatedSuffix"}},
		{"Min suffix", "const pageMin = 1;\n", []string{"abbreviatedSuffix"}},
		{"Max suffix", "const pageMax = 1;\n", []string{"abbreviatedSuffix"}},

		// Milliseconds, matched wherever the word sits rather than only at the end. Anchoring to
		// the end is how `maximumAgeMsHalf` sat unreported while every sibling was caught.
		{"Ms at the end", "const maximumAgeMs = 1;\n", []string{"millisecondSuffix"}},
		{"Ms in the middle", "const maximumAgeMsHalf = 1;\n", []string{"millisecondSuffix"}},

		// Word segments, matched wherever they sit. The anchored branches miss these.
		{"Cwd segment", "const originalInitCwd = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Dir segment", "const outputDirPath = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Env segment", "const buildEnvName = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Cli segment", "const runCliCommand = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Len segment", "const bufferLen = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Seq segment", "const eventSeq = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Db segment", "const primaryDbHost = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Tx segment", "const pendingTxHash = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Vars segment", "const themeVars = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Var segment", "const themeVarName = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Pos segment", "const resultPos = 1;\n", []string{"abbreviatedWordSegment"}},
		{"Sb segment", "const scrollSbOffset = 1;\n", []string{"abbreviatedWordSegment"}},

		// A bare `queryFn` variable fires. It lands on
		// the `Fn` suffix branch rather than the `fn` prefix one, which is the order the original
		// runs them in and the message a reader of `queryFn` needs: the repair is a role name.
		{"bare queryFn", "const queryFn = () => 1;\n", []string{"abbreviatedSuffix"}},
		{"bare mutationFn", "const mutationFn = () => 1;\n", []string{"abbreviatedSuffix"}},

		// A destructure binds a name this file owns.
		{"destructured binding", "const { props } = source;\n", []string{"abbreviatedIdentifier"}},

		// One finding per name, at its declaration: its reads, writes and type references are uses
		// of the name chosen there (#2wgkyfn).
		{
			"a declaration and its reads",
			"const idx = 0;\nuse(idx);\nuse(idx + 1);\n",
			[]string{"abbreviatedIdentifier"},
		},
		{"a declaration and a write", "let idx = 0;\nidx = 1;\n", []string{"abbreviatedIdentifier"}},
		{
			"a parameter and its read",
			"function run(ctx: number) {\n    return ctx;\n}\n",
			[]string{"abbreviatedIdentifier"},
		},
		{"a type and its reference", "type ctxShape = {};\nlet value: ctxShape;\n", []string{"abbreviatedIdentifier"}},
		{"a function and its call", "function fnCache() {}\nfnCache();\n", []string{"abbreviatedIdentifier"}},
		{"a label and its break", "idx: for (;;) {\n    break idx;\n}\n", []string{"abbreviatedIdentifier"}},
		{"a class, an enum and a namespace", "class ctxStore {}\nenum dbKind {}\nnamespace txSpace {}\n", []string{
			"abbreviatedIdentifier", "abbreviatedIdentifier", "abbreviatedIdentifier",
		}},
		{
			"a type parameter",
			"function first<ctxType>(value: ctxType) {\n    return value;\n}\n",
			[]string{"abbreviatedIdentifier"},
		},

		// `Msg` continues in lowercase, so it is not the millisecond unit. It is still the `msg`
		// prefix, which is a different branch with a different message.
		{"msgText is the msg prefix, not the Ms unit", "const msgText = 1;\n", []string{"abbreviatedIdentifier"}},

		// Every `args` is judged where it is declared, a rest as well: strict code cannot bind
		// `arguments`, so the advice names a descriptive name ending in it rather than the rule
		// exempting the conventional spelling (#2wgkyfn).
		{
			"a rest parameter",
			"function run(...args: string[]) {\n    use(args);\n}\n",
			[]string{"abbreviatedIdentifier"},
		},
		{"a rest binding element", "const { first, ...args } = source;\n", []string{"abbreviatedIdentifier"}},
		{
			"a destructured rest",
			"const [first, ...args] = source;\nuse(first, args);\n",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"a spread of a rest parameter",
			"function forward(...args: unknown[]) {\n    return target(...args);\n}\n",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"a plain args parameter",
			"function run(args: string[]) {\n    use(args);\n}\n",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"a catch binding named args",
			"try {\n    run();\n} catch (args) {\n    use(args);\n}\n",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"a rest beside a plain args",
			"function run(...args: string[]) {\n    use(args);\n}\nfunction other(args: string[]) {\n    return args;\n}\n",
			[]string{"abbreviatedIdentifier", "abbreviatedIdentifier"},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAbbreviatedIdentifier, abbreviatedFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

func TestConsistencyNoAbbreviatedIdentifierStaysSilent(test *testing.T) {
	test.Parallel()

	// The exemptions are the rule. Without them it demands renames the author cannot perform, which
	// is how a naming rule gets switched off.
	cases := []struct {
		name       string
		sourceText string
	}{
		// Already spelled out. Each of these shares a prefix or suffix with an abbreviation, which
		// is exactly what the already-spelled guards exist for.
		{"spelled out names", "const properties = 1;\nconst parameters = 2;\nconst configuration = 3;\n"},
		{"property and parameter", "const property = 1;\nconst parameter = 2;\n"},
		{"context, database, transaction", "const context = 1;\nconst database = 2;\nconst transaction = 3;\n"},
		{"current and previous", "const current = 1;\nconst previous = 2;\n"},
		{"options and percent", "const options = 1;\nconst percent = 2;\n"},
		{"index and character", "const index = 0;\nconst character = 'a';\n"},
		{
			"value, array, object, number",
			"const value = 1;\nconst array = [];\nconst object = {};\nconst number = 1;\n",
		},
		{
			"response, error, request, message",
			"const response = 1;\nconst error = 2;\nconst request = 3;\nconst message = 4;\n",
		},
		{"minimum and maximum", "const minimum = 1;\nconst maximum = 2;\n"},
		{"reference and string", "const reference = 1;\nconst string = 'a';\n"},
		{"spelled suffixes", "const rowProperty = 1;\nconst inputValue = 2;\nconst pageNumber = 3;\n"},
		{"spelled Argument suffix", "const firstArgument = 1;\nconst commandArguments = [];\n"},
		{"spelled Character suffix", "const lastCharacter = 'a';\n"},
		{"spelled Function suffix", "const compareFunction = () => 1;\n"},
		{"spelled String suffix", "const queryString = 'a';\n"},
		{"spelled milliseconds", "const durationInMilliseconds = 500;\n"},
		{"spelled word segments", "const workingDirectory = 1;\nconst environment = 2;\nconst length = 3;\n"},

		// Words that merely contain an abbreviation's letters.
		{"Direction is not Dir", "const scrollDirection = 1;\n"},
		{"Environment is not Env", "const buildEnvironment = 1;\n"},
		{"SMS is not Ms", "const sendSMS = 1;\n"},
		{"LLMs is not Ms", "const availableLLMs = 1;\n"},
		{"InnoDb is a proper noun", "const InnoDbMaximumIndexKeyBytes = 1;\n"},
		{"URLSearchParams is a Web API", "const parsed = new URLSearchParams();\n"},

		// Names somebody else chose. Renaming any of these changes what the code reaches for.
		{"a property read", "const value = thing.props;\n"},
		{"a chained property read", "const value = thing.config.idx;\n"},
		{"an import specifier", "import { config } from 'external';\n"},
		{"a namespace import", "import * as config from 'external';\n"},
		{"a default import", "import params from 'external';\n"},
		// A type property signature is not here, and it used to be. The original reports one: its
		// `reportWithTypeKeyGuard` withholds the autofix and calls `context.report` either way, on
		// the reasoning that a blind rename touches the declaration while every caller keeps the old
		// spelling through the object-literal-key skip. This port ships no fixer, so the guard has
		// nothing to withhold and collapses to a plain report.
		//
		// Measured rather than argued: `modules/pensieve/PensieveBootstrap.ts` lines 40, 41, 562,
		// 662 and 663 are `maxAgentsBytes` and `maxBootBytes` written as interface members, and a
		// full eslint run over the ahra tree reports all five as `Identifier "max" should not be
		// abbreviated`. This port reported none, which was that rule's entire parity gap.
		//
		// See `TestConsistencyNoAbbreviatedIdentifierJudgesTypeMemberKeys` for the positive case.
		{"a qualified type name member", "type Alias = Namespace.Config;\n"},
		{"a type reference", "let value: SomeConfig = load();\n"},

		// JSX. A tag is named by HTML or by the component's author, an attribute by whatever
		// component declares it. Both are plain identifiers in typescript-go and a distinct node
		// type in the TypeScript parser, so the original never needed these and this port does.
		// Their absence cost the sibling rule 3,081 false findings on the real tree.
		{"a jsx element named for a component", "export const Thing = () => <Props />;\n"},
		{"a paired jsx element", "export const Thing = () => <Config>hi</Config>;\n"},
		{"a jsx attribute name", "export const Thing = () => <div config=\"1\" idx=\"2\" />;\n"},
		{"a hyphenated jsx attribute", "export const Thing = () => <div data-config=\"1\" />;\n"},

		// A use of a name this file never declares has no declaration to be judged at.
		{"an args the file never binds", "use(args);\n"},
		{"a global read", "const value = ctx.state;\n"},

		// A destructured ref forwarded to React retains its binding exemption.
		{"a destructured ref forwarded onward", "export const Thing = ({ ref }) => <Child ref={ref} />;\n"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAbbreviatedIdentifier, abbreviatedFile, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// The framework exemptions are the half that needs options, and a rule that reads options has a
// failure mode an unconfigured one does not: it can guard everything or nothing. Both directions
// belong in fixtures.
func TestConsistencyNoAbbreviatedIdentifierFrameworkExemptions(test *testing.T) {
	test.Parallel()

	options := ConsistencyNoAbbreviatedIdentifierOptions{
		FrameworkParameterFilePatterns:  []string{"/app/"},
		FrameworkConstantFilePatterns:   []string{"/middleware.ts"},
		FrameworkParameterScopeNames:    []string{"generateMetadata", "generateStaticParams"},
		FrameworkParameterScopeSuffixes: []string{"PageRoute"},
	}

	cases := []struct {
		name       string
		fileName   string
		sourceText string
		wantIds    []string
	}{
		{
			"params in a page file is mandated by the framework",
			"/repository/app/blog/page.tsx",
			"export default function Page({ params }) {\n    return params;\n}\n",
			nil,
		},
		{
			"searchParams in a page file is mandated too",
			"/repository/app/blog/page.tsx",
			"export default function Page({ searchParams }) {\n    return searchParams;\n}\n",
			nil,
		},
		{
			"the same name outside a framework file is judged on its merits",
			"/repository/source/Thing.tsx",
			"export function read({ params }) {\n    return params;\n}\n",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"config in a middleware file is mandated",
			"/repository/middleware.ts",
			"export const config = { matcher: '/' };\n",
			nil,
		},
		{
			"config elsewhere still fires",
			"/repository/source/Thing.tsx",
			"export const config = { matcher: '/' };\n",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"a framework scope name exempts wherever the file lives",
			"/repository/source/Thing.tsx",
			"export function generateMetadata({ params }) {\n    return params;\n}\n",
			nil,
		},
		{
			"a framework scope suffix exempts a component named for the route",
			"/repository/source/Thing.tsx",
			"export function BlogPageRoute({ params }) {\n    return params;\n}\n",
			nil,
		},
		{
			// The role suffix has to be stripped before the scope suffix can match, so this is the
			// case that separates a `PageRoute` suffix from `PageRouteProperties`. It is deliberately
			// a function parameter rather than an interface key: a key is already exempt as a
			// property signature, which is how an earlier version of this fixture passed without
			// ever reaching the stripping it claimed to test.
			"a scope suffix matches after the role suffix is stripped",
			"/repository/source/Thing.tsx",
			"export function BlogPageRouteProperties({ params }) {\n    return params;\n}\n",
			nil,
		},
		{
			"the stripping covers every role suffix our conventions append",
			"/repository/source/Thing.tsx",
			"export const BlogPageRouteOptions = ({ params }) => params;\n",
			nil,
		},
		{
			// An interface key is judged, and the framework options do not change that. A type
			// member often shapes an external surface, which is why the original withholds its
			// autofix there, but it still reports: the name is one this file declares and can
			// choose. The framework exemptions above are about a name the framework mandates,
			// which is a different question and the reason these two are stated separately.
			"an interface key is judged as a property signature",
			"/repository/source/Thing.tsx",
			"interface ThingProperties {\n    params: string;\n}\n",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"a non-framework function in the same file is still judged",
			"/repository/source/Thing.tsx",
			"export function readThing({ params }) {\n    return params;\n}\n",
			[]string{"abbreviatedIdentifier"},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(test, ConsistencyNoAbbreviatedIdentifier,
				testCase.fileName, testCase.sourceText, options)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// Without options the framework exemptions cannot apply, and the rule must still run rather than
// guard everything. `boundary-no-project-import` was enabled and inert for months because a missing
// option made it decline every file, which looks exactly like a clean run.
func TestConsistencyNoAbbreviatedIdentifierRunsWithoutOptions(test *testing.T) {
	test.Parallel()

	result := rule_testing.Run(test, ConsistencyNoAbbreviatedIdentifier,
		"/repository/app/blog/page.tsx", "export default function Page({ params }) {\n    return params;\n}\n")
	rule_testing.ExpectFindings(test, result, "abbreviatedIdentifier")
}

// The suggested name travels in the message, since there is no fix to carry it. A message naming
// the wrong replacement is worse than no message, so the text is asserted rather than just the id.
func TestConsistencyNoAbbreviatedIdentifierNamesTheReplacement(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantText   string
	}{
		{"exact expansion", "const idx = 0;\n", `Use "index"`},
		{"prefix expansion keeps the tail", "const ctxValue = 1;\n", `Use "contextValue"`},
		{"suffix expansion keeps the head", "const rowProps = 1;\n", `Use "rowProperties"`},
		{"milliseconds at the end", "const maximumAgeMs = 1;\n", `Use "maximumAgeInMilliseconds"`},
		{"milliseconds in the middle", "const maximumAgeMsHalf = 1;\n", `Use "maximumAgeInMillisecondsHalf"`},
		{"a word segment in the middle", "const buildEnvName = 1;\n", `Use "buildEnvironmentName"`},
		{"a word segment at the end", "const originalInitCwd = 1;\n", `Use "originalInitWorkingDirectory"`},
		{"the longer word wins", "const themeVarsVar = 1;\n", `abbreviates "Vars". Use "Variables" for variables`},
		// One occurrence at a time, which is what JavaScript's non-global String.replace does and
		// what the original means. Expanding every occurrence at once would hand a reader a name
		// they did not ask for in a position the rule never reported.
		{"a repeated segment expands once", "const rootCfgCacheCfg = 1;\n", `Use "rootConfigurationCacheCfg"`},
		{"a repeated millisecond unit expands once", "const ageMsHalfMs = 1;\n", `Use "ageInMillisecondsHalfMs"`},
		// Strict code cannot bind `arguments` or `package`, so a binding named for their abbreviation is
		// told to end in the word rather than to be it. A rest is told the same.
		{
			"an args binding names a legal ending",
			"const args = 1;\n",
			`Use a descriptive name ending in "Arguments", since strict code cannot bind "arguments".`,
		},
		{
			"a rest args names a legal ending",
			"export function join(...args: string[]) {\n    return args.join(',');\n}\n",
			`Use a descriptive name ending in "Arguments", since strict code cannot bind "arguments".`,
		},
		{
			"a pkg binding names a legal ending",
			"const pkg = 1;\n",
			`Use a descriptive name ending in "Package", since strict code cannot bind "package".`,
		},
		// A key is no binding, and `{ arguments: [] }` is legal, so a key keeps the vocabulary's advice.
		{
			"an args key keeps the vocabulary's advice",
			"export const run = { args: [] };\n",
			`Use "arguments", "commandArguments", "commandLineArguments", or a more descriptive name.`,
		},
		// Only the whole word stands where the bare expansion would: `argsList` becomes a legal name.
		{"an args prefix keeps the vocabulary's advice", "const argsList = 1;\n", `"commandLineArguments"`},
		{"Fn asks for a role rather than an expansion", "const compareFn = () => 1;\n", `like "Handler"`},
		// A word with two common meanings names both and lets the author say which: a census found `resultPos`
		// beside `resultNeg`, where it means positive, told to become `resultPosition`.
		{
			"a word with two meanings names both",
			"const resultPos = 1;\nconst resultNeg = -1;\n",
			`Use "resultPosition" or "resultPositive", whichever it means.`,
		},
		{
			"a whole word with two meanings names both",
			"const pos = 1;\n",
			`Use "position" or "positive", whichever it means, or a more descriptive name.`,
		},
		// An audit of the Go fix's renames found `res`, `ch` and `dir` guessed: `res` read as a response where it
		// meant a result or MakeMap's reserve, `ch` as a channel where it was a rune, `dir` as a directory where it
		// was a ChanDir. Each names every meaning.
		{
			"a whole res names all four meanings",
			"const res = 1;\n",
			`Use "result", "response", "resource" or "reserve", whichever it means, or a more descriptive name.`,
		},
		{
			"a res prefix names all four meanings",
			"const resBody = 1;\n",
			`Use "resultBody", "responseBody", "resourceBody" or "reserveBody", whichever it means.`,
		},
		{
			"a res suffix names all four meanings",
			"const fetchRes = 1;\n",
			`Use "fetchResult", "fetchResponse", "fetchResource" or "fetchReserve", whichever it means.`,
		},
		{
			"a whole ch names both meanings",
			"const ch = 1;\n",
			`Use "channel" or "character", whichever it means, or a more descriptive name.`,
		},
		{
			"a ch segment names both meanings",
			"const doneCh = 1;\n",
			`Use "doneChannel" or "doneCharacter", whichever it means.`,
		},
		{
			"a dir segment names both meanings",
			"const outputDirPath = 1;\n",
			`Use "outputDirectoryPath" or "outputDirectionPath", whichever it means.`,
		},
		// `Var` can mean JavaScript's var keyword, as in `NoVar`, which no word spells out, so it offers no name.
		{
			"a var segment asks which it means",
			"const themeVarName = 1;\n",
			`Identifier "themeVarName" abbreviates "Var". ` +
				`Use "Variable" for a variable, or a name saying it means JavaScript's var keyword.`,
		},
		{
			"a vars segment asks which it means",
			"const themeVars = 1;\n",
			`Identifier "themeVars" abbreviates "Vars". ` +
				`Use "Variables" for variables, or a name saying it means JavaScript's var keyword.`,
		},
		// `sb` fits more words than a list could hold, a scroll region's bottom among them, so it suggests none.
		{
			"sb asks what it holds",
			"const sb = 1;\n",
			`Identifier "sb" should not be abbreviated. Spell out what it holds.`,
		},
		{
			"an sb segment asks what it holds",
			"const scrollSbOffset = 1;\n",
			`Identifier "scrollSbOffset" abbreviates "Sb". Spell out what it holds.`,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAbbreviatedIdentifier, abbreviatedFile, testCase.sourceText)
			if len(result.Diagnostics) == 0 {
				test.Fatalf("expected a finding for %q", testCase.sourceText)
			}
			description := result.Diagnostics[0].Message.Description
			if !strings.Contains(description, testCase.wantText) {
				test.Fatalf("expected the message to contain %q, got %q", testCase.wantText, description)
			}
		})
	}
}

// The candidate gate is the speed story: it decides whether a name is worth spending the rest of the
// rule on. It must stay a superset of the branches, because a false negative here silently stops the
// rule firing and looks exactly like a clean run.
func TestConsistencyNoAbbreviatedIdentifierCandidateGateCoversEveryBranch(test *testing.T) {
	test.Parallel()

	// Every name any branch below the gate can report, one per branch family.
	reportable := []string{
		"prop", "props", "param", "params", "ref", "config", "idx", "arg", "args", "acc",
		"char", "fn", "str", "val", "arr", "obj", "num", "res", "err", "req", "msg", "min", "max",
		"prev", "cur", "pct", "opts", "ctx", "db", "tx",
		"ctxValue", "dbCode", "txHash", "optsForRun", "curStep", "pctComplete", "prevStep",
		"idxStart", "configValue", "propName", "propsList", "paramName", "paramsText", "refCount",
		"argCount", "argsList", "charSet", "fnCache", "strValue", "valInput", "arrItems",
		"objTarget", "numPages", "resBody", "errCount", "reqBody", "msgText", "minAge", "maxAge",
		"rowProp", "rowProps", "queryParam", "searchParams", "nodeRef", "buildConfig", "rowIdx",
		"firstArg", "esBuildArgs", "lastChar", "compareFn", "queryStr", "inputVal", "itemArr",
		"targetObj", "pageNum", "fetchRes", "parseErr", "httpReq", "errorMsg", "pageMin", "pageMax",
		"maximumAgeMs", "maximumAgeMsHalf",
		"originalInitCwd", "outputDirPath", "buildEnvName", "runCliCommand", "bufferLen",
		"eventSeq", "primaryDbHost", "pendingTxHash", "themeVars", "themeVarName",
	}
	for _, name := range reportable {
		if !isAbbreviationCandidate(name) {
			test.Errorf("the gate rejects %q, which a branch below would have reported", name)
		}
	}

	// The gate has to reject the ordinary name, or it buys nothing.
	for _, name := range []string{
		"renderNodeOrComponent", "identifier", "sourceFile", "handleSubmit", "useMediaQuery",
	} {
		if isAbbreviationCandidate(name) {
			test.Errorf("the gate accepts %q, which no branch would report", name)
		}
	}
}

// A finding has to land on the line the author can suppress.
//
// A node's Pos() sits before its leading trivia, so a binding preceded by a comment anchors at the
// comment rather than at the name. That is the one line an `eslint-disable-next-line` above it
// cannot cover, since the directive matches the line after itself. Measured on the real tree:
// `params` in McpApi.ts reported at line 240 while its suppression sat on 241 covering 242, so a
// correctly-suppressed identifier still produced a finding nothing could silence.
//
// The rule anchors on rule.TokenRange explicitly rather than leaning on ReportNode to do it. Both
// trim today, so this asserts the position rather than the call: the guarantee that matters is where
// the finding lands, and a fixture that tested which helper was called would go on passing if the
// helper changed underneath it.
//
// The column is asserted alongside the line because trivia swallowed within a single line moves only
// the column, and a line-only assertion cannot see it.
func TestConsistencyNoAbbreviatedIdentifierReportsAtTheIdentifier(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantLine   int
		wantColumn int
	}{
		{
			// The real-tree shape. Leading trivia spans two lines here, so an untrimmed range lands
			// on `method` two lines above the name.
			"a destructured binding under a comment",
			"function handleRequest(message: M): void {\n" +
				"    const {\n" +
				"        id,\n" +
				"        method,\n" +
				"        // eslint-disable-next-line nexus/consistency-no-abbreviated-identifier\n" +
				"        params,\n" +
				"    } = message;\n" +
				"}\n",
			6, 9,
		},
		{
			// A parameter's trivia reaches back to the open parenthesis on the signature line.
			"a parameter under a comment",
			"function handleRequest(\n    // eslint-disable-next-line\n    params: string,\n) {}\n",
			3, 5,
		},
		{
			// A class member, whose trivia reaches back past the comment to the previous member.
			"a class property under a comment",
			"class Thing {\n    id = 1;\n    // eslint-disable-next-line\n    paramsText = 2;\n}\n",
			4, 5,
		},
		{
			// Within one line the whitespace after `const` is the trivia, so only the column moves.
			"a declaration with no comment above it",
			"const params = 1;\n",
			1, 7,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(
				test,
				ConsistencyNoAbbreviatedIdentifier,
				"/repository/source/Thing.ts",
				testCase.sourceText,
			)
			if len(result.Diagnostics) == 0 {
				test.Fatalf("expected a finding, got none")
			}

			line, column := scanner.GetECMALineAndByteOffsetOfPosition(
				result.SourceFile,
				result.Diagnostics[0].Range.Pos(),
			)
			if line+1 != testCase.wantLine || column+1 != testCase.wantColumn {
				test.Fatalf("expected the finding at %d:%d, got %d:%d",
					testCase.wantLine, testCase.wantColumn, line+1, column+1)
			}
		})
	}
}

// TestConsistencyNoAbbreviatedIdentifierJudgesTypeMemberKeys pins the shape that was silently exempt.
//
// `IsForeignName` answers true for a property signature, and it is right to for
// `consistency-no-ambiguous-identifier`, which shares it and wants the whole family skipped. This
// rule subtracts type member keys back out, because the original only withholds the autofix there
// rather than the finding, and this port has no autofix to withhold.
//
// The `maxAgentsBytes` case is the real one, reduced from `modules/pensieve/PensieveBootstrap.ts`
// where a full eslint run reports five findings this port reported zero of. A method signature is
// included because the original walks `Identifier` without distinguishing the two member kinds, and
// a nested type literal because the skip was keyed on the parent node rather than on depth.
func TestConsistencyNoAbbreviatedIdentifierJudgesTypeMemberKeys(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		{
			"an interface member carrying a prefix abbreviation",
			"interface BootstrapOptionsInterface {\n    maxAgentsBytes?: number;\n}\n",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"two interface members, the shape measured on the real tree",
			"interface BootstrapOptionsInterface {\n    maxAgentsBytes?: number;\n    maxBootBytes?: number;\n}\n",
			[]string{"abbreviatedIdentifier", "abbreviatedIdentifier"},
		},
		{
			"a type literal member",
			"type BootstrapOptionsType = {\n    maxBootBytes?: number;\n};\n",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"a method signature key",
			"interface ReaderInterface {\n    maxDepth(): number;\n}\n",
			[]string{"abbreviatedIdentifier"},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAbbreviatedIdentifier, abbreviatedFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// TestConsistencyNoAbbreviatedIdentifierStillSkipsTheRestOfTheForeignFamily is the control.
//
// The narrowing above subtracts exactly one member from `IsForeignName`'s set. Without this test a
// mutation widening it to "never skip a foreign name" would pass every assertion in the file, since
// nothing else asserts the remaining exemptions survive.
func TestConsistencyNoAbbreviatedIdentifierStillSkipsTheRestOfTheForeignFamily(test *testing.T) {
	test.Parallel()

	cases := []struct{ name, sourceText string }{
		{"a property read", "const value = thing.maxAgentsBytes;\n"},
		{"an import specifier", "import { maxAgentsBytes } from 'external';\n"},
		{"a namespace import", "import * as maxAgentsBytes from 'external';\n"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAbbreviatedIdentifier, abbreviatedFile, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// TestConsistencyNoAbbreviatedIdentifierSkipsNamesTheCodeOnlyReads holds the principle from
// api-phi-health's parity sweep (#dx1vrfm): the rule judges names the code declares, never names it
// reads from elsewhere or renames away from. Each silent row is one of the six reported sites in
// shape; each reporting row is the nearest shape the rule must still catch.
func TestConsistencyNoAbbreviatedIdentifierSkipsNamesTheCodeOnlyReads(test *testing.T) {
	test.Parallel()

	silent := []struct{ name, sourceText string }{
		// ModuleTestDiscovery.ts: tsconfig-paths' loadConfig, read in a type query and a call.
		{
			"a reference to an imported name",
			"import { loadConfig } from 'tsconfig-paths';\nlet configuration: ReturnType<typeof loadConfig> | undefined;\nconfiguration = loadConfig('.');\nexport { configuration };\n",
		},
		// OrmSchemaBuilderDrizzleMySql.ts: drizzle's char, called.
		{
			"a call of an imported function",
			"import { char } from 'drizzle-orm/mysql-core';\nexport const column = char('name', { length: 36 });\n",
		},
		{
			"a reference to a default import",
			"import loadConfig from 'config-loader';\nexport const configuration = loadConfig();\n",
		},
		// MappedJoins.test.ts: drizzle's params key, renamed on destructure.
		{
			"a destructuring key renamed away",
			"declare const query: { toSQL(): Record<string, unknown> };\nconst { sql: statement, params: parameters } = query.toSQL();\nexport { statement, parameters };\n",
		},
		// FileStorageVideoDerivativeService.ts: an external maxBitrate key, renamed on destructure.
		{
			"an external key renamed away",
			"declare const entry: Record<string, unknown>;\nconst { maxBitrate: maximumBitrate } = entry;\nexport { maximumBitrate };\n",
		},
	}
	for _, testCase := range silent {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(
				test,
				rule_testing.Run(test, ConsistencyNoAbbreviatedIdentifier, abbreviatedFile, testCase.sourceText),
			)
		})
	}

	fires := []struct{ name, sourceText, id string }{
		// A shorthand destructure declares the abbreviated name in this file.
		{
			"a shorthand destructure",
			"declare const query: { params: unknown[] };\nconst { params } = query;\nexport { params };\n",
			"abbreviatedIdentifier",
		},
		// The local half of a rename is this file's name.
		{
			"a rename onto an abbreviation",
			"declare const entry: { maximumBitrate: number };\nconst { maximumBitrate: maxBitrate } = entry;\nexport { maxBitrate };\n",
			"abbreviatedIdentifier",
		},
		// A file that shadows an import declares that name too, so it is judged rather than guessed.
		{
			"a local that shadows an import",
			"import { char } from 'drizzle-orm/mysql-core';\nexport function build() {\n    const char = 1;\n    return char;\n}\nexport { char as column };\n",
			"abbreviatedIdentifier",
		},
	}
	for _, testCase := range fires {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAbbreviatedIdentifier, abbreviatedFile, testCase.sourceText)
			if len(result.Diagnostics) == 0 {
				test.Fatalf("expected a finding, got none")
			}
			if result.Diagnostics[0].Message.Id != testCase.id {
				test.Fatalf("expected %s first, got %v", testCase.id, result.MessageIds())
			}
		})
	}
}

// TestConsistencyNoAbbreviatedIdentifierLeavesNextRouteContractsAlone covers the floor
// `nextjs.IsRouteContractExport` sets with no options at all.
//
// `maxDuration` is the case no consumer list carried: it passes the `max[A-Z]` gate, and the rule
// told route authors to write `maximumDuration`, which Next then silently ignores. Each silent case is
// paired with the same text in a file Next does not read, which must still report, so the exemption
// is shown to come from the route file rather than from the name.
func TestConsistencyNoAbbreviatedIdentifierLeavesNextRouteContractsAlone(test *testing.T) {
	test.Parallel()

	const routeFile = "/repository/app/api/report/route.ts"
	const pageFile = "/repository/app/blog/[slug]/page.tsx"
	cases := []struct {
		name       string
		fileName   string
		sourceText string
		wantIds    []string
	}{
		{"maxDuration in a route", routeFile, "export const maxDuration = 60;", nil},
		{
			"maxDuration outside a route",
			abbreviatedFile,
			"export const maxDuration = 60;",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"generateStaticParams in a page",
			pageFile,
			"export async function generateStaticParams() { return []; }",
			nil,
		},
		{
			"generateStaticParams outside a route",
			abbreviatedFile,
			"export async function generateStaticParams() { return []; }",
			[]string{"abbreviatedSuffix"},
		},
		{"dynamicParams in a page", pageFile, "export const dynamicParams = false;", nil},
		{"config in a page", pageFile, "export const config = {};", nil},
		{
			"a reference to the export is the export",
			pageFile,
			"export const maxDuration = 60;\nconsole.log(maxDuration);",
			nil,
		},
		// A nested binding sharing the spelling is a different variable, judged on its merits, at its
		// declaration, as every name is.
		{
			"a nested config shadows the contract",
			pageFile,
			"export function load() {\n    const config = 1;\n    return config;\n}",
			[]string{"abbreviatedIdentifier"},
		},
		{
			"a parameter named for the contract",
			pageFile,
			"export function load(maxDuration: number) {\n    return 1;\n}",
			[]string{"abbreviatedIdentifier"},
		},
		// Contracts differ by file: a page has no http methods and a route has no metadata, and
		// `params` is no contract export anywhere, so it keeps its own options-driven handling.
		{"a page is not a route handler", routeFile, "export const generateViewport = () => ({});", nil},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAbbreviatedIdentifier, testCase.fileName, testCase.sourceText)
			if len(testCase.wantIds) == 0 {
				rule_testing.ExpectClean(test, result)
				return
			}
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// Typed ownership fixtures use the same spelling on both sides of each boundary.
// #nweav21 controls (go test ./internal/lint/rules/nexus -run
// ^TestConsistencyNoAbbreviatedIdentifierOwnedProperties$): old code fails 30/63; skipping all
// owned properties fails 34/63; ignoring ownership fails 29/63. Appending a maxAgentsBytes
// binding to each subject fails all 63/63, including every silent fixture.
// Exact spans ensure a contextual literal is silent while its declaration reports once.
// The setup path preserves our tsconfig: the cached harness forces every source file into a
// module, which would prevent the global interface-merging fixture from actually merging.
func TestConsistencyNoAbbreviatedIdentifierOwnedProperties(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name, source string
		want         int
	}{
		{
			"own ref property",
			`interface Session { ref: unknown; params: unknown; config: unknown; args: unknown; maxDuration: number } function forward(...args: unknown[]) {}`,
			6,
		},
		{"own queryFn property", `const session = { queryFn: () => 500 };`, 1},
		{"interface property", `interface Session { durationMs: number }`, 1},
		{"type property", `type Session = { durationMs: number }`, 1},
		{"interface method", `interface Session { durationMs(): number }`, 1},
		{"class property", `class Session { durationMs = 500 }`, 1},
		{"class method", `class Session { durationMs() { return 500 } }`, 1},
		{"class accessor", `class Session { get durationMs() { return 500 } }`, 1},
		{"own literal", `const session = { durationMs: 500 };`, 1},
		{"literal method", `const session = { durationMs() { return 500 } };`, 1},
		{"literal accessor", `const session = { get durationMs() { return 500 } };`, 1},
		{"string interface key", `interface Session { "durationMs": number }`, 1},
		{"string literal key", `const session = { "durationMs": 500 };`, 1},
		{"index signature", `const session: { [key: string]: number } = { durationMs: 500 };`, 1},
		{"Record", `const session: Record<string, number> = { durationMs: 500 };`, 1},
		{
			"generic schema",
			`declare const Schema: { object<T>(shape: T): T }; const session = Schema.object({ durationMs: 500 });`,
			1,
		},
		{
			"schema index",
			`declare const Schema: { object(shape: Record<string, unknown>): unknown }; const session = Schema.object({ durationMs: 500 });`,
			1,
		},
		{
			"own interface contextual key",
			`interface Session { durationMs: number } const session: Session = { durationMs: 500 };`,
			1,
		},
		{
			"own type contextual key",
			`type Session = { durationMs: number }; const session: Session = { durationMs: 500 };`,
			1,
		},
		{
			"own satisfies key",
			`interface Session { durationMs: number } const session = { durationMs: 500 } satisfies Session;`,
			1,
		},
		{
			"dependency contextual key",
			`import { Options } from 'wire'; const session: Options = { durationMs: 500 };`,
			0,
		},
		{"dependency argument", `import { accept } from 'wire'; accept({ durationMs: 500 });`, 0},
		{"dependency string key", `import { Options } from 'wire'; const session: Options = { "durationMs": 500 };`, 0},
		{
			"dependency method",
			`import { Worker } from 'wire'; const session: Worker = { durationMs() { return 500 } };`,
			0,
		},
		{"dependency override", `import { Base } from 'wire'; class Session extends Base { durationMs = 500 }`, 0},
		{
			"dependency implementation",
			`import { Worker } from 'wire'; class Session implements Worker { durationMs() { return 500 } }`,
			0,
		},
		{
			"transitive dependency override",
			`import { Base } from 'wire'; class Middle extends Base {} class Session extends Middle { durationMs = 500 }`,
			0,
		},
		{
			"redeclared dependency override",
			`import { Base } from 'wire'; class Middle extends Base { durationMs = 100 } class Session extends Middle { durationMs = 500 }`,
			0,
		},
		{
			"static does not implement instance contract",
			`import { Options } from 'wire'; class Session implements Options { durationMs = 100; static durationMs = 500 }`,
			1,
		},
		{
			"generic constraint",
			`function define<T extends { durationMs: number }>(shape: T): T { return shape } const session = define({ durationMs: 500 });`,
			1,
		},
		{
			"own inferred contextual key",
			`const defaults = { durationMs: 100 }; const session: typeof defaults = { durationMs: 500 };`,
			1,
		},

		{"own override", `class Base { durationMs = 1 } class Session extends Base { durationMs = 500 }`, 2},
		{
			"own implementation",
			`interface Worker { durationMs(): number } class Session implements Worker { durationMs() { return 500 } }`,
			2,
		},
		{"module augmentation", `import 'wire'; declare module 'wire' { interface Options { durationMs: number } }`, 0},
		{"global augmentation", `export {}; declare global { interface Window { durationMs: number } }`, 0},
		{"merged foreign interface", `interface Host { durationMs: number }`, 0},
		{"own namespace", `namespace Local { export interface Session { durationMs: number } }`, 1},
		{"property read", `import { external } from 'wire'; const elapsed = external.durationMs;`, 0},
		{"this property read", `class Session { read() { return this.durationMs; } }`, 0},
		{"destructured key", `import { external } from 'wire'; const { durationMs: elapsed } = external;`, 0},
		{"import name", `import { durationMs } from 'wire';`, 0},
		{"JSX attribute", `declare const Widget: any; const element = <Widget durationMs={500} />;`, 0},
		{
			"dependency source",
			`import { Options } from 'source-wire'; const session: Options = { durationMs: 500 };`,
			0,
		},
		{
			"own imported declaration key",
			`import { Options } from './options'; const session: Options = { durationMs: 500 };`,
			0,
		},
		{"local library declaration", `const session: Host = { durationMs: 500 };`, 0},
		{
			"platform property",
			`class Session implements Pick<Math, 'max'> { max(...values: number[]) { return 500 } }`,
			0,
		},
		{
			"union contextual key",
			`import { Options } from 'wire'; const session: Options | { value: number } = { durationMs: 500 };`,
			0,
		},
		{
			"intersection contextual key",
			`import { Options } from 'wire'; const session: Options & { value: number } = { durationMs: 500, value: 1 };`,
			0,
		},
		{
			"nested contextual key",
			`import { Options } from 'wire'; const session: { timer: Options } = { timer: { durationMs: 500 } };`,
			0,
		},
		{
			"contextual return",
			`import { Options } from 'wire'; function create(): Options { return { durationMs: 500 }; }`,
			0,
		},
		{
			"contextual assignment",
			`import { Options } from 'wire'; let session: Options; session = { durationMs: 500 };`,
			0,
		},
		{"finite Record", `const session: Record<'durationMs', number> = { durationMs: 500 };`, 1},
		{"class expression", `const Session = class { durationMs = 500 };`, 1},
		{
			"dependency accessor",
			`import { Options } from 'wire'; class Session implements Options { get durationMs() { return 500 } }`,
			0,
		},
		{
			"dependency static override",
			`import { StaticBase } from 'wire'; class Session extends StaticBase { static durationMs = 500 }`,
			0,
		},
		{"own string class key", `class Session { "durationMs" = 500 }`, 1},
		{"own imported spelling", `import { durationMs } from 'wire'; interface Session { durationMs: number }`, 1},

		{
			"computed dependency declaration",
			`import { ComputedOptions } from 'wire'; const session: ComputedOptions = { durationMs: 500 };`,
			0,
		},
		{"computed interface key", `interface Session { ['durationMs']: number }`, 1},
		{"computed class key", `class Session { ['durationMs'] = 500 }`, 1},
		{"computed literal key", `const session = { ['durationMs']: 500 };`, 1},
		{"private class key", `class Session { #durationMs = 500; #idx = 0 }`, 2},

		{
			"full names",
			`interface Session { durationInMilliseconds: number } class Timer { durationInMilliseconds = 500 } const session = { durationInMilliseconds: 500 };`,
			0,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			fileName := "subject.tsx"
			if testCase.name == "own ref property" {
				fileName = "app/page.tsx"
			}
			result := rule_testing.RunTypedFilesWithSetupAndOptions(
				test,
				ConsistencyNoAbbreviatedIdentifier,
				map[string]string{
					fileName:                            testCase.source,
					"tsconfig.json":                     `{ "compilerOptions": { "strict": true, "target": "ES2022", "lib": ["ES2022"], "types": [] }, "include": ["**/*.ts", "**/*.tsx"] }`,
					"node_modules/wire/index.d.ts":      `export interface ComputedOptions { ['durationMs']: number } export interface Options { durationMs: number } export interface Worker { durationMs(): number } export class StaticBase { static durationMs: number } export class Base { durationMs: number } export const external: Options; export const durationMs: number; export function accept(options: Options): void;`,
					"node_modules/source-wire/index.ts": `export interface Options { durationMs: number }`,
					"options.ts":                        `export interface Options { durationMs: number }`,
					"host.d.ts":                         `interface Host { durationMs: number }`,
				},
				fileName,
				ConsistencyNoAbbreviatedIdentifierOptions{
					FrameworkParameterFilePatterns: []string{"app/page.tsx"},
					FrameworkConstantFilePatterns:  []string{"app/page.tsx"},
				},
				func(string) {
					// The fixture's files are all in the map, so the directory needs nothing more.
				},
			)
			if len(result.Diagnostics) != testCase.want {
				test.Fatalf(
					"expected %d findings, got %d: %v",
					testCase.want,
					len(result.Diagnostics),
					result.MessageIds(),
				)
			}
			for _, diagnostic := range result.Diagnostics {
				text := result.SourceFile.Text()[diagnostic.Range.Pos():diagnostic.Range.End()]
				if text != "durationMs" &&
					text != `"durationMs"` &&
					text != "ref" &&
					text != "queryFn" &&
					text != "'durationMs'" &&
					text != "#durationMs" &&
					text != "#idx" &&
					text != "params" &&
					text != "config" &&
					text != "args" &&
					text != "maxDuration" {
					test.Errorf("finding at %q instead of the property declaration", text)
				}
			}
			if strings.HasPrefix(testCase.name, "own ") &&
				strings.Contains(testCase.name, "key") &&
				strings.Contains(testCase.source, "const session") &&
				len(result.Diagnostics) == 1 {
				if result.Diagnostics[0].Range.Pos() >= strings.Index(result.SourceFile.Text(), "const session") {
					test.Fatal("reported at literal instead of declaration")
				}
			}
		})
	}
}

// These dependency declarations retain React's SVGProps -> ClassAttributes -> RefAttributes
// inheritance and CSSProperties -> csstype.Properties inheritance, including the relevant fields.
func abbreviatedReactDependencyFiles(source string) map[string]string {
	return map[string]string{
		"subject.tsx": "import * as React from 'react';\ninterface SVGSVGElement {}\n" +
			"declare const condition: boolean;\ndeclare const previous: { height?: number } | undefined;\n" +
			source,
		"node_modules/react/index.d.ts": `
			import * as CSS from 'csstype';
			export interface RefObject<T> { current: T }
			export type RefCallback<T> = { bivarianceHack(instance: T | null): void }['bivarianceHack'];
			export type Ref<T> = RefCallback<T> | RefObject<T | null> | null;
			export interface Attributes { key?: string | number | bigint | null | undefined }
			export interface RefAttributes<T> extends Attributes { ref?: Ref<T> | undefined }
			export interface ClassAttributes<T> extends RefAttributes<T> {}
			export interface CSSProperties extends CSS.Properties<string | number> {}
			export interface SVGAttributes<T> {
				className?: string | undefined;
				height?: number | string | undefined;
				style?: CSSProperties | undefined;
				viewBox?: string | undefined;
				width?: number | string | undefined;
			}
			export interface SVGProps<T> extends SVGAttributes<T>, ClassAttributes<T> {}
		`,
		"node_modules/csstype/index.d.ts": `
			export interface Properties<TLength = (string & {}) | 0> {
				maxWidth?: TLength | 'max-content' | 'min-content' | 'none' | (string & {}) | undefined;
				minHeight?: TLength | 'max-content' | 'min-content' | (string & {}) | undefined;
			}
		`,
	}
}

// #nweav21 review controls: go test ./internal/lint/rules/nexus -run
// ^TestConsistencyNoAbbreviatedIdentifier(Combined|Spread)DependencyProperties$
// fails 10/56 cases on 356201b8 and when the composition guard is removed. Dropping the
// existing contextual type for spread/branch literals fails 20/56. A maxAgentsBytes plant
// appended to every subject fails all 56/56, including every reporting control.
func TestConsistencyNoAbbreviatedIdentifierCombinedDependencyProperties(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name, source string
		want         int
	}{
		{"intersection", `type Properties = React.SVGProps<SVGSVGElement> & { ref?: React.Ref<SVGSVGElement> };`, 0},
		{"union", `type Properties = React.SVGProps<SVGSVGElement> | { ref?: React.Ref<SVGSVGElement> };`, 0},
		{
			"reversed intersection",
			`type Properties = { ref?: React.Ref<SVGSVGElement> } & React.SVGProps<SVGSVGElement>;`,
			0,
		},
		{
			"parenthesized",
			`type Properties = ((React.SVGProps<SVGSVGElement>) & (({ ref?: React.Ref<SVGSVGElement> })));`,
			0,
		},
		{
			"nested union constituent",
			`type Alternatives = React.SVGProps<SVGSVGElement> | { title: string }; type Properties = Alternatives & { ref?: React.Ref<SVGSVGElement> };`,
			0,
		},
		{
			"nested intersection constituent",
			`type Alternatives = React.SVGProps<SVGSVGElement> & { title: string }; type Properties = Alternatives | { ref?: React.Ref<SVGSVGElement> };`,
			0,
		},
		{"quoted member", `type Properties = React.SVGProps<SVGSVGElement> & { 'ref'?: React.Ref<SVGSVGElement> };`, 0},
		{
			"computed member",
			`type Properties = React.SVGProps<SVGSVGElement> & { ['ref']?: React.Ref<SVGSVGElement> };`,
			0,
		},
		{"method member", `type Properties = React.SVGProps<SVGSVGElement> & { ref?(): void };`, 0},
		{
			"only our intersection constituent declares it",
			`type Properties = React.SVGProps<SVGSVGElement> & { durationMs?: number };`,
			1,
		},
		{
			"only our union constituent declares it",
			`type Properties = React.SVGProps<SVGSVGElement> | { durationMs?: number };`,
			1,
		},
		{
			"both constituents ours",
			`type Attributes = { ref?: React.Ref<SVGSVGElement> }; type Properties = Attributes & { ref?: React.Ref<SVGSVGElement> };`,
			2,
		},
		{
			"own union",
			`type Attributes = { ref?: React.Ref<SVGSVGElement> }; type Properties = Attributes | { ref?: React.Ref<SVGSVGElement> };`,
			2,
		},
		{
			"nested property is a separate shape",
			`type Properties = React.SVGProps<SVGSVGElement> & { nested: { ref?: React.Ref<SVGSVGElement> } };`,
			1,
		},
		{
			"referenced declaration stays ours",
			`type Attributes = { ref?: React.Ref<SVGSVGElement> }; type Properties = React.SVGProps<SVGSVGElement> & Attributes;`,
			1,
		},
		{
			"mapped existing dependency property",
			`type Properties = Pick<React.SVGProps<SVGSVGElement>, 'ref'> & { ref?: React.Ref<SVGSVGElement> };`,
			0,
		},
		{
			"mapped generic declares no named property",
			`type Properties = Record<'ref', unknown> & { ref?: React.Ref<SVGSVGElement> };`,
			1,
		},
		{
			"index signature declares no named property",
			`type Properties = Record<string, unknown> & { ref?: React.Ref<SVGSVGElement> };`,
			1,
		},
		{
			"page properties unchanged",
			`type PageProperties = { params: Promise<{ slug: string }>; searchParams: Promise<Record<string, string>> };`,
			2,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedFiles(
				test,
				ConsistencyNoAbbreviatedIdentifier,
				abbreviatedReactDependencyFiles(testCase.source),
				"subject.tsx",
			)
			if len(result.Diagnostics) != testCase.want {
				test.Fatalf(
					"expected %d findings, got %d: %v",
					testCase.want,
					len(result.Diagnostics),
					result.MessageIds(),
				)
			}
		})
	}
}

// The checked-in checker already propagates context through these value-preserving expressions.
// All twenty typed cases are quiet on 356201b8; the seventeen owned/boundary cases report there.
// These pin that behavior without adding an unproven fallback walk.
func TestConsistencyNoAbbreviatedIdentifierSpreadDependencyProperties(test *testing.T) {
	test.Parallel()
	expressions := []struct{ name, source string }{
		{"direct spread", `{ maxWidth: 'x' }`},
		{"and", `condition && { maxWidth: 'x' }`},
		{"or right", `previous || { maxWidth: 'x' }`},
		{"or left", `{ maxWidth: 'x' } || previous`},
		{"nullish right", `previous ?? { maxWidth: 'x' }`},
		{"nullish left", `{ maxWidth: 'x' } ?? previous`},
		{"conditional true", `condition ? { maxWidth: 'x' } : {}`},
		{"conditional false", `condition ? {} : { maxWidth: 'x' }`},
		{"parenthesized", `((condition && ({ maxWidth: 'x' })))`},
		{"nested spreads", `{ ...(condition && { maxWidth: 'x' }) }`},
	}
	for _, expression := range expressions {
		test.Run(expression.name, func(test *testing.T) {
			test.Parallel()
			cases := []struct {
				name, source string
				want         int
			}{
				{"dependency context", "const style: React.CSSProperties = { ...(" + expression.source + ") };", 0},
				{
					"JSX style context",
					"declare function Icon(properties: React.SVGProps<SVGSVGElement>): any; " +
						"const icon = <Icon style={{ ...(" +
						expression.source +
						") }} />;",
					0,
				},
				{"own shape", "const style = { ...(" + expression.source + ") };", 1},
			}
			for _, testCase := range cases {
				test.Run(testCase.name, func(test *testing.T) {
					test.Parallel()
					result := rule_testing.RunTypedFiles(
						test,
						ConsistencyNoAbbreviatedIdentifier,
						abbreviatedReactDependencyFiles(testCase.source),
						"subject.tsx",
					)
					if len(result.Diagnostics) != testCase.want {
						test.Fatalf(
							"expected %d findings, got %d: %v",
							testCase.want,
							len(result.Diagnostics),
							result.MessageIds(),
						)
					}
				})
			}
		})
	}
	boundaries := []struct{ name, source string }{
		{"condition is not a value", `const style: React.CSSProperties = { ...({ maxWidth: 'x' } ? {} : {}) };`},
		{"and left is not a value", `const style: React.CSSProperties = { ...({ maxWidth: 'x' } && {}) };`},
		{
			"nested property is a separate shape",
			`const style: React.CSSProperties = { ...({ nested: { maxWidth: 'x' } }) };`,
		},
		{
			"no matching contextual property",
			`const properties: React.SVGProps<SVGSVGElement> = { ...(condition && { maxWidth: 'x' }) };`,
		},
		{"index signature", `const configuration: Record<string, unknown> = { ...(condition && { maxWidth: 'x' }) };`},
		{"untyped Jest configuration unchanged", `export default { maxWorkers: 4 };`},
		{"untyped ESLint configuration unchanged", `export default { maxWarnings: 1 };`},
	}
	for _, testCase := range boundaries {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedFiles(
				test,
				ConsistencyNoAbbreviatedIdentifier,
				abbreviatedReactDependencyFiles(testCase.source),
				"subject.tsx",
			)
			rule_testing.ExpectFindings(test, result, "abbreviatedIdentifier")
		})
	}
}

// An unavailable type contract cannot establish that a member spelling is ours.
func TestConsistencyNoAbbreviatedIdentifierUnresolvedTypeOwnership(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name, source, dependency string
		want                     int
	}{
		{name: "unresolved reference", source: "declare const page: PageProps;"},
		{name: "unresolved contextual contract", source: "const page: PageProps = { durationMs: 500 };"},
		{name: "unresolved inherited contract", source: "class Session implements PageProps { durationMs = 500 }"},
		{name: "unresolved combined contract", source: "type Session = PageProps & { durationMs: number };"},
		{
			name:   "own declaration",
			source: "interface PageProps { durationMs: number } declare const page: PageProps;",
			want:   2,
		},
		{
			name:       "dependency declaration",
			source:     "declare const page: PageProps;",
			dependency: "interface PageProps { durationMs: number }",
		},
		{
			name:   "own contextual declaration",
			source: "interface PageProps { durationMs: number } const page: PageProps = { durationMs: 500 };",
			want:   2,
		},
		{
			name:       "dependency contextual declaration",
			source:     "const page: PageProps = { durationMs: 500 };",
			dependency: "interface PageProps { durationMs: number }",
		},
	}
	for _, tc := range cases {
		test.Run(tc.name, func(test *testing.T) {
			test.Parallel()
			files := map[string]string{"subject.ts": tc.source}
			if tc.dependency != "" {
				files["contract.d.ts"] = "export {}; declare global { " + tc.dependency + " }"
			}
			result := rule_testing.RunTypedFiles(test, ConsistencyNoAbbreviatedIdentifier, files, "subject.ts")
			if len(result.Diagnostics) != tc.want {
				test.Fatalf("got %d findings, want %d: %v", len(result.Diagnostics), tc.want, result.MessageIds())
			}
			for _, diagnostic := range result.Diagnostics {
				if len(diagnostic.Fixes) != 0 {
					test.Fatal("abbreviation rule must not fix names")
				}
			}
		})
	}
}

// abbreviatedIntrinsicDependencyFiles lays out the slice of React's declarations an intrinsic element
// reads its attributes through, `JSX.IntrinsicElements` to `DetailedHTMLProps` to
// `ImgHTMLAttributes`, beside a DOM declaration file, since the fixture's lib is ES2022 alone, and a
// dependency interface a class can implement.
func abbreviatedIntrinsicDependencyFiles(source string) map[string]string {
	return map[string]string{
		"subject.tsx": "import * as React from 'react';\nimport { ImageLike } from 'image-like';\n" +
			"declare const url: string;\ndeclare const condition: boolean;\n" +
			source,
		"node_modules/react/index.d.ts": `
			export interface DOMAttributes<T> { children?: unknown }
			export interface HTMLAttributes<T> extends DOMAttributes<T> { className?: string; id?: string }
			export interface ImgHTMLAttributes<T> extends HTMLAttributes<T> { alt?: string; src?: string }
			export interface SourceHTMLAttributes<T> extends HTMLAttributes<T> { src?: string; type?: string }
			export interface ClassAttributes<T> { key?: string }
			export type DetailedHTMLProps<E extends HTMLAttributes<T>, T> = ClassAttributes<T> & E;
			declare global {
				namespace JSX {
					interface Element {}
					interface IntrinsicElements {
						img: DetailedHTMLProps<ImgHTMLAttributes<HTMLImageElement>, HTMLImageElement>;
						source: DetailedHTMLProps<SourceHTMLAttributes<HTMLSourceElement>, HTMLSourceElement>;
					}
				}
			}
		`,
		"node_modules/image-like/index.d.ts": `export interface ImageLike { src: string; loadSrc(): string }`,
		"dom.d.ts": `
			interface HTMLImageElement { alt: string; src: string }
			interface HTMLSourceElement { src: string }
		`,
	}
}

// A type we do not own fixes `src` on an intrinsic attribute, on a literal React or the DOM types,
// and on a member we implement from a dependency; each quiet case has a reporting twin where the
// same spelling is ours, so the silence is shown to come from the types rather than from the word.
// Controls: no contextual declaration owning the spelling fails 9/24; no inherited dependency
// member, 4/24; no combined dependency constituent, 2/24; judging a JSX attribute name, 4/24.
func TestConsistencyNoAbbreviatedIdentifierLeavesIntrinsicAndDependencyNamesAlone(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name, source string
		want         int
	}{
		{"img attribute", `const element = <img src={url} alt="" />;`, 0},
		{"source attribute", `const element = <source src={url} type="video/mp4" />;`, 0},
		{"React attributes literal", `const attributes: React.ImgHTMLAttributes<HTMLImageElement> = { src: url };`, 0},
		{
			"React attributes argument",
			`declare function render(attributes: React.SourceHTMLAttributes<HTMLSourceElement>): void; ` +
				`render({ src: url });`,
			0,
		},
		{"intrinsic element attributes literal", `const attributes: JSX.IntrinsicElements['img'] = { src: url };`, 0},
		{"DOM partial literal", `const image: Partial<HTMLImageElement> = { src: url };`, 0},
		{"literal spread into an intrinsic", `const element = <img {...{ src: url }} />;`, 0},
		{"conditional spread into an intrinsic", `const element = <img {...(condition ? { src: url } : {})} />;`, 0},
		{
			"implemented dependency members",
			`class Loader implements ImageLike { src = ''; loadSrc() { return '' } }`,
			0,
		},
		{
			"extends the intrinsic attributes",
			`interface ImageProperties extends React.ImgHTMLAttributes<HTMLImageElement> { src: string }`,
			0,
		},
		{
			"intersects the intrinsic attributes",
			`type ImageProperties = React.ImgHTMLAttributes<HTMLImageElement> & { src: string };`,
			0,
		},
		{
			"picks from the intrinsic attributes",
			`type ImageProperties = Pick<React.ImgHTMLAttributes<HTMLImageElement>, 'src'> & { src: string };`,
			0,
		},
		{
			"extends a pick of the intrinsic attributes",
			`interface ImageProperties extends Pick<React.ImgHTMLAttributes<HTMLImageElement>, 'src'> { src: string }`,
			0,
		},

		{
			"own property passed to an attribute",
			`interface ImageProperties { src?: string } ` +
				`function Image(properties: ImageProperties) { return <img src={properties.src} />; }`,
			1,
		},
		{
			"own property spread into an intrinsic",
			`interface ImageProperties { src?: string } ` +
				`function Image(properties: ImageProperties) { return <img {...properties} />; }`,
			1,
		},
		{
			"own literal typed by own properties",
			`interface ImageProperties { src?: string } const image: ImageProperties = { src: url };`,
			1,
		},
		{
			"own member beside the intrinsic attributes",
			`type ImageProperties = React.ImgHTMLAttributes<HTMLImageElement> & { thumbnailSrc: string };`,
			1,
		},
		// These two would be as quiet as their twins if the DOM file or the intrinsic table failed to
		// resolve, since an unresolved contract cannot prove a spelling ours, so they show it resolves.
		{
			"own member beside the DOM members",
			`const image: Partial<HTMLImageElement> = { src: url, thumbnailSrc: url };`,
			1,
		},
		{"own member spread into an intrinsic", `const element = <img {...{ src: url, thumbnailSrc: url }} />;`, 1},
		{
			"own member beside an implemented dependency",
			`class Loader implements ImageLike { src = ''; loadSrc() { return '' } thumbnailSrc = '' }`,
			1,
		},
		{
			"untyped literal read into an attribute",
			`const image = { src: url }; const element = <img src={image.src} />;`,
			1,
		},
		{
			"request body mirroring a wire format",
			`declare function send(url: string, init: { body: unknown }): void; ` +
				`send(url, { body: { cmd: 'forget-sta' } });`,
			1,
		},
		{"serialized request body", `const body = JSON.stringify({ cmd: 'forget-sta' });`, 1},
		{"request body type", `interface ForgetRequest { body: { cmd: string } }`, 1},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedFiles(
				test,
				ConsistencyNoAbbreviatedIdentifier,
				abbreviatedIntrinsicDependencyFiles(testCase.source),
				"subject.tsx",
			)
			if len(result.Diagnostics) != testCase.want {
				test.Fatalf(
					"expected %d findings, got %d: %v",
					testCase.want,
					len(result.Diagnostics),
					result.MessageIds(),
				)
			}
			for _, diagnostic := range result.Diagnostics {
				text := result.SourceFile.Text()[diagnostic.Range.Pos():diagnostic.Range.End()]
				if text != "src" && text != "thumbnailSrc" && text != "cmd" {
					test.Errorf("finding at %q instead of the declared member", text)
				}
			}
		})
	}
}

// JavaScript's var keyword is a word, not short for "variable", so a name holding one of
// policy/KeywordTerms.json's spellings for it reads clean (#hvyvfr5). A bare `Var`, a spelling run on
// into a longer word, and a keyword term next to another abbreviation or a bare `Var` are still
// reported, and `NoVar`
// is the control: the rule runs on this file and judges `Var`, so the silence on the rest is the
// keyword term's.
func TestConsistencyNoAbbreviatedIdentifierReadsAKeywordTermAsAWord(test *testing.T) {
	test.Parallel()

	sourceText := "export function isVarDeclaration(kind: string): boolean {\n    return kind === 'var';\n}\n" +
		"export class VarKeywordFix {}\n" +
		"export const TopLevelVarScopedDeclarations = 1;\n" +
		"export const NoVar = 1;\n" +
		"export const themeVar = 1;\n" +
		"export const VarScopedness = 1;\n" +
		"export const VarDeclarationSrc = 1;\n" +
		"export const themeVarBeforeVarKeyword = 1;\n"
	result := rule_testing.Run(test, ConsistencyNoAbbreviatedIdentifier, abbreviatedFile, sourceText)
	want := []string{
		`Identifier "NoVar" abbreviates "Var". Use "Variable" for a variable,`,
		`Identifier "themeVar" abbreviates "Var". Use "Variable" for a variable,`,
		`Identifier "VarScopedness" abbreviates "Var". Use "Variable" for a variable,`,
		`Identifier "VarDeclarationSrc" abbreviates "Src". Use "VarDeclarationSource".`,
		`Identifier "themeVarBeforeVarKeyword" abbreviates "Var". Use "Variable" for a variable,`,
	}
	if len(result.Diagnostics) != len(want) {
		test.Errorf("%d findings, want %d", len(result.Diagnostics), len(want))
	}
	for index, diagnostic := range result.Diagnostics {
		if index >= len(want) || !strings.HasPrefix(diagnostic.Message.Description, want[index]) {
			test.Errorf("finding %d is %q", index, diagnostic.Message.Description)
		}
	}
}
