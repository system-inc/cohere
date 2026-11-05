package rename

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/locale"
	"github.com/system-inc/cohere/internal/types/program"
)

// fixturePath resolves a fixture under testdata to an absolute path.
//
// Absolute rather than relative because `program.Build` resolves a tsconfig against a current
// directory, and a test binary's working directory is its own package directory.
func fixturePath(test *testing.T, name string) string {
	test.Helper()
	absolute, absolutePathError := filepath.Abs(filepath.Join("testdata", name))
	if absolutePathError != nil {
		test.Fatalf("resolving the %s fixture: %v", name, absolutePathError)
	}
	return absolute
}

// buildFixture builds the symbols fixture as a program.
//
// Single-threaded on purpose: symbol pointers are the identity this whole package turns on, and a
// symbol obtained from one checker cannot be compared against one from another. One checker makes
// the comparison meaningful in a test the same way `CheckerForFile` makes it meaningful in a run.
func buildFixture(test *testing.T) (*program.Graph, string) {
	test.Helper()
	root := fixturePath(test, "symbols")
	graph, buildError := program.Build(program.Options{
		ConfigFileName:   root + "/tsconfig.json",
		CurrentDirectory: root,
		SingleThreaded:   true,
	})
	if buildError != nil {
		test.Fatalf("building the fixture program: %v", buildError)
	}
	return graph, root
}

// planFor builds a plan against a position in the fixture, named the way the command line names one.
func planFor(
	test *testing.T,
	graph *program.Graph,
	root string,
	relative string,
	line int,
	column int,
	newName string,
) *Plan {
	test.Helper()
	plan, buildError := Build(context.Background(), graph, Position{
		FileName: filepath.Join(root, relative),
		Line:     line,
		Column:   column,
	}, newName)
	if buildError != nil {
		test.Fatalf("building the plan for %s:%d:%d: %v", relative, line, column, buildError)
	}
	return plan
}

// describeEdits renders a plan so a failure says what was found rather than only that a count moved.
func describeEdits(plan *Plan, root string) string {
	var lines []string
	for _, edit := range plan.Edits {
		relative := strings.TrimPrefix(edit.FileName, root+"/")
		lines = append(lines, fmt.Sprintf("%s:%d:%d %s -> %q", relative, edit.Line, edit.Column, edit.Kind, edit.Text))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestRenameFindsEveryReferenceAndNothingElse is the firing half and the declining half in one
// assertion, which is what makes it evidence rather than a count.
//
// The exported `err` has exactly four sites: its declaration, a plain import, the source half of an
// aliased import, and one call. Everything else in the fixture that spells `err` must be declined
// (the shadowed inner binding, the local alias `failure`, and the string key), and each of those is a
// different mechanism, so a rule that got any one wrong shows up here as a different count.
func TestRenameFindsEveryReferenceAndNothingElse(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)
	plan := planFor(test, graph, root, "src/origin.ts", 1, 17, "reportError")

	if len(plan.Refusals) > 0 {
		test.Fatalf("the plan was refused, which it must not be: %v", plan.Refusals)
	}

	got := describeEdits(plan, root)
	want := strings.Join([]string{
		"src/consumer.ts:1:10 reference -> \"reportError\"",
		"src/consumer.ts:2:10 import source -> \"reportError\"",
		"src/consumer.ts:9:12 reference -> \"reportError\"",
		"src/origin.ts:1:17 declaration -> \"reportError\"",
	}, "\n")
	if got != want {
		test.Fatalf("the edit set is wrong.\ngot:\n%s\n\nwant:\n%s", got, want)
	}

	if plan.FilesTouched != 2 {
		test.Fatalf("touched %d files, want 2", plan.FilesTouched)
	}
}

// TestRenameLeavesTheLocalAliasAlone pins the aliased-import half that no count can see.
//
// `import { err as failure }` has two identifiers and they are two symbols. Renaming the export must
// rewrite `err` and leave `failure`, and the failure mode if it does not is a file that still parses
// while referring to an import that no longer exists.
func TestRenameLeavesTheLocalAliasAlone(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)
	plan := planFor(test, graph, root, "src/origin.ts", 1, 17, "reportError")

	for _, edit := range plan.Edits {
		// Column 17 on line 2 is `failure`; column 10 is the `err` half.
		if strings.HasSuffix(edit.FileName, "consumer.ts") && edit.Line == 2 && edit.Column != 10 {
			test.Fatalf("the rename touched the local alias at 2:%d, which belongs to the importing file", edit.Column)
		}
	}
}

