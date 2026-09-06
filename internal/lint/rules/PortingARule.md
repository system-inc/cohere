# Porting a rule into cohere

> **If you read one thing, read this.** Ten of this document's sections are about
> INSTRUMENTS THAT FAILED SILENTLY, and almost all of them were written in a
> single night. Not one was a hard rule-porting problem. They were: a probe that
> printed to stdout, a `go test -run` pattern that matched no test and printed
> `ok`, a grep that matched nothing and read as a clean tree, a mutant that would
> not compile and read as a passing control, a lint run from the wrong directory,
> a guard that could not fail, and a comment its own author cited back as
> evidence.
>
> The pattern under all of them: **absence of a signal reported as a negative
> result.** A measurement that did not happen looks exactly like a measurement
> that found nothing wrong, and it looks that way from the inside, to a careful
> person who fully intends to check.
>
> So the discipline is not "be careful". It is: make the instrument refuse.
> Baseline-green, mutant-red, restored-green — three runs, never one. Find the
> line that only appears when real work happened, and refuse to print a result
> without it. Then break the refusal once and watch it fire.
>
> The rules are the easy part. The instruments are where the night went.

You are porting one lint rule to Go, from the implementation that DEFINED it.
This document is the standard and the order; follow it over your instincts. Every
line in it was earned by a real defect shipping.

## A note on this document's own history

This was written when the program was called `verify`, and it has been rewritten
for `cohere` and for the directory layout the repository has today. Where a
lesson was about a thing that has since been removed or replaced, the lesson is
kept and the replacement is named at the line, because the reasoning usually
outlives the mechanism.

Three things changed in ways worth knowing before you read further:

- **`rule-inventory.json` is gone.** Sections that used to tell you to add an
  entry, to compute parity against it, or to keep its counters honest are marked
  where they appear. What replaced it is named in each case.
- **The word `verify` still legitimately appears in three places** and only one
  of them is this program. See "Three families of the word `verify`" below before
  you grep or sweep for it.
- **The control-flow substrate grew.** The `no-useless-return` lesson below was
  written when a dataflow join was impossible here. It is now
  `control_flow_graph.Solve`, and the section says so.

## Three families of the word `verify`

This matters more than it looks, because a sweep already broke it once.

1. **The program.** It is `cohere` now. Binary `cohere`, launcher command `c`,
   Go module `github.com/system-inc/cohere`, config file `CohereSettings.json`.
2. **Ordinary English.** "Verify that the fixture fails." Leave it alone.
3. **Base's validation decorators.** `VerifyIsEmail`, `VerifyIsArray`,
   `VerifyBy` and about forty siblings, plus the two rules
   `base/verify-array-parity` and `base/verify-optional-parity`. These are Kam's
   public API in the Base framework and they keep that spelling forever.
   `internal/lint/rules/base/doc.go` explains why, and it is worth reading before
   you touch anything in that package.

The failure mode for family 3 is silent in the worst way: those rules key on
those literal strings, so a renamed table matches nothing, the rules report
nothing, and the tree goes green having checked less than it appears to.

## Where things are

    cohere tree      /Users/kirkouimet/Projects/system/cohere
    rules            internal/lint/rules/<namespace>/
    shared shelf     internal/lint/ecmascript/ and internal/lint/checking/
    type graph       internal/types/program/
    write layer      internal/edit/
    vendored tsc     TypeScript/ (submodule) with shims in TypeScript-shim/
    upstream source  /tmp/lint-sources          (cloned from GitHub, see below)
    installed build  ~/Projects/ahra/node_modules    (the RUNNING rule, for ground truth)
    ahra CLI         only resolves from /Users/kirkouimet/Projects/ahra, so cd there for ahra commands

The namespaces under `internal/lint/rules/` are `base`, `core`, `next`, `nexus`,
`react`, `structure`, `tailwind`, `typescript`. Each is a Go package and each is
blank-imported by `internal/lint/registry/registry.go`.

### The upstream trees, and why both of these matter

`/tmp/lint-sources` holds the repositories that define the rules being ported.
They are shallow clones of the default branch; the React one is sparse. If a tree
is missing, reclone it:

    mkdir -p /tmp/lint-sources && cd /tmp/lint-sources
    gh repo clone eslint/eslint eslint -- --depth 1
    gh repo clone jsx-eslint/eslint-plugin-react eslint-plugin-react -- --depth 1
    gh repo clone typescript-eslint/typescript-eslint typescript-eslint -- --depth 1
    git clone --depth 1 --filter=blob:none --sparse https://github.com/facebook/react.git react
    cd react && git sparse-checkout set compiler/packages/babel-plugin-react-compiler

Where your rule lives in each:

    eslint/*              /tmp/lint-sources/eslint/lib/rules/<rule>.js
                          /tmp/lint-sources/eslint/tests/lib/rules/<rule>.js
    @typescript-eslint/*  /tmp/lint-sources/typescript-eslint/packages/eslint-plugin/src/rules/<rule>.ts
                          /tmp/lint-sources/typescript-eslint/packages/eslint-plugin/tests/rules/<rule>.test.ts
    react/*               /tmp/lint-sources/eslint-plugin-react/lib/rules/<rule>.js
                          /tmp/lint-sources/eslint-plugin-react/tests/lib/rules/<rule>.js
    react-hooks/*         /tmp/lint-sources/react/compiler/packages/babel-plugin-react-compiler/src/
                          and the readable development bundle in node_modules, see "Know which kind of rule you have"

There is also a `/tmp/lint-sources/tse-fresh` checkout at 8.69.0, newer than the
main `typescript-eslint` clone. Check which one you are reading before you cite a
line number, and see "When the clone beats the installed build" for how to decide
between artifacts that disagree.

**A `react-hooks` rule name does not name its file, and the mapping is in one
place.** `src/CompilerError.ts` holds a switch from `ErrorCategory` to the rule's
public name, description and preset. Find your rule's name there, take the
`ErrorCategory` beside it, then grep that category through `src/Validation/` and
`src/Inference/` to find the pass that raises it. Measured examples:

    memoized-effect-dependencies    EffectDependencies            ValidateExhaustiveDependencies.ts
    exhaustive-effect-dependencies  EffectExhaustiveDependencies  ValidateExhaustiveDependencies.ts
    no-deriving-state-in-effects    EffectDerivationsOfState      ValidateNoDerivedComputationsInEffects.ts

Note the first two share a pass and the third has an `_exp` variant beside it.
**Check the `preset` field while you are there**: a rule marked
`LintRulePreset.Off` is off in upstream's own recommended config, which is
usually why it is not already enabled here and is worth a line in your report.

**A rule already in cohere may have been ported against oxc, and oxc is no longer
the authority.** This is not hypothetical: `react/no-string-refs` was already
here, ported from oxc, and the two authorities disagreed about **seven inputs**
with the old port wrong on all seven. Reading the sources was wrong about five of
them; only running the installed build settled it.

The residue is often a whole behaviour rather than a detail. That re-port found a
`.tsx`-only file gate that upstream does not have anywhere, which cost every
finding in every `.ts` file. **If your rule exists and its doc comment cites oxc,
treat it as unported.** Re-port it against the authority, measure every
divergence by driving the installed rule, and say in your commit that it was a
re-port and what changed.

**Check whether your rule is already in cohere before you start.** A rule can be
ported and enabled here and still appear on an audit of what ESLint enforces,
because the two configurations are separate surfaces. Twenty-six of seventy-eight
rules on one shortlist turned out to be in this state, and one porter was
dispatched to port a rule that had already shipped. One command settles it:

    <cohere binary> --rules | grep <your rule's last path segment>

If it is there, say so and stop: the task is wrong, not the rule. There may still
be real work (moving its registration to `<rule>_register.go`, or enabling it on
the ESLint side through `EnableRule.ts`), but that is a different task than
porting and should be reported as such.

**The clone is the source; node_modules is the oracle.** They are different
artifacts and you need both. The clone carries the original TypeScript or
JavaScript with comments, the option schema, the fixer, and above all the **test
corpus**, which the published packages do not ship at all. The installed build in
`node_modules` is the version this repository actually runs, and it is
executable: you can drive it with the ESLint Linter API on inputs you invent,
which is how you settle a question the source leaves ambiguous.

Where the clone and the installed build disagree, the installed build is what our
gate compares against, so measure on it and note the drift. The two documented
exceptions are below.

**Your scratch space is shared with every other agent.** One porter's mutation
script was overwritten mid-session by another agent's file and they nearly ran
somebody else's mutants against their own rule. Put anything you write into a
subdirectory named for your rule, never at the top of a shared temp path, and
name your throwaway packages for your rule too.

Work in this order. Do not skip ahead to writing the rule.

---

## 0. You are not blocked

**But if the substrate is genuinely absent, the deliverable changes from a port
to a measurement.** This section pushes hard toward proving absence with a
compiling probe and then continuing, and the instinct that creates is to build
the missing substrate anyway, which produces a half-built binder nobody can
review. When a probe with a control shows the foundation is not there, say what
is missing, what you probed with, how big the missing piece is, and stop.

A sized absence is a real result. One porter stopped on `no-redeclare` this way:
the checker returns 1 declaration for duplicate `class E {}` and duplicate
`type G` (the exact cases the rule must report) while legitimately-merging pairs
correctly return 2, `LocalSymbol()` recovers nothing, and a local `var Object`
shadows rather than merges so `builtinGlobals` has no substrate. They extracted
the 52-case corpus anyway and reported 48/52 reproducing with all 4 mismatches in
builtin globals. That is a scope-and-binding pass, new substrate rather than a
rule port, and the next person starts from measurement instead of a blank page.

There is no rule in this lane that waits for someone else. If a substrate looks
missing, prove it with a compiling probe before believing it (three capabilities
were called unavailable in one night on the evidence of a grep through a Go type
alias, and all three were reachable). If a rule's source looks absent, check the
clone before believing it. If a rule is too large for one pass, decompose it
yourself and say how you split it. If a sibling's half-written file breaks the
package, wait it out or work in a package they are not holding, and never repair
their file.

**Use your judgment and say what you decided.** A port you can defend beats a
question you asked.

**The react-hooks rules are not where the clone's name suggests.**
`/tmp/lint-sources/react` is a SPARSE checkout and currently holds only
`compiler/packages/babel-plugin-react-compiler`. There is no
`eslint-plugin-react-hooks` directory in the ref at all; the package is named
**`eslint-plugin-react-compiler`**, and the sparse checkout has to be widened
before any of it is on disk:

    cd /tmp/lint-sources/react
    git sparse-checkout add compiler/packages/eslint-plugin-react-compiler

Rule source lands at `compiler/packages/eslint-plugin-react-compiler/src/rules/`,
and the corpora are in `__tests__/`, named after the RULE rather than after the
file: `NoCapitalizedCallsRule-test.ts` for `capitalized-calls`,
`InvalidHooksRule-test.ts` for `hooks`, `NoRefAccessInRender-tests.ts` for
`refs`.

This matters because the failure is silent in the worst way. A porter who greps
`/tmp/lint-sources` for their rule name finds nothing, concludes the corpus does
not exist, and invents fixtures, which is exactly the thing this document exists
to prevent. The plugin ships 29 rules; the absence is a checkout artifact, not
upstream.

**A rule the config already turned off may look unconfigured to your port.**
Rules register under their full upstream name, and `settingFor`
(`internal/lint/configuration/resolve.go:96`) resolves a config key by exact
match, then by trimming the key by the rule name and requiring what remains to
end in `/`. A key written in an OLD short spelling is *shorter* than the new
name, so the trim is a no-op and the branch never fires:

    config key : typescript/require-array-sort-compare
    rule name  : @typescript-eslint/require-array-sort-compare
    endswith?  : False

Each such key is a standing decision somebody made. Porting one of those rules
and running `EnableRule.ts` would turn it back on **through a spelling difference
rather than because anyone changed their mind**, the worst way to reverse a
decision, because it leaves no trace and reads as an accident nobody authored.

So before enabling any rule, grep the config for its BARE name as well as its
full one:

    grep 'require-array-sort-compare' /Users/kirkouimet/Projects/ahra/CohereSettings.json

If you find a prior `off`, stop. Register the rule, leave it unenabled, and say
in the commit that the prior `off` is why, naming the line.

**You no longer have to take this on faith.** A `--lint` run names every config
key that matches no registered rule:

    config: key "no-dupe-keys" matches no registered rule, so its off never
    applies — either the rule is not ported yet, or the key is spelled for an
    older name

Measured on the current tree: four such keys, down from thirteen when this was
first written, and none of them is now a stranded rename. If your rule's bare
name shows up in that list after you enable it, your enable did not take effect.
Check it; the line is cheap and the alternative is a rule that passes every
fixture and lints nothing.

**A fixer that rebuilds a signature loses the return annotation too — check BOTH
sides of the parameter list.** `no-arrow-function-lifecycle` already declined the
repair when a parameter could not be rendered by name, and its test cited the
eight declarations `no-undef-init` widened to `any` as the reason. It still
dropped `: void`, because a return annotation sits OUTSIDE the parameter list and
the parameter check could not see it:

    componentDidMount = (): void => {}   ->   componentDidMount() {}

Fixed by declining, matching what the rule already did for typed parameters. Two
rules in one night lost type information this way, so if your fixer constructs a
replacement span rather than deleting one, enumerate what lives inside that span:
parameters, return type, `!`, `?`, generics, `readonly`.

The pattern that works is the one this rule already had for parameters: **report
the finding, withhold the repair, and say why in a test.** A rule that reports
without fixing is useful; a fixer that quietly deletes a type is not. Prove the
decline with a mutation: disable the guard and confirm the new cases fail.

**`$?` after a pipe is the exit of the LAST stage, not the build.** This is the
same trap as the stale binary, one layer down, and it bit a coordinator's own
checkpoint script:

    go build -o out ./command/cohere 2>&1 | head -2; echo "exit=$?"   # reports head's 0

`go build -o` does not overwrite its target when compilation fails, so the old
binary stays in place answering `--rules` with a plausible number while your exit
check says everything is fine. Redirect to a file and read the exit directly
instead:

    go build -o out ./command/cohere > build.log 2>&1; echo "exit=$?"
    head -3 build.log

Then treat a non-empty `build.log` as a reason to distrust every number that
follows. A count that comes back surprisingly low, surprisingly high, or simply
unchanged when you expected movement is the symptom; the cause is usually that
you are reading a binary from before your edit.

**Checker methods are spelled `Checker_getX`, not `GetX`, and grepping the
natural name finds nothing.** A porter probing for `getContextualType` found zero
hits for `GetContextualType` and four for `Checker_getContextualType`. Grepping
the spelling you expect returns an empty result that reads exactly like a missing
substrate.

    grep "GetContextualType"          -> 0     looks absent
    grep "Checker_getContextualType"  -> 4     is present and reachable

That porter caught it only because their control returned zero on the same
pattern, which is the standard applied to a search rather than to a rule. Same
family as `ast.IsParameter` vs `ast.IsParameterDeclaration`: the substrate is
there under a name you did not guess.

**And if a checker method genuinely is absent, the shim is extendable.**
`TypeScript-shim/checker/extra-shim.json` allowlists unexported methods for the
generator; `ExtraMethods.Checker` already carries a couple of dozen including
`getContextualType` and `getResolvedSignature`, so adding one is routine rather
than novel. But dozens of rules import that shim, so land a shim change as its
own commit with its own probe, never in the same diff as a rule. The file's own
`ExtraDeclarations` block records why: substituting half a type left the mirror
24 bytes short and silently corrupted every field after it, read by offset
through `unsafe.Pointer`, with nothing crashing and no test failing. Run
`go test ./...` after regenerating, not just your package. The generator lives at
`internal/types/tools/generate_shims/`.

**`undefined: ast.Something` is usually a spelling difference, not a missing
substrate.** This tree's predicates are named after the DECLARATION, not the
concept: there is no `ast.IsParameter`, there is `ast.IsParameterDeclaration`.
Same for the class, the constructor, the call signature and most of the rest.
Before concluding the substrate is absent and writing up a measurement, grep for
the concept rather than the exact name you tried:

    grep -rhoE "func Is[A-Z][a-zA-Z]*" TypeScript/tsc/internal/ast/*.go | sort -u | grep -i param
    grep -n "IsParameterDeclaration" TypeScript-shim/ast/shim.go

The second command matters: a helper exists for you only if the shim linknames
it. `TypeScript-shim/ast/shim.go` is the list of what this binary can actually
reach, and something present in the vendored compiler but absent from the shim is
a real gap.

This distinction is worth a minute because this document elsewhere tells you that
a sized absence is a real result and to stop when the foundation is not there.
That guidance is correct and it does not apply to a helper you have merely
misnamed.

**If your rule touches `ctx.Program`, declare `ReadsProgram: true`.** Two rules
have shipped without it, and in the second case the guard was already failing
when the rule was committed. It is two for two on type-aware rules, because the
flag is easy to write last and easy to forget.

Reading the program means the rule's verdict for one file depends on the program
that file was compiled in: compiler options like `NoImplicitThis` or
`IsolatedDeclarations`, whether a declaration lives in the default library, a
symbol resolved across a module boundary. Undeclared, a findings cache keyed on
this file's hash keeps serving an answer computed under options that have since
changed — silence rather than a crash, which is the worse failure. The field's
own doc comment on `rule.Rule` carries the reasoning and a dated count.

Check before you commit, not after:

    grep -n "ctx.Program" internal/lint/rules/<pkg>/<rule>.go
    go test -count=1 ./internal/types/program/

`NeedsTypeChecker` and `ReadsProgram` are different claims. The first says you
need the checker; the second says your answer depends on the whole program. Many
rules want both. There is a third, `ResolvesReactValueTypes`, which is narrower
still: it declares that the rule identifies a React value by asking the checker
for its TYPE, so a file where the hook call resolves to `any` costs it every
finding silently. Read the field's doc comment before declaring it.

**Do not copy a `.tsx`/`.jsx` file gate, and do not assert one in a test.** Three
shipped react rules — `no-did-mount-set-state`, `no-direct-mutation-state`,
`no-this-in-sfc` — declined every file not ending `.tsx`/`.jsx`. That is oxc's
`source_type().is_jsx()`, carried over when they were ported from oxc.
eslint-plugin-react gates none of them, and a React class in a `.ts` file is
ordinary and legal. Measured: a byte-identical class reported in `.tsx` and was
silent in `.ts`, across thousands of `.ts` files. Removing the gates produced
zero new findings, so it closed a blind spot rather than opening work.

Each of the three carried a TEST asserting the gate, one of them saying outright
that upstream's corpus is entirely `.tsx` so nothing in it can tell a working gate
from an absent one. The suite locked the bug in rather than catching it. If you
are about to write a case asserting your rule is silent in a `.ts` file, check
upstream first: it almost certainly is not.

**A file gate is not automatically wrong, though.** `getter_return` legitimately
skips TypeScript, because upstream's `should_run` reads
`!ctx.source_type().is_typescript()`: the compiler already reports a
non-returning getter, and `get x(): boolean | undefined` is correct TypeScript
that the rule would wrongly flag. The test is whether UPSTREAM gates, not whether
a gate looks tidy. Every whole-rule early exit in the tree was audited when the
three above were found — the Next.js directory scopes and the
`ctx.SourceFile == nil` guards are all correct.

**Write the `_register.go` early, not last.** Between writing `<rule>.go` and
writing `<rule>_register.go`, `TestEveryRuleIsRegistered` fails for the whole
package, and the failure names YOUR rule to every sibling agent and to the
coordinator. It is indistinguishable from an abandoned port. One rule sat in that
state for eight minutes and read as stalled work when the agent was simply
writing the rule body.

The register file is three lines and can be written the moment the rule variable
has a name:

    func init() { rule.Register(rule.Registration{Rule: <RuleVariable>}) }

Write it second, immediately after the rule declaration compiles.

**Your new file shares a namespace with every committed rule beside it.** A Go
package is one namespace, so a helper you add can break a rule you never opened.
Two files in `internal/lint/rules/react/` each defined `jsxElementTagName`; the
committed one was clean in `git status` and untouched, and it stopped compiling
the moment the second file appeared. The error even points at the *committed*
file, which reads as someone else's breakage until you check `git status` and see
your own file is the untracked one.

Before naming a package-level helper, grep for the name in your rule's directory:

    grep -rn "func <helperName>" internal/lint/rules/<package>/

If it exists and does what you need, call it. If it exists and does something
different, prefix yours with the rule name (`forbidElementsTagName`). Note the
two above also had different signatures, so the collision surfaced as a type
error on the other rule's line rather than as a redeclaration on yours.

**Copy upstream's fixture tsconfig verbatim when you build a typed oracle.**
`tests/fixtures/tsconfig.json` in the typescript-eslint tree has no DOM lib and
does carry the node types. An agent used their own tsconfig instead and three of
`unbound-method`'s cases disagreed with the oracle; all three were the instrument
rather than the rule.

**An audit count can be structurally wrong rather than merely stale.** Four rules
in one night diverged from an audit because the tree changed after it ran, which
re-running would fix. `strict` diverges for a different reason: the audit was
measured through ESLint, whose flat config sets no `sourceType`, so its default of
`module` applies to every file. cohere derives moduleness from the source text. On
a file with no top-level import or export the two instruments answer different
questions, and re-running the audit reproduces the same wrong number.

    ESLint, sourceType module  ->  0 findings
    ESLint, sourceType script  ->  4 findings

The rule and upstream agree exactly once source type is held constant. Any rule
whose verdict depends on source type is in this class, and the count itself cannot
reveal it. If your rule's findings disagree with an audit, check whether the two
instruments are being asked the same question before assuming either is wrong.

**A test-case generator can collapse contradictory expectations onto one row.**
Keying rows on `(code, options)` looked sufficient and was not: the same source
under the same options carries opposite verdicts with and without a parser feature
like `ecmaFeatures.impliedStrict`. Eight apparent rule defects were one generator
defect. Key on everything that changes the verdict, including source type, ecma
version and parser features.

**A confident zero is the most dangerous result in this project, and most of them
are instrument failures.** One agent's sweep returned `0` three times running and
every one was the harness rather than the tree:

- a zsh glob that expanded to nothing, so the command ran on no files
- a flat-config `files: ['**/*']`, which matches no file that HAS an extension
- absolute paths outside the config base directory, which make the Linter answer
  "No matching configuration found" and report nothing rather than erroring

