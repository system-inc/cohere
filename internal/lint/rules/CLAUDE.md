# Adding a rule to cohere

Every guard below names the defect that earned it. None is a style preference, and the numbers are
the reason to believe them: a rule shipped 187 green fixtures while every finding pointed at the
wrong place, and another reported 3,407 registrations while being completely inert. Both passed
everything a careful author would have thought to run.

The failure this file exists to prevent is narrower than "writing a bad rule". It is **a rule that is
wrong in a way its own tests cannot see**, which is the only kind that ships.

## The shape of a rule here

Read two or three rules already shaped like the one you are writing, whole, including their
`_test.go`. Pick by the shape of the question rather than by the name:

    anchors on a declaration and looks for writes to it
        core/no_class_assign.go, core/no_const_assign.go, core/no_func_assign.go
    walks a regex pattern
        core/no_useless_backreference.go, core/no_regex_spaces.go
    needs the type checker
        grep -rl NeedsTypeChecker internal/rules/
    proposes a fix or a suggestion
        grep -rl ReportNodeWithFixes internal/rules/
    reasons about scope and bindings
        core/no_shadow_restricted_names.go, core/no_unassigned_vars.go
    gathers once over the file and then matches
        core/no_const_assign.go's KindSourceFile pre-pass

What you are taking is anchoring, `ctx` threading, type-switch layout, where reasoning goes in the
doc comment, how fixtures are tabled, how spans are asserted, how `NeedsTypeChecker` is declared.
Fourteen authors each re-invented this and no guard caught the divergence, because every version
passed its own tests.

### Read siblings for shape, never for semantics

Copy how a rule is built. Never copy what it believes about its inputs.

`no-nonoctal-decimal-escape` looks like a regex rule and is not: `/\8/` is a **pass** in its corpus.
Ported by analogy from the rule finished an hour earlier, its code and its fixtures would have shared
one wrong belief, **and every fixture would have passed.**

When the code and the tests come from the same analogy they agree with each other and disagree with
reality. No fixture pair, no mutation sweep, and no span assertion can see it, because none of them
gets to vote on the premise. The corpus is the only thing that can.

If you find yourself writing a fixture because a neighbouring rule had one like it, stop and go find
that case in your own corpus. If it is not there, it is not yours.

## Fidelity is to what a rule decides, not how it obtains what it needs

The originals walk the filesystem, read `process.cwd()`, or re-parse a neighbouring file, because
ESLint hands them one file at a time and gives them no program. Cohere has the program, one resident
type graph, every file already parsed. **Reproducing a workaround for a constraint we do not have is
not fidelity.** Reproduce the decision: which inputs report, which do not, where the finding points,
what it offers.

Three consequences, and the third is the one that ships.

**A port is not obliged to carry a defect it can see.** Two rules here report without the fix their
original ships, because those fixers drop type annotations, lose `async`, or replace a whole
`VariableDeclaration` while reporting per declarator, which silently deletes a component. Reporting
without a fix is the subset you can show correct. A wrong fix is applied unattended.

**Where a rule has no tree exposure and upstream gives no reasoning, your sense of what the rule
should do is the thing most likely to be wrong.** Record the intuitive reading beside the actual one,
at the line, so the next reader does not helpfully correct it back.

**Never document a limit you did not measure.** This is worse than the other two. Reproducing a
defect leaves it findable; silently improving leaves a difference the harness can see. A confident
doc comment asserting "this case is a genuine divergence, reproduced as silence" **inoculates the
next reader against finding it.** It happened: an author wrote exactly that about computed lifecycle
keys, and oxc's `key.static_name()` resolves them and reports. If you state a divergence, state the
command that established it, or do not state it.

## Check the shelf before writing any helper

`internal/utilities/` is 20 packages and 85 files, with `ecmascript/` subdivided by question:
`binding`, `imports`, `literal`, `module`, `property`, `reference`, `regexp`, `regexpattern`,
`regexsyntax`, `scope`. Alongside it: `comments`, `controlflow`, `hir`, `jsx`, `nextjs`, `react`,
`text`, `typecheck`.

