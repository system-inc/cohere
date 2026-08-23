package nexus

import (
	"regexp"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

// ConsistencyNoAbbreviatedIdentifierOptions names the files and scopes where a framework owns an
// identifier's spelling.
//
// The rule itself knows nothing about any framework. A consumer names the paths and scopes where a
// framework mandates a name, because only the consumer knows which framework it is running.
// Structure passes Next.js's page, layout, route, and middleware paths; a codebase with no framework
// passes nothing and every identifier is judged on its merits.
type ConsistencyNoAbbreviatedIdentifierOptions struct {
	// FrameworkParameterFilePatterns are path substrings whose files may use framework-mandated
	// parameter names verbatim (`params`, `searchParams`). Next.js demands these spellings in page,
	// layout, and route files, and a rename there breaks the framework contract rather than
	// improving the name.
	FrameworkParameterFilePatterns []string

	// FrameworkConstantFilePatterns are path substrings whose files may declare a framework-mandated
	// `config` constant. Next.js middleware requires `export const config` by that exact name.
	FrameworkConstantFilePatterns []string

	// FrameworkParameterScopeNames are exact names of functions, interfaces, or type aliases inside
	// which framework-mandated parameter names are allowed, wherever the file lives. Next.js reads
	// `generateMetadata` and `generateStaticParams` by name.
	FrameworkParameterScopeNames []string

	// FrameworkParameterScopeSuffixes have the same effect, matched against the declaration name and
	// against that name with role suffixes stripped, so `PageRoute` also covers
	// `SomethingPageRouteProperties`.
	FrameworkParameterScopeSuffixes []string
}

// abbreviationCandidatePattern is every spelling any branch below could possibly match, in one test.
//
// **This is no longer the runtime gate.** `isAbbreviationCandidate` in abbreviation_gate.go answers
// the same question with lookups, 76 times faster, and this pattern is now the specification it is
// tested against: `abbreviation_gate_test.go` asserts the gate admits every name this matches, over
// 82,367 identifiers from the ahra tree. Keeping the pattern is what makes that test meaningful, so
// a branch gaining a new abbreviation still updates this first and the differential test then fails
// until the gate follows.
//
// This exists purely to decide whether a name is worth spending the rest of the rule on, and it is
// the reason the guards are ordered the way they are. In the TypeScript original the expensive step
// was a scope walk that cost the tree roughly two and a half seconds, more than every other house
// rule combined, and the overwhelming majority of that work was spent proving that names like
// `renderNodeOrComponent` are not imports, which no branch would have asked about anyway. Measured:
// 2,580ms of a 3,700ms run became 152ms once this gate ran first, with findings byte-identical.
//
// The port has no scope walk, so the saving here is smaller in absolute terms, but the shape is the
// same and it still matters: this listener fires on every identifier in the tree, and the walk
// through parent kinds plus roughly forty string comparisons per name is real work that 99.7% of
// identifiers do not need to do. A rule that declines cheaply is the whole speed story.
//
// The alternation is deliberately a superset of the branches, never a subset. It over-matches
// slightly (a name that passes here may still fall through every branch and report nothing), which
// is the safe direction: a false positive here costs one cheap traversal, while a false negative
// would silently stop the rule firing, and a rule that stops firing looks exactly like a clean run.
//
// If a branch below gains a new abbreviation, it must be added here too, or the rule will go quiet
// for it. The four groups map one-to-one onto the branch families: whole-word names, `abbrev[A-Z]`
// prefixes, `Abbrev` suffixes and the `Ms` special case, and mid-name word segments.
var abbreviationCandidatePattern = regexp.MustCompile(
	`^(prop|props|param|params|ref|config|idx|arg|args|acc|char|fn|str|val|arr|obj|num|res|err|req|msg|min|max|prev|cur|pct|opts|ctx|db|tx|queryFn|mutationFn)$` +
		`|^(ctx|db|tx|opts|cur|pct|prev|idx|config|prop|props|param|params|ref|arg|args|char|fn|str|val|arr|obj|num|res|err|req|msg|min|max)[A-Z]` +
		`|(Prop|Props|Param|Params|Ref|Config|Idx|Arg|Args|Char|Fn|Str|Val|Arr|Obj|Num|Res|Err|Req|Msg|Min|Max)$` +
		`|[a-z]Ms($|[A-Z])` +
		`|(Cwd|Dir|Env|Cli|Len|Seq|Db|Tx|Vars|Var)($|[A-Z0-9])`,
)

// millisecondSegmentPattern matches a millisecond unit written as a camelCase word.
//
// Go's regexp has no lookahead, so where the TypeScript wrote `[a-z]Ms(?=$|[A-Z])` this captures the
// character after instead and puts it back in the replacement. The distinction the pattern is making
// is unchanged: a lowercase letter before, and either the end of the name or the start of the next
// word after.
var millisecondSegmentPattern = regexp.MustCompile(`([a-z])Ms($|[A-Z])`)

// allowedAbbreviatedNames would otherwise be flagged and are not ours to rename.
var allowedAbbreviatedNames = map[string]bool{
	"URLSearchParams": true, // Web API
}

// allowedAbbreviationSegments are words that merely contain an abbreviation's letters.
//
// `InnoDb` is MySQL's storage engine and a proper noun, so the `Db` inside it is not standing in for
// anything: renaming it to `InnoDatabase` names a thing that does not exist. Matched as a segment
// rather than as a whole identifier, because it appears inside longer names like
// `InnoDbMaximumIndexKeyBytes`.
var allowedAbbreviationSegments = []string{"InnoDb"}

func containsAllowedAbbreviationSegment(name string) bool {
	for _, segment := range allowedAbbreviationSegments {
		if strings.Contains(name, segment) {
			return true
		}
	}
	return false
}

// exactAbbreviation is a whole-word abbreviation and the word it stands in for.
type exactAbbreviation struct {
	name      string
	full      string
	messageId string
}

// suffixAbbreviation is an abbreviation appearing at the end of a name.
//
// lowerWord and upperWord are the already-spelled-out forms that must not re-fire: `Value` ends with
// neither `Val` nor... it does end with `Val`'s letters is exactly the trap, so `parsedValue` is
// excluded by ending with `Value`.
type suffixAbbreviation struct {
	suffix      string
	replacement string
	lowerWord   string
	upperWord   string
	messageId   string
}

// prefixAbbreviation is an abbreviation appearing at the start of a camelCase name.
type prefixAbbreviation struct {
	prefix      string
	replacement string
	fullWord    string
	messageId   string
}

// wordSegmentAbbreviation is an abbreviation that is a camelCase word wherever it sits.
type wordSegmentAbbreviation struct {
	word        string
	replacement string
}

var exactAbbreviations = []exactAbbreviation{
	{"val", "value", "noVal"},
	{"arr", "array", "noArr"},
	{"obj", "object", "noObj"},
	{"num", "number", "noNum"},
	{"res", "response", "noRes"},
	{"err", "error", "noErr"},
	{"req", "request", "noReq"},
	{"msg", "message", "noMsg"},
	{"min", "minimum", "noMin"},
	{"max", "maximum", "noMax"},
}

var suffixAbbreviations = []suffixAbbreviation{
	{"Val", "Value", "value", "Value", "noValSuffix"},
	{"Arr", "Array", "array", "Array", "noArrSuffix"},
	{"Obj", "Object", "object", "Object", "noObjSuffix"},
	{"Num", "Number", "number", "Number", "noNumSuffix"},
	{"Res", "Response", "response", "Response", "noResSuffix"},
	{"Err", "Error", "error", "Error", "noErrSuffix"},
	{"Req", "Request", "request", "Request", "noReqSuffix"},
	{"Msg", "Message", "message", "Message", "noMsgSuffix"},
	{"Min", "Minimum", "minimum", "Minimum", "noMinSuffix"},
	{"Max", "Maximum", "maximum", "Maximum", "noMaxSuffix"},
}

var prefixAbbreviations = []prefixAbbreviation{
	{"val", "value", "value", "noVal"},
	{"arr", "array", "array", "noArr"},
	{"obj", "object", "object", "noObj"},
	{"num", "number", "number", "noNum"},
	{"res", "response", "response", "noRes"},
	{"err", "error", "error", "noErr"},
	{"req", "request", "request", "noReq"},
	{"msg", "message", "message", "noMsg"},
	{"min", "minimum", "minimum", "noMin"},
	{"max", "maximum", "maximum", "noMax"},
}

// wordSegmentAbbreviations are matched wherever they sit rather than only at an anchor.
//
// The branches above anchor: `^max[A-Z]` catches `maximumAge` and misses `DatabaseMaxPageSize`, and
// a suffix check catches `userWorkingDirectory` and misses `originalInitCwd`. Both spellings are the
// same abbreviation and a reader meets them the same way, so the position it happens to occupy
// should not decide whether the rule speaks.
//
// A segment is matched when the characters before it are not lowercase letters continuing a longer
// word, and what follows is either the end of the name or the start of the next word. That is what
// keeps `Direction` from reading as `Dir`, `Environment` from reading as `Env`, and `SMS` from
// reading as anything.
//
// `Vars` precedes `Var` so the longer word wins.
var wordSegmentAbbreviations = []wordSegmentAbbreviation{
	{"Cwd", "WorkingDirectory"},
	{"Dir", "Directory"},
	{"Env", "Environment"},
	{"Cli", "CommandLineInterface"},
	{"Len", "Length"},
	{"Seq", "Sequence"},
	{"Db", "Database"},
	{"Tx", "Transaction"},
	{"Vars", "Variables"},
	{"Var", "Variable"},
}

// prefixContinuationPattern is `^<prefix>[A-Z]`, built once per abbreviation rather than per name.
var prefixContinuationPatterns = buildPrefixContinuationPatterns()

func buildPrefixContinuationPatterns() map[string]*regexp.Regexp {
	patterns := map[string]*regexp.Regexp{}
	for _, prefix := range []string{
		"ctx", "db", "tx", "opts", "cur", "pct", "prev", "idx", "config", "prop", "props",
		"param", "params", "ref", "arg", "args", "char", "fn", "str",
	} {
		patterns[prefix] = regexp.MustCompile(`^` + prefix + `[A-Z]`)
	}
	for _, entry := range prefixAbbreviations {
		patterns[entry.prefix] = regexp.MustCompile(`^` + entry.prefix + `[A-Z]`)
	}
	return patterns
}

// wordSegmentPatterns are the two shapes a mid-name segment can take, plus the replacement form.
type wordSegmentPatterns struct {
	boundary *regexp.Regexp
	camel    *regexp.Regexp
	replace  *regexp.Regexp
}

var compiledWordSegmentPatterns = buildWordSegmentPatterns()

func buildWordSegmentPatterns() map[string]wordSegmentPatterns {
	patterns := map[string]wordSegmentPatterns{}
	for _, entry := range wordSegmentAbbreviations {
		patterns[entry.word] = wordSegmentPatterns{
			boundary: regexp.MustCompile(`(^|[^a-zA-Z])` + entry.word + `($|[A-Z0-9])`),
			camel:    regexp.MustCompile(`[a-z0-9]` + entry.word + `($|[A-Z0-9])`),
			replace:  regexp.MustCompile(entry.word + `($|[A-Z0-9])`),
		}
	}
	return patterns
}

// messageAbbreviation is the shape every finding takes: name the abbreviation, name the replacement.
//
// Every message states the reasoning rather than just the verdict, because a rule that only says
// what is wrong gets disabled the first time it is inconvenient.
func messageAbbreviation(messageId string, description string) rule.Message {
	return rule.Message{Id: messageId, Description: description}
}

func messageExactAbbreviation(messageId string, name string, suggestion string) rule.Message {
	return messageAbbreviation(messageId, `Identifier "`+name+`" should not be abbreviated. Use "`+
		suggestion+`" or a more descriptive name. `+abbreviationReasoning)
}

func messageSuggestedRename(messageId string, name string, suggestion string) rule.Message {
	return messageAbbreviation(messageId, `Identifier "`+name+`" should not be abbreviated. Use "`+
		suggestion+`". `+abbreviationReasoning)
}

func messageSuffixRename(messageId string, name string, suffix string, suggestion string) rule.Message {
	return messageAbbreviation(messageId, `Identifier "`+name+`" should not end with "`+suffix+
		`". Use "`+suggestion+`". `+abbreviationReasoning)
}

// abbreviationReasoning is written once and shared, because it is the same argument every time.
const abbreviationReasoning = "A name is written once and read everywhere, so the letters saved at " +
	"the declaration are paid back at every call site by a reader who has to expand the abbreviation " +
	"themselves and hope they expanded it the way the author meant."

var messageNoAcc = rule.Message{
	Id: "noAcc",
	Description: `Identifier "acc" should not be abbreviated. Pick a name that describes what is ` +
		`being accumulated, "total", "sum", "groupedItems", or whatever fits the reduce. ` +
		abbreviationReasoning,
}

var messageNoArg = rule.Message{
	Id: "noArg",
	Description: `Identifier "arg" should not be abbreviated. Use "argument", "commandArgument", or ` +
		`a more descriptive name. ` + abbreviationReasoning,
}

var messageNoArgs = rule.Message{
	Id: "noArgs",
	Description: `Identifier "args" should not be abbreviated. Use "arguments", "commandArguments", ` +
		`"commandLineArguments", or a more descriptive name. ` + abbreviationReasoning,
}

var messageNoFn = rule.Message{
	Id: "noFn",
	Description: `Identifier "fn" should not be abbreviated. Use "callback", "handler", "factory", ` +
		`or a more descriptive name. ` + abbreviationReasoning,
}

func messageNoFnSuffix(name string) rule.Message {
	return rule.Message{
		Id: "noFnSuffix",
		Description: `Identifier "` + name + `" should not end with "Fn". Use a more descriptive ` +
			`name, drop the "Fn" suffix or rename to a role like "Handler", "Callback", "Factory". ` +
			abbreviationReasoning,
	}
}

func messageNoMsSuffix(name string, suggestion string) rule.Message {
	return rule.Message{
		Id: "noMsSuffix",
		Description: `Identifier "` + name + `" should not abbreviate milliseconds as "Ms". Use "` +
			suggestion + `", which is how the rest of the tree spells a millisecond value: ` +
			`"durationInMilliseconds" outnumbers "durationMs" more than two to one for the identical ` +
			`value, so the rename follows what the codebase already decided rather than introducing a ` +
			`third spelling.`,
	}
}

func messageNoWordSegment(name string, word string, suggestion string) rule.Message {
	return rule.Message{
		Id: "noWordSegment",
		Description: `Identifier "` + name + `" abbreviates "` + word + `". Use "` + suggestion +
			`". ` + abbreviationReasoning,
	}
}

// ConsistencyNoAbbreviatedIdentifier rejects abbreviated identifier names in favor of full words.
//
//	valid:   const properties = getProperties()
//	valid:   const configuration = load()
//	valid:   const durationInMilliseconds = 500
//	valid:   import { config } from 'external'
//	invalid: const props = getProperties()
//	invalid: const cfg = 1        (not matched; see the candidate gate)
//	invalid: const idx = 0
//	invalid: const durationMs = 500
//	invalid: const originalInitCwd = process.cwd()
//
// The exemptions carry the judgment, and they divide into two kinds. Some names are not ours: a
// property read off another object, an object-literal key, an import specifier, a member of a
// qualified type name, a JSX tag or attribute. Renaming any of those changes what the code reaches
// for rather than what it calls something. The others are names a framework mandates, which the
// caller names through options because only the caller knows which framework it is running.
//
// No fix, and that is a deliberate departure from the TypeScript original, which renames the
// identifier in place. The original guards that rename with one scope-analysis call,
// `resolvesToImportedBinding`, and that guard's own comment says what it is for: it stops `--fix`
// from renaming a name this file does not own, after `loadConfig` from `tsconfig-paths` lost its
// import exactly that way. This port has no scope analysis, and a rule that reports without renaming
// cannot break an import, so the dependency disappears along with the fix. The suggested name
// travels in the message instead, where a reader applies it with the scope in front of them. Four
// other ported rules made the same call for the same reason.
//
// One consequence worth stating plainly: without the scope walk, a reference to an imported
// abbreviated name is still reported here where the original stayed silent. That is a message a
// reader can dismiss, not a rename that breaks a file, which is the trade the missing fix makes
// affordable. The import specifier itself is still exempt, so the finding lands on the use rather
// than on the declaration.
var ConsistencyNoAbbreviatedIdentifier = rule.Rule{
	Name: "consistency-no-abbreviated-identifier",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings, _ := options.(ConsistencyNoAbbreviatedIdentifierOptions)

		fileName := normalizedFileName(ctx.SourceFile)
		isFrameworkParameterFile := matchesAnyFilePattern(fileName, settings.FrameworkParameterFilePatterns)
		isFrameworkConstantFile := matchesAnyFilePattern(fileName, settings.FrameworkConstantFilePatterns)

		return rule.Listeners{
			ast.KindIdentifier: func(node *ast.Node) {
				name := node.Text()

				// Cheapest question first: could this name match any branch at all? Everything below
				// is a skip or a report keyed on the same spellings, so a name no branch could match
				// reports nothing whichever order the guards run in. See the pattern's own comment.
				if !isAbbreviationCandidate(name) {
					return
				}

				// These are names somebody else chose, not bindings this file has to live with.
				if isNonRenameableIdentifier(node) {
					return
				}

				if allowedAbbreviatedNames[name] {
					return
				}

				// TanStack Query — `queryFn` and `mutationFn` are property names the library
				// contract requires. Allowed only as an object-literal key, where the intent is
				// clearly "I am passing this to a TanStack hook". A bare variable named `queryFn`
				// still fires.
				if (name == "queryFn" || name == "mutationFn") && isObjectLiteralKey(node) {
					return
				}

				// The identifier's own text, not its leading trivia.
				//
				// `node.Loc.Pos()` sits before the trivia, so a binding preceded by a comment
				// reports at the comment. That is the one line an `eslint-disable-next-line` above
				// it cannot cover, since the directive matches the line after itself: `params` in
				// McpApi.ts reported at line 240 while its suppression sat on 241 covering 242, so a
				// correctly-suppressed identifier still produced a finding nothing could silence.
				identifierRange := rule.TokenRange(ctx.SourceFile, node)
				report := func(message rule.Message) {
					ctx.ReportRange(identifierRange, message)
				}

				switch name {
				case "prop":
					report(messageExactAbbreviation("noProp", name, "property"))
					return
				case "props":
					report(messageExactAbbreviation("noProps", name, "properties"))
					return
				case "param":
					report(messageExactAbbreviation("noParam", name, "parameter"))
					return
				case "params":
					// Next.js requires this spelling in page, layout, and route files, and inside
					// the functions it reads by name.
					if isFrameworkParameterFile || isInsideFrameworkScope(node, settings) {
						return
					}
					report(messageExactAbbreviation("noParams", name, "parameters"))
					return
				case "ref":
					// React 19 made `ref` a regular property on function components. The canonical
					// name is load-bearing: interface keys, destructured shorthand, and
					// forward-into-child JSX attribute values all have to read as `ref` for
					// consumers to wire refs up correctly.
					if isReactReferencePropertyContext(node) {
						return
					}
					report(messageExactAbbreviation("noRef", name, "reference"))
					return
				case "config":
					// Next.js middleware requires `export const config` by that exact name.
					if isFrameworkConstantFile {
						return
					}
					report(messageExactAbbreviation("noConfig", name, "configuration"))
					return
				case "idx":
					report(messageExactAbbreviation("noIdx", name, "index"))
					return
				case "arg":
					report(messageNoArg)
					return
				case "args":
					// A rest parameter in a variadic or framework function keeps the conventional
					// spelling: `...args` is the shape everyone reads.
					if isRestElement(node) {
						return
					}
					report(messageNoArgs)
					return
				case "acc":
					report(messageNoAcc)
					return
				case "char":
					report(messageExactAbbreviation("noChar", name, "character"))
					return
				case "fn":
					report(messageNoFn)
					return
				case "str":
					report(messageExactAbbreviation("noStr", name, "string"))
					return
				}

				// Abbreviations that expand identically whether alone or as a camelCase prefix. The
				// already-spelled guard is a prefix of the full word rather than the two letters,
				// because `dbCode` and `database` both begin `d`,`b` and only the second is already
				// spelled out.
				for _, entry := range []struct {
					abbreviation   string
					replacement    string
					alreadySpelled string
					messageId      string
				}{
					{"ctx", "context", "context", "noCtx"},
					{"db", "database", "database", "noDb"},
					{"tx", "transaction", "transaction", "noTx"},
					{"opts", "options", "options", "noOpts"},
					{"cur", "current", "current", "noCur"},
					{"pct", "percent", "percent", "noPct"},
					{"prev", "previous", "previous", "noPrev"},
				} {
					// `InnoDb` is a proper noun; the `Db` in it stands in for nothing.
					if entry.abbreviation == "db" && containsAllowedAbbreviationSegment(name) {
						continue
					}
					if name == entry.abbreviation {
						report(messageSuggestedRename(entry.messageId, name, entry.replacement))
						return
					}
					if prefixContinuationPatterns[entry.abbreviation].MatchString(name) &&
						!strings.HasPrefix(name, entry.alreadySpelled) {
						suggestion := entry.replacement + strings.TrimPrefix(name, entry.abbreviation)
						report(messageSuggestedRename(entry.messageId, name, suggestion))
						return
					}
				}

				// The rest of the whole-word family, all deterministic expansions.
				for _, entry := range exactAbbreviations {
					if name == entry.name {
						report(messageExactAbbreviation(entry.messageId, name, entry.full))
						return
					}
				}

				// Suffix checks. `Properties` already ends with `Props`'s letters, which is why the
				// guard tests the spelled-out word rather than the abbreviation.
				if strings.HasSuffix(name, "Prop") && !strings.HasSuffix(name, "Properties") {
					report(messageSuffixRename("noPropSuffix", name, "Prop",
						strings.TrimSuffix(name, "Prop")+"Property"))
					return
				}
				if strings.HasSuffix(name, "Props") && !strings.HasSuffix(name, "Properties") {
					report(messageSuffixRename("noPropsSuffix", name, "Props",
						strings.TrimSuffix(name, "Props")+"Properties"))
					return
				}
				if strings.HasSuffix(name, "Param") {
					report(messageSuffixRename("noParamSuffix", name, "Param",
						strings.TrimSuffix(name, "Param")+"Parameter"))
					return
				}
				if strings.HasSuffix(name, "Params") {
					// `searchParams` is required verbatim by Next.js in the same files and scopes.
					if isFrameworkParameterFile || isInsideFrameworkScope(node, settings) {
						return
					}
					report(messageSuffixRename("noParamsSuffix", name, "Params",
						strings.TrimSuffix(name, "Params")+"Parameters"))
					return
				}

				// A millisecond suffix, read as a camelCase word rather than as two letters. It
				// requires a lowercase letter before it and either the end of the name or the start
				// of the next word after, so the unit is matched wherever it sits: `maximumAgeMs` and
				// `maximumAgeMsHalf` both count, while `SMS`, `LLMs`, and `CountryCodeMS` are words
				// that merely contain the letters, and `msgText` and `someMsgHandler` fail because
				// `Msg` continues in lowercase.
				//
				// Anchoring only to the end would have missed the middle, which is how
				// `maximumAgeMsHalf` sat unreported while every one of its siblings was caught.
				//
				// Foreign names like Node's `mtimeMs` on `Stats` still match, and a consumer cannot
				// rename those, so they take a stated suppression at their single site the way every
				// other foreign spelling does.
				if millisecondSegmentPattern.MatchString(name) {
					suggestion := replaceFirst(millisecondSegmentPattern, name, "${1}InMilliseconds${2}")
					report(messageNoMsSuffix(name, suggestion))
					return
				}

				if strings.HasSuffix(name, "Ref") {
					report(messageSuffixRename("noRefSuffix", name, "Ref",
						strings.TrimSuffix(name, "Ref")+"Reference"))
					return
				}
				if strings.HasSuffix(name, "Config") {
					report(messageSuffixRename("noConfigSuffix", name, "Config",
						strings.TrimSuffix(name, "Config")+"Configuration"))
					return
				}
				if strings.HasSuffix(name, "Idx") {
					report(messageSuffixRename("noIdxSuffix", name, "Idx",
						strings.TrimSuffix(name, "Idx")+"Index"))
					return
				}
				if strings.HasSuffix(name, "Arg") && !strings.HasSuffix(name, "argument") &&
					!strings.HasSuffix(name, "Argument") {
					report(messageSuffixRename("noArgSuffix", name, "Arg",
						strings.TrimSuffix(name, "Arg")+"Argument"))
					return
				}
				if strings.HasSuffix(name, "Args") && !strings.HasSuffix(name, "arguments") &&
					!strings.HasSuffix(name, "Arguments") {
					report(messageSuffixRename("noArgsSuffix", name, "Args",
						strings.TrimSuffix(name, "Args")+"Arguments"))
					return
				}
				if strings.HasSuffix(name, "Char") && !strings.HasSuffix(name, "character") &&
					!strings.HasSuffix(name, "Character") {
					report(messageSuffixRename("noCharSuffix", name, "Char",
						strings.TrimSuffix(name, "Char")+"Character"))
					return
				}
				if strings.HasSuffix(name, "Fn") && !strings.HasSuffix(name, "function") &&
					!strings.HasSuffix(name, "Function") {
					report(messageNoFnSuffix(name))
					return
				}
				if strings.HasSuffix(name, "Str") && !strings.HasSuffix(name, "string") &&
					!strings.HasSuffix(name, "String") {
					report(messageSuffixRename("noStrSuffix", name, "Str",
						strings.TrimSuffix(name, "Str")+"String"))
					return
				}
				for _, entry := range suffixAbbreviations {
					if strings.HasSuffix(name, entry.suffix) &&
						!strings.HasSuffix(name, entry.lowerWord) &&
						!strings.HasSuffix(name, entry.upperWord) {
						report(messageSuffixRename(entry.messageId, name, entry.suffix,
							strings.TrimSuffix(name, entry.suffix)+entry.replacement))
						return
					}
				}

				// Prefix checks, camelCase continuation: `paramsText`, `configValue`, `idxStart`.
				for _, entry := range []struct {
					prefix         string
					replacement    string
					alreadySpelled string
					messageId      string
				}{
					// `idx` has no already-spelled guard in the original: `index` does not start
					// with `idx`, so there is nothing to re-fire on.
					{"idx", "index", "", "noIdx"},
					{"config", "configuration", "configuration", "noConfig"},
					{"prop", "property", "propert", "noProp"},
					{"props", "properties", "propert", "noProps"},
					{"param", "parameter", "parameter", "noParam"},
					{"params", "parameters", "parameter", "noParams"},
					{"ref", "reference", "reference", "noRef"},
					{"char", "character", "character", "noChar"},
					{"str", "string", "string", "noStr"},
				} {
					if !prefixContinuationPatterns[entry.prefix].MatchString(name) {
						continue
					}
					if entry.alreadySpelled != "" && strings.HasPrefix(name, entry.alreadySpelled) {
						continue
					}
					suggestion := entry.replacement + strings.TrimPrefix(name, entry.prefix)
					report(messageSuggestedRename(entry.messageId, name, suggestion))
					return
				}

				// `arg`, `args`, and `fn` prefixes report without naming a replacement, because
				// `arguments` is a reserved binding and `fn` needs a role rather than an expansion.
				if prefixContinuationPatterns["arg"].MatchString(name) && !strings.HasPrefix(name, "argument") {
					report(messageNoArg)
					return
				}
				if prefixContinuationPatterns["args"].MatchString(name) && !strings.HasPrefix(name, "arguments") {
					report(messageNoArgs)
					return
				}
				if prefixContinuationPatterns["fn"].MatchString(name) && !strings.HasPrefix(name, "function") {
					report(messageNoFn)
					return
				}

				for _, entry := range prefixAbbreviations {
					if prefixContinuationPatterns[entry.prefix].MatchString(name) &&
						!strings.HasPrefix(name, entry.fullWord) {
						suggestion := entry.replacement + strings.TrimPrefix(name, entry.prefix)
						report(messageSuggestedRename(entry.messageId, name, suggestion))
						return
					}
				}

				if containsAllowedAbbreviationSegment(name) {
					return
				}

				for _, entry := range wordSegmentAbbreviations {
					patterns := compiledWordSegmentPatterns[entry.word]
					if !patterns.boundary.MatchString(name) && !patterns.camel.MatchString(name) {
						continue
					}
					suggestion := replaceFirst(patterns.replace, name, entry.replacement+"${1}")
					if suggestion == name {
						continue
					}
					report(messageNoWordSegment(name, entry.word, suggestion))
					return
				}
			},
		}
	},
}