**That last one also has a false-POSITIVE costume, and it is the more convincing of the
two.** The Linter does not merely stay silent: it returns its complaint as a MESSAGE in
the ordinary messages array, so an extractor counting `messages.length` reads a
configuration failure as a finding. A corpus extraction reported "183 of 183 cases
reported through the installed rule" and every one of the 183 was the same complaint
about the fixture path. That number looks like a working oracle rather than a broken one,
which is worse than a zero: a zero at least invites suspicion.

**The mechanical fix generalises past this instrument.** A real finding carries a
`messageId`; the Linter's own complaints do not. So refuse any message without one rather
than counting it:

    const complaints = messages.filter(m => !m.messageId);
    if (complaints.length > 0) { /* refuse, naming the first */ }

Then seed fixtures INSIDE the config base with a relative filename, which is what makes
the complaint stop happening at all.

The coordinator hit the same class twice in one night: `$?` after a pipe reporting
`head`'s success while the build failed, and a `cd` that drifted into another repo
so `git status` came back clean.

**The clone and node_modules are different versions, and where they differ on a
FIXER the clone may be the one to trust.** When this was measured,
`/tmp/lint-sources/typescript-eslint` was 8.68.0 against ahra's 8.67.0. 8.68 added
`shouldWrapInParentheses` to `return-await`'s `removeAwait`, and without it:

    const test = async () => await { a: 1 };
      ->  const test = async () => { a: 1 };

Verified with the compiler: that body parses as a **Block**, not a parenthesized
expression, so the function silently stops returning anything. A meaning-changing
repair, same class as `no-undef-init` and `no-arrow-function-lifecycle`.

The usual guidance is that the installed build is the oracle, because the
differential compares against it. **That reason does not hold when the difference
is a bug fix rather than a behaviour disagreement**: porting the older behaviour
would mean deliberately reproducing corruption. Take the clone's behaviour there
and say so at the line.

Note that `/tmp/lint-sources` now carries two typescript-eslint checkouts at
different versions (`typescript-eslint` and `tse-fresh` at 8.69.0). Check the
`package.json` version of whichever you are reading before you cite it, and diff
the clone's source against the installed build's before trusting either.

**Two smaller fixer traps from the same porter.** Reaching for the "invalid"
precedence constant when you mean "must not win" is negative, so the comparison
still goes the wrong way and it fails SILENTLY toward writing less-parenthesized
code — they made it twice in one rule. And when two findings on one expression
propose overlapping edits, the harness refuses to apply them rather than guessing;
that refusal is correct, because upstream reaches its final text by running the
fixer repeatedly. Assert the proposed fix text rather than working around it with
an applied result the real pipeline never produces.

**When upstream's algorithm reads `allPrevSegments`, it is doing a dataflow join
and a tree walk cannot substitute.** A porter spent four rounds on
`no-useless-return` writing source-order models, each right on the shapes they had
measured and wrong on a different set:

    round 1  source-order model            21 failures
    round 2  branches as alternatives      10
    round 3  switch fall-through + dedup    7
    round 4  unreachability gate           12   <- worse

The tell was in upstream all along, in `getUselessReturns`:

    getUselessReturns(uselessReturns, segment.allPrevSegments.filter(isReturned))

Each segment inherits its pending returns from its PREDECESSORS, recursing through
unreachable ones. That is a join over a control-flow graph, and a source-order walk
has no representation of a merge point, so every model has to approximate it.
`if (a) { return; } return;` needs two findings and `return; return;` needs one,
and the difference is not source order or nesting but whether the two returns sit
on segments that join.

The porter's first read was that the CFG was the wrong tool, on the evidence that
three graph predicates failed to separate those cases. The correct reading was that
they had tried three WRONG predicates.

**This is the section whose mechanism has since changed, and the lesson survived
it.** When it was written, the graph exposed `Blocks`, `Successors` and `Reachable`
and nothing else, so the predecessor direction and the per-block fixpoint had to be
built by hand. `internal/lint/ecmascript/control_flow_graph/` now ships that
analysis for every consumer at once:

    Solve[V, E](graph, direction, lattice)   monotone dataflow to a fixed point,
                                             forward or backward, unreachable
                                             blocks excluded
    AnalyzeDominators[E](graph)              dominator tree
    AnalyzePaths[E](graph)                   path analysis

`no-useless-return` shipped. `Solve`'s own doc comment records why it exists rather
than each analysis writing its own loop, and names the two things
`no-useless-assignment`'s hand-rolled version settled for.

So the guidance that survives is the diagnosis, not the workaround: if upstream's
implementation is segment bookkeeping, do not port it as a visitor, reach for
`Solve`. And if three attempts at a graph predicate all fail, suspect the
predicates before concluding the graph is wrong.

**A control is only a control if you read where it reports.** A porter seeded a
violating file, swept, and got the same count as before with nothing from the seed.
Their script already carried a guard for exactly that case and printed

    UNMATCHED: /tmp/wd/seed/Control.ts

to **stderr**, while they were grepping stdout for a count. The guard was correct,
ran, detected the fault, and told nobody. They had written it after hitting the
same trap two batches earlier, and then walked past its output.

This is the zero-assertion probe one layer out: an instrument that CAN report
failure, into a channel nobody is watching, is not much better than one that
cannot. So merge stderr (`2>&1`) into whatever you actually read, or assert on the
guard's output rather than eyeballing it.

The underlying cause is worth knowing on its own: a seeded file placed OUTSIDE the
config base matches no flat-config entry, and the Linter answers "No matching
configuration found" rather than erroring. **Seed inside the tree**, sweep, then
remove the seed. The porter's corrected run gave 12 with the seed and 11 without,
which is what a working control looks like.

**Never accept a zero without a control** — source that unambiguously violates the
rule, run through the same harness. If the control does not fire, the instrument is
broken and the zero means nothing. If you are measuring a rule's coverage, say in
your report whether your control fired; a number from a harness with no control is
not a measurement.

This is the same failure the `.tsx` file gate produced for three rules, the same
one `unicode-bom` produces, and the same one an unconfigured options-driven rule
produces. They are indistinguishable from a clean tree in a findings count and only
a control separates them.

**A rule package may not import another rule package.** A guard enforces it and
states the cost: a leaf edit rebuilds in about 1.8s, a deep one in about 8.5s, paid
by every agent in the tree on every edit.

This bites extension rules specifically, because upstream's is literally
`baseRule.create(context)` and reaching for our ported core rule mirrors what
upstream does. `@typescript-eslint/no-dupe-class-members` shipped that way and the
guard failed on committed code.

Delegating is still the right shape — re-deriving a core rule's logic gives the two
rules two chances to disagree about the same question. What has to change is where
the shared code lives: lift it into `internal/lint/ecmascript/...`, which both
packages can reach. `imports.BindingsOf`
(`internal/lint/ecmascript/imports/bindings.go:36`) and `jsx.ElementParts`
(`internal/lint/ecmascript/jsx/attributes.go:124`) are both exactly this, lifted
out of rule packages for the same reason.

Run the guard suites BEFORE committing, not after:

    go test -count=1 ./internal/lint/registry/ ./internal/types/program/

Those carry the registration, wiring, fixture-pair, crash-corpus, checker-declaration
and scratch-package guards between them.

**Read decorated parameters through `node.Parameters()`, and do not write a
resolver.** Upstream's `resolveDecoratedParameterNode` is thirty lines and exists
for two estree constraints we do not have. Measured across all five shapes it
normalises:

    m(@D(K) plain: string)                          constructor(@D(K) private readonly p: string)
    m(@D(K) defaulted: string = 'x')                constructor(@D(K) public optional?: string)
    m(@D(K) rest: string[])

All five are a single flat `KindParameter` here, and the decorator's parent is
`KindParameter` in every case. There is no `TSParameterProperty` wrapper to unwrap:
a parameter property is an ordinary parameter carrying modifiers, and
`AsParameterDeclaration()` reads `Name()`, `Type`, `QuestionToken` and `Initializer`
off all five identically. The membership test against the enclosing function's
`params` is also unnecessary, because you iterate `node.Parameters()` and read
decorators off each rather than starting from a decorator and walking up.

So: `node.Parameters()` plus `decorators.Of(parameter)`
(`internal/lint/ecmascript/decorators/`), and stop there.

**But `Node.Text()` panics on a destructured parameter's name**, and that IS worth
guarding:

    m({ a, b }: Options)   ->   Unhandled case in Node.Text: *ast.BindingPattern

Reproduced directly: the name node is `KindObjectBindingPattern` and `Text()` panics
on it, while a rest parameter's name is a plain identifier and is fine. The walk
recovers per FILE rather than per rule, so one such parameter costs every rule in
the package its verdict on that file. This is the 167-file class.

Any rule reading a parameter's NAME is exposed. Guard the kind first
(`name.Kind == ast.KindIdentifier`), and add a test row covering a destructured
parameter, an array pattern and a rest parameter. **Check the row against a
control**: make the rule read a parameter name and confirm the row fails, or it
passes vacuously and proves nothing.

**Porting into a NEW rule namespace needs two shared edits, and without them every
rule in it is dead.** Six `base/` rules were on disk, well written and
fixture-green, and not one could execute:

    internal/lint/registry/registry.go        needs  _ ".../internal/lint/rules/base"
    internal/lint/registry/rule_names_test.go needs  "base": true in knownNamespaces

The first is the fatal one. Without that blank import the package never
initialises, so no `init()` runs, nothing registers, and `--rules` simply does not
list your rule. The second makes
`TestRegisteredNamesMatchTheirUpstreamSpelling` fail for any name in the new
namespace. (This guard used to live in a file called `parity_test.go`; it is
`rule_names_test.go` now, and `knownNamespaces` is at line 47.)

This is the "a green fixture set does not mean the rule runs" failure at package
scale, and it is worse than the per-rule version because the whole namespace is
affected at once and each porter's own tests still pass. **If you are first into a
namespace, make both edits and say so in your commit so the others do not each
rediscover them.**

**The source repository may not be able to run its own lint, and you still need an
oracle.** `api-phi-health` was at one point mid-merge with 72 unresolved conflicts
and no `@nexus` package in `node_modules`, so `eslint --config LintConfiguration.ts`
died on a missing module. Do not repair another project from a porting session.
Import the rule module directly with a loader hook mapping `@nexus/` to
`libraries/base/libraries/nexus/` and drive it through ESLint's `Linter` API
instead.

That is worth the setup rather than falling back to invented fixtures. Driving the
real rule caught two behaviours a porter would not have written from first
principles: a property KEY named `getGlobalContainer` reports, and so does a locally
declared function of that name, because the rule tests the name rather than
resolving it. Both would have been wrong fixtures.

**When porting OUR rules rather than upstream ones, the original's walk may exist
for a parser difference we do not have.** The base-layer rules were written against
ESTree, and `resolveDecoratedParameterNode` is thirty lines normalising three
shapes: a plain identifier, an `AssignmentPattern` for a defaulted parameter, and a
`TSParameterProperty` for a constructor parameter property, plus a membership test
against the enclosing function's `params` so an identifier inside the decorator's
own arguments is not mistaken for the parameter.

Measured across all four shapes in our parser: **the decorator's parent is
`KindParameter` every time**, and the defaulted and parameter-property forms differ
only in fields hanging off that same node. So the walk is `node.Parent` with a kind
guard, and the membership test protects against nothing, since a decorator's own
arguments are not its parent.

That is fidelity to the DECISION rather than to the workaround, and it is the
opposite instinct from an upstream port, where reproducing the original exactly is
usually right. Ask whether each piece of machinery exists for a judgment or for a
parser limitation, and measure before assuming.

**One part stayed load bearing and it is the part that looks droppable.** The kind
guard itself: a decorator can sit on a class, a method, a property or an accessor,
and all four were measured reaching the listener with a parent of some other kind.
Without it a class decorator walks to the class and is treated as a parameter. When
you simplify a walk, the guard is usually what has to survive.

**A core/extension pair is often one job, and there are three distinct ways to
discover that.** All three have cost an agent a cycle:

1. **The extension wraps core** (`getESLintCoreRule`), so the core algorithm already
   lives inside the shipped namespaced port. `init-declarations`, `no-invalid-this`.
2. **The extension is a standalone reimplementation and is strictly stronger**, so a
   faithful core port would be inert here. `no-implied-eval`: core resolves
   `setTimeout` through global scope and this config declares no globals.
3. **Our core already folded the extension's filters in**, so the two are
   behaviourally identical. `no-useless-constructor`: the extension layers three
   accessibility filters and a parameter-property filter over core, and our core
   carries all of them as named helpers. Measured at 65 inputs with zero divergence,
   including 11 adversarial shapes built to attack each filter.

For case 3 the deciding question is whether anything needs the second NAME. It
usually does not, and there is no `Aliases` field on `rule.Rule`, so registering one
rule under two names is a registry change rather than a port.

> **What changed here.** This paragraph used to say "check `rule-inventory.json` for
> the name before building a wrapper", and to lean on
> `TestParityAgainstInventory`. That file was deleted and that test is gone. Ask the
> binary instead — `<cohere binary> --rules | grep <name>` — which is the more direct
> question anyway, and was always the better instrument: the inventory was a claim
> about the tree, and `--rules` is the tree.

When you find any of the three, **write the measurement into the code** rather than
only into your report. The next person reads the audit, sees an unported line item,
and re-derives it otherwise.

**A core rule may already be shipped under its `@typescript-eslint/` name — check
BOTH spellings before you write anything.** An audit lists `eslint-core/x.md` and
`typescript-eslint/@typescript-eslint__x.md` as separate line items, and for an
`extendsBaseRule` pair they are usually ONE job. Four core rules read as unported
this way and three agents were dispatched to build duplicates before catching it
themselves:

    default-param-last · no-implied-eval · no-invalid-this · prefer-promise-reject-errors

All four are live, registered and enabled under `@typescript-eslint/`. Two of them
were reported as "Strong Yes rules that were missed" for three checkpoints running.
They were not missed; the census matched bare names only.

    <binary> --rules | grep -E "^(@typescript-eslint/)?<rule>$"

One expression, both spellings. But finding the namespaced one does NOT
automatically mean your work is done:

**If the extension WRAPS core** (`getESLintCoreRule` in its source), the core
algorithm necessarily already lives inside our namespaced port, because there is
nowhere else it could be. A bare port is a second implementation of code already
present.

**If the extension is a STANDALONE reimplementation**, the core rule is genuinely
absent and porting it is real work, even though the tree is already enforced in some
form by the enabled namespaced rule. Expect such a port to add no new findings on
this tree; that is the rule being already covered in practice, not a broken port,
and you should say so in your report rather than chase the zero.

    grep -l getESLintCoreRule \
      /tmp/lint-sources/typescript-eslint/packages/eslint-plugin/src/rules/<rule>.ts

**Three typescript-eslint targets are extension rules, and an audit's line count is
less than half the job.** `@typescript-eslint/consistent-return`,
`no-empty-function`, and `no-useless-constructor` each open with

    const baseRule = getESLintCoreRule('<same-name>');

and their `create` is `const rules = baseRule.create(context)` plus a
TypeScript-specific filter on top. The wrapper cannot report anything on its own:
every finding comes from the ESLint core rule underneath.

So the real size is wrapper + core, not the wrapper the audit measured:

    consistent-return        132 wrapper +  207 core  =  339
    no-empty-function        184 wrapper +  235 core  =  419
    no-useless-constructor    75 wrapper +  262 core  =  337

Port the core rule first, as its own bare-named rule with its own corpus, then the
extension on top of it. An agent handed "consistent-return, 132 lines" sized against
the wrapper alone will run out of room.

**A screening caveat, paid for by a wrong answer.** Counting `context.report` to
decide whether a rule can fire flags 33 of one remaining pool, and 32 of those are
false. eslint-plugin-react calls a wrapped `report(context, ...)` helper imported
from `../util/report`, and typescript-eslint extension rules delegate to a base
rule. Match `\breport\s*\(` and check for `getESLintCoreRule` before concluding a
rule cannot report. The only true zero in that pool was `react/jsx-uses-vars`.

**Some `react-hooks` targets are not rules at all.** Every one of
`capitalized-calls`, `exhaustive-effect-dependencies`, `fbt`, `hooks`,
`memo-dependencies`, `memoized-effect-dependencies`, `rule-suppression`, and
`syntax` is a React Compiler diagnostic category, not a portable AST rule. Two of
them (`hooks`, `memo-dependencies`) reported **21 violations** in one audit, so the
count alone makes them look like ordinary work. They have no `create()` and no
visitor. They are defined as `ErrorCategory` cases in
`react/compiler/packages/babel-plugin-react-compiler/src/CompilerError.ts`, and
producing any of these findings requires lowering the function to the compiler's own
HIR, inferring reactivity and memoization, and validating against that inference.
There is no visitor to port. Confirm for yourself with:

    grep -n "name: '<rule>'" \
      /tmp/lint-sources/react/compiler/packages/babel-plugin-react-compiler/src/CompilerError.ts

A hit there means the name is a compiler category and there is nothing to port.

Their zero is the same trap as `unicode-bom` and `jsx-uses-vars`: **a rule that is
off by default, or that cannot report at all, measures zero violations and an audit
reads that zero as a clean tree.** Whenever an audit note pairs a strong
recommendation with "violations: none", confirm the rule can fire before you believe
the count.

Note that this tree now carries a real React Compiler conformance harness at
`internal/lint/rules/react/conformance/`, with a vendored corpus and its own
scoring. If your target is one of these categories, read that package before
concluding anything about what is and is not reachable.

**A fixer that is right about JavaScript can be wrong about TypeScript.**
`no-undef-init` once rewrote `let x: SomeType | undefined = undefined;` to `let x;`
across eight files in libraries/structure. Upstream's fixer removes the initializer,
which is correct in JavaScript; here it also stranded the type annotation, and every
one of those declarations silently widened to `any`. It compiled, so nothing failed
— the damage was only visible in `git diff`.

The cause is structural and it will bite any fixer that computes a span from a
node's neighbour. Upstream reads `node.id.range[1]`, which is right in ESTree, where
a TypeScript annotation is **part of the id node**. In our AST `Type` and
`ExclamationToken` are **siblings of the name**, so the same offset sits before them
and the removal swallows them. Fixed by starting the removal at
`declaration.Type.End()` when there is an annotation.

Three things follow.

1. Any fixer whose range starts or ends at a binding name must account for `Type`
   and `ExclamationToken` sitting between the name and the initializer.
2. **Add TypeScript cases upstream cannot have.** Its corpus is JavaScript, so all
   eight of `no-undef-init`'s `output` cases pass while every annotated declaration
   in the tree is destroyed. The gap between upstream's corpus and our tree is
   exactly TypeScript syntax, and that is your job to close.
3. **Mutate your fixer test.** Disable the branch and confirm the new cases fail.
   All six of the added cases fail under mutation; a test that passes both ways
   proves nothing.

And the operational lesson, learned three times on the same eight files: **reverting
the files does not fix the fixer, and fixing the fixer does not fix the binary.**
The eight were reverted, damaged again ten minutes later, the rule was corrected and
mutation-tested, and they were damaged a THIRD time forty minutes after that.

The third round was not the rule. The binary the ahra tree ran was a **symlink into
a scratchpad build** that was 19 hours stale, predating the fix. Every agent
invoking it from that directory ran the old fixer no matter what was committed.

So when you fix a rule that writes:

1. Fix the rule and mutation-test it.
2. Rebuild whatever the ahra tree's binary actually points at. Check its mtime and
   its `--rules` count against the tree's; a low count is the tell.
3. Only then repair the damaged files.

Doing 3 before 2 means doing 3 again.