// TestRenameExpandsShorthandRatherThanReplacingIt is the correctness requirement whose failure
// compiles.
//
// `{ punned }` is `{ punned: punned }`. The property name is part of the object's contract and is a
// different symbol from the binding, so renaming the binding must produce `{ punned: renamedValue }`
// and never `{ renamedValue }`. Writing the collapsed form changes the shape of the object, which
// type-checks and is wrong at runtime for every consumer reading that property.
func TestRenameExpandsShorthandRatherThanReplacingIt(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)
	plan := planFor(test, graph, root, "src/consumer.ts", 13, 11, "renamedValue")

	if len(plan.Refusals) > 0 {
		test.Fatalf("the plan was refused, which it must not be: %v", plan.Refusals)
	}

	var expansion *Edit
	for index := range plan.Edits {
		if plan.Edits[index].Kind == EditShorthandExpansion {
			expansion = &plan.Edits[index]
		}
	}
	if expansion == nil {
		test.Fatalf(
			"no shorthand expansion in the plan, so `{ punned }` would be left binding to a name that no longer exists.\n%s",
			describeEdits(plan, root),
		)
	}
	if expansion.Text != "punned: renamedValue" {
		test.Fatalf(
			// The dash is written as its escape, the same bytes, so the message reads as it always has.
			"the shorthand writes %q, want %q \u2014 the collapsed form silently rewrites the object's contract",
			expansion.Text,
			"punned: renamedValue",
		)
	}
}

// TestRenameDeclinesAShadowedBinding proves symbol identity is what decides rather than the name.
//
// The inner `err` inside `useBoth` is a different symbol that happens to share a spelling. A rename
// driven by name matching would capture it; one driven by identity must not.
func TestRenameDeclinesAShadowedBinding(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)
	plan := planFor(test, graph, root, "src/origin.ts", 1, 17, "reportError")

	for _, edit := range plan.Edits {
		if strings.HasSuffix(edit.FileName, "consumer.ts") && (edit.Line == 6 || edit.Line == 7) {
			test.Fatalf(
				"the rename captured the shadowed inner binding at line %d, which is a different symbol",
				edit.Line,
			)
		}
	}
}

// TestRenameOfTheShadowedBindingTouchesOnlyItsOwnScope is the other direction of the same property.
// Renaming the inner one must not escape into the outer scope.
func TestRenameOfTheShadowedBindingTouchesOnlyItsOwnScope(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)
	plan := planFor(test, graph, root, "src/consumer.ts", 6, 15, "innerValue")

	if len(plan.Refusals) > 0 {
		test.Fatalf("the plan was refused, which it must not be: %v", plan.Refusals)
	}
	if len(plan.Edits) != 2 {
		test.Fatalf(
			"the inner rename produced %d edits, want 2 (its declaration and its one use).\n%s",
			len(plan.Edits),
			describeEdits(plan, root),
		)
	}
	for _, edit := range plan.Edits {
		if edit.Line != 6 && edit.Line != 7 {
			test.Fatalf("the inner rename escaped its scope, touching line %d", edit.Line)
		}
	}
}

// TestRenameRefusesADeclarationOutsideTheProject is the requirement that bites hardest, because
// getting it wrong rewrites somebody else's package.
//
// `toUpperCase` is declared in the bundled `lib.es5.d.ts`, which is in the program and not in the
// project. Renaming it would rewrite our call site and leave the declaration, or worse, rewrite a
// file we do not own.
func TestRenameRefusesADeclarationOutsideTheProject(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)
	plan := planFor(test, graph, root, "src/consumer.ts", 22, 18, "toUpper")

	if len(plan.Refusals) == 0 {
		test.Fatalf(
			"renaming a declaration outside the project was allowed, which would break every other consumer of that package.\n%s",
			describeEdits(plan, root),
		)
	}
	if !strings.Contains(plan.Refusals[0], "declared outside this project") {
		test.Fatalf("the refusal does not name the cause: %q", plan.Refusals[0])
	}
	if len(plan.Edits) != 0 {
		test.Fatalf("a refused plan carries %d edits, and a refused plan must carry none", len(plan.Edits))
	}
}