**Search everything reachable, not only `internal/utilities/`.** The `shim/` packages expose
typescript-go's own helpers by linkname and are invisible to a search of the utils shelf. An author
needing an edit distance nearly used `core.GetSpellingSuggestionForStrings`: closest name on any
shelf, reachable, and wrong, because it charges 0.1 for a case-only substitution and 2 for anything
else. Measured, it returns suggestions for three of upstream's own passing cases.

**Print the node's own fields before writing a value-comparison helper.** An author nearly wrote a
numeric parser before probing a `NumericLiteral`: its `.Text` already holds the canonical rendering
of the double, and a `StringLiteral`'s `.Text` is the cooked value. The node is a shelf nobody
searches.

**A rule whose subject is trivia has no node kind to grep for.** `comments.ForFile` will not surface
that way. Two authors nearly reimplemented `scanner.GetLeadingCommentRanges` beside it.

### Probe a shelf helper against upstream, not against its doc comment

The failure mode is not "weaker than documented". It is "different from upstream", and it runs in
both directions, **including the direction where the helper is more careful.**

`react.IsEs6ComponentClass` skips parentheses on the heritage receiver and its doc advertises that
skip as a correctness feature. It is a divergence: `extends (React.Component)` is silent upstream and
the helper answers true. Invisible from the doc comment, which is accurate about what the code does,
and invisible from the corpus, which writes no parens.

Both helpers an author reached for on `no-this-in-sfc` were wrong in opposite directions:
`react.IsLikelyComponentName` uses `unicode.IsUpper` where oxc uses `is_ascii_uppercase`, so `Фoo` is
silent upstream and true on the shelf; `react.IsEs5ComponentCall` accepts `createClass` where oxc
keys on `createReactClass` alone.

If a shelf function is load-bearing for your rule, probe it on your own corpus before building on
it, and report what you found either way.

### On the shelf census

`SHELF-CENSUS.md` (attached to `#m2fm4zb`) is a whole-corpus read from 2026-08-23 and remains the
best map of duplicated judgment. **Its "missing utilities" section is now stale in every entry
checked**: `WritesToBinding` lives in `ecmascript/reference`, `EnclosingFunctionLike` in
`ecmascript/scope`, and the static-property-name decision it called missing shipped as
`property.Name` / `property.NameTagged` rather than under the name it proposed.

Read it for the disagreements it documents, not for its gap list, and **search the shelf by the
question rather than by the census's proposed name**, because the name is the part that drifted.

## Fixtures, and the four ways they lie

A fixture pair is required: the violation fires, the clean case stays silent. That is the floor, and
every failure below passed it.

**`ExpectFindings` asserts message ids and count and nothing else.** A rule whose defect is where it
points, or what it rewrites, passes a complete fixture pair while being wrong.

**Assert the span of every rule, whether or not it carries a repair.** An author derived file
positions from a string literal's cooked value while the file holds raw text (`"[á]"` is six bytes
raw and four cooked), so every finding pointed at the wrong place. All 187 verbatim fixtures were
green, and the rule carried no fix at all, so gating span checks on "does it have a fix" would have
shipped it. Slice the source with the finding's own range and compare the text:

    reported := source[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]

**When two upstream implementations disagree about where to point, the span is the only fixture that
records which one you ported.** One rule had two plausible spans, oxc picking one and ESLint the
other, with an identical message id. Every id fixture stayed green over the wrong choice.

**Assert the message text exactly.** An author's rule rendered `'##unusedField'` with a doubled hash,
because a private name node's `Text()` already carries the `#`. The fixture used
`strings.Contains(got, "#forgotten")`, and a two-hash string contains a one-hash needle, so it stayed
green while the message was wrong. The general form is worth more than the instance: **a fixture
whose predicate is weaker than the property it guards is not a guard.**

**If the rule carries a fix, assert it by applying the repair and comparing the resulting source**,
never by comparing the fix's text. A fix writing the right string over the wrong span passes a text
comparison, and that is a real defect we shipped.

**Route option fixtures through the rule's own exported decoder**, not by building the options struct
directly. That is what puts a default inversion or a serde alias under test, and those are the two
lines most likely to have no upstream counterpart.

## If you score a rule in two directions, pin both