> **That symlink is gone, and finding it was worth the paragraph it took.** While this
> document was being rewritten, `~/Projects/ahra/node_modules/.bin/verify` still pointed
> at a 77 MB binary under `~/.local/bin/` built before the rename, with no `cohere` entry
> beside it. Anyone typing the old name got a linter from a previous day and no warning.
> Both were removed on 2026-09-06.
>
> The lesson survives the cleanup, because the shape recurs every time a name changes:
> **a resolved binary is not the binary you just built.** Check the mtime and the
> `--rules` count of whatever actually answered before you trust a number from it, and
> drive your own build by absolute path.

**Our parser keeps parentheses; typescript-eslint's folds them away.**
`KindParenthesizedExpression` is a real node here and does not exist in the tree
upstream recurses over, so **any ported rule that walks through an expression will
see a node upstream never sees**. Measured on `prefer-literal-enum-member`: without
an unwrap the port reported two of upstream's OWN passing cases.

**It runs in BOTH directions, and one porter hit each in a single batch.** The extra
node does not only make you over-report:

- **It cost findings.** `prefer-object-has-own` missed five sites until unwrapped,
  because upstream's parser delivers `(( Object.prototype.hasOwnProperty )).call(...)`
  pre-folded.
- **It removed a step.** `no-unexpected-multiline` walks past closing parens
  upstream; ours already ends past them, so the same code over-walked.
- **It hid a real defect from the ENTIRE imported corpus.** In
  `max-nested-callbacks`, both of upstream's immediately-invoked passing cases are
  parenthesized, so its `callee === node` test is never consulted here — deleting
  the Go equivalent survived all 32 corpus cases. The distinguishing shape is
  `!function(){}()`, which carries no parens and which upstream had no reason to
  write.

That last one is the whole argument for testing shapes the corpus does not write. A
guard can be dead against every imported case and still be load-bearing on real
source.

No imported fixture can flag this, because upstream's corpus cannot express the
shape — its parser deleted the node before the test was written. That makes it
invisible to the one check this document otherwise leans on hardest.

Unwrap in a loop rather than a single step, because `((2))` nests:

    for expression.Kind == ast.KindParenthesizedExpression {
        expression = expression.AsParenthesizedExpression().Expression
    }

**Do not reach for `ast.SkipParentheses` here.** It dereferences its argument, and
the thing you are unwrapping is often optional — an enum initializer, a default, a
return argument. That helper is how this project lost 167 files to a nil panic.
Write the loop with the nil check you need.

**`react/jsx-uses-vars` is not a rule you can port, and two agents correctly
declined it before anyone checked.** It contains **zero `context.report` calls**.
Its entire body calls `markVariableAsUsed` to feed `no-unused-vars`, which is why
upstream disables the message-id lint on it. There is nothing to report, so there is
no fires-and-silent pair, and `TestEveryRuleShipsAFixturePair` cannot be satisfied
by a faithful port.

**A THIRD agent was dispatched to it anyway, and measured something better than that
argument.** The reason to decline is stronger than "it cannot report", and this is
the version that survives someone adding a marking surface later:

**A faithful port would be REDUNDANT, because our checker already resolves the
reference.** Upstream needs this rule because eslint-scope does not connect a JSX
element name to its declaration. Ours does. `GetSymbolAtLocation` on a JSX tag name
returns the same symbol OBJECT as the declaration, controlled against an intrinsic
`div` resolving elsewhere so it is not nil-equals-nil.

Measured end to end: six bindings referenced only from JSX, taken from
`jsx-uses-vars`'s own corpus, all come back CLEAN from our `no-unused-vars`, with
two controls that do report. On the real tree, `no-unused-vars` ran on every file
and none of its findings were in any `.tsx` file. No JSX-only component is falsely
flagged anywhere, which is exactly the outcome this rule exists to produce.

**And the coordination lesson.** That section already said "Do not assign this as a
port" and named two prior declines. It reached a third agent because the claim-check
tested four axes and never read the file. **Knowledge that exists but is not
CHECKABLE does not prevent anything.** If you decline a rule, put the decline
somewhere a mechanical check can find it, not only in prose.

## 0b. Know which kind of rule you have before you start

Establish this in the first two minutes, because it decides what your fixture floor
is.

**The authority is whoever wrote the rule, and its source is in
`/tmp/lint-sources`.**

  | rules | authority | source |
  |---|---|---|
  | core `eslint/*` | ESLint | `/tmp/lint-sources/eslint` |
  | `@typescript-eslint/*` | typescript-eslint | `/tmp/lint-sources/typescript-eslint` |
  | `react/*` | jsx-eslint | `/tmp/lint-sources/eslint-plugin-react` |
  | `react-hooks/*`, `rules-of-hooks`, `exhaustive-deps` | React | `/tmp/lint-sources/react` |
  | `@next/next/*` | Vercel | not cloned; read the installed build |
  | `structure/*`, `nexus/*`, `base/*` | Kirk's and Kam's own TypeScript lint layers | in the ahra and api-phi-health trees, and those are the source |

**Every one of these ships a test corpus in its repository, and none of them ship it
in the published package.** That corpus is your fixture floor and it is richer than
you expect: `eqeqeq` alone carries 75 cases and 49 fix vectors. Read step 3 before
you write a fixture.

**Check where the judgment actually lives before trusting the rule file's size.** A
rule file can be a thin caller over machinery that holds the real decision:

- **ESLint's scope analysis.** A body calling `sourceCode.getScope()`,
  `context.sourceCode.getDeclaredVariables()`, or walking `variable.references` is
  delegating to `eslint-scope`. The rule file may be forty lines and the port large.
- **typescript-eslint's type services.** `getParserServices(context)` followed by
  `getTypeAtLocation` means the rule asks the type checker. We have that through the
  shim, but establish what the checker actually answers before assuming, because the
  two are not the same question.
- **React's compiler passes.** A `react-hooks/*` rule usually names a pass in
  `src/Validation/` or `src/Inference/`. The rule surface is trivial; the pass is
  the port.

**A rule that destructures an enum whose variants encode a classification is telling
you the classification happened somewhere else.** Go find where.

**Read the installed build too, and prefer running it over reading it.** The
published packages carry the compiled rule that this repository actually executes,
and the ESLint Linter API runs from `~/Projects/ahra`, so the real rule can be
driven on inputs you invent rather than reasoned about. A porter was told `08` and
`09` must not report and the dispatch was wrong: `/^0\d/u` matches them, and they
are the same banned legacy production. They found it by running the rule. Note that
ESLint defaults to module mode, where an octal is a fatal parse error and the rule
reports nothing, so ground truth needs `sourceType: "script"`.

**`eslint-plugin-react-hooks` ships two bundles and the development one is
readable.** `~/Projects/ahra/node_modules/eslint-plugin-react-hooks/cjs/eslint-plugin-react-hooks.development.js`
is compiled TypeScript with types stripped: real function names, real comments,
original variable names. Use it when you want to see what the shipped rule does; use
the clone when you want comments and tests.

**React's own pass documentation exists, and it is SECONDARY to the executable.**
`compiler/packages/babel-plugin-react-compiler/docs/passes/` has a numbered page per
pass with algorithm and edge cases, and it settled a rule's semantics in one read
for one porter. **It is also wrong.** `40-validateUseMemo.md` states that a callback
containing a bare `return;` "will trigger the error"; the lowering hardcodes
`returnVariant: 'Explicit'` regardless of argument and the case is **clean**,
measured against the running rule. A porter who found the docs early and trusted
them would ship a false positive with a confident citation. Read them for the shape
of the algorithm; take every verdict from the executable.

**`testing.RunTyped` trims your fixture, and a span assertion sliced from the Go
literal will be off by one.** `internal/lint/testing/program.go:169` writes each
fixture as `strings.TrimSpace(contents)+"\n"`. Every case copied from an upstream
tester carries a leading newline, so **the file on disk is one byte offset from the
literal in your test**. Slicing the literal to assert a span reports `"<Componen"`
for a finding correctly anchored on `"Component"`, which reads exactly like an
off-by-one in the rule. One porter chased it through the intermediate representation
and the shim before finding it in the harness.

**`testing.Run` does not trim**, so this is invisible to any rule that does not need
the checker.

## 1. Read the task

Read the whole task body before you touch anything. Parent bodies are the constraint
surface; standing rules live there.

## 1b. Read two or three rules in this tree that work the way yours will

Read them whole, including their `_test.go`. Pick by the shape of the question
rather than by the name:

    anchors on a declaration and looks for writes to it
        core/no_class_assign.go, core/no_const_assign.go, core/no_func_assign.go
    walks a regex pattern
        core/no_useless_backreference.go, core/no_regex_spaces.go
    needs the type checker
        grep -rl NeedsTypeChecker internal/lint/rules/
    proposes a fix or a suggestion
        grep -rl ReportNodeWithFixes internal/lint/rules/
    reasons about scope and bindings
        core/no_shadow_restricted_names.go, core/no_unassigned_vars.go
    gathers once over the file and then matches
        core/no_const_assign.go's KindSourceFile pre-pass

What you are taking is anchoring, `ctx` threading, type-switch layout, where
reasoning goes in the doc comment, how fixtures are tabled, how spans are asserted,
how `NeedsTypeChecker` is declared. Fourteen authors each re-invented this and no
guard caught the divergence, because every version passed its own tests.

### Read siblings for shape, never for semantics

Copy how a rule is built. Never copy what it believes about its inputs.

`no-nonoctal-decimal-escape` looks like a regex rule and is not: `/\8/` is a **pass**
in its corpus. Ported by analogy from the rule finished an hour earlier, its code and
its fixtures would have shared one wrong belief, **and every fixture would have
passed.**

When the code and the tests come from the same analogy they agree with each other and
disagree with reality. No fixture pair, no mutation sweep, and no span assertion can
see it, because none of them gets to vote on the premise. The corpus is the only
thing that can.

If you find yourself writing a fixture because a neighbouring rule had one like it,
stop and go find that case in your own corpus. If it is not there, it is not yours.

**Say in your report which rules you read and what you took from each.**

## 2. Read the reference implementation

The authority's rule file, from the clone. Read the whole rule body and every helper
it calls, not the first match.

Four things to establish explicitly before continuing:

- **`meta.schema` is the authoritative option surface.** Audit columns describing
  options have been wrong for sixteen of forty-one core rules. `schema: []` means no
  options and is checkable in one line.

- **`meta.messages`** names every distinct finding the rule can produce, which is how
  you tell one input reporting twice from two different judgments.

- **`meta.fixable`, and the fixer body.** If it says `"code"` or `"whitespace"`, the
  rule ships a repair and you are porting that too. The fixer is usually a
  `fix(fixer)` closure inside the report call. Read what it actually writes, because
  the corpus asserts the output text and your fixer has to produce it byte for byte.

- **Does it delegate?** If the body reaches for scope analysis, the type checker, or
  a compiler pass, then the rule file is a thin caller and the algorithm is
  elsewhere. A forty-line rule file can be a large port.

  **Needing the checker is not the same as the checker being sufficient.** Establish
  both what resolution answers and what it cannot. `react/jsx-no-undef` needs
  `GetSymbolAtLocation`, and resolution alone does not separate an intrinsic element
  from a typo: `<img />` and `<x-gif />` resolve to no symbol, identically to
  `<Uddefined />`. A port using resolution alone passes all eight of upstream's
  reported cases and then reports every `<div>` in the tree. The case predicate is
  load-bearing rather than an optimization, and only the corpus says so.

  **Two rules about searching, because both have cost real work.**

  *Search for the thing, not for the citation.* If somebody tells you a claim lives
  at commit X or file Y, search the whole tree for the thing itself. A search bounded
  by someone else's reference cannot find what their reference got wrong, which is
  the only case where checking was worth doing.

  *Run a control alongside any zero.* Grep for something you know is present in the
  same place with the same command. A zero from a real absence and a zero from a bad
  pattern, a wrong path, or a shim alias are the same zero. This costs one extra
  command and it is the difference between a measurement and a guess.

  **If you conclude our tree lacks the substrate it needs, prove it with a compiling
  probe before you stop.** A grep over `TypeScript-shim/` cannot see through a Go
  type alias, and the shim is full of them: `type Checker = checker.Checker` carries
  every exported upstream method without restating one. Three capabilities were
  called unavailable in one night on the evidence of a grep, and all three were
  reachable. Write a throwaway test that calls the thing, run it, delete it. Two
  minutes, and it cannot lie the way the grep did.

- **What does it deliberately not catch?** Commented-out cases and `TODO`s are
  upstream telling you what it knowingly misses. Reproduce those gaps; do not
  silently improve on them. A divergence is fine when it is stated.

**Record every divergence at the line, whichever way you resolve it.** A documented
disagreement is recoverable later; a silent choice is not.

## 2b. Fidelity is to what the rule DECIDES, not to how it obtains what it needs

The originals walk the filesystem, read `process.cwd()`, re-parse a neighbouring
file, or cache by path, **because ESLint hands them one file at a time and gives them
no program.** cohere has the program, one resident type graph, every file already
parsed.

**Reproducing a workaround for a constraint we do not have is not fidelity, it is
cargo cult.** If the reference implementation shells out to the filesystem to answer
a question the checker can answer directly, answer it directly and say so in the
commit. What you must reproduce exactly is the *decision*: which inputs report, which
do not, where the finding points, what it offers.

Three consequences, and the third is the one that ships.

**A port is not obliged to carry a defect it can see.** Two rules here report without
the fix their original ships, because those fixers drop type annotations, lose
`async`, or replace a whole `VariableDeclaration` while reporting per declarator,
which silently deletes a component. **Reporting without a fix is the subset you can
show correct.** Shipping a fix you cannot defend is worse than shipping no fix,
because a wrong fix is applied unattended.

**Where a rule has no tree exposure and upstream gives no reasoning, your own sense
of what the rule should do is the thing most likely to be wrong.** Record the
intuitive reading beside the actual one, at the line, so the next reader does not
helpfully correct it back to the wrong one.

**A surviving mutant in an arm you believe is unreachable is the arm telling you the
grammar is not the parser.** A porter emptied the getter and setter arms of
`default-param-last` and both mutants lived through all 87 corpus cases. The doc
comment beside them said those arms can never fire, because a setter takes exactly
one parameter — which is true of the *grammar* and false of the *parser*, which
recovers from illegal source and hands back a two-parameter setter. They probed the
parse instead of writing the survivor off, measured upstream reporting all four such
shapes, and added the fixtures. Two mutants caught, one doc comment corrected from an
argument into a measurement.

**And never document a limit you did not measure.** This is the third failure mode
and it is worse than the other two, because reproducing a defect leaves it findable
and silently improving leaves a difference the harness can see, while a confident doc
comment asserting "this case is a genuine divergence, reproduced as silence"
**inoculates the next reader against finding it.** They read the comment, believe the
narrowness was deliberate, and stop checking. It happened: a porter wrote exactly
that sentence about computed lifecycle keys, and upstream resolves them and reports.
Their sibling found it by running the installed rule. **If you state a divergence,
state the command that established it, or do not state it.**

## 3. Read the corpus, and take your fixtures from it