// TestRenameRefusesACollision pins the case where the new name is already taken.
//
// A collision may still compile (a shadowing one does), so this cannot be deferred to the type
// phase. Refusing loudly beats writing and hoping something downstream complains.
func TestRenameRefusesACollision(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)
	plan := planFor(test, graph, root, "src/origin.ts", 5, 14, "err")

	if len(plan.Refusals) == 0 {
		test.Fatalf(
			"renaming into a name that is already declared was allowed, which produces a duplicate binding or a silent shadow",
		)
	}
	if !strings.Contains(plan.Refusals[0], "already declared") {
		test.Fatalf("the refusal does not name the cause: %q", plan.Refusals[0])
	}
}

// TestRenameReportsWhatItCouldNotSee is the honesty requirement.
//
// `table['err']` is invisible to the checker and always will be. A rename that misses it produces a
// codebase that compiles and is broken at runtime, and nothing after this phase catches it. So the
// verb must report the blind spot rather than hand back a count that implies completeness.
func TestRenameReportsWhatItCouldNotSee(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)
	plan := planFor(test, graph, root, "src/origin.ts", 1, 17, "reportError")

	if len(plan.Blind) == 0 {
		test.Fatalf(
			"the plan reported no blind spots, but the fixture holds `table['err']`, which the checker cannot see",
		)
	}
	found := false
	for _, blind := range plan.Blind {
		if blind.Text == "err" && blind.Line == 18 {
			found = true
		}
	}
	if !found {
		test.Fatalf("the string-keyed use at line 18 was not reported as invisible: %+v", plan.Blind)
	}
}

// TestRenameRefusesANameThatIsNotAnIdentifier keeps the tool from writing source that cannot parse.
func TestRenameRefusesANameThatIsNotAnIdentifier(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)
	for _, name := range []string{"class", "with a space", "9lives", "has-a-dash", ""} {
		plan := planFor(test, graph, root, "src/origin.ts", 1, 17, name)
		if len(plan.Refusals) == 0 {
			test.Fatalf("renaming to %q was allowed, which would produce source that does not parse", name)
		}
	}
}

// TestRenameRefusesRenamingToTheSameName catches the no-op that would otherwise report edits.
func TestRenameRefusesRenamingToTheSameName(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)
	plan := planFor(test, graph, root, "src/origin.ts", 1, 17, "err")
	if len(plan.Refusals) == 0 {
		test.Fatalf("renaming a symbol to its own name was allowed, which writes bytes for no reason")
	}
}

// TestBareNameRefusesWhenAmbiguous is the interface decision, pinned.
//
// A bare name is not a symbol. `err` names both the exported function and the shadowed inner
// binding, so there is no single right answer and guessing one is worse than refusing.
func TestBareNameRefusesWhenAmbiguous(test *testing.T) {
	test.Parallel()
	graph, _ := buildFixture(test)
	candidates, symbols, resolveBareNameError := ResolveBareName(context.Background(), graph, "err")
	if resolveBareNameError != nil {
		test.Fatalf("resolving the bare name: %v", resolveBareNameError)
	}
	if len(symbols) < 2 {
		test.Fatalf(
			// The dash is written as its escape, the same bytes, so the message reads as it always has.
			"`err` resolved to %d symbols, want at least 2 \u2014 the fixture declares it more than once, "+
				"so a single answer means the search is not seeing them all",
			len(symbols),
		)
	}
	if len(candidates) != len(symbols) {
		test.Fatalf(
			"%d candidates against %d symbols, and the two must correspond one for one",
			len(candidates),
			len(symbols),
		)
	}
}

// TestBareNameResolvesWhenUnambiguous is the other half: the sugar must actually work, or its
// refusal above proves nothing.
func TestBareNameResolvesWhenUnambiguous(test *testing.T) {
	test.Parallel()
	graph, _ := buildFixture(test)
	candidates, symbols, resolveBareNameError := ResolveBareName(context.Background(), graph, "taken")
	if resolveBareNameError != nil {
		test.Fatalf("resolving the bare name: %v", resolveBareNameError)
	}
	if len(symbols) != 1 {
		test.Fatalf("`taken` resolved to %d symbols, want exactly 1", len(symbols))
	}
	if candidates[0].Kind != "variable" {
		test.Fatalf("the candidate is a %q, want a variable", candidates[0].Kind)
	}
}