// replaceFirst replaces only the first match, which is what JavaScript's non-global String.replace
// does and what every suggestion in this rule means.
//
// Go's ReplaceAllString would rewrite every occurrence, so `variablesForVars` would come back
// double-renamed while the original renamed one segment and left the reader to judge the rest.
func replaceFirst(pattern *regexp.Regexp, text string, replacement string) string {
	location := pattern.FindStringSubmatchIndex(text)
	if location == nil {
		return text
	}
	expanded := pattern.ExpandString(nil, replacement, text, location)
	return text[:location[0]] + string(expanded) + text[location[1]:]
}

// matchesAnyFilePattern reports whether the linted file matches any caller-supplied path substring.
func matchesAnyFilePattern(fileName string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(fileName, pattern) {
			return true
		}
	}
	return false
}

// isNonRenameableIdentifier reports whether an identifier names something this file does not own.
//
// Every entry here is an exemption the TypeScript original spells out, plus the JSX cases it never
// needed to. That difference is the highest-risk part of porting an identifier rule: an exemption
// the original gets from its AST for free has to be written down in ours. A JSX tag name is a
// distinct node type in the TypeScript parser and a plain KindIdentifier in typescript-go, and the
// sibling rule cost 3,081 false findings by reproducing only the written-down exemptions.
func isNonRenameableIdentifier(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil {
		return false
	}

	switch parent.Kind {
	case ast.KindPropertyAccessExpression:
		// The property half of `thing.props`, but not the object half.
		//
		// `this.props` is deliberately not exempt: the property belongs to the class being read,
		// which is ours, and its declaration is a property declaration that no member check
		// protects. Treating it like any other property read is how `BackoffTask` ended up
		// declaring `maximumBackoff` while reading `this.maximumBackoff`.
		access := parent.AsPropertyAccessExpression()
		if access == nil || access.Name() != node {
			return false
		}
		return access.Expression == nil || access.Expression.Kind != ast.KindThisKeyword

	case ast.KindPropertyAssignment:
		// The key half of `{ config: 1 }`, but not a shorthand value.
		assignment := parent.AsPropertyAssignment()
		return assignment != nil && assignment.Name() == node

	case ast.KindImportSpecifier, ast.KindExportSpecifier, ast.KindNamespaceImport,
		ast.KindImportClause:
		// An imported name is external surface we cannot rename, in all three of its spellings:
		// `import { config }`, `import * as config`, and `import config from`.
		return true

	case ast.KindPropertySignature:
		// An interface or type-literal key shapes an external surface often enough that the
		// original skips the whole family: HTML props, the cookie spec, Google API wire fields.
		signature := parent.AsPropertySignatureDeclaration()
		return signature != nil && signature.Name() == node

	case ast.KindQualifiedName:
		// The right half of `TSESLint.FlatConfig.Config` is typescript-eslint's spelling, reachable
		// here only because we import the namespace. The left root is still judged, since a locally
		// declared namespace is ours.
		qualified := parent.AsQualifiedName()
		return qualified != nil && qualified.Right == node

	case ast.KindTypeReference:
		// `validators: FormValidateOrFn<T>` names an external type we cannot rename. The import is
		// already exempt above; this covers the usage sites.
		reference := parent.AsTypeReferenceNode()
		return reference != nil && reference.TypeName == node

	case ast.KindJsxOpeningElement, ast.KindJsxClosingElement, ast.KindJsxSelfClosingElement,
		ast.KindJsxAttribute:
		// A JSX tag and a JSX attribute are identifiers syntactically and neither is a binding this
		// file owns: an intrinsic element is named by HTML, and an attribute is named by whatever
		// component declares it.
		return true
	}
	return false
}