Every authority ships its tests in the repository and none of them ship it in the
published package. That corpus is your fixture floor.

    eslint/*              /tmp/lint-sources/eslint/tests/lib/rules/<rule>.js
    @typescript-eslint/*  /tmp/lint-sources/typescript-eslint/packages/eslint-plugin/tests/rules/<rule>.test.ts
    react/*               /tmp/lint-sources/eslint-plugin-react/tests/lib/rules/<rule>.js
    react-hooks/*         alongside the pass in the compiler package, plus `__tests__` fixtures

**Read every case before writing code.** Each invalid case carries its **own**
`messageId` and, where the rule is fixable, its **own** `output`. So per-input
diagnostic counts are stated rather than recovered.

What to pull out, in order:

- **The valid cases.** These are the false positives upstream already thought about.
  Every one you skip is a class of false positive you will ship.

- **The invalid cases, with their `messageId`.** A case naming two entries in
  `errors:` reports twice, and a fixture asserting one finding per input is wrong for
  it.

- **The `options` on each case.** A rule with an option surface usually tests the same
  input under several settings, and the same code can be valid under one and invalid
  under another. `eqeqeq` does exactly this with `"smart"` and `"always"`.

- **The `output` on each invalid case, if the rule is fixable.** These are
  before-and-after pairs asserting what the repair writes rather than whether a
  finding appears, and **they are the highest-value artifact in a fixable rule's
  corpus.** `eqeqeq` carries 49 of them against 75 cases. Assert them with
  `ExpectFixedSource`, not by eye. An `output: null` means the case is reported but
  deliberately not fixed, which is itself a decision to reproduce.

- **Anything commented out.** Upstream telling you what it knowingly misses.

**A corpus file may call `RuleTester.run` more than once, and a single-slot interceptor
silently keeps the last one.** The reliable way to extract a corpus is to stub or
intercept `RuleTester` and run upstream's own test file, so its own cases produce your
fixtures rather than a parser of yours. That works, and it has one failure mode: an
interceptor written as `capturedCases = tests` rather than `capturedCases.push(...)`
keeps whichever call came last and drops the rest, with no error and a plausible-looking
count.

Measured: `no-invalid-this` makes two `run` calls, a 474-case JavaScript matrix and a
98-case TypeScript block, and an earlier single-slot interceptor kept only the second and
**lost 83% of the corpus**. `init-declarations` also makes two. Neither file looks unusual
from the outside, and neither count looks wrong once you have it.

So accumulate across calls, and **print the call count beside the case count** -- a
one-call extraction from a two-call file is the whole defect, and it is invisible in the
cases themselves:

    console.error(`captured ${cases.length} cases from ${runCalls} run() call(s)`);

Then refuse to emit anything when either number is zero, for the same reason every other
instrument here refuses: an extraction that captured nothing looks exactly like a rule
with a small corpus.

**Take the cases verbatim.** Retyping a case is how a fixture ends up testing what
you believe instead of what upstream asserts.

**Measure parenthesis behavior rather than assuming it.** `SkipParentheses` reads as
a free correctness improvement, so a porter adds it and moves on. Upstream applies it
inconsistently within a single rule, and our AST does not agree with any upstream's
about where a parenthesis survives: measured on `no-direct-mutation-state`,
`(this.state.x) = 1` reports with the span excluding the parens, `(this.state.x)++`
reports with the span including them, and `(this).state.x = 1` is silent. Two
accessors, three answers, one rule.

Most corpora write no parenthesized forms at all, so guessing wrong costs nothing at
fixture time and ships a divergence either way. **Look before you assume your corpus
is silent on parens.** If your rule matches a receiver or a callee, write `(x)` forms
into your own fixtures and run the installed rule to see which way each one falls.

**When upstream gates on source type, check whether OUR parser can produce the node in
the excluded files.** This is fidelity to a **parser difference** and it is
load-bearing rather than vestigial: the vendored compiler resolves JSDoc into real
type nodes, so `/** @type {any} */ let x = 1;` in a `.js` file produces a live
`KindAnyKeyword` that other parsers cannot produce at all. Dropping the gate as an
optimization would have reported every JSDoc `any` in every JavaScript file — **a
false-positive class no imported fixture can see.**

**The clones ship no `node_modules`, and each rule needs a different one.** Any
upstream tester or rule file that imports `ast-utils`, `eslint-visitor-keys`,
`esutils`, and friends fails to load with a *different* missing module each time, so
installing them one at a time costs a cycle per rule. Resolve them out of
`~/Projects/ahra/node_modules/.pnpm` by package name and they all land at once.

**Render `options` and `languageOptions` beside every extracted case.** A case can
carry its verdict above the rule: `no-alert` has byte-identical source in both the
valid and the invalid list, separated only by `ecmaVersion`. Read as prose that looks
like a contradiction or a duplicate in the corpus. A case appearing twice with
opposite verdicts is a version gate, not an error — and the extractor is the only
place you can see it.

## 3b. If the rule is fixable, the fixer is half the port

`meta.fixable` in the rule file tells you. If it is set, you are porting a repair as
well as a judgment, and the corpus already asserts what that repair must write.

**Upstream's fixer is a closure; ours is a value.** ESLint hands the rule a `fixer`
object and the rule calls `fixer.replaceText(node, "===")`. cohere builds the same
thing as data:

    rule.ReplaceRange(textRange, "===")
    rule.RemoveRange(textRange)

    ctx.ReportNodeWithFixes(node, message, rule.ReplaceRange(operatorRange, "==="))
    ctx.ReportNodeWithSuggestions(node, message, suggestion)

The distinction between the two report calls is not cosmetic. **A Fix is applied
unattended; a Suggestion needs a human to choose it.** Upstream draws the same line
with `fix` versus `suggest`, and it is part of what you are porting: shipping a
suggestion as a fix means the edit engine rewrites code upstream would only have
offered to rewrite. Read which one the rule uses before writing either. The engine
that applies them is `internal/edit/`.

**The `output` field in the corpus is the specification.** Assert with
`testing.ExpectFixedSource`, which compares the whole rewritten file rather than a
message id, so it catches a fixer that repairs the right span with the wrong text.

    testing.ExpectFixedSource(t, result, "a === b")

**And they are not extra coverage for a fixer. They are the ONLY coverage it has.**
This is the asymmetry to hold onto, because it is what makes skipping them feel safe:
a fixer that reports in the right place and repairs wrongly is *indistinguishable*, at
the message-id layer, from a correct one. Every id fixture stays green. The span
assertions stay green. The rule looks finished.

Measured, on `dot-notation`, which is worth stating concretely because the abstract
version of this argument does not survive contact with a deadline. The port passed all
69 of upstream's corpus verdicts, then failed **11 of 38** `output` fixtures on the
first run. Two defects, neither reachable by any id:

    5_000['prop']   ->  5_000.prop      wanted 5_000 .prop
    foo['bar']instanceof baz  ->  foo.barinstanceof baz

The first is a transcription failure: `DECIMAL_INTEGER_PATTERN` was written from what
the rule *looked like* it needed rather than read from `ast-utils.js:64`, and the real
one accepts numeric separators and a leading zero before an 8 or 9.

**The second is the one to remember.** It is not a wrong fix, it is a fix that produces
valid-looking source meaning something else. `foo.barinstanceof` is a plausible
identifier; a reviewer reading that diff sees nothing wrong, the file compiles, and the
behaviour changed. The missing piece was upstream's third yield, which inserts a space
when the repaired text would abut the next token.

So: if `meta.fixable` is set, the `output` cases are not a nice-to-have you add if there
is time. Porting the judgment without them ships a repair that runs unattended over the
whole tree with nothing able to say it is wrong.

**The decision to port a fixer at all turns on whether the repair is SPECIFIED, not on
whether fixers are frightening.** Two rules in one batch went opposite ways for the same
reason. `react/no-invalid-html-attribute`'s suggestions were declined because one of them
does a substring replacement on SOURCE text -- `value.raw.replace(value.value, '')` --
which repairs the wrong span whenever a bad token contains a good one as a prefix, and
nothing upstream pins that. `dot-notation`'s fixer was ported because 34 `output` cases
state exactly what it must write and `ExpectFixedSource` compares the whole file. Ask
which of those two situations you are in.

**`output: null` is a decision, not an omission.** It means upstream reports the case
and deliberately declines to fix it, usually because the repair would change
behaviour rather than spelling. Reproduce the decline. A fixer that repairs a case
upstream refuses to touch is a defect that no message-id fixture can see.

**A fix that is right and anchored wrong is worse than no fix.**
`ReportNodeWithFixes` anchors the finding on the node's own text, and its doc comment
says why this matters twice over: a finding reported at the wrong line while carrying
a fix means the edit lands somewhere the reader was never shown. Assert the span on
every fixable rule, not just the message.

**Read a fixable rule already in the tree before writing yours:**

    grep -rl ReportNodeWithFixes internal/lint/rules/

`internal/lint/rules/core/prefer_const.go` and
`internal/lint/rules/core/no_unused_labels.go` are both worth reading whole. They show
where the range comes from, how a multi-edit repair is assembled, and how the test
asserts the output.

**And when we decline a fixer upstream ships, say so at the line.** The tailwind rules
did this deliberately: a class literal in this codebase wraps across lines with
indentation that carries intent, and a fixer would be the first thing to reflow it.
That is a defensible decline and it is recorded where the next reader will look. An
undocumented missing fixer just reads as an unfinished port.

**Two harness facts that will look like defects in your rule.**

`/tmp` is a symlink to `/private/tmp` on macOS, so any comparison against a
`from: file` specifier path silently fails to match. Same hazard class as the
case-insensitivity warning and a different channel, and it presents as your rule
ignoring a configured allowance.

The test harness's tsconfig pins `moduleDetection: "force"`, which makes an ambient
`declare module` unresolvable. A porter's one upstream case using an `errors` package
could not be expressed because of this. They recorded it in its own test with two
controls rather than weakening the rule to make it green, which is the right shape: a
case you cannot express is a fact about the harness, and hiding it inside a relaxed
rule turns it into a fact about the rule.

## 3c. If the message interpolates anything, the ids cannot see it either

**This is 3b's argument arriving through a different door, and the pair is what makes it a
property of fixtures rather than a quirk of fixers.** There, message ids cannot see a
repair, so `output` is a fixer's only coverage. Here, a single message id cannot see a
RENDERING, so a name-and-span test is the only coverage a rule with an interpolated
message has. In both cases the fixtures are complete and the assertion layer is the wrong
one.

The trap is sharpest on a rule with exactly ONE message id, because then a fixture table
records only a count. `complexity` is the measured case: 165 imported fixtures, all green,
with three defects live and two more the corpus could not reach at all.

    an arrow assigned to a class field    rendered "Arrow function", upstream says "Method 'x'"
    an arrow in an object property        rendered "Arrow function", upstream says "Method 'b'"
    the span for a property's function     started at `function `, upstream starts at `c: function `
    a computed property key                counted inside the field's path, upstream counts it
                                           in the ENCLOSING function's
    a private method's name                rendered "Private method '#p'", upstream renders it
                                           UNQUOTED as "Private method #p"

None of the five moves a message id and none moves a count. The first three were found by
writing a table of expected NAME and SPAN per shape; the last two by mutation.

**Read `meta.messages` for placeholders, and treat each one as an assertion you owe.** A
message with `{{name}}` in it is a computed string, and the computation is usually a
helper large enough to have its own bugs: `complexity` interpolates a name built by
upstream's `getFunctionNameWithKind`, 77 lines assembling `static`, `private`, `async`,
`generator`, a kind word and a quoted name, plus a span from `getFunctionHeadLoc`. Its
corpus exercises **14 distinct renderings across 103 findings**, so the name is most of
what the rule computes and none of what its ids record.

**Test the builder DIRECTLY, not only through the rule's messages.** An end-to-end row
that fails tells you the rendering is wrong without telling you which of twenty branches
produced it, and a table keyed on the branch is what turns a red line into a location.
Doing that on `complexity` immediately found a fourth defect the end-to-end table could
not: `{ d: function named() {} }` renders `Method 'd'`, because upstream tries the
PROPERTY name first and falls back to the function's own `id` only when the property name
is not static. No corpus case writes a named function expression in a property position,
so nothing else could have caught it. The same table then exposed dead code, two accessor
arms in a branch only an arrow or a function expression can reach.

**And the parser difference cuts the other way here, so port the DECISION rather than the
dispatch.** ESTree wraps a method in a `MethodDefinition` around a `FunctionExpression`,
so upstream reads modifiers off the parent and the kind word off `parent.kind`. Measured,
our parser has no wrapper at all: `KindMethodDeclaration` is both the member and the
function. A reader porting those 77 lines faithfully will write parent-dispatch branches
this tree can never reach, and a mutation sweep will report them as survivors because they
are genuinely dead. The reachable version is smaller, and the arms that survive gutting
are the ones to delete rather than to document.

## 4. Check the shelf before writing any helper

The shared shelf is `internal/lint/ecmascript/` and `internal/lint/checking/` —
roughly a hundred non-test files across twenty packages. `ecmascript/` is subdivided
by question:

    binding      classmembers  comments   control_flow_graph   decorators
    high_level_intermediate_representation             imports  jsx
    literal      module        nextjs     property             react
    reference    regexp        regexpattern             regexsyntax
    scope        text

`checking/` holds the type-checker-facing helpers: `builtins.go`, `checker.go`,
`nodes.go`, `set.go`, `specifier.go`, `types.go`.

**Before writing a value-comparison helper, print the node's own fields.** Our parser
has often already done the work. A porter nearly wrote a numeric parser before probing
a `NumericLiteral`: its `.Text` already holds the canonical rendering of the double —
`0x10`→`16`, `1_0`→`10`, `1.0`→`1`, `1e21`→`1e+21`, `1e400`→`Infinity` — a bijection
onto the double upstream compares, and a `StringLiteral`'s `.Text` is the cooked
value. **The node is a shelf nobody thinks to search.**

**`comments.ForFile` is invisible to a search for AST helpers.** A rule whose subject
is trivia has no node kind to grep for, so the shelf's comment reader
(`internal/lint/ecmascript/comments/cache.go:19`) will not surface that way. Two
porters have now nearly reimplemented `scanner.GetLeadingCommentRanges` beside it.

**Search everything reachable, not only the shelf.** The `TypeScript-shim/` packages
expose the vendored compiler's own helpers by linkname and they are invisible to a
search of the shelf. A porter needing an edit distance nearly used
`core.GetSpellingSuggestionForStrings` — closest name on any shelf, reachable, and
**wrong**: it charges 0.1 for a case-only substitution and 2 for anything else, then
accepts within 0.4 of the name length. Measured, it returns suggestions for three of
upstream's own *passing* cases. Read the body of anything whose name matches your
question, wherever it lives.

Search the shelf, and search the rule package you are writing in, before adding a
function.

A name collision at build time means the decision already has a home; use the existing
one and read it first, because it is usually better than what you were about to write.

**Probe a shelf helper against UPSTREAM, not against its own doc comment.** The
failure mode is not "weaker than documented" — it is "different from upstream", and it
runs in both directions, **including the direction where the helper is MORE careful.**

`react.IsEs6ComponentClass` skips parentheses on the heritage receiver and its doc
advertises that skip as a correctness *feature*. It is a divergence:
`extends (React.Component)` is **silent** upstream and the helper answers true. That
is invisible from the doc comment, which is accurate about what the code does, and
invisible from the corpus, which writes no parens. A helper doing something sensible
that upstream does not do is still a defect in a port.

Both helpers a porter reached for on `no-this-in-sfc` were wrong opposite ways:
`react.IsLikelyComponentName` uses `unicode.IsUpper` where upstream tests ASCII only,
so `Фoo` and `Éoo` are silent upstream and true on the shelf; `react.IsEs5ComponentCall`
accepts `createClass` where upstream keys on `createReactClass` alone. Both live in
`internal/lint/ecmascript/react/`.

**A shelf function's doc comment is a claim, not a guarantee.** One agent found
`RegexCharElement.IsLoneSurrogate` documented as the astral-expansion mechanism with
**zero assignment sites**, validated against a control field that had three. Another
measured that `regexpattern.Walk` skips set escapes and does not recurse into
`v`-flag nested classes, contradicting what its name implies. **If a shelf function is
load-bearing for your rule, probe it on your own corpus before building on it**, and
report what you found either way. The shelf is younger than the rules it serves.
`TestShimFieldAccessorsReadTheFieldsTheyName` and the wrapped-accessor guard both
police this boundary from the other side.

**And the harder case: a shelf helper that is entirely CORRECT and answers a
different question than the one you are asking.** Every example above is a helper
that is wrong about something. This one is not, and that makes it worse: there is
nothing to find by reading it, because the code and its doc comment agree with each
other and both are accurate.

Measured on `@typescript-eslint/consistent-return`, whose void filter asks "does this
return type include `void`". The shelf has `checking.IsTypeFlagSet`, whose name is
exactly that question. It reads the type's OWN flags. typescript-eslint's
`isTypeFlagSet` first decomposes a union and ORs its constituents' flags, which is a
different question with the same name:

    void                     shelf T   upstream T   agree
    void | number            shelf F   upstream T   DISAGREE
    Promise<void | number>   shelf F   upstream T   DISAGREE   (on the awaited type)

Both disagreements are upstream PASSING cases — `function foo(flag?: boolean): number
| void` and a `Promise<void | number>` alias — so the shelf helper reports two clean
inputs. **Neither is a fixture you would think to add**, because both come from
upstream's valid list and a port failing two of nineteen valid cases reads as two
unlucky fixtures rather than as a wrong helper.

**The trap is reaching for it BY NAME.** The standard already tells you to search the
shelf before writing a helper, and that instruction is what delivers you to the wrong
function: you search for your question, you find a name that is your question, and
the match itself is the evidence you stop on. A name cannot encode which of several
readings of a question a function took, and the more natural the name the more
readings it covers.

So the rule that survives is narrower than "probe a shelf helper against upstream". It
is: **when a shelf helper's name IS your question, that is when to go read upstream's
implementation of the same-named helper and diff the two**, because a name collision
between the shelf and upstream is the one case where the collision is doing your
thinking for you.

**Write the divergent one local rather than fixing the shelf.** Correcting
`checking.IsTypeFlagSet` to decompose unions would silently change the answer for
every other caller, none of which asked for it, and the shelf helper is not wrong —
it is the other reading, which some of those callers presumably want. A local
`consistentReturnUnionFlagSet` with the measurement at the line costs one function and
moves nobody. Adding a second helper to the shelf beside the first is the option to
avoid: two shelf functions whose names differ by a word are how two callers end up
asking different questions believing they asked the same one.

> **On the shelf census.** Earlier revisions pointed at a `SHELF-CENSUS.md`, a
> whole-corpus read from 2026-08-23. It is not in this repository, and its "missing
> utilities" section was already stale in every entry checked when it was last
> consulted: `WritesToBinding` lives in `ecmascript/reference/write.go:100`,
> `EnclosingFunctionLike` in `ecmascript/scope/enclosing.go:45`, and the
> static-property-name decision it called missing shipped as `property.Name` /
> `property.NameTagged` (`ecmascript/property/name.go:82` and `:138`) rather than
> under the name it proposed. **Search the shelf by the question rather than by any
> census's proposed name**, because the name is the part that drifts.

## 4b. "Complete" is not a status you can report before the fixtures have run

Two authors paused describing their work as complete: compiling, gofmt clean, fixtures
written. When the next ran them, one had **two failing span assertions and seven blind
spots the sweep found**, and the other had **four rule defects and nine wrong
fixtures**, including twenty-two clean upstream cases being reported as violations.
None of it was visible from outside.

**Never describe work as complete before the fixtures have executed.** Say what
compiles, say what has run, and keep those separate. **If you inherit work described
as complete, run it first and expect failures.** A paused artifact that compiles is a
plausible artifact, not a verified one.

## 5. Write the fixtures first, from the corpus, verbatim

Before the rule. Every pass and fail case goes in **as written upstream**. This is
mandatory.

**Verify the copied strings mechanically, byte against byte.** Reading them is not
enough and this has cost three porters. One found four transcription errors its own
reading had passed over, the worst being a pass case whose `\u0020\u0020` had been
cooked into real spaces, so it would have sat in the clean list asserting the exact
opposite of upstream. Another had two fixtures pass for the wrong reason because a
heredoc collapsed `\\1` into `\1`. A third watched a mutant come back alive because
the tool writing its fixture cooked `\u0041` into `A`.

**The tool you use to write the fixture can change it, and the result still compiles
and still goes green.** Compare the bytes on disk against the bytes upstream, with a
script, not with your eyes.

**This reaches the RULE file too, not only the fixtures.** A porter wrote `String('')`
with two ASCII apostrophes into a Go doc comment and the file came back holding a
single U+201D. It compiles, `gofmt` passes, the tests stay green, and they only
noticed because the harness reported the file changed on disk.

**A second porter traced it to the editing tool.** `sed -i ''` on macOS converted a
pair of ASCII apostrophes in their Go source into a single smart quote, and the file
still compiled. Two porters, two different rules, same damage. Scan the whole file for
non-ASCII rather than grepping four characters, and run a control so a zero means
something:

    LC_ALL=C grep -n '[^\x00-\x7F]' <your file>     # should be empty
    LC_ALL=C grep -c '[^\x00-\x7F]' <a file you know has none>   # control

**`grep -c` returning 0 exits 1**, which breaks the `&&` chain this scan is naturally
written with — it reads as a failure when it is the success case. Use
`LC_ALL=C grep -c … ; echo` rather than `&&`.

The reason to copy at all: a fixture you invent encodes the same belief as the port
you are about to write, so it passes for exactly the reason the code is wrong.
Upstream's clean cases are the ones that catch you, because each was added when
somebody hit that bug.

Split them into a Fires test and a StaysSilent test. **Read
`internal/lint/rules/core/no_iterator_test.go` for the shape, not the rule file** —
the rule file will not tell you the assertion API and you will invent one that does
not exist.

The API, so you do not have to guess. The package is
`internal/lint/testing` (import it and call it `testing` or alias it; it is not named
`rule_testing` any more):

    result := testing.Run(t, YourRule, fileName, sourceText)                    // untyped
    result := testing.RunTyped(t, YourRule, fileName, sourceText)               // rules that declare NeedsTypeChecker
    result := testing.RunWithOptions(t, YourRule, fileName, sourceText, opts)
    result := testing.RunTypedWithOptions(t, YourRule, fileName, sourceText, opts)
    result := testing.RunTypedFiles(t, YourRule, files, subjectFileName)        // cross-file resolution
    testing.ExpectFindings(t, result, "yourMessageId")                          // one id per expected finding
    testing.ExpectClean(t, result)                                              // no findings; there is no ExpectNoFindings
    testing.ExpectFixedSource(t, result, wantSource)                            // applies the fixes, compares the result

`Run`, `RunWithOptions`, `ExpectFindings`, `ExpectClean` and `ExpectFixedSource` live
in `rule_testing.go`; every `RunTyped*` variant lives in `program.go` beside the
program cache.

The rule struct is `rule.Rule{Name, Run, NeedsTypeChecker, ReadsProgram,
ResolvesReactValueTypes}`. A porter once wrote a `Messages:` field from the shape of
other linters; it does not exist. Messages are `rule.Message{Id, Description}` values
passed at the report site.

**`Decode` is not on `Rule`.** It lives on `rule.Registration`, alongside
`RequiresOptions`, because `Rule` is what a rule author writes and a decoder is the
config layer's business:

    rule.Register(rule.Registration{
        Rule:   Eqeqeq,
        Decode: rule.DecodeOptionsInto[EqeqeqOptions](),
    })

If your rule takes no options, omit `Decode` entirely — that is the common case.

`Run` is `func(ctx rule.Context, options any) rule.Listeners`. **Every rule takes the
options parameter, including one with no option surface.**

**There is no `rule.OnExit` and the walk is pre-order.** Two porters reached for one
and neither found it. If your rule has to gather before it can judge, do both inside a
`KindSourceFile` listener: it fires before its children, so you can walk the tree
yourself, collect, and report at the end.
`internal/lint/rules/structure/react_component_no_multiple_primary.go` is the shipped
example, and `internal/lint/rules/core/no_class_assign.go` uses the same shape for a
declaration anchor.

`ExpectFindings` takes one id per finding, so an input reporting twice passes two ids.

Then add cases upstream does not cover, from reading **our** code, and say in a comment
why each exists.

**Never type an escape sequence on the way to a fixture.** Byte-verification catches a
cooked escape, but the reliable fix is upstream of it: build every source string from a
list of physical lines and emit it through `json.dumps`, so no `\n` is ever typed and
nothing can cook it. A porter's `\n` sequences became real newlines through a
Python-heredoc-through-Bash path, and their piecemeal repair then silently damaged a
second case because one placeholder was a **prefix of another**. Their verification
script also asserted *before* writing, which is why the failed run left the file
untouched. Both are the pattern.

**A passing case in the imported corpus is not evidence about the thing you think it
is.** The "read siblings for shape, never for semantics" hazard also arrives through
the **imported** corpus, where it is far harder to see because the case is genuine
upstream data.

Measured on `no-this-in-sfc`: upstream ships a passing `React.createClass` case that
reads as direct evidence the namespaced factory exempts. It is not. It passes because
its `render: function () {...}` is **anonymous**, so no name can be read, and removing
the factory entirely leaves it passing. A porter treating that fixture as evidence
about factory names would have concluded the exact opposite of the truth.

**Before you let a passing case teach you a rule, change the one thing you think makes
it pass and check the verdict moves.** A case that stays green under that edit was
never testing what its shape suggests.

**A SEMANTIC ERROR decides cases above the rule too, and it looks nothing like a
suppression comment.** A porter's most misleading passing case was
`export * from '_'; export = {};` — it reads as evidence that an export assignment
suppresses the rule beside a star export. It is not evidence about the rule at all:
that input is TypeScript error 2309, and the run stops before the rule fires. **The
tell is that the OTHER diagnostics vanish too**, which you only see by reading the full
output rather than grepping for your own rule name.

**A third fixture category exists: a case whose verdict is decided ABOVE the rule.**
`testing.Run` never consults `internal/lint/suppression`, so an upstream case that is
silent only because of an `eslint-disable-next-line` comment cannot be reproduced as
silent by a rule test. A porter following step 5 literally will either break their port
to green it or quietly delete the case, and both are wrong.

**Pin it at the layer that actually decides it.** Record the case as *reporting*, with
the reasoning at the line, and say in your report that the suppression layer is what
makes it clean upstream. The porter who hit this confirmed the mechanism by deleting
the disable comment and watching the same file report.

**Driving the installed rule does not dissolve this, and there the case arrives as a
DISAGREEMENT rather than as an annotation you can read.** A porter who replayed
upstream's whole corpus through the Linter API — the method this document recommends,
because it answers what the rule does HERE rather than what the corpus file claims — got
262 of 263 rows agreeing and one row where the port reported and the oracle did not. The
natural reading of a single disagreement against a measured oracle is that the port is
wrong. It was not: the row's source carried `// eslint-disable-line`, so what the oracle
had recorded was the suppression layer removing a finding the rule had already produced.