// TestBareNamePositionRoundTrips is the defect this test was written for, and it is not hypothetical.
//
// The candidate listing prints a position the user is told to paste back, so that position must
// resolve to the same symbol. It did not: the listing derived its column from `node.Pos()`, which
// sits before leading trivia, while the resolver matches against the token's own trimmed span. The
// printed column was short by the indentation and resolving it failed. Same root cause as the one
// `rule.TokenRange` exists to prevent, arriving through the reporting path instead of the fix path.
func TestBareNamePositionRoundTrips(test *testing.T) {
	test.Parallel()
	graph, _ := buildFixture(test)
	candidates, _, resolveBareNameError := ResolveBareName(context.Background(), graph, "err")
	if resolveBareNameError != nil {
		test.Fatalf("resolving the bare name: %v", resolveBareNameError)
	}
	for _, candidate := range candidates {
		plan, buildError := Build(context.Background(), graph, Position{
			FileName: candidate.FileName,
			Line:     candidate.Line,
			Column:   candidate.Column,
		}, "roundTripped")
		if buildError != nil {
			test.Fatalf("the position this tool printed for %s:%d:%d does not resolve: %v",
				candidate.FileName, candidate.Line, candidate.Column, buildError)
		}
		if plan.OldName != "err" {
			test.Fatalf("the printed position resolved to %q rather than to `err`", plan.OldName)
		}
	}
}

// TestApplyProducesATreeThatStillTypechecks is the only end-to-end proof that matters.
//
// A rename that produces a compiling program with different semantics is the failure mode this verb
// exists to hunt, and the only way to know a rename did not do that is to rebuild the program from
// the written bytes and ask the compiler. The fixture is copied first, because this test writes.
func TestApplyProducesATreeThatStillTypechecks(test *testing.T) {
	test.Parallel()
	source := fixturePath(test, "symbols")
	working := test.TempDir()
	copyTree(test, source, working)

	graph, buildError := program.Build(program.Options{
		ConfigFileName:   working + "/tsconfig.json",
		CurrentDirectory: working,
		SingleThreaded:   true,
	})
	if buildError != nil {
		test.Fatalf("building the copied fixture: %v", buildError)
	}

	// The baseline has to be clean, or "still typechecks" afterwards proves nothing. A fixture that
	// was already broken would satisfy this test with a rename that broke it further.
	if before := len(typeDiagnostics(test, graph)); before != 0 {
		test.Fatalf(
			"the fixture does not typecheck before the rename (%d diagnostics), so this test cannot measure anything",
			before,
		)
	}

	plan, buildError := Build(context.Background(), graph, Position{
		FileName: filepath.Join(working, "src/origin.ts"), Line: 1, Column: 17,
	}, "reportError")
	if buildError != nil {
		test.Fatalf("building the plan: %v", buildError)
	}
	if len(plan.Refusals) > 0 {
		test.Fatalf("the plan was refused: %v", plan.Refusals)
	}

	written, applyError := Apply(plan)
	if applyError != nil {
		test.Fatalf("applying the rename: %v", applyError)
	}
	if written != 2 {
		test.Fatalf("wrote %d files, want 2", written)
	}

	rebuilt, buildError := program.Build(program.Options{
		ConfigFileName:   working + "/tsconfig.json",
		CurrentDirectory: working,
		SingleThreaded:   true,
	})
	if buildError != nil {
		test.Fatalf("rebuilding after the rename: %v", buildError)
	}
	if after := typeDiagnostics(test, rebuilt); len(after) != 0 {
		test.Fatalf("the renamed tree does not typecheck: %v", after)
	}

	// The bytes themselves, because "it compiles" is necessary and not sufficient. A rename that
	// collapsed the shorthand or rewrote the local alias would still compile here.
	consumer, readError := os.ReadFile(filepath.Join(working, "src/consumer.ts"))
	if readError != nil {
		test.Fatalf("reading the renamed consumer: %v", readError)
	}
	text := string(consumer)
	if !strings.Contains(text, "import { reportError as failure }") {
		test.Fatalf("the aliased import's source half was not rewritten correctly:\n%s", text)
	}
	if !strings.Contains(text, "const err = 'inner'") {
		test.Fatalf("the shadowed inner binding was captured by the rename:\n%s", text)
	}
	if !strings.Contains(text, "table['err']") {
		test.Fatalf("the string key was rewritten, which the checker cannot justify:\n%s", text)
	}
	if !strings.Contains(text, "return { punned }") {
		test.Fatalf("an unrelated shorthand was disturbed:\n%s", text)
	}
}