// isObjectLiteralKey reports whether an identifier is the non-computed key of an object literal
// property.
func isObjectLiteralKey(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil || parent.Kind != ast.KindPropertyAssignment {
		return false
	}
	assignment := parent.AsPropertyAssignment()
	return assignment != nil && assignment.Name() == node
}

// isRestElement reports whether an identifier is the name bound by a rest parameter or a rest
// binding element, the `...args` shape.
func isRestElement(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindParameter:
		parameter := parent.AsParameterDeclaration()
		return parameter != nil && parameter.DotDotDotToken != nil && parameter.Name() == node
	case ast.KindBindingElement:
		element := parent.AsBindingElement()
		return element != nil && element.DotDotDotToken != nil && element.Name() == node
	}
	return false
}

// isReactReferencePropertyContext reports whether a `ref` identifier is participating in React 19's
// ref-as-property pattern.
//
// React 19 made `ref` a regular property on plain function components, so the canonical name became
// load-bearing: any other spelling silently breaks ref forwarding for consumers. Three positions
// count, matching the original: an interface member, a shorthand in a destructure, and an
// expression-position reference inside a function whose parameter destructures `ref`.
func isReactReferencePropertyContext(node *ast.Node) bool {
	if node.Text() != "ref" {
		return false
	}
	parent := node.Parent
	if parent == nil {
		return false
	}

	// An interface or type-literal member: `ref?: React.Ref<T>`. Already exempt via the property
	// signature case above, kept here so this predicate answers the whole question on its own.
	if parent.Kind == ast.KindPropertySignature {
		signature := parent.AsPropertySignatureDeclaration()
		if signature != nil && signature.Name() == node {
			return true
		}
	}

	// A shorthand in an object literal or a binding pattern: `{ ref }`.
	if parent.Kind == ast.KindShorthandPropertyAssignment {
		return true
	}
	if parent.Kind == ast.KindBindingElement {
		element := parent.AsBindingElement()
		if element != nil && element.Name() == node {
			return true
		}
	}

	// An expression-position reference inside a function that destructures `ref` from its
	// properties object, which is the forwarding case: `<Child ref={ref} />`.
	for current := parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
			if functionDestructuresReference(current) {
				return true
			}
		}
	}
	return false
}