So the Linter API is an oracle for the rule PLUS everything ESLint layers above it, and a
rule fixture wants only the first. **Grep your corpus for `eslint-disable` before
trusting any row of it**, whether the verdicts came from annotations or from a driven
run — one line, and it tells you exactly which rows are about the wrong layer:

    grep -c "eslint-disable" tests/lib/rules/<rule>.js

Confirm each hit the same way: re-run that source through the oracle with the comment
deleted. If it reports, the rule's own verdict is *reporting*, and that is what the
fixture asserts.

## 6. Write the rule until the fixtures pass

Doc comment carrying valid and invalid examples, and a message whose Description says
why the code is wrong rather than restating the rule name.

When a fixture fails, read it before changing the rule. Upstream's clean cases encode
distinctions you have not thought of yet, and the failure is usually telling you one.

## 7. Prove the fixtures can see: the mutation sweep

    internal/lint/rules/tools/score_one_mutation.sh <file> <package> "<python expression rewriting the string `source`>" [TestName]

**Mutate every discrimination the rule makes**, one at a time. **A green suite proves
nothing until something has been shown able to turn it red.**

**The script takes an expression, not statements.** `import re; source = re.sub(...)`
fails with a bare `SyntaxError` that names nothing and reads like a broken script. Keep
the rewrite to a single expression. Separately, gofmt's alignment silently no-ops a
rewrite on the first and last entries of a `switch` case list (they end in `:` rather
than `,`), and map literals have the same hazard.

**The `./` on the package path is mandatory and its absence fails confidently.**
`score_one_mutation.sh x internal/lint/rules/core ...` reports that the package does
not compile, because Go resolves a bare path against the standard library:
`package internal/lint/rules/core is not in std`. Two porters hit this in one wave, and
the message primes you to go hunting a sibling's half-written file, which is exactly
the wrong place to look. Write `./internal/lint/rules/core`.

The tool refuses a rewrite that changed no bytes, refuses a mutant that does not
compile, and refuses to score at all unless the package is green before the mutation.
All three refusals are correct and **none is a pass.** Before the byte check existed, a
mutation changing one space inside a comment came back "caught by thirty-two lines".

**"Does not compile" is now a narrower message than it used to be, and this is worth
knowing.** Earlier revisions told you to read that refusal as a question rather than a
verdict, because the check was tree-wide and could not tell your broken mutation from a
sibling's half-written file. The script now runs `go vet` **on the package under test
only**, both for the baseline and for the mutant, and says so in its own output: "This
checked only $package, so the breakage is your rewrite rather than a sibling's file."
So the refusal is trustworthy about whose fault it is. The general habit still holds:
check whether the package compiles without your mutation before believing anything.

The fourth argument scopes both the baseline and the mutant to one rule's tests, which
is how you proceed when the package is held by other agents. **State the cost when you
use it:** a scoped run cannot see a guard in another package, so a mutation renaming a
rule reads as a survivor while the name guard in `internal/lint/registry` would have
caught it. Anything resting on a cross-package guard needs one unscoped run. The script
carries this warning in its own comments.

A sweep against a red package is not a weaker measurement. It is not a measurement. A
scoped sweep is a real measurement of a smaller thing.

**"Unscoped" does not mean "sees everything", and the difference has a measured
instance.** The fourth argument scopes the run to one rule's tests, and dropping it
widens the run to the whole PACKAGE — not to the tree. The tool builds and tests one
package either way, so any guard living elsewhere is invisible to both spellings.

Scored on `@typescript-eslint/consistent-return`, mutating the rule's `Name` string:

    scoped to TestConsistentReturn        SURVIVED
    unscoped over ./internal/lint/rules/typescript   SURVIVED
    go test ./internal/lint/registry/     CAUGHT, twice

Twice, because two different guards see it: `TestEveryRuleShipsAFixturePair` loses the
rule's test file, and `TestEveryRegisteredRuleIsReachableFromTheLiveConfig` reports the
renamed name as unreachable from the config. Neither lives in the rule's package and
neither can.

So the reflex "re-run it unscoped to be sure" does not close this gap. **Anything
resting on a cross-package guard is scored by running that package's suite against the
mutant directly**, which for a rename means editing the file, running
`go test ./internal/lint/registry/`, and restoring it. Say in your report which of the
two you did, because a survivor from a package sweep and a survivor from the registry
suite are different claims.

**A survival verdict can be a sampling artifact, so re-run a survivor before believing
it.** A porter's map-iteration mutant was caught 1 run in 8: Go randomises map order,
and with two entries it often preserves the original order by chance. A single run
reported "survived" and the honest answer was "survives most of the time". If your rule
iterates a map and the mutant reorders it, score it several times before writing the
equivalence argument.

### Mutation results are evidence about FIXTURES, not about the parser

Everything above establishes that a sweep proves a fixture set can see. Here is the next
thing, and it is a different claim: **a sweep proves nothing whatsoever about the
substrate.** Reading it as though it does produces a confident wrong belief about how the
AST behaves, and one that survives its own author's review, because mutation evidence
feels like measurement.

The shape first, because it generalises past the instance:

    a SURVIVING mutant   your fixtures cannot discriminate this change
    a CAUGHT mutant      your fixtures can discriminate this change

Both are statements about the discriminating power of a corpus. Neither is a statement
about what the parser returns, what a node kind contains, or what a helper answers. The
only instrument for those is a probe that asks the parser and prints what comes back.

**Measured, and the author had it backwards for two commits.** A rule needed a private
class member's name spelled the way `exceptMethods` entries are written, `#foo`. Upstream
builds it as `hashIfNeeded + name` because ESTree's `key.name` omits the hash, so the port
guarded with `if !strings.HasPrefix(text, "#")`. Two mutations were then run:

    remove the guard entirely          SURVIVED
    add the hash unconditionally       CAUGHT by 7 assertions

The author read that pair as "the condition is dead and the body is load-bearing,
therefore `Text()` omits the hash", simplified the code to `"#" + name.Text()`, and broke
two fixtures. A five-line probe rule printing `name.Text()` for a private identifier
answered `"#foo"`: the hash was already there, the guard was correct, and its condition is
simply always true in this tree.

**Both mutation results are consistent with either belief**, which is why the pair could
not settle it and why the wrong reading felt supported. The survivor says the fixtures
cannot see the guard removed; that is equally true whether the guard is redundant or
whether removing it happens to leave behavior unchanged for another reason. The caught
mutant says the fixtures can see a doubled hash; that is equally true whether `Text()`
supplies the hash or the code does.

So when a sweep tempts you into a claim of the form "therefore the AST does X", stop and
write the probe. It is two minutes, it cannot be misread, and the alternative is a
simplification that compiles, reads better, and is wrong.

**The corollary for equivalence arguments.** A survivor you intend to write off as
equivalent needs a reason stated in terms of the CODE -- this branch is unreachable, these
two expressions compute the same value -- and that reason has to be established
independently. "It survived, so it must be redundant" is the inference this section exists
to stop. Elsewhere in this document a porter emptied two arms of `default-param-last`,
watched both survive, and found the arms genuinely reachable because the parser recovers
from illegal source. Same lesson from the other direction: the mutant was right that the
fixtures were blind, and wrong about why.


## 7b. If the rule's judgment is a matcher, the corpus is not enough

**A corpus tests a matcher only on inputs upstream thought of.** That sentence is the
whole section, and it is a different claim from the mutation sweep's. The sweep proves
your fixtures can SEE. This is about what they are pointed AT: a fixture set can be
demonstrably able to fail and still be blind, because every input in it is well-formed.

Measured on `no-restricted-imports`, whose `patterns` option matches a module specifier
with the `ignore` npm package — gitignore semantics, which nothing in this tree had, so
the port had to reimplement them. The differential was built the way this document asks:
every `group` configuration in upstream's corpus crossed with every module specifier
appearing in it, driven through the real `ignore` package. 864 pairs, 90 of them
matching, so neither degenerate answer could pass.

**It passed 864 of 864, the rule passed all 263 corpus rows, and three genuine defects
were still live:**

    a `./` trim on the candidate    made ["foo/bar"] match "./foo/bar"      upstream: false
    a `./` trim on the pattern      made ["./types"] match "types"          upstream: false
    `*` allowed to match nothing    made ["foo/*"] match "foo/" and         upstream: false
                                    "foo//bar"

None of the three is reachable from the corpus, and the reason is structural rather than
bad luck: **every specifier upstream wrote is a well-formed module name.** No corpus case
imports from `"./foo/bar"`, or `"foo/"`, or `"foo//bar"`, so no corpus case can separate a
matcher that is right about them from one that is not.

**The second instrument is the same cross product against inputs the corpus never
contains.** Take the configurations you already have and cross them with adversarial
inputs: a leading `./`, a trailing separator, a doubled separator, `../` and `../../`, a
bare `.` and `..`, a leading `#`, a trailing dot, a case-flipped name, the empty string.
540 more pairs, 76 matching, and all three defects fell out at once. Both tables now live
in the rule's test.

The general shape, for any rule whose verdict comes from a matcher rather than from the
tree:

- The corpus proves you agree with upstream **where upstream looked.**
- The adversarial cross product proves you agree **where it did not.**
- Neither is optional, and the second is the one that finds things.

This applies wherever a port reimplements a library rather than reading the AST: a glob
matcher, a regular-expression translation, a path normaliser, a name-mangling scheme. The
corpus tests the RULE. It does not test the library, because upstream never had to.

**The third defect is the one worth reading twice, because re-deriving it from first
principles gets it wrong the same way.** The bug was `foo/*` matching `foo/` and
`foo//bar`, which reads exactly like `*` needing to be one-or-more instead of
zero-or-more. It is not. Measured against the installed `ignore`:

    ["f*"]     against "f"          TRUE     `*` really is zero-or-more
    ["foo/*"]  against "foo/"       false
    ["foo/*"]  against "foo//bar"   false

So `*` is zero-or-more and correct as written, and the real rule is that **an empty path
SEGMENT never matches.** The constraint belongs on the segment, not on the wildcard, and
"fixing" the wildcard breaks `f*` while appearing to fix the symptom. Write the comment
that says so at the line, because the next reader's instinct will be the wrong one and
the fix that follows from it passes the failing case.

## 7c. Your fixtures never cross the config boundary, so they cannot test your decoder

**Every fixture reaches your decoder with bytes the TEST built. The config layer builds
different bytes.** That sentence is the whole section, and it names a blindness neither
of the two instruments above can reach.

The mutation sweep proves your fixtures can SEE. The adversarial cross product proves
they are pointed at enough INPUTS. **Neither can see this one at all**, because the
defect does not live in the rule or in its corpus. It lives on the boundary between the
config layer and the rule, and a fixture never crosses that boundary: it hands the
decoder a literal it wrote itself, so the decoder is exercised on the shape the fixture
author already believed in.

The mechanism is one line. `internal/lint/configuration/configuration.go` parses a rule
setting as `["severity", <options>]` and keeps exactly one element:

    setting.Options = tuple[1]

ESLint does not. `context.options` is **every** element after the severity, so an
upstream rule with more than one option reads a variadic list where cohere reads a single
JSON value. Any rule whose upstream option surface is variadic is therefore configured
differently here, and its decoder has to accept the cohere shape or it is wrong on every
real config while being right on every fixture.

Measured, on two rules ported together, and the pair is the point because **they fail in
opposite directions:**

    id-denylist   ["error", "data", "err", "cb"]              upstream's variadic spelling
                  decoder receives  "data"                     a bare string, not a list
                  -> rule configuration: rule id-denylist: decoding []string:
                     json: cannot unmarshal string into Go value of type []string

    id-match      ["error", "^[a-z]+$", { "properties": true }]
                  decoder receives  "^[a-z]+$"                 the flag object is GONE
                  -> no error. The pattern arrives, the rule runs, reports, and looks
                     configured, with every option silently off.

**The loud one is the lucky one.** `id-denylist` refused to start, which is a failure
nobody can ship past. `id-match` is the shape this repository exists to refuse: a rule
that registers, passes a complete fixture pair in both directions, appears in `--rules`,
and enforces something other than what the config says. Its corpus has 100 cases and 52
of them carry `properties: true`; every one still passed, because every one reached the
decoder through the test's own bytes.

**Scored, rather than asserted**, because the first draft of this section claimed the
blindness without measuring it and was wrong about which mutant demonstrates it. Restoring
the original defect -- a decoder that understands only upstream's flat tuple and not the
nested shape a config delivers -- gives:

    against the 100 imported corpus rows          SURVIVED
    against the same rows plus the contract test  CAUGHT, 4 failing lines

`id-denylist` scores the same way on its own defect, and the symmetry is the point: the
loud failure and the silent one are one blindness, and the noise was luck rather than a
difference in kind.

    against the 143 imported corpus rows          SURVIVED
    against the same rows plus the contract test  CAUGHT, 4 failing lines

That is the section in two measurements. The corpus cannot see it and a table written
specifically for the boundary can, so the boundary needs its own table.

**The instrument is a dry run against the real tree, through the real config layer**, and
it is the only one that sees this. Not `go test`. Not the sweep. Seed a small tree with a
config that actually exercises your options, and read what comes back:

    <scratchpad>/probetree/CohereSettings.json     {"rules": {"<your rule>": ["error", <options>]}}
    <scratchpad>/probetree/tsconfig.json           the harness tsconfig, copied
    <scratchpad>/probetree/source/Probe1.ts        source that must report
    <scratchpad>/probetree/source/Probe2.ts        source that must not

    cd <scratchpad>/probetree && <your binary> --no-fix --lint

The second file is not optional. A configured rule that reports on everything and one
that reports on the right things look identical from the first file alone, and a decoder
that silently dropped your options usually still reports SOMETHING.

Then write the contract into the test suite, because a dry run is a thing you did once
and a fixture is a thing that keeps being true. The rows to pin are the shapes the config
layer can actually deliver:

    ["error", ["data", "err"]]      the cohere spelling: one options value after the severity
    ["error", "data"]               upstream's variadic form, collapsed to its first element
    ["error", []]                   explicitly empty
    a shape that is neither         must ERROR, never decode to an empty configuration

That last row matters more than it looks. A decoder that answers "no options" to a
malformed config produces exactly the inert rule this section is about, one layer further
in. Refuse instead.

**The general form, for any rule with an option surface:** upstream's spelling of its
options is not necessarily the spelling this tree can hand you. Check `meta.schema` for
how many elements it declares, and if the answer is more than one, your decoder has a
second shape to accept and your fixtures cannot tell you whether it does.

### A presence check followed by a truthiness fallback is a family, and a written zero splits it

Not one rule's quirk. Found in `max-depth` and `complexity`, and the shape is common
enough in ESLint's option handling that the next rule carrying it should be expected
rather than discovered:

    if (typeof option === "object" && (hasOwn(option, "maximum") || hasOwn(option, "max")))
        threshold = option.maximum || option.max;

The guard tests PRESENCE and the `||` tests TRUTHINESS. Those agree on every value except
zero, and on zero they do something a reading of either half alone does not predict.
Measured against the installed rules, reading each limit out of the rule's own message
rather than inferring it from a count:

    2                        limit 2      the ordinary spelling
    0                        limit 0      a bare zero reports everything
    {"max": 0}               limit 0      `undefined || 0` is 0, so the zero WINS
    {"maximum": 0}           SILENT       `0 || undefined` is undefined, and every
                                          `count > undefined` is false, so the rule
                                          reports NOTHING at all
    {"max": 0, "maximum": 3} limit 3      the `||` consults maximum first
    {}                       the default  neither key present, so the branch is never
                                          entered: 4 for max-depth, 20 for complexity

The same six rows hold for both rules, checked on each rather than generalised from one.
**And the last row carries a trap of its own worth naming**, because it caught this
document: probing `{}` against source that does not exceed the DEFAULT reports nothing,
which reads exactly like the silencing row above it. Two rows that mean opposite things
produce identical output unless the probe input is chosen to exceed the default. Use
source past the threshold you are testing, or the measurement quietly merges two cases.

**The two zero spellings do opposite things**, and one of them disables the rule entirely.
That is not expressible as an integer threshold, so a decoder needs either a flag or a
sentinel; collapsing the rows into "zero means zero" is wrong on both.

**The mechanical tell, so this is recognised rather than rediscovered:** a presence check
gating a truthiness pick means a written zero and an absent option take DIFFERENT paths.
Any decoder that treats "absent" and "zero" as the same thing is wrong on at least one
row, and whether a fixture catches it is luck. `max-depth`'s corpus happens to carry both
zero spellings, which is the only reason the first draft of its decoder was caught.
`complexity`'s carries neither, and the rows are pinned there by hand from measurement.

So when a decoder's upstream is `hasOwn(...)` then `a || b`, write the option table out and
drive the installed rule once per row before writing any Go. It is six inputs and it
settles what no amount of reading the expression will.

## 7d. A screen for an ABSENT thing is most confident when it is broken

**An audit's zero cannot tell a clean tree from a rule that cannot fire here.** Several
rules carry the verdict "Yes, violations: none" while being structurally incapable of
reporting in this tree, and the count reads identically either way. The two causes look
the same from outside and are completely different:

    the rule is configuration-driven      it reads its OWN `context.options`, which the
                                          config layer hands to rule.Context. Port it;
                                          the zero means "nothing is configured yet".
    the rule reads a SHARED setting       it reads `context.settings.*`, which
                                          rule.Context has no path to and which ahra
                                          configures nowhere. That arm cannot work here.

`react/prefer-exact-props` is the second kind, measured independently by two agents weeks
apart with identical results: all 12 of its reporting cases go silent with no settings,
because `getExactPropWrapperFunctions(context)` is the only input its reporting branch
has. The ruling is at `internal/lint/rules/react/forbid_prop_types.go:158-183`.
`id-denylist` and `id-match` are the first kind. Both zeros looked the same in the audit.

**The obvious screen is to grep the rule file for `context.settings`, and it is broken.**
This is the part to read twice, because a reader who re-derives the screen will write the
broken one:

    grep -cE "context\.settings" .../rules/id-denylist.js             0    correct
    grep -cE "context\.settings" .../rules/forbid-prop-types.js       0    WRONG

`forbid-prop-types` reads that setting **indirectly**, through
`eslint-plugin-react/lib/util/propWrapper.js:21`:

    return new Set(context.settings.propWrapperFunctions || []);

So it passes a direct scan clean and then registers a port that cannot fire. Run that
screen across a queue and you get a confident clean sweep over exactly the rules it
cannot see.

**And this is the general hazard, not a detail about one plugin.** A grep can only find
the names you thought of. That is the same failure as a corpus containing only the inputs
upstream thought of (7b), one level out: in both cases the instrument's blind spot is
shaped like the author's imagination, and in both cases the output of a blind instrument
is a clean result. **A screen looking for an absent thing gives its most confident answer
when it is broken**, because "found nothing" is simultaneously the success signal and the
failure signal.

**The executable check needs no guess about which helper name to look for.** Drive the
INSTALLED rule twice over every corpus case, once with a settings block and once without,
and compare. A rule whose reporting path reaches `context.settings` moves; one that reads
only its own options does not. Measured across three instruments on the two rules above:

    the rule file names a shared setting          0        0        control: 0  (missed it)
    its transitive imports do                     0        0        control: 1  (found it)
    corpus cases that move with a settings block  0/145    0/100    control: 0 -> 1

Only the third needs no guess, and only the third is worth running across a queue.

**The refusal is the load-bearing piece, not a nicety.** Require a control that MOVES
before believing a subject that does not, and make the probe exit non-zero when the
control stays flat. Without it a flat subject result is a number rather than a
measurement, and the two are indistinguishable in the output:

    if (controlMoved === 0) {
      console.log('REFUSING: the control did not move, so this probe is not measuring ' +
                  'a settings dependency');
      process.exit(1);
    }

That refusal fired twice while this was being built, and both causes were the instrument
rather than the rules:

    a settings block of the wrong SHAPE    propWrapperFunctions wants
                                           [{property: 'exact', exact: true}], and a bare
                                           string array leaves the control silent
    a hand-written control case            it never triggered the rule at all

Both were fixed by taking the settings block and a reporting case **verbatim from the
control rule's own corpus** rather than inventing them, which is step 3's rule applied to
the instrument instead of to the fixtures. Only then did the control move 0 -> 1 and the
subjects' flat 0-of-245 become a result.

**So the screen, in order:** read `meta.schema` for the rule's own option surface, then
drive the installed rule with and without settings over its whole corpus, with a control
that is known to depend on settings and is required to move. If any part of a rule's
reporting path reaches outside `meta.schema`, that arm cannot work here, and saying so
with the measurement is a better outcome than a port that registers and never fires.

## 8. Assert spans, not just message ids

**`ExpectFindings` asserts message ids and count and nothing else.** A rule whose
defect is where it points, or what it rewrites, passes a complete fixture pair while
being wrong.

**Assert the span of every rule, whether or not it carries a repair.** An author
derived file positions from a string literal's cooked value while the file holds raw
text (`"[á]"` is six bytes raw and four cooked), so every finding pointed at the wrong
place. All 187 verbatim fixtures were green, and the rule carried no fix at all, so
gating span checks on "does it have a fix" would have shipped it. Slice the source with
the finding's own range and compare the text:

    reported := source[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]