// TestApplyRefusesARefusedPlan pins the guard that a refusal is not advisory.
//
// # Why this test asserts the file's bytes rather than only an error
//
// The first version of this test handed Apply a plan pointing at a nonexistent path and asserted
// that some error came back. A mutant disabling the refusal guard entirely survived it, because
// with the guard gone the call still failed: one line later, in `os.ReadFile`, for a completely
// different reason. The test could not tell "refused before touching anything" from "tried to
// apply and fell over", which are the two outcomes it exists to distinguish.
//
// That is the brief's fixtures-assert-the-wrong-layer survivor, and no amount of extra error-shaped
// assertions fixes it. The property being guarded is that a refused plan writes no bytes, so the
// test now points at a real file that Apply could successfully rewrite, and asserts it is unchanged.
func TestApplyRefusesARefusedPlan(test *testing.T) {
	test.Parallel()
	working := test.TempDir()
	target := filepath.Join(working, "writable.ts")
	original := "export const err = 1;\n"
	if writeError := os.WriteFile(target, []byte(original), 0o644); writeError != nil {
		test.Fatalf("seeding the writable file: %v", writeError)
	}

	// An edit that would apply cleanly if the guard were not there: the range covers `err` exactly.
	plan := &Plan{
		OldName:  "err",
		NewName:  "renamed",
		Refusals: []string{"declared outside this project"},
		Edits: []Edit{{
			FileName: target,
			Range:    core.NewTextRange(13, 16),
			Text:     "renamed",
			Kind:     EditDeclaration,
		}},
	}

	written, applyError := Apply(plan)
	if applyError == nil {
		test.Fatalf("a refused plan was applied, which is the half-renamed tree this verb exists to prevent")
	}
	if written != 0 {
		test.Fatalf("a refused plan reported %d files written, and a refused plan must write none", written)
	}

	after, readError := os.ReadFile(target)
	if readError != nil {
		test.Fatalf("reading the target back: %v", readError)
	}
	if string(after) != original {
		test.Fatalf("a refused plan rewrote the file.\ngot:  %q\nwant: %q", string(after), original)
	}
}

// typeDiagnostics reports the compiler's own diagnostics over the project files.
func typeDiagnostics(test *testing.T, graph *program.Graph) []string {
	test.Helper()
	var messages []string
	for _, file := range graph.ProjectFiles() {
		for _, diagnostic := range graph.Diagnostics(context.Background(), file) {
			// `Localize` rather than `MessageText`, which returns the empty string for every
			// diagnostic the checker produces. A failure here would otherwise print a file name and
			// nothing after the colon, telling the reader a rename broke the tree and not how.
			messages = append(messages, fmt.Sprintf("%s: %s", file.FileName(), diagnostic.Localize(locale.Locale{})))
		}
	}
	return messages
}