A rule measured against a corpus usually gets two numbers: how often it fires where it should, and
how often it fires where it should not. **Pinning one exactly and asserting only `> 0` on the other
is not two instruments. It is one instrument and a formality.**

The asymmetry runs one way, which is why it survives review. A change that trades true positives away
for false-positive removals moves the pinned number in the direction that reads as success, while the
loose side stays green because it is still non-zero.

Measured, on `preserve-manual-memoization`: freezing component parameters in the aliasing graph took
the clean-fixture rate from 31 of 70 to 19, a 39% improvement against the number that rule was being
judged on. The golden count went from 15 of 33 to 9 in the same run. Six programs upstream reports on
had gone silent underneath a result that looked like progress, and the author had the 19 and was
composing the commit message before checking the other side.

So: assert both counts exactly, and make each failure message say which direction means what. "This
went down, so either the rule improved and you lower it deliberately, or it stopped reporting things
upstream reports on" is the sentence that does the work. A ceiling cannot distinguish a fix from a
rule that went quiet.

## Prove the fixtures can see: the mutation sweep

    internal/lint/rules/tools/score_one_mutation.sh <file> <package> "<python expression rewriting the string `source`>" [TestName]

Mutate every discrimination the rule makes, one at a time. **A green suite proves nothing until
something has been shown able to turn it red.**

The tool refuses a rewrite that changed no bytes, refuses a mutant that does not compile, and refuses
to score at all unless the package is green before the mutation. All three refusals are correct and
**none is a pass.** Before the byte check existed, a mutation changing one space inside a comment came
back "caught by thirty-two lines".

**Read "does not compile" as a question rather than a verdict.** It cannot tell your broken mutation
from a sibling's half-written file, so the message points at your mutation and is sometimes wrong.
Check whether the package compiles without your mutation before believing it.

The fourth argument scopes both the baseline and the mutant to one rule's tests, which is how you
proceed when the package is held by other agents. **State the cost when you use it:** a scoped run
cannot see a guard in another package, so a mutation renaming a rule reads as a survivor while the
parity guard in `internal/registry` would have caught it. Anything resting on a cross-package guard
needs one unscoped run.

A sweep against a red package is not a weaker measurement. It is not a measurement. A scoped sweep is
a real measurement of a smaller thing.

## Registering, and the ways a rule is inert

Add to the package's `register.go`, and add the rule name to `CohereSettings.json` in the rules
block. **Not `.oxlintrc.json`**, which is oxlint's own config and which cohere no longer reads.

**Grep that file for your rule name first.** Many rules are already enabled. A second entry produces
valid JSON with a duplicate key, one silently wins, and nothing complains.

**Done means enforcing.** A rule registered and enabled nowhere passes every fixture and lints zero
files.

**A non-zero registration count with zero findings is the positive tell that a rule works.** `was
offered no files` is the failure signal. That separates "inert" from "correctly declining" in about
ten seconds without building a probe tree.

**Except for a rule with a decoder, where it is not.** An author's rule reported 3,407 registrations
over 3,407 files and was completely broken. A rule configured as bare `"error"` is handed **nil**
options: `rule.DecodeOptionsInto` errors on empty input, `config.OptionsRegistry.Decode` turns that
into nil for a non-required rule, and `options.(T)` on nil yields the zero value. Every fixture
reached the rule through the decoder, so nothing in the suite could see it, and 21 of 21 imported
cases passed. **Give your rule an explicit nil-options fallback and a fixture that bypasses the
decoder.**

## Run it dry against the real tree

    go build -o /tmp/cohere-<yourname> ./cmd/cohere
    cd /Users/kirkouimet/Projects/ahra && /tmp/cohere-<yourname> --no-fix --lint --timing 2>&1 | grep <your-rule>

**`--no-fix` is not optional, and `--fix` is not what causes writing.** The fix phase runs by
default, so a bare `cohere --timing` mutates the tree. A seeded probe file was rewritten this way.

This tells you three things fixtures cannot: whether it fires on real code and whether those findings
are right, what it costs (one rule was 64.5% of all rule time because it rebuilt a map per file), and
whether it explodes on files larger than a fixture.