**When two upstream implementations disagree about where to point, the span is the only
fixture that records which one you ported.** One rule had two plausible spans, with an
identical message id. Every id fixture stayed green over the wrong choice.

**Assert the message text exactly.** An author's rule rendered `'##unusedField'` with a
doubled hash, because a private name node's `Text()` already carries the `#`. The
fixture used `strings.Contains(got, "#forgotten")`, and a two-hash string contains a
one-hash needle, so it stayed green while the message was wrong. The general form is
worth more than the instance: **a fixture whose predicate is weaker than the property
it guards is not a guard.**

**If the rule carries a fix, assert it by applying the repair and comparing the
resulting source**, never by comparing the fix's text. A fix writing the right string
over the wrong span passes a text comparison, and that is a real defect we shipped.

**Route option fixtures through the rule's own exported decoder**, not by building the
options struct directly. That is what puts a default inversion or a serde alias under
test, and those are the two lines most likely to have no upstream counterpart.

### If you score a rule in two directions, pin both

A rule measured against a corpus usually gets two numbers: how often it fires where it
should, and how often it fires where it should not. **Pinning one exactly and asserting
only `> 0` on the other is not two instruments. It is one instrument and a formality.**

The asymmetry runs one way, which is why it survives review. A change that trades true
positives away for false-positive removals moves the pinned number in the direction
that reads as success, while the loose side stays green because it is still non-zero.

Measured, on `preserve-manual-memoization`: freezing component parameters in the
aliasing graph took the clean-fixture rate from 31 of 70 to 19, a 39% improvement
against the number that rule was being judged on. The golden count went from 15 of 33
to 9 in the same run. Six programs upstream reports on had gone silent underneath a
result that looked like progress, and the author had the 19 and was composing the
commit message before checking the other side.

So: assert both counts exactly, and make each failure message say which direction means
what. "This went down, so either the rule improved and you lower it deliberately, or it
stopped reporting things upstream reports on" is the sentence that does the work. A
ceiling cannot distinguish a fix from a rule that went quiet.

### A narrow assertion throws away corpus coverage you already have

Two mutants survived a sweep not because the corpus lacked the cases, but because the
assertion only checked the message PREFIX. The union text in the message was never
compared, so the rule could print anything after the prefix and the fixtures stayed
green. Widening the assertion to the whole message killed both mutants immediately,
with no new fixtures.

That is the cheapest coverage in existence: it was already paid for and being discarded
at the last step. Before adding a fixture to kill a survivor, check whether the fixture
you have is being fully read.

The same sweep then found two real defects behind that gap: a parenthesised type was
not unwrapped, so `(/* before */ string /* after */)` printed the author's parentheses
AND the comments inside them instead of `string`; and a parenthesised union counted as
one member instead of flattening.

**Assert the whole message, not its prefix, whenever the message carries computed
text.** A message id proves which branch fired. The text proves what the rule computed,
and that is usually the part with the bug.

## 8b. If your rule anchors on a declaration and looks for writes to it

The shape is: resolve the declaration's symbol once, then for each candidate identifier
resolve it and LOOP over `symbol.Declarations` looking for the anchored declaration by
NODE IDENTITY. Never index `[0]`.

**Three traps, all of which cost real work and none of which the imported corpus can
catch.**

**Identity alone over-reports.** `A.x = 0` and `foo(A)` both resolve to the anchor and
neither reassigns it. You need structural write detection as well, and report only
where both agree. `reference.WritesToBinding`
(`internal/lint/ecmascript/reference/write.go:100`) is the shelf's answer to the second
half.

**Comparing declaration KIND instead of identity survives the entire upstream corpus.**
A kind mutant passed all 25 upstream cases plus thirteen invented ones, because
upstream's only same-kind pair has a write that resolves to nothing and exits early.
The distinguishing input is a nested redeclaration:

    class A {} { class A {} A = 1; }        two anchors, same kind, identity reports once

Write the equivalent for your declaration type, and mutate to confirm it catches.
Without it the trap ships and nothing tells you.

**This trap is not confined to declaration-anchor rules.** It bit a flow-analysis rule
for **223 false positives on the real tree**, fifty in one file, while all 71 imported
clean cases stayed green. Any rule that resolves an identifier through the checker can
hit it.

**A shorthand destructuring target needs a different accessor.** `({A} = 0)` resolves
through `GetSymbolAtLocation` to the *property's own* symbol, which reads exactly like
a correctly-declined shadow. Use
`ctx.TypeChecker.GetShorthandAssignmentValueSymbol(identifier.Parent)`, which lives in
the vendored compiler's `checker/services.go` rather than `checker.go`, so a grep
scoped to the wrong file reports it absent.

**Cost is settled: dispatch freely.** The per-file checker lock measured `setup` 0.52ms
for a class anchor against 0.51ms for a much rarer anchor over identical files. It is
flat and does not scale with what the rule does with the checker.

## 9. Register and enable

**Write `<your_rule>_register.go`. Do not open the package's `register.go`.**

    // internal/lint/rules/core/eqeqeq_register.go
    package core

    import "github.com/system-inc/cohere/internal/lint/rule"

    func init() {
        rule.Register(rule.Registration{Rule: Eqeqeq})
    }

    // with options:
    //   rule.Register(rule.Registration{Rule: Eqeqeq, Decode: rule.DecodeOptionsInto[EqeqeqOptions]()})

`rule.Register` appends into a map keyed by name and is callable from any file's
`init`, so a package may hold as many of these as it has rules. This is what makes a
port **three new files and no shared edit**: the rule, its test, and its registration.
Two authors landing at once touch nothing in common. There are over two hundred
`_register.go` files in the tree; this is the live convention, not a suggestion.

The `register.go` still present in most packages is a historical list, not a
requirement. Adding to it works and costs you a conflict with every other author in
your package, which is the whole reason not to. A duplicate name panics at startup
rather than one rule silently losing, so a collision is a crash in the first second
rather than a gap nobody measures.

Then add the rule name to `/Users/kirkouimet/Projects/ahra/CohereSettings.json` in the
rules block. **Not `.oxlintrc.json`**, which is oxlint's own config and which cohere no
longer reads. The live guards name `CohereSettings.json` at
`internal/lint/registry/live_wiring_test.go:12` and `internal/lint/configuration/live_test.go`.

**Grep that file for your rule name first.** Many rules are already enabled there.
Adding a second entry produces valid JSON with a duplicate key, one silently wins, and
nothing complains. If it is already present, that step is done.

> **`rule-inventory.json` is gone.** Earlier revisions of this document devoted most of
> this section to it: how to edit it as text rather than round-tripping it through a
> JSON library (a re-serialise silently reformatted all 2,156 lines from spaces to
> tabs), how to keep its `total` / `byNamespace` / `bySeverity` / `byEnabledBy` headers
> consistent with `TestInventoryCountersMatchItsOwnRules`, how an entry had already
> been lost to a concurrent write and gone unnoticed for two waves, and how to
> re-read the file afterwards to confirm your entry survived.
>
> None of that applies now. The file was deleted and its guards with it. What survives
> is the reasoning, and it generalises to any shared tracked file: **a shared file
> edited in place by several agents loses entries silently, a round-tripped
> serialisation produces an unreviewable diff that passes every check, and a derived
> counter that nothing asserts is a number that decays.** Prefer a per-rule file to a
> shared list wherever the choice exists, which is exactly why registration moved to
> `<rule>_register.go`.
>
> Where you used to ask the inventory a question, ask the binary: `--rules` lists what
> is actually registered, and `--lint` names every config key that resolves to nothing.

**Done means enforcing.** A rule registered and enabled nowhere passes every fixture
and lints zero files. `TestEveryRegisteredRuleIsReachableFromTheLiveConfig` catches
this, and it is the one you are most likely to skip, because your own `_test.go` is
green and that feels like the answer. It is not. Your fixtures prove the rule is
correct; only the live config proves it is reachable.

**Seeing `ctx.scoping()` upstream is not evidence you need the checker.** That table
answers two different kinds of question and only one of them costs anything:

    scope flags        is_set_accessor, is_function, is_static, is_private
                       properties of the enclosing construct. The AST already knows.
                       No checker.
    name resolution    which declaration does this identifier bind to
                       needs the checker. `resolvesToAGlobal` in
                       no_new_native_nonconstructor.go is the shipped example.

**But `resolvesToAGlobal` is a trap for any rule about `undefined` or `globalThis`.**
It returns false when `len(symbol.Declarations) == 0`, and the real `undefined` global
has zero declarations — so it answers **false on every case such a rule must report**.
An author following this exemplar ships a rule that is silently inert and whose
fixtures all pass, which is the worst combination available. The guard is
`symbol == nil || len(symbol.Declarations) == 0`. For `undefined`, `window`, and
`globalThis` alike, the correct predicate is the **complement** — ask whether the name
is declared in source, and treat "not declared" as the global. One rule had to report
every `globalThis.RegExp` case and silently reported none for exactly this reason.

`no-setter-return` reaches for `ctx.scoping()` and asks it only the first kind, so it
declares nothing. An author nearly declared the checker on the strength of seeing the
call. **Determine which one your rule asks, by probe rather than by reading the call
site**, and say in your report how you determined it.

**If your rule declares `NeedsTypeChecker`, two things bite here.**

Your fixtures must use `testing.RunTyped` rather than `testing.Run`. The plain harness
hands the rule a nil checker.

**What happens next depends on whether the rule guards.** Measured both ways: `no-eval`
guards, so it goes *completely silent* — every StaysSilent case passes vacuously and
every Fires case fails in a way that looks like a rule bug. A rule that dereferences a
checker result **without** a nil check may panic and take the whole run down — **but
the shim tolerates more than you would expect.** Measured: `GetSymbolAtLocation` on a
nil checker returns nil rather than crashing, so for that call the failure mode is
**silence**, which is the more dangerous of the two. Keep the guard regardless; just
know that a missing guard often buys you a vacuous green rather than an obvious crash.
An author found their own rule doing exactly that, and a sweep of the tree found
**three shipped typed rules with no guard at all**.

And the registry guard is per-file textual: it looks for a `.TypeChecker` selector in
the rule's own file. If you reach the checker only through a shelf helper in another
file, the guard reports that you over-declared. That message is wrong and the
declaration is right.

## 10. Run it dry against the real tree

    go build -o <your scratchpad>/cohere-<yourname> ./command/cohere
    <your scratchpad>/cohere-<yourname> --no-fix --lint --timing 2>&1 | grep <your-rule>

**`--no-fix` is not optional, and `--fix` is not what causes writing.** The fix phase
runs by default, so a bare `cohere --timing` mutates the tree. A seeded probe file was
rewritten this way. `--no-fix` mutates nothing; `--lint` also skips the type phase,
which you do not need for a rule timing and which costs a second.

This tells you three things fixtures cannot: whether it fires on real code and whether
those findings are right, what it costs (one rule was 64.5% of all rule time because it
rebuilt a map per file), and whether it explodes on files larger than a fixture.

**A zero is three different things and the file count does not separate them.** An
author saw thousands of files and "registered no listener on any of them", which read
as inert and was not: the tree's tsconfig includes only TypeScript extensions and their
rule declines JavaScript, exactly as upstream does. Two moves resolve it: compare
against a control rule offered the same files and known to be in the same state, and
build a small tree that should trigger the rule.

The run's own output separates the two cases for you, and the wording is deliberate:

    note: rule @typescript-eslint/no-base-to-string was offered no files — nothing
    wired it, so its silence says nothing about the tree

**A non-zero registration count with zero findings is the positive tell that a rule
works.** `was offered no files` is the failure signal. That separates "inert" from
"correctly declining" in about ten seconds without building a probe tree.

**Except for a rule with a decoder, where it is not.** An author's rule reported 3,407
registrations over 3,407 files and was completely broken. A rule configured as bare
`"error"` is handed **nil** options: `rule.DecodeOptionsInto` errors on empty input,
the options registry turns that into nil for a non-required rule, and `options.(T)` on
nil yields the zero value. Every fixture reached the rule through the decoder, so
nothing in the suite could see it, and 21 of 21 imported cases passed. **Give your rule
an explicit nil-options fallback and a fixture that bypasses the decoder.** If the rule
genuinely cannot work without its options, set `RequiresOptions: true` on the
registration, whose doc comment records the inert-rule defect that field exists for.

**A probe tree's config must be named `CohereSettings.json` and puts rules at the JSON
top level.** A wrong filename is worse than a wrong shape: cohere exits before linting
and prints nothing your grep will catch, so you get a zero that was never a
measurement. An author seeded `{"cohere":{"rules":...}}` and got `was offered no
files`, which reads exactly like the inert case. The shape is
`{ "rules": { "react/your-rule": "error" } }`.

**Name probe files by index, never by the identifier under test.** macOS has a
case-insensitive filesystem, so `d_getStaticProps.tsx` and `d_getstaticprops.tsx`
collapse into one file and the last write wins. An author measured a case-only change
as "does not report" and nearly recorded a rule-level divergence; their control fired,
which is the only reason they went looking. This produces a confident **wrong**
measurement rather than a zero.

**Unless the rule gates on the FILENAME, in which case index-prefixing IS the identifier
under test.** An author's `h1_document.tsx` probes silently failed upstream's
`starts_with("_document.")` and all twelve came back with the wrong message arm — a
coherent-looking result, not an error. **When a rule reads the path, vary the DIRECTORY
and keep the basename exact.**

**A path-bearing option specifier makes the FIXTURE FILENAME part of the corpus.** An
author's imported clean case failed and the rule was right: a `from: file` specifier
with a `path` compares against the **declaring file's absolute name**, so that case is
clean only because upstream named its fixture `file.ts`, while three reporting cases
depend on the same comparison failing for `tests/fixtures/file.ts`. Varying only the
filename reproduced all three rows.

**A panic in your rule costs every rule that file.** The walk recovers per *file*, not
per rule, so one nil dereference does not lose your verdict on one file — it takes the
whole file away from every registered rule. A single unguarded
`ast.SkipParentheses(memberAccessObject(callee))` in `no-alert` crashed **167 files,
about five percent of the tree**, and the run still printed green: crashed files are
named in the output but a summary line reading `lint: 126 findings` looks identical
either way.

So grep the crash line before you believe a clean run:

    cohere --lint 2>&1 | grep -c "crashed:"     # must be 0

Two things make this class hard to find from fixtures. **`ast.SkipParentheses`
dereferences its argument**, so any helper that can answer nil is a panic one call
later — `memberAccessObject` answers nil for every callee that is neither a property
nor an element access. And **the shapes that reach it are ordinary**: `super(...)` and
`import(...)` are call expressions whose callee is a bare keyword, and every subclass
constructor carries a `super()`. Two independent authors hit this same class in one
wave, in different rules. `TestAPanickingRuleLosesOnlyItsFile` and the crash corpus in
`internal/lint/registry/` are the standing guards.

**Where a nil check is unnecessary, say why at the line.** `no-useless-call` and
`prefer-spread` both take the identical shape and are safe by a gate established
fifteen lines earlier. Both carry a comment naming the invariant *and* the consequence
of widening it. That is the difference between a guard someone deletes as dead and a
guard someone keeps.

**And a fixture that passes against the unfixed rule proves nothing.** The `no-alert`
fixtures never reach the nil path in the rule harness; the proof was an A/B on the real
tree, same flags, differing only in that file — 167 crashed before, 0 after. When your
unit test cannot reach the defect, say so in the test comment and name what did prove
it, rather than letting a green suite imply coverage it does not have.

## 10b. A prebuilt binary is probably not yours

**This has been the highest-cost trap in this setup, and the first author to hit it
nearly drew the wrong conclusion from it.**

The ahra tree resolves a linter binary through `node_modules/.bin/`, and that is a
symlink to a binary somebody built earlier. Rebuilding into your own scratchpad does
not update it. So the sequence that looks like a wiring failure is:

    go build -o <scratchpad>/cohere ./command/cohere   # your rule is in this
    <scratchpad>/cohere --lint probe.tsx               # reports, N rules
    <the node_modules one>                             # 0 findings, N-2 rules

**The intermediate state is coherent, which is what makes it dangerous.** The gap moves
by exactly one or two, which reads as evidence that your rule has a false negative
rather than as evidence that two different binaries answered.

**Do not stop at recognising the path.** Two authors in one wave found that symlink
pointing at a binary sitting *inside their own session's scratchpad directory* which
they had never built. The path looks like yours, which is precisely what disarms the
check — you glance at it, see your session id, and conclude you are running your own
build. Compare the rule COUNT instead, which cannot be faked by a familiar path:

    <that binary> --rules | wc -l          # what the tree actually runs
    <your binary> --rules | wc -l          # what you built

**And a small gap is more dangerous than a large one.** 224 against 248 reads as "my
two new rules plus some noise," which feels roughly right. It was 24 apart because a
dozen sibling agents had landed ports since that binary was built. A gap of two would
have been more convincing and just as wrong.

**Your own freshly built binary goes stale too.** An author chased a phantom
false-positive class for a while: an hour-old build reported substituted templates
(`${tag}`) that the current rule declines. A seeded probe disagreed with the saved log
and settled it; the dry run went 49 findings to 3 on rebuild. Rebuild before you
measure, and when a log and a live probe disagree, trust the probe.

**A dead ESLint still prints a comparison, and the comparison is against zero.** One
wrong plugin prefix — `react/no-deriving-state-in-effects`, which actually lives in
`react-hooks` — makes ESLint treat the key as fatal and refuse to lint a single file. A
both-linters run still completes and still prints "reported 438 more than eslint",
because 438 is being compared against nothing. Every agreement claim made in that
window was measured against a linter that never ran.

**When ESLint finally ran, the instruments were the problem, not the linters.** The
first real comparison read 230 against 117 across fourteen diverging rules, which looked
like a serious parity failure. It was three measurement bugs stacked:

- **A prefix-only regex.** Extracting rule names with a pattern that required a
  `plugin/` prefix silently dropped every bare core rule, so `no-param-reassign` read as
  41-versus-0 when ESLint reports it at the identical line.
- **Wrapped message text counted as findings.** `grep -c "\[no-misused-spread/"` counted
  the rule's own explanation, which contains its tag and spills onto a second line.
  Anchor on lines that *start with a path*; a rule with long help text otherwise
  inflates its own count.
- **Untracked scratch in the linted tree.** A previous agent's directory of oracle and
  extractor files was linted by ESLint and ignored by cohere, inventing nine findings
  across three rules.

After all three: **225 versus 223, two diverging rules out of eighteen.** The lesson is
not "check your regex" but that a cross-tool comparison has *two* extraction paths and
both are hypotheses. Before reporting a gap, pick one finding the diff claims is
one-sided and grep for it directly in the other tool's raw output — a single confirmed
line settles it faster than any amount of re-reading the aggregate.

**The root cause was in `EnableRule.ts`, and it is a class rather than an incident.**
`eslintNameFor` derived the ESLint namespace from the rule's prefix, but `react/` covers
**two** plugins: `eslint-plugin-react` and `eslint-plugin-react-hooks` (which carries
everything from the React Compiler). The function already handled exactly this failure
for `typescript/` to `@typescript-eslint/`; react-hooks was the same door nobody had
walked through. It now asks the plugin which names it owns rather than keeping a list —
measured, the two rule sets have **zero** overlap, so the question has one answer.

**It recurred two waves later through a different door.**
`use-unknown-in-catch-callback-variable` was registered under the **bare** name instead
of `typescript/`. `eslintNameFor` translates `typescript/` to `@typescript-eslint/`; a
bare name never triggers that translation, so the bare spelling went straight through to
the ESLint surface and ESLint refused to start again. Same fatal, same fake comparison
line against zero.

The general rule: **an ESLint plugin rule needs its plugin prefix, and the generator can
only add a prefix it can see.** Before you enable, check what cohere actually registers
— `cohere --rules | grep <name>` — against how its siblings are spelled in the config. A
rule whose cohere-side name carries no namespace is the one that will slip through, and
it looks completely ordinary in the diff.

**And the first fix silently restored the bug.** A dynamic `require` in this ESM project
threw, into a catch that returned an empty set, which left every name spelled `react/`
again. It read fine and was wrong. The static import fails at load instead of at the one
call site that matters — which is the general lesson: **a fallback that returns "nothing
found" turns a load error into a silent wrong answer.**

So before you believe any cross-linter verdict, confirm **both** sides produced output,
and read the whole tail rather than the summary line. A `TypeError: Key "rules"` or
`Could not find "<rule>" in plugin "<name>"` anywhere in that output means the eslint
side is zero and the comparison is meaningless.

## 11. The gate

    go build ./... && go test -count=1 ./...

Every package green. **Never pipe a build or test through `head` when you care about the
exit code**: a pipe reports the exit of the last command, not the first.

**Run the dry-run from the ahra tree, not the cohere tree.** Pointed at the wrong root
it emits a one-line failure that greps as silence — zero findings, which is the same
thing a clean rule produces. It cost one author a cycle twice. Confirm the run offered
files before reading its findings as a result.

**A zero-length measurement reads exactly like agreement.** An author nearly drew a
conclusion from two empty result files. Check that your measurement produced output at
all before comparing outputs.