// copyTree copies a fixture so a writing test never touches the checked-in one.
func copyTree(test *testing.T, source string, destination string) {
	test.Helper()
	walkError := filepath.Walk(source, func(path string, info os.FileInfo, pathError error) error {
		if pathError != nil {
			return pathError
		}
		relative, relativeError := filepath.Rel(source, path)
		if relativeError != nil {
			return relativeError
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		return os.WriteFile(target, contents, info.Mode())
	})
	if walkError != nil {
		test.Fatalf("copying the fixture: %v", walkError)
	}
}

// TestApplyRefusesAPlanThatWouldNotParse pins the last-resort guard.
//
// # Why this test exists when the planner cannot produce such a plan
//
// Every plan `Build` produces replaces an identifier's own span with a valid identifier, so the
// rewritten file parses by construction and no planner-produced input can reach this branch. That
// makes the guard defense in depth rather than a filter, and a mutation disabling it survived the
// whole suite for exactly that reason.
//
// It is still worth guarding and worth testing, because `Plan` is an exported type: a caller can
// hand `Apply` a plan it built itself, and a range off by one token writes the right characters into
// the wrong place, which is the failure `CONTRIBUTING.md` names as having removed an entire function with
// every existing assertion still passing. The test constructs that caller directly rather than
// asserting the branch is unreachable, because "unreachable" is a verdict that expires the moment
// someone adds a caller, and this package exports the type that makes one possible.
func TestApplyRefusesAPlanThatWouldNotParse(test *testing.T) {
	test.Parallel()
	working := test.TempDir()
	target := filepath.Join(working, "broken.ts")
	original := "export const value = 1;\n"
	if writeError := os.WriteFile(target, []byte(original), 0o644); writeError != nil {
		test.Fatalf("seeding the file: %v", writeError)
	}

	// A range that swallows the `const` keyword as well as the name, which is precisely the
	// enclosing-node-instead-of-token mistake. The replacement text is a perfectly good identifier;
	// the range is what is wrong, and the result does not parse.
	plan := &Plan{
		OldName: "value",
		NewName: "renamed",
		Edits: []Edit{{
			FileName: target,
			Range:    core.NewTextRange(7, 18),
			Text:     "renamed",
			Kind:     EditDeclaration,
		}},
	}

	written, applyError := Apply(plan)
	if applyError == nil {
		test.Fatalf("a plan producing unparseable source was written, which corrupts the file for every later phase")
	}
	if !strings.Contains(applyError.Error(), "unparseable") {
		test.Fatalf("the refusal does not name the cause: %v", applyError)
	}
	if written != 0 {
		test.Fatalf("reported %d files written, want 0", written)
	}

	after, readError := os.ReadFile(target)
	if readError != nil {
		test.Fatalf("reading the target back: %v", readError)
	}
	if string(after) != original {
		test.Fatalf("the file was rewritten despite the refusal.\ngot:  %q\nwant: %q", string(after), original)
	}
}

// TestApplyRefusesOverlappingEdits pins that overlapping edits are refused and write nothing.
//
// # What this test does and does not measure
//
// It measures the outcome, which is the property that matters: a plan whose edits collide never
// reaches disk. It does not isolate the explicit overlap guard, and that is stated here because a
// later reader will otherwise assume it does. Disabling that guard leaves this test green, because
// the bounds check inside the apply loop rejects the identical input one step later (enumerated
// over 20,736 ordered range pairs with zero disagreements, recorded at the guard itself).
//
// So this is a subsumption receipt rather than a guard test. The assertion is deliberately on the
// refusal and the unchanged bytes rather than on the message, since the message is the half that
// moves depending on which of the two checks fires.
func TestApplyRefusesOverlappingEdits(test *testing.T) {
	test.Parallel()
	working := test.TempDir()
	target := filepath.Join(working, "overlap.ts")
	original := "export const value = 1;\n"
	if writeError := os.WriteFile(target, []byte(original), 0o644); writeError != nil {
		test.Fatalf("seeding the file: %v", writeError)
	}

	plan := &Plan{
		OldName: "value",
		NewName: "renamed",
		Edits: []Edit{
			{FileName: target, Range: core.NewTextRange(13, 18), Text: "renamed", Kind: EditDeclaration},
			{FileName: target, Range: core.NewTextRange(15, 20), Text: "other", Kind: EditReference},
		},
	}

	written, applyError := Apply(plan)
	if applyError == nil {
		test.Fatalf("overlapping edits were applied, so the tool wrote bytes it could not justify")
	}
	if written != 0 {
		test.Fatalf("reported %d files written, want 0", written)
	}

	after, readError := os.ReadFile(target)
	if readError != nil {
		test.Fatalf("reading the target back: %v", readError)
	}
	if string(after) != original {
		test.Fatalf("the file was rewritten despite the refusal.\ngot:  %q\nwant: %q", string(after), original)
	}
}

// TestRenamingFromAnImportSiteRenamesTheExport pins that a position on an import specifier resolves
// through the alias to the real declaration.
//
// This is what an editor does and it is the behavior a user expects: putting the cursor on an
// imported name and renaming it renames the thing being imported, everywhere, rather than renaming
// one file's local binding. The plan produced from an import site must therefore be identical to the
// plan produced from the declaration site.
//
// Written for a surviving mutant. Dropping the alias resolution in `resolveAnchor` left every
// existing fixture green, because all of them anchored on a declaration where there is no alias to
// follow. The gap was in the fixtures rather than in the code.
func TestRenamingFromAnImportSiteRenamesTheExport(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)

	fromDeclaration := planFor(test, graph, root, "src/origin.ts", 1, 17, "reportError")
	fromImport := planFor(test, graph, root, "src/consumer.ts", 1, 10, "reportError")

	if len(fromImport.Refusals) > 0 {
		test.Fatalf("renaming from an import site was refused: %v", fromImport.Refusals)
	}
	if got, want := describeEdits(fromImport, root), describeEdits(fromDeclaration, root); got != want {
		test.Fatalf(
			"anchoring on the import produced a different plan than anchoring on the declaration.\ngot:\n%s\n\nwant:\n%s",
			got,
			want,
		)
	}
	if fromImport.OldName != "err" {
		test.Fatalf("the import anchor resolved to %q, want `err`", fromImport.OldName)
	}
}