**A zero is three different things and the file count does not separate them.** An author saw 3,407
files and "registered no listener on any of them", which read as inert and was not: the tree's
tsconfig includes only TypeScript extensions and their rule declines JavaScript, exactly as upstream
does. Two moves resolve it: compare against a control rule offered the same files and known to be in
the same state, and build a small tree that should trigger the rule.

**A probe tree's config must be named `CohereSettings.json` and puts rules at the JSON top level.** A
wrong filename is worse than a wrong shape: cohere exits before linting and prints nothing your grep
will catch, so you get a zero that was never a measurement. An author seeded `{"cohere":{"rules":...}}`
and got `was offered no files`, which reads exactly like the inert case.

**Name probe files by index, never by the identifier under test.** macOS has a case-insensitive
filesystem, so `d_getStaticProps.tsx` and `d_getstaticprops.tsx` collapse into one file and the last
write wins. An author measured a case-only change as "does not report" and nearly recorded a
rule-level divergence. **Unless the rule gates on the filename**, in which case vary the directory and keep
the basename exact.

## The gate

    go build ./... && go test -count=1 ./...

**Never pipe a build or test through `head` when you care about the exit code**: a pipe reports the
exit of the last command, not the first.

**Read the whole of a tool's output before concluding anything from it.** A `tail -2` cut the first
line off a two-line refusal and left only the continuation, which read as a different verdict. An
author concluded three times that a guard was not firing while it fired every time.

**A zero-length measurement reads exactly like agreement.** Check that your measurement produced
output at all before comparing outputs.

## Two measurement rules that apply to everything above

**Run a control alongside any zero.** Grep for something you know is present, in the same place, with
the same command. A zero from a real absence and a zero from a bad pattern, a wrong path, or a shim
alias are the same zero. One extra command, and it is the difference between a measurement and a
guess.

**Search for the thing, not for the citation.** If somebody tells you a claim lives at commit X or
file Y, search the whole tree for the thing itself. A search bounded by someone else's reference
cannot find what their reference got wrong, which is the only case where checking was worth doing.

## Committing in a shared tree

    git add <your files> && git commit -F <message file> -- <your files>

Other agents work in this tree concurrently. Pass paths explicitly to both commands, never `git add
-A`, never `git commit -a`, and read `git status` for files that are not yours before staging.

`register.go` is the one file every author edits and git cannot stage half of it. If another agent's
registration is in your diff, commit it and say so.

**Check git history, not just the filesystem, before concluding what is yours.** An author told a
previous one had not finished found the rule file committed by that author and its registration
committed by a third, so `git status` was clean for both. The reverse is likewise invisible: another
agent may commit your registration inside their commit, so a clean status does not mean your
registration landed. Grep for it after committing.

## "Complete" is not a status you can report before the fixtures have run

Two authors paused describing their work as complete: compiling, gofmt clean, fixtures written. When
the next ran them, one had two failing span assertions and seven blind spots the sweep found, and the
other had four rule defects and nine wrong fixtures, including twenty-two clean upstream cases being
reported as violations. None of it was visible from outside.

Say what compiles, say what has run, and keep those separate. **If you inherit work described as
complete, run it first and expect failures.** A paused artifact that compiles is a plausible
artifact, not a verified one.

## Check the thing you were just credited for

The defects most worth finding live past the point where anyone else would look. Review reaches work
on its way in. It cannot reach work that has already been accepted, praised, and moved on from, and
that is where this repository keeps finding its real defects.

Measured over one night, 91 commits, four of them carrying the author's own defect in the subject
line. A guard cited as a task's closing condition that could not fail, because it read a surface
where the two things it compared had already been merged. A doc comment stating a false premise about
another package, whose conclusion happened to survive on different grounds. A diagnosis two nodes
independently confirmed that named the wrong compiler pass. Three comments quoting a rule count that
had been accurate and silently stopped being.

**None was found by review.** Every one was found by the author re-checking something already
credited.

So the discipline is not more review, it is a habit: after you are told the work is good, go check the
thing the praise attached to. **Applause means everyone else has stopped looking**, which makes it
simultaneously the cheapest moment to find what remains and the moment least likely to feel like it
needs checking.

This only works if reporting a defect in your own accepted work is costless. It is, here. The only
person who can reach that population is the author, and only after the applause.