**An oracle can be wrong in the PESSIMISTIC direction, and that is rarer and just as
misleading.** The usual failure is an oracle that reports nothing and reads as clean.
This one reported *fewer cases than it had* and read as a smaller corpus.

Measured on `dot-notation`. Five of its reporting cases are legacy octals -- `01['prop']`,
`08['prop']` and friends -- and driven through `@typescript-eslint/parser` all five come
back as FATAL PARSE ERRORS rather than verdicts, because a legacy octal is a syntax error
in TypeScript. The oracle said "linted 64 of 69" and the five silences looked exactly like
five clean cases. Driving the same corpus through espree in script mode moved it to 69 of
69, and the findings from 34 to 39.

The part worth copying is what came next. A fatal parse error in the ORACLE says nothing
about OUR parser, so the five were probed here directly: our parser reads all five and
reports on every one, agreeing with espree. The port was stronger than the oracle could
show, and taking the oracle's number would have recorded a limitation this rule does not
have.

So when an oracle reports fewer cases than the corpus contains, find out which ones and
why before treating the remainder as the measurement. `linted N of M` with `M > N` is a
result about the harness, not about the rule.

**And a rule can report zero because the substrate removed what it asks about.**
`unicode-bom` asks whether a file starts with a byte order mark. Every source file in a
real run is read through the vendored virtual filesystem, and that read strips a leading
mark before any rule sees the text; the vendored compiler's own test asserts exactly
that. So the rule was offered every file, registered a listener on every file, and
reported zero, and a seeded tree holding a genuinely marked file reported zero too.

Its author probed with two controls — a file whose first bytes on disk are `ef bb bf`
arrives three bytes shorter, an unmarked control of the same length is unchanged, and a
mark written mid-file survives — which localizes the removal to position zero, the only
position the rule asks about. Registered rather than abandoned, because the port is
correct against upstream and the missing piece is one line elsewhere.

The general shape: **when a rule reports zero on a tree, ask whether it could report
anything.** Seed the violation and re-run. A rule that stays silent on a seeded tree is
not clean, it is blind, and a fixture suite that never touches the filesystem cannot
tell you which.

**And an ORACLE that batches cases into one program lies about any rule comparing types
by identity.** An author building a typed replay harness put many upstream cases in a
single program. Several case files each declared `type Type`, the checker interned one
per file, reference identity failed, and **two of upstream's own failing cases came back
clean**. It reads exactly like a rule that cannot see object types.

One file per program reproduced all twenty. `RunTyped` is naturally one-file-per-call,
so fixtures are safe — this bites the *oracle you build to check them*, which is the
instrument you trust most and scrutinize least. If your rule asks the checker whether
two types are the same object rather than the same shape, batch nothing.

### The six kinds of zero

They are collected here because the class is the point, not any one instance.

1. **A run that never ran.** An author grepped a `go test` log for failures, got zero,
   and reported the corpus green. The log was fourteen lines of a *sibling's* compile
   error — the package never built, so no test in it ever executed. Zero failures, zero
   tests, and the grep cannot tell those apart. Check that the log ends in `ok` or
   `FAIL` for your package before you read a count out of it.

2. **A probe that only logs.** An author's discrimination probe ran 23 cases, printed
   `AGREE` or `*** DISAGREE ***` per case with `t.Logf`, and never called `t.Errorf` —
   so `go test` said `ok` while the log carried **seven disagreements**, five of them
   false negatives on the most common shapes the rule had to catch. The author read the
   green and reported the discriminator as working. An instrument that cannot report
   failure is not measuring.

3. **`go test -run <wrong name>`.** A proven fixer was nearly filed as unproven: mutated,
   run with `-run SingleLineJsdoc`, zero failures. The tests are named `...JsDoc...`,
   capital D. The pattern matched nothing, and Go reports that as **`ok` on the same
   line as `[no tests to run]`**. Grepping for `FAIL` finds nothing, which reads exactly
   like a passing mutation. **Run the BASELINE through the same `-run` pattern first.**
   If the unmutated code also reports `ok` with no test count, your pattern is wrong.

4. **A truncated read.** Reading through `tail -2` cut the first line off a two-line
   refusal message and left only the continuation, which read as a different verdict
   entirely. One coordinator concluded three times that a guard was not firing while it
   fired every time. `head -5` on a short message costs nothing; a truncated read costs
   the conclusion.

5. **A grep pattern that matches nothing**, or a mutant that would not compile, or a lint
   run from the wrong directory. All read as clean.

6. **A hanging fixture.** The five above all produce output you can read. This one
   produces none. A test that never returns emits no failure line and no summary, so
   `grep -c FAIL` reads 0 and a run that hung looks exactly like a run that passed. Found
   on `RegExp('\b')`: `SkipPatternEscape` returns a LENGTH while `ClassEnd` returns an
   INDEX. Conflating them spun the scanner forever. **Check the units on shelf helpers
   that answer the same shape of question:** length and index are both "a number about a
   position" and neither name says which.

### Make the instrument refuse, rather than remembering to check

A run that measured NOTHING looks identical to a run that found nothing wrong. In every
one of the cases above the fix is not "remember to check" — the coordinator did
remember, three times, and slipped the fourth. The fix is to make the tool refuse.

A lint wrapper that ends with:

    if ! grep -qE "^lint:" "$LOG"; then
      echo "REFUSING TO REPORT: no summary line, so this run linted nothing"
      exit 1
    fi

Then MUTATE THE WRAPPER to prove the refusal fires: point it at a tree with nothing to
lint and confirm it exits non-zero. An unproven guard is the thing it guards against.

The general form, for any measurement script you write:

    find the line that only appears when real work happened
    refuse to print a result if it is missing
    break it once and watch the refusal fire

A number with no evidence behind it should be impossible to obtain, not merely
discouraged. `score_one_mutation.sh` is built on exactly this principle and its header
comment is worth reading as an example of the shape.

### Two guards that catch what your own tests cannot

**`TestEveryRuleShipsAFixturePair` matches the CALL NAME, not the strength of your
assertion.** It walks for `ExpectFindings` specifically. A test that asserts counts,
spans and rendered message text but never names that helper reads to the guard as a rule
nothing proves can fire — one author shipped a rule that way and the guard caught it a
commit later. Assert whatever else you like, but call `ExpectFindings` at least once.

**Scratch packages are not free.** Building a one-rule isolation package to test against
is good practice, and every one of them must be gone before you commit. A scratch copy
that registers a real rule name breaks `TestEveryRuleIsRegistered` for every other agent
in the tree, and the failure names a package they have never heard of and cannot find in
their own diff. `TestNoScratchPackagesRemain` and `TestNoScratchFilesRemain` catch the
leftovers. Delete file by file; `-r` is blocked at the harness.

Before you commit, confirm both halves:

    grep '"<rule-name>"' /Users/kirkouimet/Projects/ahra/CohereSettings.json   # returns a hit
    go test -count=1 ./internal/lint/registry/                                 # green

Run `EnableRule.ts` from `/Users/kirkouimet/Projects/ahra` rather than hand-editing the
JSON: it writes all three surfaces, routes frontend rules to the structure config and
universal ones to nexus, and translates `typescript/` to `@typescript-eslint/` so the two
spellings do not drift.

**After you wire a rule, confirm the line is still in the config.** `EnableRule.ts` wires
a rule by read / parse / mutate / write. Whole file, no lock, no compare-and-swap. With
several agents running, two near-simultaneous writes could in principle drop one rule's
line.

Stated carefully, because a night watch got this wrong once: this race is a real property
of the code, but it has **not** been observed firing. It was diagnosed from a config line
that appeared at one checkpoint and was gone at the next, which turned out to be an agent
DELIBERATELY removing its own wiring and recording the rule as intentionally-off. Two
observations plus a plausible mechanism is not evidence. Before concluding your wiring
was clobbered, read whether somebody made a decision.

The check is worth doing either way, and it is cheap. Do it as the last step before you
report done. If the line is missing and you did not remove it yourself, say so rather
than restoring it silently, so the cause gets established rather than papered over.

## 12. Commit by pathspec

    git add <your files> && git commit -F <message file> -- <your files>

Other agents work in this tree concurrently. Pass paths explicitly to both commands,
never `git add -A`, never `git commit -a`, and read `git status` for files that are not
yours before staging.

If another agent's change is in your diff, commit it and say so.

**Check git history, not just the filesystem, before concluding what is yours.** An
author told a previous one had not finished found the rule file committed by that author
and its registration committed by a third, so `git status` was clean for both. The
reverse is likewise invisible: another agent may commit your registration inside their
commit, so a clean status does not mean your registration landed. Grep for it after
committing.

**A green suite does not mean committed.** An agent reported a sibling's rule as landed
because `go test ./...` was green across every package. It had never been committed —
three untracked files, with the sibling still writing.

**Untracked `.go` files compile into their package exactly like tracked ones.** So an
uncommitted rule passes every guard, registers in the binary, appears in the lint output,
and satisfies wiring checks whose entries are also sitting uncommitted in the working
tree. Everything looks finished.

The test for "did it land" is `git log -- <path>` or `git status`. Never the suite.
Working tree and committed record are different questions. Say which one you looked at.

### What landed is a snapshot of someone else's moment, not of your last verified state

The section above asks whether your work landed. This one asks **which version** of it did,
and the two come apart the moment somebody else does the committing.

A coordinator batching several agents' work commits whatever is on disk when they run. If
you correct a file after they read it and before they commit, the correction is not in the
commit, and nothing anywhere reports that. Measured: a decline's audit document was
committed saying its probe had been "since removed", in the **same commit** that added the
probe. A committed file asserting the absence of a file committed beside it, and both were
mine.

The check is one command, and it is cheap enough to be routine after any commit you did not
make yourself:

    git diff HEAD -- <your files>          # empty means what landed is what you verified

**The general shape is worth more than the git mechanics, because it is not about git.**
Both of that author's corrections in one session were *true statements decaying* rather
than wrong ones. "Nothing is committed by me" was accurate when written and false four
minutes later. "The probe was removed" was accurate when written and false once it was
restored. Neither was an error at the time; both became one.

In a tree with this many concurrent agents, **a report decays faster than it is wrong**,
and decay is the dominant failure mode rather than mistake. That has two consequences:

- **Timestamp the volatile claims, or re-check them before you send.** Anything about the
  working tree, the commit log, what other agents hold, or which tests are red has a short
  half-life. Anything about what a rule decides on a given input does not.
- **A red package is a question about timing before it is a question about correctness.**
  Three false failures were called out loud in one session, each an agent reading a
  sibling's package mid-write. Re-run before reporting; the second run is the measurement.

The same decay reaches tracked files, which is worse, because a stale sentence in a
committed document carries the authority of the codebase while being only what one author
believed at one moment. That is the failure "Your own comment is not evidence" describes,
arriving through time rather than through carelessness. When you correct a claim in a
tracked file, check that the correction is in HEAD and not only in your working tree.

**And `git diff HEAD` is not enough, because the part most likely to be left behind is
not in your files.** The check above answers "did the version that landed match the
version I verified", and it answers it only about paths you name. A rule's legality
usually depends on at least one hunk that lives somewhere else, and a sweep-up commit
takes what it recognises as yours.

Measured. A rule was ported, registered, fixture-green and byte-identical to its author's
working tree when a coordinator's batch commit swept it up. `git diff HEAD` over every one
of the author's files was empty. The rule compiled, appeared in `--rules`, passed its own
package's suite, and was **illegal at HEAD**, because the entry excusing it from
`TestEveryRegisteredRuleIsReachableFromTheLiveConfig` was one hunk in
`internal/lint/registry/live_wiring_test.go` and had not been committed with it. A fresh
checkout was red and the guard named the rule.

Every instrument an author or a reviewer would reach for said fine:

    the rule's own package suite      green
    go build ./... && go vet ./...    clean
    git status                        clean for the author's paths
    git diff HEAD -- <author files>   empty
    internal/lint/registry            RED, and it names the rule

**Only the last one can see it, and it is the only one that is not about your files.** The
failure is structural rather than careless: cross-package edits are exactly the work a
commit scoped by pathspec is most likely to miss, and they are also the work whose absence
your own package cannot detect. That is the same asymmetry as the mutation sweep's
cross-package blind spot in section 7, arriving through the commit rather than through the
scoring.

So after any commit you did not make yourself, and before reporting a port complete:

    git diff HEAD -- <your files>                 what landed is what you verified
    go test ./internal/lint/registry/             what landed is LEGAL

Run the second even when the first is empty, and especially then, because an empty diff is
what makes the situation look finished. And when the guard names your rule, confirm the
attribution with a control rather than assuming: stash the hunk, watch the guard name it,
restore the hunk, watch the name disappear. Six other rules were in that failure list and
all six belonged to other agents mid-flight; without the stash there is no way to tell your
own omission from someone else's in-progress work, and the two look identical in the
output.

### Editing a shared tracked file is one mistake with three faces

`internal/lint/checking/specifier.go` is imported by dozens of rules. An agent left it
half-written one night and every one of those rules became unbuildable, which meant every
OTHER agent's `go test ./...` went red with an error in a file they had never opened.

A half-written rule file costs its author. A half-written shared utility costs everyone,
and they cannot tell it is not theirs without investigating.

One cause, three symptoms:

    a wide build window          an uncompilable line sat in a shared utility while
                                 others' `go test ./...` went red in a file they
                                 never opened
    a misattributed entry        a shared-file line was in the working tree when
                                 ANOTHER agent committed that file, so it landed in
                                 the wrong commit
    an unreviewable packaging    94 lines of shared utility buried inside a 4,629-line
                                 rule diff

**Know the count before you edit.**

    grep -rl "lint/ecmascript/<name>" internal/lint/rules/ | wc -l

That takes a second and tells you whether you are holding a rule or a load-bearing wall.

**Commit the shared change SEPARATELY, before the rule.** Same reason a shim extension
commits alone: a file that dozens of rules read should be reviewable on its own, and
buried under a rule diff nobody can see what moved.

**Run `go test ./...`, never just your package.** Your package passing says nothing about
the others that import the same file. The full-suite run IS the control for a
shared-utility change, exactly as it is for a shim regeneration.

**And the reasoning that talks you out of it is seductive, so name it.** One agent's was:
the helper exists only for this rule and would be dead code alone, so it reads as one
change. That is wrong because reviewability is about WHO CAN BE HURT by the file, not
about which rule motivated the edit.

Do not rewrite history to fix the packaging afterwards. Attribution is not worth a
rebase. Split it from the start next time.

### Put scratch outside the rules tree from the first minute

This happened three times in one night, with three different agents:

    internal/lint/rules/posprobe/                    scratch package
    internal/lint/rules/react/zz_unc_debug_test.go   debug test, and it FAILED

Both made a package red for every other agent, with a failure naming a file they had
never opened. The second is the worse shape: a FAILING debug test in the rules tree is
indistinguishable, from outside, from the rule under test being broken.

**So make the probe's home the first thing you create, not an afterthought:**

    internal/<your_rule>_probe/

The guards and package test runs scan only `internal/lint/rules/`, so a probe outside it
costs nobody anything and you can keep it as long as you want.

**A `zz_` prefix does not help.** It sorts the file to the end of a listing; it does not
exempt it from the package's test run.

And when you are told to move one: MOVE it, do not delete it. Live instrumentation you
are actively reading is worth more than a green guard.

## 13. Report back

Say what you ported, what you measured, and which of the two you are claiming. Name the
binary you drove, whether your control fired, and any divergence you recorded with the
command that established it.

---

## Standing lessons that do not belong to one step

### A control is only a control if you break it and watch it fail

Every instrument should be checked by breaking what it measures. One author did this
wholesale and it is worth copying:

    corpus extraction check   goes red when one fixture body is corrupted
    span arithmetic check     goes red (50 of 62) when the shift is removed
    shape probe               flips when the token reads are stubbed
    fixture suite             goes red when the rule is neutered

And the detail that matters most: their first neutering mutation DID NOT COMPILE, and
they threw it out rather than reading the build failure as a passing control. A mutant
that fails to build proves nothing about your tests. **Score by "could this have failed",
never by "did something go red".**

### A corpus can be complete about a rule and still unable to reach one guard

This is 7b arriving in a rule that is not a matcher, which is what makes it easy to miss.

`default-case` excuses a missing default when a comment after the last case matches
`/^no default$/iu`. Removing BOTH anchors survives all 23 of upstream's cases. The corpus
is not thin -- it covers the comment, the casing, the position, and the option -- but every
case that exercises the DEFAULT pattern writes `no default` exactly, and the two cases with
a near-miss comment supply their own `commentPattern`, under which anchored and unanchored
agree. Nothing in it can separate the two readings.

Driving the installed rule on two shapes the corpus never writes settles it in one command,
with a control that fires:

    // no default          excuses      (control)
    // no default here     REPORTS
    // say no default      REPORTS

So the anchors are load-bearing and an unanchored pattern would silently excuse two shapes
upstream flags -- in the direction that hides findings rather than inventing them.

The general form: **a surviving mutant on a guard whose corpus coverage looks thorough is
usually telling you the corpus tests the guard's SUBJECT rather than its BOUNDARY.** Every
case wrote the matching string; none wrote a near-miss. Ask what input would sit just
outside the guard, and check whether the corpus contains one before concluding the mutant
is equivalent.

### A flaky test is worse than no test

A mutation on map iteration order was caught by a two-group fixture only about 8% of the
time — measured, 22 reversals in 200 runs — because Go's randomisation happens to favour
insertion order when there are only two keys.

The author did the right thing: put the determinism in the RULE, and wrote at the line
that the fixture is not the guard. A test that fails 8% of the time reads as green, gets
trusted, and then blames something unrelated on the run where it does fail.

If a control only fires probabilistically, it is not a control. Fix the code and say so
at the line.

### Proving a zero: count the subject matter, and use the parser to do it

A rule reporting zero findings is either correct or inert, and the number cannot tell you
which. Three levels of proof, in increasing strength.

**Weakest: a seeded file.** Hand the rule a case you built to violate it and confirm it
fires. This proves the rule is not dead, and nothing more. Necessary, not sufficient.

**Better: a differential zero.** Run the reference implementation over the same files and
confirm it also reports zero. Now two implementations agree, though both could be missing
the same thing.

**Strongest, and the bar to aim for: count the subject matter.** Parse the tree and count
the constructs the rule EXISTS to judge. `unified-signatures` did this and found 11 real
overload sets across 10 files, one of them 96 signatures deep, then spot-checked two to
explain why each correctly declined — one pair differing in RETURN type, which fails the
unification gate before parameters are compared.

That converts "found nothing" into "examined eleven real candidates and correctly
declined all of them", which is a different and much stronger claim.

**Do the count with the PARSER, not with grep.** The author tried grep first and both
attempts failed, in opposite directions:

    first pattern    returned zero across the whole tree
    second pattern   returned hundreds of false hits

The first is the dangerous one, and it is the reason to always seed a control. **An
unconstrained pattern that matches nothing is a broken pattern, not a result** — it looks
exactly like a clean tree. They caught it only because the seeded file also came back
empty. The second failed because `condition ? a : b;` has precisely the shape of a
bodiless signature to a regex.

So: count with the parser, and control the counter against a seeded file, or you have
replaced one unproven zero with another one wearing a lab coat.

**Then write it where it is durable, not in a probe.** The probe directory gets deleted.
The rule's own doc comment and its test survive. Anyone reading that rule later should see
what was searched and what was found.

### Two measurement rules that apply to everything above

**Run a control alongside any zero.** Grep for something you know is present, in the same
place, with the same command. A zero from a real absence and a zero from a bad pattern, a
wrong path, or a shim alias are the same zero. One extra command, and it is the
difference between a measurement and a guess.

**Search for the thing, not for the citation.** If somebody tells you a claim lives at
commit X or file Y, search the whole tree for the thing itself. A search bounded by
someone else's reference cannot find what their reference got wrong, which is the only
case where checking was worth doing.

### Two broken instruments that share an assumption corroborate each other

A coordinator reported a defect in a correct rule, and the reason they believed it is the
part worth keeping.

A finding whose message quotes multi-line source puts its rule tag on a DIFFERENT line
from its `file:line:column` address. Three extractions assumed one finding per line, and
the first two both produced 28-against-29 — the same wrong answer, from the same wrong
assumption. Agreement between them read as confirmation.

The third instrument broke differently, giving 43 findings with 14 extra. Checking two of
those at source showed `String(x)` and a bare property name, neither a coercion, and only
then was the instrument the obvious suspect.

**So: independent-looking measurements are only independent if they fail differently.**
Two greps over the same log, written by the same person in the same minute, share every
assumption their author has. When two checks agree, ask what assumption they share before
treating the agreement as evidence.

And the cheap protection, which is what finally worked: **verify a sample at the SOURCE,
not in the log.** A finding you can read in the file is real; a count you extracted is a
claim about your extractor.

**The author then went further and found the general rule.** Their own two extractions had
MIRROR bugs: one dropped a multi-line finding, the other counted a wrapped continuation as
its own finding. One lost, one gained, and the total came back as exact agreement with
zero position mismatches. **It was wrong in both columns and looked perfect.**