// functionDestructuresReference reports whether any parameter of a function destructures `ref` at
// the top level of an object pattern.
//
// Only the top level, because React 19's ref-as-property only ever sits there.
func functionDestructuresReference(functionNode *ast.Node) bool {
	parameters := functionNode.Parameters()
	for _, parameterNode := range parameters {
		parameter := parameterNode.AsParameterDeclaration()
		if parameter == nil {
			continue
		}
		binding := parameter.Name()
		if binding == nil || binding.Kind != ast.KindObjectBindingPattern {
			continue
		}
		pattern := binding.AsBindingPattern()
		if pattern == nil || pattern.Elements == nil {
			continue
		}
		for _, elementNode := range pattern.Elements.Nodes {
			element := elementNode.AsBindingElement()
			if element == nil {
				continue
			}
			elementName := element.Name()
			if elementName != nil && elementName.Kind == ast.KindIdentifier && elementName.Text() == "ref" {
				return true
			}
		}
	}
	return false
}

// isInsideFrameworkScope reports whether an identifier sits inside a declaration the framework owns.
//
// A path pattern alone is not enough. Next.js reads `generateMetadata` and `generateStaticParams` by
// name wherever they are declared, and a project may route through a component named `*PageRoute`
// that lives outside the app directory entirely. In both cases the enclosing declaration, not the
// filename, is what makes the spelling mandatory, so this checks the enclosing function and the
// enclosing interface or type alias.
//
// A type name is matched after stripping the role suffixes our conventions append, so that
// `PublicProfilePageRouteProperties` matches a `PageRoute` suffix the same way the function does.
func isInsideFrameworkScope(node *ast.Node, settings ConsistencyNoAbbreviatedIdentifierOptions) bool {
	if len(settings.FrameworkParameterScopeNames) == 0 && len(settings.FrameworkParameterScopeSuffixes) == 0 {
		return false
	}

	matches := func(name string) bool {
		for _, scopeName := range settings.FrameworkParameterScopeNames {
			if name == scopeName {
				return true
			}
		}
		if len(settings.FrameworkParameterScopeSuffixes) == 0 {
			return false
		}
		baseName := name
		for _, roleSuffix := range []string{"Properties", "Interface", "Options", "Type"} {
			baseName = strings.TrimSuffix(baseName, roleSuffix)
		}
		for _, suffix := range settings.FrameworkParameterScopeSuffixes {
			if strings.HasSuffix(name, suffix) || strings.HasSuffix(baseName, suffix) {
				return true
			}
		}
		return false
	}

	if functionName, hasName := enclosingFunctionName(node); hasName && matches(functionName) {
		return true
	}
	if typeName, hasName := enclosingTypeDeclarationName(node); hasName && matches(typeName) {
		return true
	}
	return false
}

// enclosingFunctionName walks up to the nearest function and returns its name.
//
// The walk stops at the first function rather than continuing past it, matching the original: a name
// mandated by an outer scope does not reach through an inner function that chose its own.
func enclosingFunctionName(node *ast.Node) (string, bool) {
	for current := node.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindFunctionDeclaration:
			if name := current.Name(); name != nil {
				return name.Text(), true
			}
			return "", false

		case ast.KindFunctionExpression, ast.KindArrowFunction:
			// An arrow or function expression assigned to a variable takes the variable's name.
			parent := current.Parent
			if parent != nil && parent.Kind == ast.KindVariableDeclaration {
				declaration := parent.AsVariableDeclaration()
				if declaration != nil {
					if name := declaration.Name(); name != nil && name.Kind == ast.KindIdentifier {
						return name.Text(), true
					}
				}
			}
			return "", false
		}
	}
	return "", false
}

// enclosingTypeDeclarationName walks up to the nearest interface or type alias and returns its name.
func enclosingTypeDeclarationName(node *ast.Node) (string, bool) {
	for current := node.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
			if name := current.Name(); name != nil {
				return name.Text(), true
			}
			return "", false
		}
	}
	return "", false
}