// TestPositionResolvesTheIdentifierUnderTheCursor pins that a column selects the name it points at
// rather than the first name on the line.
//
// # What this test taught, which was not what it was written to show
//
// It was written for a surviving mutant, on the hypothesis that the resolver needed an
// innermost-wins rule because `value.toUpperCase()` nests two identifiers. The mutant survived the
// new fixture too, which per the discipline means the hypothesis was wrong rather than the fixture
// being insufficient.
//
// Probing the parse shape settled it: identifiers are leaf tokens and their spans never overlap
// (25 identifiers across the fixture, zero offsets contained by two spans). `value` and `toUpperCase`
// are siblings, not nested, and the enclosing property-access expression is not an identifier so the
// walk never sees it. First-match and last-match are therefore equivalent, recorded at the line in
// `resolveAnchor`.
//
// The test is kept because the property it actually asserts is still worth holding: a column in the
// middle of a line selects the identifier at that column. It simply is not the guard for the
// mutation that prompted it.
func TestPositionResolvesTheIdentifierUnderTheCursor(test *testing.T) {
	test.Parallel()
	graph, root := buildFixture(test)

	// Column 18 on line 22 is `toUpperCase` in `value.toUpperCase()`. The enclosing property-access
	// expression begins at `value`, so a resolver preferring the outermost match answers `value`.
	plan, buildError := Build(context.Background(), graph, Position{
		FileName: filepath.Join(root, "src/consumer.ts"), Line: 22, Column: 18,
	}, "toUpper")
	if buildError != nil {
		test.Fatalf("resolving the inner identifier: %v", buildError)
	}
	if plan.OldName != "toUpperCase" {
		test.Fatalf(
			// The dash is written as its escape, the same bytes, so the message reads as it always has.
			"the position resolved to %q, want `toUpperCase` \u2014 a column selects the identifier it points at",
			plan.OldName,
		)
	}
}

// A plan or a candidate list that never reached the writer is an error, not a silent gap in what the user
// was shown, and a writer that takes everything returns none. A pipe whose reader is closed refuses every
// write, so it stands in for a closed stdout.
func TestWritersReturnTheWriteError(test *testing.T) {
	test.Parallel()
	plan := &Plan{OldName: "before", NewName: "after", Edits: []Edit{{FileName: "a.ts", Line: 1, Column: 1}}}
	refused := &Plan{OldName: "before", Refusals: []string{"the symbol is declared outside the project"}}
	candidates := []Candidate{{Name: "before", FileName: "a.ts", Line: 1, Column: 1, Kind: "variable"}}
	for name, write := range map[string]func(io.Writer) error{
		"a plan":         func(out io.Writer) error { return Write(out, plan, false) },
		"a refused plan": func(out io.Writer) error { return Write(out, refused, false) },
		"the candidates": func(out io.Writer) error { return WriteCandidates(out, "before", candidates) },
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			reader, writer := io.Pipe()
			if closeError := reader.Close(); closeError != nil {
				test.Fatal(closeError)
			}
			if writeError := write(writer); !errors.Is(writeError, io.ErrClosedPipe) {
				test.Errorf("writing to a closed pipe returned %v, want %v", writeError, io.ErrClosedPipe)
			}
			var buffer bytes.Buffer
			if writeError := write(&buffer); writeError != nil || !strings.Contains(buffer.String(), "before") {
				test.Errorf("writing to a buffer returned %v and printed %q", writeError, buffer.String())
			}
		})
	}
}