So:

**An instrument that reads a linter's output line by line is assuming one finding is one
line.** That assumption breaks for every rule whose message quotes the code it is
complaining about, which is not exotic — cohere's messages are long and explanatory by
design, and a glance at any dry run shows them wrapping. Split the output into RECORDS at
each path-anchored line and look for the rule tag anywhere in the record; do not filter
line by line.

**Compare addresses on both sides with the SAME extractor.** Two scripts that disagree
with each other while neither disagrees with the rule is the signature of this bug.

**And a result that comes back perfect deserves the same suspicion as one that comes back
surprising.** Symmetric errors cancel, and cancellation reads as confirmation.

**A third failure mode is worse than losing or duplicating: FABRICATION.** A walk-forward
parser paired each address with the NEXT rule tag it saw, which for a multi-line finding
belongs to a different rule. It did not merely miscount, it invented findings, and they
were plausible enough to read as real ones.

The tell is what caught it, and it generalises: two of the fabricated "extras" were
`String(x)` and a bare property name, neither of which is an implicit coercion at all.
**When a diff produces findings that are wrong in KIND rather than in count, the
instrument is the first suspect, not the last.** A miscount can be a real disagreement; a
category error cannot.

**Scope your comparison explicitly, and say the scope out loud.** Two correct measurements
of the same rule read as a contradiction when one is whole-tree and the other is one
library: 59 against 29 looked like a conflict and was not. Both numbers were right.

### An extractor that drops languageOptions manufactures fake disagreements

Measured on `no-implicit-coercion`. Its corpus contains `!!(foo + bar)` TWICE with
opposite verdicts:

    :292  output: "Boolean(foo + bar)"      fixable
    :319  output: null                      declined
          languageOptions: { globals: { Boolean: "off" } }

Same code, opposite answers, and the only difference is a field a naive extractor throws
away. An author's first extractor dropped it, the replay reported a disagreement with the
installed rule, and for a minute that read exactly like a real divergence in the port.

**The failure looks like the rule and lives in the instrument.** So: render
`languageOptions` beside every extracted case, and when a replay disagrees on a case whose
code appears more than once in the corpus, suspect the extractor before suspecting the
port.

**The load-bearing behaviour underneath, worth knowing on its own:** the `!!` fix is
withheld when `Boolean` is not a GLOBAL, not merely when it is shadowed. Shadowing by
`var`, by `let`, and by a parameter each withdraw it, measured separately.

### "auto-fixable: yes" in an audit can mean one arm out of nine

Same rule. An audit said auto-fixable and a coordinator repeated that in a dispatch.
Measured against the installed build, exactly ONE of nine arms carries an applicable fix:

    !!foo             fix (and only when Boolean resolves to the global)
    +foo              suggestion only
    1 * foo           suggestion only
    foo - 0           suggestion only
    '' + foo          suggestion only
    foo + ''          suggestion only
    foo += ''         suggestion only
    -(-foo)           suggestion only
    ~foo.indexOf(x)   neither, reports bare

The corpus confirms it: 4 top-level non-null outputs, all `Boolean(...)`, against 44
explicit `output: null`. Counting `output:` lines flatly gives 43 and is WRONG, because
outputs nested inside a `suggestions:` array look identical to a grep. Match on
indentation or parse it.

**This changes what "prove the fixer" means.** `ExpectFixedSource` can only cover the 4 fix
cases. The other 44 need assertions reaching into `Diagnostics[].Suggestions`, the way
`internal/lint/rules/core/array_callback_return_test.go` already does, and the two surfaces
must be mutated SEPARATELY — a fix mutation cannot see a suggestion defect.

### When the clone beats the installed build: a same-tag strict superset

The usual guidance is that the INSTALLED build is the oracle, because the clone runs ahead
and porting to unreleased behaviour makes us disagree with the tool we are replacing.
`react/no-unknown-property` is a measured exception, and the conditions are narrow enough
to state exactly.

The author first read the data tables from the installed build and ended up carrying four
corpus cases at a verdict that contradicted upstream's own expectation. They reversed it:

    both artifacts declare 7.37.5      the clone is unreleased commits on the SAME
                                       tag, not a newer release we lag
    onScrollEnd, onScrollEndCapture,   present in the clone's rule file, ABSENT
    closedby                           from the installed one
    onLoad                             widened to include body
    removals                           NONE. Diffing both attribute tables in
                                       both directions returns nothing missing
    repairs                            none of the four carries a fix

So the clone is a strict superset: four additions, no removals, no behaviour reversals, and
nothing that rewrites source.

**That combination is what makes it safe.** A superset of a permit-list only ever makes the
rule quieter, so the worst case is a missed finding rather than a false one, and with no
repair attached there is no way for the difference to rewrite somebody's code. Reverse any
of those conditions and the installed build wins again.

**The general test, before you take the clone over the release:** same version string on
both, additions only in both directions of the diff, and no fixer on the differing entries.
Check all three. Two out of three is not the exception.

### Size the rule before you dispatch it

An agent at 293K returned empty on `no-unnecessary-condition` after nine minutes, leaving
nothing on disk and no probe. That reads as exhaustion and was not: the rule is simply too
big for the budget it was given, and the dispatch was the error.

Measured, against its own wave-mates:

    no-unnecessary-condition          970 lines rule    4,316 lines corpus
    no-unnecessary-type-parameters    571                1,908
    no-redundant-type-constituents    534                  851
    no-unnecessary-template-expression 472

Nearly double its wave on the rule and more than double on the corpus, and the corpus is
the part that costs context, because a faithful port reads all of it.

**Two commands before every dispatch, and they take a second:**

    wc -l /tmp/lint-sources/<pkg>/src/rules/<rule>.ts
    wc -l /tmp/lint-sources/<pkg>/tests/rules/<rule>.test.ts

**And the CORPUS is the number that matters, not the rule.** A coordinator sized
`react/no-unused-prop-types` by its rule file, saw 171 lines, and called it small. Its
corpus is 6,778 lines and it needs a further 578-line shared substrate (`usedPropTypes.js`)
that nothing in the tree had. That is ~7,500 lines of reading behind a 171-line rule, and
the agent came back empty after doing excellent measurement work it never got to spend.

A tiny rule over a huge corpus is the trap, because the rule file is what you instinctively
check. Add the SUBSTRATE the rule imports to the count too — a thin rule that is a caller
over 2,900 lines of `Components.js` plus `usedPropTypes.js` is not a thin port.

Rough calibration: a rule under ~600 lines with a corpus under ~2,000 lands comfortably in
an agent with 300K of headroom. Past that, send it to the freshest agent available or
expect a handoff rather than a rule.

**And when an agent returns empty, check the rule's size before concluding the agent is
spent.** That inference has been made wrongly in the other direction too, reading a rate
limit as context exhaustion. The transcript's token count and the rule's line count are both
one command away; neither needs guessing.

### When two agents need the same collector, scope and STATE the boundary

Two rules in one wave both needed "what propTypes does this component declare". The author
noticed before writing, and the resolution is worth reusing.

The wrong answers first. Writing it twice gives two rules two chances to disagree about one
question. And serialising the agents so one lands the collector first costs more than the
duplicate, while lifting ~600 lines of shared machinery inside a rule diff is the
unreviewable packaging warned about above.

**The right answer: scope to what is reachable now, land it, and STATE the boundary in the
commit.** Name which shapes you cover, which you decline, and where the shared collector
belongs when someone lifts it. A stated boundary is a handoff. A silent one is a trap for
the next reader.

Three things that made this concrete rather than vague, and they generalise:

**Check the package first.** `internal/lint/rules/react/` already carried
`componentPropsTypeNode`, `collectTypeMembers` and `resolvedTypeDeclarationBody`.
Same-package sharing is the pattern, not a workaround.

**Check whether the lifting target already exists.** `internal/lint/ecmascript/react/` was
already there with `component.go` and `names.go`, carrying nothing about propTypes. Naming
an existing empty home in the commit beats "someone should extract this".

**Check whether the overlap is total or partial.** It was partial: both rules need
declarations, but one needs defaultProps and the other needs a 578-line usage pass nothing
in the tree has. Partial overlap means both agents still have real independent work, which
is what makes scoping viable instead of merely polite.

### Reusing a sibling rule's helpers is encouraged; importing another rule PACKAGE is not

The guard forbids a rule package importing another rule package. That says nothing about
reusing helpers from a sibling file INSIDE your own package, and the difference matters.

`react/no-unstable-nested-components` was made tractable by exactly this.
`no_multi_comp.go` already carries over a thousand lines reproducing upstream's
`Components.js`, and the new rule calls straight into it:

    collectDetectedComponents   isComponentClass       returnsJsx
    createElementCallCounts     isCreateReactClassCall semanticParentOf
    skipParenthesesOptional     parentComponentOf  (from no_this_in_sfc.go)

Eight helpers, no new package, guards green. Re-deriving that component detection would have
given two rules two chances to disagree about one question.

**So the decision tree is:**

    helper in a sibling file, same package   just call it
    helper in ANOTHER rule package           lift the judgment into
                                             internal/lint/ecmascript/..., and have BOTH
                                             rules call it, as classmembers did
    logic that is genuinely one rule's       keep it in your file

Before writing a helper, grep your own package. In `internal/lint/rules/react/` in
particular, a great deal of upstream's shared machinery is already ported.

### Two bugs, not nine: read the failure PATTERN before debugging cases

Nine fixtures failed, all under-reporting, clustered 12-15 then 23, 27, 30-32. That shape —
a contiguous run plus scattered ones — was two bugs, because upstream orders its corpus by
shape and a contiguous block is usually one cause.

Both were the same KIND of error:

    isInsideRenderMethod                read as "anywhere inside a render method"
                                        upstream tests the node's DIRECT parent
    isPropertyOfObjectExpressionMatcher read as a grandparent test, on the strength
                                        of its NAME; it is `node.parent`

The author had written a confident doc comment asserting the intuitive reading was wrong.
The comment was what was wrong.

**A function's name is not its contract.** Both were settled by evaluating upstream's own
condition at every validated node rather than by reading the identifier. When a cluster of
fixtures under-reports, suspect one predicate read too broadly before suspecting many
separate bugs.

### Your own comment is not evidence

The sharpest self-diagnosis of one night, in the author's own words: "a comment I wrote is
not evidence, it is my own earlier claim, and citing it back laundered a guess into a fact."

They had written a backwards statement into a file at commit time, then repeated it from
their own comment as though reading an observation. The two greps that would have settled it
were available the whole time.

This is worse than an unchecked memory, because a comment in a file LOOKS like documentation.
It carries the authority of the codebase while being only what one author believed on one
afternoon.

When a comment is your evidence for a claim, go check the thing it describes.

### A justification you wrote will shape the fixtures you write next

The section above says a comment is not evidence. This is the sharper form, and it is worse:
**a justification you wrote becomes the premise your fixtures are derived from, so the
fixtures agree with it and cannot falsify it.** You end up with a wrong belief, a test that
confirms it, and a green suite. The loop is closed and nothing inside it can open it.

Three instances in one batch, by two authors, all with locally sound reasoning at every step:

    max-depth        "without this arm, sibling methods accumulate depth"
                     They do not; the depth decrements on exit. Fixtures were written from
                     that argument, and the mutation deleting the arm survived BOTH the
                     corpus and those fixtures.

    consistent-return
                     "the equality on the flag word is not a mask, because a union carries
                     its constituents' bits" -- with a test named for the distinction and a
                     case on each side. A union does NOT carry those bits: `undefined|number`
                     has flags 134217728, `Union` alone, so `flags & Undefined` is already
                     zero. No input separates the two, and the mutation survives as genuine
                     equivalence.

    a whitespace scan
                     An ASCII-only scan documented as a deliberate narrowing, with upstream's
                     counterexample sitting in the corpus.

**What breaks the loop is a mutation you did not choose the shape of.** In all three the
survivor was the only signal, and in two of them the author's first instinct on reading it
was to add another fixture -- which would have been written from the same premise and would
have passed for the same reason. The third round of the same mistake is one step away, and it
looks like diligence.

So when a mutant survives an arm you have DOCUMENTED, suspect the documentation before the
fixtures. A surviving mutant has exactly two readings, and they need different responses:

    the fixtures cannot see it        add an input that discriminates
    there is nothing to see           the arms are equivalent; say so and stop

Telling them apart means going to the thing itself -- print the flag word, delete the arm and
watch which rows move -- rather than reasoning about it again in the same terms that produced
the claim.

**Keep the rows that do NOT discriminate.** When `max-depth`'s real shape turned out to be a
class nested inside an already-deep block rather than sibling methods, both sets stayed in the
test, labelled. The non-discriminating rows are the evidence that the original argument was
wrong rather than merely unproven, and without them the next reader deletes the arm, sees the
sibling rows stay green, and concludes it was dead code.

**And correcting the fixtures does not correct the justification.** This is the part with no
instrument at all. `max-depth` shipped in one commit with a comment saying siblings accumulate
and, a hundred lines below it, a comment recording that exact argument as disproved -- a file
arguing with itself, with every test green. The mutation sweep found the fixture defect and
could not touch the sentence that caused it, because **nothing fails when a comment is wrong.**

So the discipline is a step, not an attitude: when an instrument corrects you, go back and
re-read what you WROTE about the thing, not only what you tested about it. The comment is
where the wrong belief actually lives, and it is the part that outlives the commit and teaches
it to the next reader.

### A nil-receiver method call can be correct, so check the method before calling it a bug

A latent panic was nearly filed against `subject.Alias().Symbol()`, because two other call
sites in the same file guard `alias == nil` first and this one does not.

`TypeAlias.Symbol()` opens with `if a == nil { return nil }`. In Go a method with a
nil-receiver guard is safe to call through a nil pointer, so the unguarded version is correct
AND shorter — it delegates the nil case to the method that already handles it.

Two lessons. Read the callee before calling a missing guard a bug. And when your code is
correct but looks wrong beside its neighbours, leave a one-line comment saying why, or the
next reader will "fix" it.

### Type.Alias, not Type.AliasSymbol

`AliasSymbol()` does not exist on `Type` in the shim or upstream. The shim's only
`AliasSymbol` spelling is `AliasSymbolLinks`, an unrelated link store, which makes the wrong
name look plausible when you grep for it.

The real chain, and the shim already exposes it so no extension is needed:

    func (t *Type) Alias() *TypeAlias
    func (a *TypeAlias) Symbol() *ast.Symbol
    func Type_alias(v *checker.Type) *checker.TypeAlias

    if alias := checker.Type_alias(subject); alias != nil {
        symbol = alias.Symbol()
    }

Guard the nil: `Type_alias` returns nil for every type that is not an alias, which is most of
them.

### NeedsTypeChecker does not save you from a nil checker

`NeedsTypeChecker: true` governs the REGISTRATION path: the engine will not hand your rule a
file without a checker. It says nothing about the HARNESS path, where a `rule.Context` can be
built by hand — and `TestNoRegisteredRuleCrashesOnAbsentOptionalNodes` does exactly that.

A rule declared it correctly and still panicked with `resolveEntityName(0x0, ...)`, a nil
receiver, because the declaration felt like the guard. It is not. Write both:

    Run: func(ctx rule.Context, options any) rule.Listeners {
        if ctx.TypeChecker == nil {
            return nil
        }

Put it in `Run`, not at the top of each listener: it declines the file once instead of once
per node. `internal/lint/rules/typescript/await_thenable.go` and
`internal/lint/rules/typescript/consistent_generic_constructors.go` are the precedent, and
`await_thenable`'s comment carries the reasoning.

**Pin it with a test in your own package**, covering both the declaration and the decline.
`await_thenable` does this "so a later revert fails loudly rather than going vacuously
green." A guard nobody tests is one refactor away from being deleted as dead code, and the
panic returns with nothing left to catch it.

**And this is the concrete reason a crash outranks a wrong finding.** The walk recovers per
FILE, not per rule. One nil dereference costs every rule on that file. A wrong finding is
visible and arguable; a panic silently deletes other rules' coverage and looks like clean
output.

### A scope rule sees an EMPTY symbol table under Run and a populated one under RunTyped

The binder populates a per-scope symbol table reachable through `ast.IsLocalsContainer` plus
`ast.GetLocals`. If you are porting a rule that upstream writes against eslint-scope
(`findVariable`, `scope.upper`, `scope.variables`), you very likely do NOT need to build a
scope graph. The binder already did it.

But the table is only populated when the binder has run, and the binder runs with the
PROGRAM. Under `testing.Run` the locals table is EMPTY. Under `testing.RunTyped` it is
populated. An author who probes with `Run`, sees nothing, and concludes the substrate is
missing has measured an unbound tree, not an absent scope graph. That is a confident zero in
a new costume, and it is the trap in minute five of any scope-shaped port.

Consequence for registration: such a rule needs `NeedsTypeChecker: true` **for binding**, not
because it asks any type question. Declaring it false hands you a nil checker, and the usual
nil-guard then silently takes the wrong branch on every file.

**`ast.GetLocals` PANICS on a node that is not a locals container.** It does not return nil.
An enum declaration reaches it. Guard with `ast.IsLocalsContainer` first, every time. Same
hazard class as `ast.SkipParentheses`.

Two shapes that a walk written from upstream's structure misses silently:

- An enum declaration is NOT a locals container. Its members live on the enum symbol's
  `Exports` table, so an enum-shadowing check is portable only through a different accessor
  than everything else in the rule.
- A function expression IS a locals container, but its own name is not in it. Upstream's
  `functionExpressionName` scope has no equivalent here, so the `var a = function a() {}`
  exemption has to come from AST shape rather than from the scope table.

Better than upstream, once you are there: eslint-scope carries `isValueVariable` /
`isTypeVariable` as a boolean pair, while the symbol here carries real `SymbolFlags`
(`0x2` BlockScopedVariable, `0x40000` TypeParameter, `0x80000` TypeAlias, `0x200000` Alias).
All seven of upstream's DefinitionType variants map onto them.

### Some corpora are vendored in-repo, not under /tmp/lint-sources

The standing instruction is to import fixtures verbatim from `/tmp/lint-sources`. For React
Compiler rules that instruction points at the wrong place, and a provenance check written
against it returns a confident zero on a rule whose fixtures are impeccable.

`react/preserve-manual-memoization` draws from a vendored corpus at
`internal/lint/rules/react/conformance/testdata/fixtures/`. Grepping its test file for
`/tmp/lint-sources` or `babel-plugin-react-compiler` finds nothing, which reads as invented
fixtures and is not.

Two things make the vendored path the stronger option, and they are worth copying:

- The test file records the upstream path each fixture came from, per case.
- The conformance package DIFFS the copies against the originals, byte for byte, per file
  (`TestVendoredCorpusPairsAreComplete`, `TestCorpusIsTheSizeItClaimsToBe`, and the
  attribution tests beside them). The copy cannot silently drift from its source, which is
  more than a one-time verbatim paste guarantees.

So before concluding a rule invented its fixtures, find out where its corpus actually lives.
Check for a self-diffing test first; a rule that ships one has better provenance than a rule
that merely claims a verbatim import.

### A core rule and its typescript-eslint extension can both be on, and both will fire

Several rules in the live config are enabled twice: as the bare ESLint core rule AND as
`@typescript-eslint/<same name>`. Seeded probes on two of them emit two findings at the
identical line and column.

Two ways this bites, both of which look like a bug in your own rule:

**Your findings count looks doubled.** You port the core rule, measure it on the tree, and
the number reads as though the rule fires twice per site. It does, because the extension is
separately enabled. Check `CohereSettings.json` for `@typescript-eslint/` plus your rule's
bare name before you write the count down, and say so in your report.

**The temptation is to narrow your rule so it stops overlapping.** Do not. Your rule is
measured against upstream's corpus; narrowing it to dodge a config overlap makes it disagree
with the corpus, which is a real defect traded for a cosmetic one. Port faithfully and let
the duplicate be a config question.

Turning either rule off is Kirk's decision, not an author's, and not a night-watch one.
Report the overlap; do not resolve it.

### Check the thing you were just credited for

The defects most worth finding live past the point where anyone else would look. Review
reaches work on its way in. It cannot reach work that has already been accepted, praised, and
moved on from, and that is where this repository keeps finding its real defects.

Measured over one night, 91 commits, four of them carrying the author's own defect in the
subject line. A guard cited as a task's closing condition that could not fail, because it read
a surface where the two things it compared had already been merged. A doc comment stating a
false premise about another package, whose conclusion happened to survive on different
grounds. A diagnosis two nodes independently confirmed that named the wrong compiler pass.
Three comments quoting a rule count that had been accurate and silently stopped being.

**None was found by review.** Every one was found by the author re-checking something already
credited.

So the discipline is not more review, it is a habit: after you are told the work is good, go
check the thing the praise attached to. **Applause means everyone else has stopped looking**,
which makes it simultaneously the cheapest moment to find what remains and the moment least
likely to feel like it needs checking.

This only works if reporting a defect in your own accepted work is costless. It is, here. The
only person who can reach that population is the author, and only after the applause.
