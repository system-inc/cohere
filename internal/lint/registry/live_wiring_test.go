package registry

import (
	"os"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/configuration"
)

// The live config, which is the only thing that decides whether a registered rule ever runs.
const liveConfigPath = "/Users/kirkouimet/Projects/ahra/CohereSettings.json"

// A rule's fixture proves it works. The config decides whether it runs, and nothing else connects
// the two: `rule_testing` never reads `CohereSettings.json`, so a rule can pass both directions of its own
// pair and be inert on every real file.
//
// That happened. The first `@next/next` rule registered as `next-no-assign-module-variable` while
// the config said `nextjs/no-assign-module-variable`, which resolves on a `/` boundary to
// `no-assign-module-variable`. The names did not match, the rule ran on nothing, and its tests were
// green. Only the coverage line caught it, and only because a second sentence happened to print.
//
// This is the mechanical version: every registered rule must be a name the live config can resolve,
// or be listed below as deliberately not enabled.
func TestEveryRegisteredRuleIsReachableFromTheLiveConfig(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(liveConfigPath); err != nil {
		t.Skipf("the live config is not present at %s", liveConfigPath)
	}

	loaded, err := configuration.Load(liveConfigPath)
	if err != nil {
		t.Fatalf("loading the live config: %v", err)
	}
	resolved := loaded.Resolve("app/Probe.tsx")

	// Rules cohere implements that the live config deliberately does not enable. Each needs a reason,
	// because an entry here silences the guard for that rule permanently.
	deliberatelyNotEnabled := map[string]string{
		// The differential harness's cohere-only control. The gate's oxlint plugin has no such rule,
		// which is the asymmetry the control depends on, so the config cannot name it.
		"import-require-path-alias": "the directional control for the differential",

		// Adamic's soundness rules (#drbrp8c), built ahead of the set that carries them. cohere:adamic and
		// the re-composed cohere:typescript land through @system_cohere_lint_sets under the set-change
		// gate; until then nothing enables these. Each entry leaves when cohere:adamic does.
		"adamic/invariant-mutable":      "built ahead of cohere:adamic, which lands through @system_cohere_lint_sets (#drbrp8c)",
		"adamic/no-definite-assignment": "built ahead of cohere:adamic, which lands through @system_cohere_lint_sets (#drbrp8c)",
		"adamic/no-optional-widening":   "built ahead of cohere:adamic, which lands through @system_cohere_lint_sets (#drbrp8c)",
		"adamic/no-type-predicate":      "built ahead of cohere:adamic, which lands through @system_cohere_lint_sets (#drbrp8c)",
		"adamic/no-unchecked-cast":      "built ahead of cohere:adamic, which lands through @system_cohere_lint_sets (#drbrp8c)",
		"adamic/nominal-class":          "built ahead of cohere:adamic, which lands through @system_cohere_lint_sets (#drbrp8c)",
		"adamic/single-spread":          "built ahead of cohere:adamic, which lands through @system_cohere_lint_sets (#drbrp8c)",

		// Kirk decided every suppression must carry a stated reason, and asked for the port to land
		// registered rather than enabled. Dry-run on the ahra tree with the rule switched on through
		// a scratch copy of CohereSettings.json (`--no-fix --lint`): 337 findings over 166 files,
		// 321 of them `// eslint-disable-next-line` with no ` -- reason`. The same files run through
		// the cloned upstream rule on the installed ESLint 10.8.1 report 335; the 2 extra are prose
		// line comments starting `// eslint-disable`, which cohere honors as directives and ESLint
		// does not. ESLint itself cannot run the rule today: the plugin is not installed in ahra.
		"@eslint-community/eslint-comments/require-description": "ported and registered; 337 undescribed directives over 166 files to give reasons first, and the ESLint plugin is not installed, so enabling is Kirk's call",

		// Both ported and registered without being enabled, because neither rule can be enabled by
		// a porting step: each does nothing at all until somebody writes the list of what this
		// project bans. Upstream's own default for both is an empty configuration, and the audits
		// recorded zero violations here for exactly that reason rather than because the tree is
		// clean -- an unconfigured options-driven rule and a clean tree are indistinguishable in a
		// violation count, which is the shape this repository keeps finding.
		//
		// Measured through the installed ESLint 10.8.1: `export var a;` with no options and
		// `import fs from 'fs';` with no options both report nothing. Neither name appears anywhere
		// in CohereSettings.json under any spelling, so there is no standing decision to honour or
		// reverse; there is simply nothing to enable yet.
		"no-restricted-exports": "ported and registered; enforces nothing until a project names which export names it bans, which is a decision about this codebase rather than a wiring step",
		"no-restricted-imports": "ported and registered; enforces nothing until a project names which modules it bans, which is a decision about this codebase rather than a wiring step",

		// Three stylistic typescript-eslint rules ported and registered without being enabled,
		// because each carries real cleanup and the audits recommend against paying it right now.
		// Unlike the two above, these are NOT inert: each was dry-run against the whole repository
		// through the config layer and differentialled against the installed 8.67.0 rule over the
		// same files, agreeing position for position.
		//
		//	@typescript-eslint/consistent-type-definitions   872 findings, 42 files
		//	@typescript-eslint/no-inferrable-types           204 findings, 89 files
		//	@typescript-eslint/prefer-regexp-exec            179 findings, 87 files
		//
		// All three are auto-fixable and all three fixers are pinned by upstream's own `output`
		// cases, so enabling any of them is one config line plus a fix pass plus a review of the
		// diff. None of their names appears in CohereSettings.json under any spelling, so there is
		// no standing decision being honoured or reversed here.
		"@typescript-eslint/consistent-type-definitions": "ported and registered; 872 findings to clean up first, which is a decision about this codebase",
		"@typescript-eslint/no-inferrable-types":         "ported and registered; 204 findings to clean up first, which is a decision about this codebase",
		"@typescript-eslint/prefer-regexp-exec":          "ported and registered; 179 findings to clean up first, which is a decision about this codebase",

		// The same class as the two above, and for the same measured reason. Neither rule can
		// report anything until somebody writes the configuration that says what to enforce:
		// id-denylist has no denylist until one is named, and id-match's default pattern is
		// upstream's `^.+$`, which every non-empty name matches. Both audits recorded zero
		// violations here, and for both that zero is an artifact of being unconfigured rather than
		// evidence about the tree -- the shape this repository keeps finding.
		//
		// Measured through the installed ESLint 10.8.1 under sourceType module with the
		// typescript-eslint parser: `var foo = 1;` reports nothing under either rule with no
		// options. Neither name appears anywhere in CohereSettings.json under any spelling, so
		// there is no standing decision to honour or reverse; there is simply nothing to enable
		// until Kirk chooses a denylist or a pattern.
		"id-denylist": "ported and registered; denies nothing until a project names which identifiers it bans, which is a decision about this codebase rather than a wiring step",
		"id-match":    "ported and registered; matches everything until a project names a naming pattern, which is a decision about this codebase rather than a wiring step",

		// Both audited **No** -- a judgment about ENABLING, on cleanup cost rather than on
		// correctness, and it stands. The counts are why: id-length measured 862 violations on the
		// ahra tree and max-depth 176, neither rule ships a fixer, so every one is a hand edit or a
		// refactor. Porting them is still worth doing, because a No that was never ported cannot be
		// revisited without redoing the work.
		//
		// Unlike the four rules above, these are NOT inert when unconfigured: id-length enforces a
		// minimum of 2 by default and max-depth a depth of 4, so enabling either would start
		// reporting immediately. That is exactly why enabling is somebody's decision rather than a
		// wiring step, and why they are listed here rather than switched on.
		"id-length": "ported and registered; audited No at 862 violations with no fixer, so enabling is a decision for whoever takes that cleanup",
		"max-depth": "ported and registered; audited No at 176 violations with no fixer, so enabling is a decision for whoever takes that cleanup",

		// Audited **Strong No**, and the measurement is why rather than a matter of taste. Re-run
		// on the current tree with the rule enabled through the real config layer, one-var's
		// default of "always" reports 25,882 findings across 2,029 of 3,540 project files -- 57%
		// of the codebase, every one of them `combine`. (The audit in core/one_var.md recorded
		// 24,698 when it ran; the tree has grown since and the two agree in magnitude.)
		//
		// A count that size and that uniform is a convention clash, not a defect signal: this
		// codebase declares one binding per statement deliberately, and one-var's default asks for
		// the opposite. Enabling it would rewrite more than half the tree to a style nobody chose.
		//
		// Not inert when unconfigured, which is what separates this from the four rules above:
		// upstream's `defaultOptions: ["always"]` means a bare "error" starts reporting
		// immediately. The name appears nowhere in CohereSettings.json under any spelling, so
		// there is no standing decision being honoured or reversed here -- there is a new one to
		// be made, and it is Kirk's rather than a porting step's.
		//
		// The port exists so that decision can be revisited without redoing the work, and so the
		// opposite setting stays available: `{"const": "never"}` would enforce the convention the
		// tree already follows, and on the same measurement it reports far less. That is a
		// different config line rather than a different rule.
		"one-var": "ported and registered; audited Strong No and re-measured at 25,882 findings over 2,029 files under its default, which is a convention clash rather than a defect signal, so enabling is Kirk's decision",

		// Audited **No** at 326 violations, and there is a second argument against enabling beyond
		// the cleanup cost: the Tricorder paper the standard cites names cyclomatic complexity as
		// failing the bar for a useful diagnostic, because the number correlates poorly with what
		// actually makes code hard to read. Both arguments are about ENABLING rather than about
		// porting, and the port exists so the decision can be revisited without redoing the work.
		//
		// Not inert when unconfigured: the default threshold is 20, so enabling would start
		// reporting immediately.
		"complexity": "ported and registered; audited No at 326 violations with no fixer, and the metric itself is contested, so enabling is a decision rather than a wiring step",

		// Audited **No** at 2 violations, which is a judgment about ENABLING and stands. The audit's
		// stated reason does not: it says the rule "overlaps a rule we already enforce in-house, so
		// it would report the same defect under a second name", and no such rule exists. Checked --
		// the only sort-named rules the binary registers are
		// `@typescript-eslint/require-array-sort-compare`, `react/sort-comp` and
		// `react/sort-default-props`, none of which looks at a variable declaration block.
		//
		// So the honest reason to leave it off is the one the count gives rather than the one the
		// audit wrote: two sites, both in libraries/structure, against a convention nobody has
		// agreed to adopt. Unlike the rules above it this one DOES ship a fixer, so enabling it is
		// cheaper than most -- which is a reason to revisit it deliberately rather than to flip it
		// as part of a port.
		"sort-vars": "ported and registered; audited No at 2 violations, and its stated overlap reason does not hold, so enabling is a decision somebody should make on the real reason",

		// Audited **Strong No** at 5619 violations, measured at 6000 on the current tree. That is a
		// judgment about ENABLING and it plainly stands: the rule ships a fixer, but six thousand
		// automated rewrites of function expressions into arrows is a change to how the codebase
		// reads rather than a cleanup, and several of them would be semantic -- the fixer
		// deliberately declines fourteen shapes in upstream's own corpus precisely because the
		// repair would alter `this`.
		//
		// One correction to the audit's metadata while it is being cited: it records "needs type
		// information: no", which is true of ESLint and false of a cohere port. Telling a genuine
		// self-reference from a shadowed one is name resolution, and a text comparison gets it
		// wrong -- `foo(function bar() { function bar() {} bar(); })` reports upstream and a text
		// match calls it clean. Upstream reads eslint-scope for this, which is not a type checker;
		// here the equivalent is the checker, so the rule declares NeedsTypeChecker.
		"prefer-arrow-callback": "ported and registered; audited Strong No at 5619 violations, measured 6000, so enabling is a codebase-wide decision rather than a wiring step",

		// Audited **No** at 1736 violations, which is a judgment about ENABLING and stands. What
		// makes it a decision rather than a wiring step is the same thing that made it worth
		// porting carefully: the rule has SIX mutually exclusive modes, and which one this codebase
		// wants is a style choice nobody has made. The default, "always", is not a safe pick by
		// omission either -- it is the strictest of the six, requiring shorthand for methods and
		// properties at once.
		"object-shorthand": "ported and registered; audited No at 1736 violations, and its six mutually exclusive modes mean enabling is picking a style rather than flipping a switch",

		// Ported and registered without being enabled, because enabling it is a decision with work
		// attached rather than a wiring step. The audit measured 54 violations and the rule has no
		// fixer, so every one is a hand edit; the config has never named it under either spelling.
		"@typescript-eslint/no-deprecated": "ported and registered; the audit measured 54 violations and the rule has no fixer, so enabling is a decision for whoever takes that cleanup",

		// Ported and registered without being enabled, and the reason is a decision rather than a
		// wiring gap. The bare `consistent-return` is already enabled at CohereSettings.json:506,
		// and the two are NOT interchangeable: the extension is the core rule plus two type-driven
		// filters, measured at 13 divergences over upstream's own 30-case corpus, every one of them
		// the extension going silent where the core reports.
		//
		// So enabling this is not "turn on a ported rule". It is choosing between two rules that
		// enforce different things on the same tree, and doing it by adding a key would leave the
		// bare one enabled as well, reporting the 13 the extension exists to suppress. The audit
		// measured 121 violations for the extension and it has no fixer, so every site is a hand
		// edit either way.
		//
		// The namespaced name does not resolve to the existing short key, which was checked rather
		// than assumed: `settingFor` trims the CONFIGURED name by the RULE name, and a short key is
		// not a suffix of a longer rule name, so the trim is a no-op. Confirmed by driving the
		// built binary from the ahra tree -- 387 rules in `--rules-enabled`, `consistent-return`
		// present, `@typescript-eslint/consistent-return` absent.
		"@typescript-eslint/consistent-return": "ported and registered; the bare consistent-return is already enabled and the two enforce different things, so which one this tree wants is a decision rather than a wiring step",

		// Four stylistic rules ported together, registered and left unenabled because each is a
		// convention this codebase has not adopted and adopting one is a cleanup with a measured
		// price rather than a wiring step. The audits recommend "No" for all four, and their counts
		// are what that recommendation rests on: no-underscore-dangle 100, prefer-destructuring 789,
		// no-negated-condition 504, no-bitwise 82. Only prefer-destructuring has a fixer, so three of
		// the four are entirely hand edits.
		//
		// None of the four names appears anywhere in CohereSettings.json under any spelling, so
		// there is no standing decision to honour or reverse; there is simply nothing to enable
		// until Kirk chooses the convention. Unlike the options-driven rules above, all four report
		// out of the box, so their zero here is the config not naming them rather than a rule that
		// cannot fire.
		"no-underscore-dangle": "ported and registered; the audit measured 100 violations with no fixer and recommends against, so adopting the convention is a decision rather than a wiring step",
		"no-negated-condition": "ported and registered; the audit measured 504 violations with no fixer and recommends against, so adopting the convention is a decision rather than a wiring step",
		"no-bitwise":           "ported and registered; the audit measured 82 violations with no fixer and recommends against, and this tree uses bitwise operators deliberately in hashing and bit-packing code",
		"prefer-destructuring": "ported and registered; the audit measured 789 violations and recommends against, so adopting the convention is a decision rather than a wiring step even though the rule has a fixer",

		// Ported and registered without being enabled, and the reason is a decision with a measured
		// price rather than a wiring step. The audit recommends against at 774 violations across all
		// three of upstream's arms; this port implements the `||` and `||=` arm only, and measured
		// on the real tree that arm alone is 677 findings. The rule proposes suggestions rather than
		// fixes, so nothing applies unattended and every one of the 677 is a hand edit.
		//
		// The name appears nowhere in CohereSettings.json under any spelling, so there is no
		// standing decision to honour or reverse. Unlike the options-driven rules above it reports
		// out of the box, so its absence from the enabled set is the config not naming it rather
		// than a rule that cannot fire.
		"@typescript-eslint/prefer-nullish-coalescing": "ported and registered; the || arm alone measures 677 findings on this tree with suggestions rather than fixes, so adopting it is a decision about a hand cleanup rather than a wiring step",

		// The live config sets `react/jsx-key` to "off" explicitly, in a block of ten-plus rules
		// this project has deliberately turned off alongside `react/react-in-jsx-scope`. That is a
		// decision about this codebase rather than a wiring gap, and flipping it here would
		// override it silently, so the rule is ported, registered and inventoried while staying
		// off. Turning it on is a config change for whoever owns that block to make.
		"react/jsx-key": "the live config turns react/jsx-key off deliberately, beside react-in-jsx-scope",

		// The live config already turns this rule off, at the config key `no-useless-rename`, in the same
		// block as react/jsx-key and react/react-in-jsx-scope. That decision was recorded BEFORE
		// the rule was ported, which is how this migration is meant to work, and running
		// EnableRule.ts would have reversed it through the porting process rather than because
		// anybody changed their mind.
		//
		// Unlike the typescript/ case below, no spelling difference is involved: the key is the
		// bare name and it resolves against the registered rule exactly. So the off applies, the
		// rule is offered no files, and this exemption is what stops that reading as a wiring gap.
		//
		// The port is complete and proven either way. The audit measured 3 violations, all
		// auto-fixable, so turning it on later is one config line plus a fix pass.
		"no-useless-rename": "the live config turns no-useless-rename off deliberately, beside react/jsx-key",

		// The live config already turns this rule off, at the config key `typescript/require-array-sort-compare`, under the
		// old short spelling. Somebody decided against it, and the
		// port does not get to reverse that.
		//
		// What makes this worth spelling out is that enabling it would have LOOKED like a normal
		// port rather than like an override. The registered name here is the full
		// `@typescript-eslint/` spelling, and `settingFor` resolves an exact match first and then a
		// suffix trim on a `/` boundary, so the existing `typescript/` key does not resolve against
		// it. Measured directly against `settingFor`: the bare and `typescript/` spellings both
		// resolve to that "off" and the `@typescript-eslint/` one does not. Adding an "error" line
		// would therefore have silently won over a standing decision through a spelling difference,
		// with nothing in any diff to show that is what happened.
		//
		// So the rule is ported, registered and tested while staying off, the same shape as
		// `react/jsx-key` above. Turning it on is a config change for whoever owns that "off" to
		// make, and the audit puts the cost at four sites, three of them `results.sort()` over small
		// number arrays inside test assertions where the default sort is harmless.
		"@typescript-eslint/require-array-sort-compare": "the live config turns it off deliberately at the config key `typescript/require-array-sort-compare`, under the typescript/ spelling",

		// The same situation as the entry above, one line earlier in the same block: the live
		// config turns this off at the config key `typescript/require-array-sort-compare` under the `typescript/` spelling, which
		// does not resolve against the `@typescript-eslint/` name registered here. Enabling it was
		// attempted and reverted rather than kept, because the two rules sit in the same
		// hand-maintained list of deliberate disables and treating them differently would be
		// arbitrary.
		//
		// The audit measures eleven sites, and unlike its neighbour this rule IS auto-fixable, so
		// the cleanup is a command plus a review of the diff rather than eleven judgments. That
		// makes it the cheaper of the two to turn on, and it is still not a porter's call.
		"@typescript-eslint/no-meaningless-void-operator": "the live config turns it off deliberately at the config key `typescript/no-meaningless-void-operator`, under the typescript/ spelling",

		// Left off for a reason that is not a config decision at all: the rule cannot see its own
		// subject in this architecture, so enabling it would wire up a rule that is guaranteed to
		// report nothing on every file forever.
		//
		// The rule judges whether a file begins with a byte order mark. Measured against
		// `osvfs.FS().ReadFile`, which is the read every source file in a real run goes through:
		// a file whose first three bytes on disk are `ef bb bf` arrives as text beginning with
		// `65 78 70`, the mark already removed, while an unmarked control of the same length is
		// unchanged and a mark written in the MIDDLE of a file survives intact. So the stripping is
		// specific to position zero, which is the one position this rule asks about, and it happens
		// below every rule rather than in any of them. `cachedvfs` over the same reads gives the
		// same answer, so it is not the cache.
		//
		// A dry run against the ahra tree agrees: the rule is offered all 3,513 files, registers a
		// listener on all 3,513, and reports zero. A seeded two-file tree holding one genuinely
		// marked file reports zero as well, and that zero is what separates this from the audit's
		// predicted zero. The audit rated it Yes on the strength of a clean tree; the tree is clean
		// AND the rule could not tell if it were not.
		//
		// The rule is ported, tested and registered anyway rather than abandoned, because the port
		// itself is correct against upstream and the missing piece is one line elsewhere: a lint
		// phase that read the file's real bytes, or a source-file flag carrying whether a mark was
		// stripped, makes it work as written. Worth knowing while that is decided: the FIX phase
		// reads through `os.ReadFile` and keeps the mark, while the lint phase reads through
		// `osvfs` and does not, so the two phases disagree by three bytes about where everything in
		// a marked file lives.
		"unicode-bom": "the leading byte order mark is stripped by osvfs before any rule runs, so the rule cannot see its own subject; measured against osvfs.FS().ReadFile with an unmarked control and a mid-file control",

		// Registered but deliberately not enabled, and the reason is volume rather than
		// correctness. Measured against the ahra tree with the installed @typescript-eslint 8.67.0
		// build: 2,846 findings across 672 files, with 324 of them in one file. The rule ships no
		// fixer, and every site is a judgment about what the code actually guarantees rather than a
		// mechanical rewrite, so adopting it is a project with an owner rather than a config line a
		// porter adds.
		//
		// The port itself is complete and agrees with upstream on all twenty two of its corpus
		// cases plus eight more measured shapes. Nothing here needs fixing before it can be turned
		// on; somebody has to decide to spend the 2,846 decisions.
		"@typescript-eslint/no-unsafe-type-assertion": "registered but left off pending a decision about volume: measured at 2,846 findings across 672 files on the ahra tree with the installed 8.67.0 build, no fixer, every site a human judgment",

		// Registered but deliberately not enabled, and the reason is that turning it on is a
		// stylistic decision with edits attached rather than a correctness one. Upstream marks it
		// `recommended: false` and neither gate enforced it, so nothing is being reversed here and
		// no prior `off` exists under either spelling; the decision has simply not been made.
		//
		// Measured with the rule temporarily enabled and the config restored afterward: 11 findings
		// across 10 files, from 173,533 registrations over 3,516 files, 12.3ms, zero crashed files.
		// Every finding was read at its source and every one is a real unnecessary brace, mostly
		// `className={'...'}` where a plain string attribute says the same thing. Unlike the volume
		// case above, this one DOES ship a fixer and all eleven repairs were verified byte for byte
		// against the installed 7.37.5 build, so adopting it is one fix run rather than eleven
		// judgments. It is left off because eleven files changing spelling is still somebody's call
		// about house style, not because anything about the port is unfinished.
		"react/jsx-curly-brace-presence": "registered but left off pending a decision about house style: 11 findings across 10 files on the ahra tree, every one verified against the installed 7.37.5 build at the same position and with the same repair text, and all of them mechanically fixable",

		// Registered but deliberately not enabled, because the config already carries a standing
		// decision against this rule and that decision cannot be seen by the resolver.
		// the config key `typescript/restrict-template-expressions` reads `"typescript/restrict-template-expressions": "off"`, written
		// under the old short spelling. `settingFor` matches a key exactly, then trims the RULE NAME
		// off the CONFIG KEY and requires what remains to end in a slash; the old key is SHORTER
		// than the full name, so the trim is a no-op and the branch never fires. Measured rather
		// than read: `"typescript/restrict-template-expressions".endswith("@typescript-eslint/restrict-template-expressions")`
		// is false.
		//
		// So enabling this would reverse somebody's decision through a spelling difference, with
		// nothing in the diff to show a decision was reversed. The port is complete and its
		// eighty six imported cases pass; turning it on is a decision for whoever wrote that line.
		"@typescript-eslint/restrict-template-expressions": "registered but left off because the config key `typescript/restrict-template-expressions` carries a prior off under the old short spelling, which the resolver cannot match against the full name; enabling would reverse a standing decision invisibly",

		// Same shape as the entry above, for the same reason and at a different line.
		// the config key `typescript/no-useless-default-assignment` reads `"typescript/no-useless-default-assignment": "off"`, written
		// under the old short spelling. The config key is 40 characters and the registered name is 48,
		// so the key is SHORTER than the name, the trim is a no-op, and the branch never fires.
		// Measured with two controls that do resolve, rather than read off the brief.
		"@typescript-eslint/no-useless-default-assignment": "registered but left off because the config key `typescript/no-useless-default-assignment` carries a prior off under the old short spelling, which the resolver cannot match against the full name; enabling would reverse a standing decision invisibly",

		// The same shape as the entry above, and the same reason. the config key `typescript/no-useless-default-assignment` reads
		// `"typescript/no-duplicate-type-constituents": "off"`, written under the old short
		// spelling, and the resolver cannot match a key that is shorter than the registered name.
		// Confirmed by the linter itself rather than by argument: a `--lint` run prints
		// `config: key "typescript/no-duplicate-type-constituents" matches no registered rule, so
		// its off never applies`, which is the instrument that now names all thirteen orphans.
		//
		// The port is complete and agrees with upstream on all eighty two of its corpus cases plus
		// fifteen more TypeScript shapes measured against the installed build. The audit puts the
		// cleanup at three sites and the rule is auto-fixable, so turning it on is cheap; it is
		// still a decision for whoever wrote that off rather than for a porter.
		"@typescript-eslint/no-duplicate-type-constituents": "registered but left off because the config key `typescript/no-duplicate-type-constituents` carries a prior off under the old short spelling, which the resolver cannot match against the full name; enabling would reverse a standing decision invisibly",

		// The third of this shape, and the reason is the same one. the config key `typescript/no-duplicate-type-constituents` reads
		// `"typescript/unbound-method": "off"`, written under the old short spelling. The key is 25
		// characters and the registered name is 34, so the key is SHORTER than the name, the trim is
		// a no-op, and the resolver's slash-boundary branch never fires.
		//
		// Confirmed by the linter rather than by argument. A `--lint` run prints all three of:
		//
		//	rule @typescript-eslint/unbound-method was offered no files
		//	rule @typescript-eslint/unbound-method is not in the config, so it ran on no files
		//	key "typescript/unbound-method" matches no registered rule, so its off never applies
		//
		// The port is complete and agrees with upstream on two hundred and ten of its two hundred
		// and eleven corpus cases, plus seventeen further shapes measured against the installed
		// build. The one disagreement is a union whose constituents reach different arms of the
		// danger test, where the two type checkers normalize the constituent order differently; it
		// reports the same node with the same span and the other message, and it is recorded in its
		// own test rather than smoothed over.
		//
		// The audit puts the cleanup at eighteen sites and the rule is not auto-fixable, so turning
		// it on is a real decision and it belongs to whoever wrote that off rather than to a porter.
		"@typescript-eslint/unbound-method": "registered but left off because the config key `typescript/unbound-method` carries a prior off under the old short spelling, which the resolver cannot match against the full name; enabling would reverse a standing decision invisibly",

		// The fourth of this shape. the config key `typescript/unbound-method` reads
		// `"typescript/no-base-to-string": "off"`, again under the old short spelling, and again the
		// key is shorter than the registered name so the resolver's slash-boundary branch cannot
		// fire. Confirmed by the linter, which prints that the key matches no registered rule.
		//
		// This one was dispatched as an ordinary enable, on the understanding that only its sibling
		// carried a prior decision. It carries one too, and it is the first entry on the brief's own
		// list of eight stranded `typescript/` keys, so it gets the same treatment rather than a
		// different one for having been described differently.
		//
		// The port agrees with upstream on all three hundred and seventeen of its corpus cases,
		// including the rendered message text with its interpolated name and three-valued certainty.
		// The audit measured fifty-three violations and notes that several are deliberate String()
		// fallbacks in generic serializers that already branch on typeof, so enabling is a judgment
		// about those sites rather than a cleanup, and it belongs to whoever wrote the off.
		"@typescript-eslint/no-base-to-string": "registered but left off because the config key `typescript/no-base-to-string` carries a prior off under the old short spelling, which the resolver cannot match against the full name; enabling would reverse a standing decision invisibly",

		// A THIRD shape: not turned off, and not unmentioned for want of a config layer, but held
		// back because enabling it is a codebase-wide convention decision rather than a cleanup.
		//
		// This rule requires an options object and is INERT without one. Upstream reads its pattern
		// from `context.options[0]`, and ESLint fills a schema default only into an options object
		// that is present, so a bare `"error"` makes every listener early-return. Measured on the
		// installed build with one violating input three ways: no options reports zero, `{}` reports
		// one, an explicit pattern reports one. `EnableRule.ts` writes a bare `"error"`, which is
		// exactly the inert state, so enabling it through the usual path would have registered a
		// rule that lints nothing while looking enforced.
		//
		// Registered with RequiresOptions and a decoder that refuses empty input, so that state now
		// fails loudly with the pattern to write rather than passing silently.
		//
		// Left unenabled because the pattern it would enforce is a naming convention for the whole
		// tree. Measured with the documented default `^(is|has)[A-Z]([A-Za-z0-9]?)+`: 313 findings
		// across 168 files, spot-checked against the installed build on three files which agreed
		// exactly. They are correct rather than false: `open`, `modal`, `openOnPress` and
		// `sessionIdHttpOnlyCookieExists` are all real boolean props that do not start with is or
		// has. Adopting that convention, choosing a different pattern, or declining is Kirk's call,
		// and the config line is where he would say so.
		"react/boolean-prop-naming": "registered but not enabled because it requires an options object and is inert without one, and because its documented default pattern reports 313 findings across 168 files, which is a naming convention for the whole tree rather than a cleanup a porter should choose",

		// A DIFFERENT shape from every entry above it, and the first of its kind in this map.
		//
		// The entries above are rules the live config deliberately turned off, where the exemption
		// records a standing decision. This one is not off; it is unmentioned, because ahra's config
		// has no `base/` keys at all. `base` is api-phi-health's own lint layer rather than ahra's,
		// so there is no line anybody wrote for it and nothing to reverse.
		//
		// The rule is ported and registered so it exists to be turned on; where it gets enabled is a
		// question about which trees run base's rules, which is Kirk's to answer rather than a
		// porter's. Recorded here so that "unmentioned" reads as a pending decision rather than as a
		// port somebody forgot to wire.
		"base/correctness-require-matching-inject-type": "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		"base/consistency-no-hand-built-declared-error": "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		"base/boundary-no-global-container":             "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",

		// Same shape and same reason as the entry above: ahra's config names no base/ rules, so there
		// is nothing to reverse and nothing anybody has decided yet. The source repository runs this
		// one at `warn` rather than `error`, which is a severity question for whoever enables it here
		// and is recorded so the answer is not silently `error` by default.
		"base/correctness-require-optional-relation": "registered but not enabled because ahra's config names no base/ rules at all; the source repository runs it at warn rather than error, which is part of the same pending decision",

		// Same shape and same reason as its two siblings above. The source repository runs this one
		// at error, unlike relation-must-be-optional's warn, which is worth knowing when the pending
		// decision is finally made rather than discovering it after enabling.
		"base/correctness-require-orm-column-declare":           "registered but not enabled because ahra's config names no base/ rules at all; the source repository runs it at error, which is part of the same pending decision",
		"base/consistency-no-bare-throw":                        "registered but not enabled because ahra's config names no base/ rules at all; the source repository enforces it, with disable comments carrying reasons at the few places it is waived, so its silence here is the pending decision rather than a judgment about the tree",
		"base/security-require-context-access":                  "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make. This rule additionally ships with NO default requirements, so enabling it without supplying the protected context keys would look enabled while enforcing nothing",
		"base/consistency-no-console":                           "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		"boundaries/dependencies":                               "registered for Base projects, whose config enables it in two path-scoped blocks with their own elements (api-phi-health: 0 findings, matching ESLint); ahra's ESLint config loads no boundaries plugin and draws no layers, so there is nothing here for it to enforce until somebody draws them",
		"base/correctness-require-orm-column-nullable-parity":   "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		"base/correctness-require-serializable-nullable-parity": "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		"base/correctness-require-matching-operation-context":   "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		"base/correctness-require-graphql-nullable-parity":      "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		"base/consistency-require-pagination-argument-name":     "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		"base/correctness-require-matching-provider-return":     "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		"base/correctness-require-verify-optional-parity":       "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		"base/correctness-require-verify-array-parity":          "registered but not enabled because ahra's config names no base/ rules at all; base is api-phi-health's own lint layer, so which trees enforce it is a decision nobody has made yet rather than one this port should make",
		// The two react entries below are a THIRD shape, distinct from both groups above.
		//
		// They are not rules the config turned off, and they are not rules from a namespace ahra
		// does not name: ahra enables 58 other `react/` rules. These two were ported deliberately
		// unenabled because the task that ported them said to register without enabling, and because
		// each is a guardrail against drift rather than a cleanup. Both were measured on the real
		// tree before this entry was written, rather than inheriting the audit's number.
		//
		// Measured 2026-09-06 with both enabled in a throwaway copy of the config, `--no-fix --lint`
		// over 3,540 files: ZERO findings each, zero crashed files. That zero is a real one rather
		// than an inert rule, established three ways. Neither appears in the run's "was offered no
		// files" list, both appear among the 300 rules that "watched files and reported nothing",
		// and six other `react/` rules reported 220 findings in the same run, so the instrument that
		// found nothing here demonstrably works. A seeded two-file tree reports one finding from
		// each and stays silent on its clean sibling.
		//
		// So enabling either is a decision about whether to hold the tree to a convention it already
		// satisfies, which is Kirk's call and costs nothing today. The config line is where he would
		// say so.
		"react/sort-comp":                 "registered but not enabled: ported as a guardrail rather than a cleanup, and measured at zero findings over 3,540 files with the rule demonstrably live (not in the offered-no-files list, and a seeded probe reports). Enabling it is a decision about adopting a member-ordering convention the tree already satisfies",
		"react/no-invalid-html-attribute": "registered but not enabled: ported as a guardrail rather than a cleanup, and measured at zero findings over 3,540 files with the rule demonstrably live (not in the offered-no-files list, and a seeded probe reports). Enabling it costs nothing today and would catch a future invalid rel value",
		// Four ESLint core rules ported in one batch, all deliberately unenabled.
		//
		// A DIFFERENT shape from the react pair above, and the difference is the point: those two
		// measured zero on this tree, so enabling them would cost nothing. These four measure
		// 121, 62, 178 and 217 findings respectively over 3,540 files, so enabling any of them is a
		// cleanup somebody has to schedule rather than a guardrail somebody can flip on.
		//
		// Each was audited "No", and that judgment was about ENABLING rather than about the port.
		// The refusals stand; the rules are here so the decision is a config line rather than a
		// missing implementation.
		//
		// The counts were re-measured on 2026-09-06 rather than inherited from the audit, and one
		// had moved: class-methods-use-this was audited at 194 and now reports 217. The tree grew.
		// All four were then differentially compared against ESLint driving the same rule over the
		// same files, and class-methods-use-this agrees on all 1,163 files it touches with zero
		// disagreements -- a comparison that found two real defects here first, both TypeScript
		// shapes upstream's JavaScript corpus cannot express.
		"dot-notation":           "ported and registered, not enabled: 121 findings over 3,540 files, so enabling it is a scheduled cleanup rather than a guardrail. Audited No, and that refusal was about enabling rather than about the port",
		"default-case":           "ported and registered, not enabled: 62 findings over 3,540 files, so enabling it is a scheduled cleanup rather than a guardrail. Audited No, and that refusal was about enabling rather than about the port",
		"func-name-matching":     "ported and registered, not enabled: 178 findings over 3,540 files, so enabling it is a scheduled cleanup rather than a guardrail. Audited No, and that refusal was about enabling rather than about the port",
		"class-methods-use-this": "ported and registered, not enabled: 217 findings over 3,540 files (the audit said 194; the tree grew, and the new count agrees with ESLint on all 1,163 files it touches). Audited No, and that refusal was about enabling rather than about the port",
		// Three stylistic core rules, registered and left unenabled. Same shape as the four above:
		// each has a real cost on this tree, so enabling one is a scheduled cleanup rather than a
		// guardrail somebody can flip on.
		//
		// Counts measured 2026-09-06 over 3,540 files, and each was differentially compared against
		// ESLint driving the same rule over the same files. All three agree exactly:
		//
		//	no-plusplus           616 findings, 221 files, eslint agrees to the finding
		//	no-continue         1,191 findings, 330 files, eslint agrees to the finding
		//	no-inline-comments  3,979 findings, 1,304 files, eslint agrees on every dense file
		//
		// `no-inline-comments` is the largest and the count is load-bearing on two shelf fixes
		// landed alongside it: the comment scanner could not see a comment alone inside a JSX
		// expression, nor one after the last element of a comma-terminated list. Before those, the
		// rule was silent on 31 of its own 49 corpus cases and under-reported here by 73. Both fixes
		// are committed separately and the full-tree control shows no other rule's count moved.
		"no-plusplus":        "ported and registered, not enabled: 616 findings over 3,540 files. Enabling it is a scheduled cleanup rather than a guardrail, and `++` in a for-loop update is idiomatic enough that the allowForLoopAfterthoughts option is probably the real question",
		"no-continue":        "ported and registered, not enabled: 1,191 findings over 3,540 files, which is a control-flow convention for the whole tree rather than a defect class",
		"no-inline-comments": "ported and registered, not enabled: 3,979 findings over 3,540 files, the largest count in this batch. A trailing-comment convention is a formatting decision, and at this volume it is Kirk's call rather than a porter's",
		// The one rule in these batches with a repair, and the count is not the interesting number.
		//
		// 746 findings over 294 files, agreeing with ESLint exactly. What makes it worth a note is
		// that its fixer is string surgery -- escaping `${` and backticks by backslash parity,
		// unescaping the original quote, carrying comments into curlies, emitting a leading `;`
		// against automatic semicolon insertion -- and every one of those was wrong in the first
		// draft. The 74 `output` fixtures caught all of it; the message ids caught none of it.
		//
		// Left unenabled because 746 hand edits is a scheduled cleanup. The fixer means it could be
		// a mechanical one, which is a genuinely different decision from the other six in this
		// batch and is Kirk's to make.
		"prefer-template": "ported and registered, not enabled: 746 findings over 294 files, agreeing with ESLint exactly. It ships a working fixer, so adopting it could be mechanical rather than manual, which makes enabling it a different decision from the unfixable stylistic rules above",
		// The first type-aware rule in these batches, and the first whose findings are defects
		// rather than style.
		//
		// 508 findings over 56 files, verified differentially: a real type-aware ESLint run over the
		// 12 densest files gives 303 against our 303. Every finding is one message id,
		// `mismatchedCondition`, and the shape is overwhelmingly one thing -- comparing
		// typescript-eslint's own `AST_NODE_TYPES` enum against a bare string, as in
		// `node.callee.type === 'Identifier'`. That compiles today and silently stops matching if the
		// enum's value ever changes, which is exactly what the rule exists to catch.
		//
		// Not enabled, because 508 is a scheduled cleanup and because the concentration matters: 108
		// of them are in one file. Whether to adopt the convention, and whether to do it file by
		// file, is Kirk's call. This is the strongest candidate in these batches for actually being
		// turned on.
		"@typescript-eslint/no-unsafe-enum-comparison": "ported and registered, not enabled: 508 findings over 56 files, agreeing with a type-aware ESLint run exactly on the densest 12. Unlike the stylistic rules above these are real defects -- enum members compared against bare string literals -- so enabling it is a cleanup worth scheduling rather than a convention question",
	}

	rules := All()
	if len(rules) == 0 {
		t.Fatal("the registry is empty, so this test proves nothing")
	}

	var unreachable []string
	for _, subject := range rules {
		if _, excused := deliberatelyNotEnabled[subject.Name]; excused {
			continue
		}
		// `StatusOf` rather than `Enabled`, because Enabled collapses two different worlds into
		// one false. A rule the config turns off is a decision somebody made and recorded; a rule
		// the config never mentions is a wiring gap. Twelve rules landed deliberately unenabled
		// tonight, each honouring a standing `off`, and this guard was reporting three of them as
		// running on nothing alongside genuine gaps. That is a false positive on correct work, and
		// a guard that cries wolf on the right answer gets ignored on the wrong one.
		status, _ := resolved.StatusOf(subject.Name)
		if status == configuration.StatusUnconfigured {
			unreachable = append(unreachable, subject.Name)
		}
	}

	// An exemption is a claim about the world, and the world moves. `import-require-path-alias` is
	// exempt because the gate's oxlint plugin has no such rule, which is what makes it the
	// differential's cohere-only control. If somebody adds it to that plugin, the exemption becomes
	// wrong silently: the guard keeps passing and the rule stays unwired for a reason that no longer
	// exists.
	//
	// So the reason gets checked rather than trusted. This is the same discipline as proving a
	// detector can fail: an allowlist nobody validates is an allowlist that outlives its premise.
	const gatePluginPath = "/Users/kirkouimet/Projects/ahra/libraries/structure/libraries/nexus/code-quality/oxlint/OxlintNexusPlugin.mjs"
	if pluginSource, err := os.ReadFile(gatePluginPath); err == nil {
		if strings.Contains(string(pluginSource), "import-require-path-alias") {
			t.Errorf(
				"the gate's oxlint plugin now defines import-require-path-alias, so exempting it here " +
					"is no longer correct: it was exempt because the gate could not name it",
			)
		}
	}

	if len(unreachable) > 0 {
		t.Errorf(
			"%d registered rules are not mentioned by the live config, so they run on nothing and "+
				"nobody has said whether they should: %v\n"+
				"a rule whose name the config cannot resolve passes its own fixtures and lints no "+
				"files. A rule the config explicitly turns off is not this: that is a decision, and "+
				"it is reported separately by the run itself as scoped off",
			len(unreachable), unreachable,
		)
	}
}
