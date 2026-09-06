package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// A scoped transform must format what is in scope and skip what is not, and the skip must carry the
// scope's own description.
//
// The skip channel rather than silence is the whole point: a file passed over without a word is
// indistinguishable from a file that was already correctly formatted, and a run that formatted 12 of
// 3,407 files must not print like a run that formatted everything.
func TestAScopedTransformFormatsOnlyWhatIsInScope(t *testing.T) {
	inner := func(_ string, text string) (string, error) {
		return strings.ToUpper(text), nil
	}
	scope := formatScope{
		FileNames:   []string{"/repo/in.ts"},
		index:       map[string]struct{}{"/repo/in.ts": {}},
		Description: "1 changed files (working tree, staged, and untracked)",
	}

	transform := scopedTransform(inner, scope)

	formatted, err := transform("/repo/in.ts", "hello\n")
	if err != nil {
		t.Fatalf("a file in scope was not formatted: %v", err)
	}
	if formatted != "HELLO\n" {
		t.Fatalf("the inner transform did not run: %q", formatted)
	}

	_, err = transform("/repo/out.ts", "hello\n")
	if !errors.Is(err, edit.ErrSkipped) {
		t.Fatalf("a file outside scope should skip, got %v", err)
	}
	if !strings.Contains(err.Error(), "outside the format scope") {
		t.Fatalf("the skip did not say it was a scope decision: %v", err)
	}
	if !strings.Contains(err.Error(), scope.Description) {
		t.Fatalf("the skip did not carry the scope description: %v", err)
	}
}

// A whole-tree scope must not wrap the transform at all, so the flag costs nothing beyond the
// formatting it asks for.
func TestAWholeTreeScopeDoesNotFilter(t *testing.T) {
	transform := scopedTransform(func(_ string, text string) (string, error) {
		return text + "!", nil
	}, wholeTreeScope())

	for _, fileName := range []string{"/anywhere/a.ts", "/elsewhere/b.tsx", "/deep/c.md"} {
		formatted, err := transform(fileName, "x")
		if err != nil {
			t.Fatalf("a whole-tree scope skipped %s: %v", fileName, err)
		}
		if formatted != "x!" {
			t.Fatalf("the inner transform did not run for %s", fileName)
		}
	}
}

// A nil transform stays nil through scoping. Wrapping nothing would produce a transform that
// reports skips for a formatter that does not exist, which is a different and less useful statement
// than "no formatter is configured".
func TestScopingANilTransformStaysNil(t *testing.T) {
	if scopedTransform(nil, wholeTreeScope()) != nil {
		t.Fatalf("a nil transform became non-nil")
	}
	if scopedTransform(nil, formatScope{Description: "some scope"}) != nil {
		t.Fatalf("a nil transform became non-nil under a narrow scope")
	}
}

// An empty scope skips everything, which is what a failed git call must produce.
//
// The alternative, falling back to the whole tree, would turn a failed subprocess into a
// five-minute formatting run nobody asked for. Withholding formatting and saying why is the safe
// direction.
func TestAnEmptyScopeSkipsEverythingAndSaysWhy(t *testing.T) {
	transform := scopedTransform(func(_ string, text string) (string, error) {
		t.Fatalf("the inner transform ran under an empty scope")
		return text, nil
	}, formatScope{Description: "nothing (could not determine what changed: git exploded)"})

	_, err := transform("/repo/a.ts", "x")
	if !errors.Is(err, edit.ErrSkipped) {
		t.Fatalf("expected a skip, got %v", err)
	}
	if !strings.Contains(err.Error(), "could not determine what changed") {
		t.Fatalf("the reason did not survive: %v", err)
	}
}

// changedFilesScope must find real changes in a real repository, and must not report files that did
// not change.
//
// Both directions matter. A resolver that returned everything would silently defeat the scoping;
// one that returned nothing would silently format nothing while reporting a clean run. The second
// is the more dangerous shape and is the one a passing test can hide, so the clean control is here
// deliberately.
func TestChangedFilesScopeFindsRealChanges(t *testing.T) {
	directory := t.TempDir()

	run := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Skipf("git is unavailable or refused (%v): %s", err, output)
		}
	}

	run("init", "--quiet")
	run("config", "user.email", "fixture@example.com")
	run("config", "user.name", "fixture")

	committed := filepath.Join(directory, "committed.ts")
	if err := os.WriteFile(committed, []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "committed.ts")
	run("commit", "--quiet", "-m", "baseline")

	// A clean tree must report nothing changed. This is the control: without it, a resolver that
	// always returned an empty set would pass every other assertion here.
	clean, err := changedFilesScope(directory)
	if err != nil {
		t.Fatalf("unexpected error on a clean tree: %v", err)
	}
	if len(clean.FileNames) != 0 {
		t.Fatalf("a clean tree reported changes: %v", clean.FileNames)
	}

	// Now three kinds of change at once, which is the set a person means by "what I am working on".
	if err := os.WriteFile(committed, []byte("const a = 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(directory, "staged.ts")
	if err := os.WriteFile(staged, []byte("const b = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "staged.ts")
	untracked := filepath.Join(directory, "untracked.ts")
	if err := os.WriteFile(untracked, []byte("const c = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := changedFilesScope(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{committed, staged, untracked} {
		if !changed.includes(want) {
			t.Fatalf("%s changed but is not in scope: %v", want, changed.FileNames)
		}
	}
	if len(changed.FileNames) != 3 {
		t.Fatalf("expected exactly 3 changed files, got %v", changed.FileNames)
	}
	if !strings.Contains(changed.Description, "3 changed files") {
		t.Fatalf("the description does not state the count: %q", changed.Description)
	}
}

// A deleted file has nothing to format and must not enter the scope. Reaching for it would be a
// read of a path that is gone.
func TestADeletedFileIsNotInScope(t *testing.T) {
	directory := t.TempDir()

	run := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Skipf("git is unavailable or refused (%v): %s", err, output)
		}
	}

	run("init", "--quiet")
	run("config", "user.email", "fixture@example.com")
	run("config", "user.name", "fixture")

	doomed := filepath.Join(directory, "doomed.ts")
	if err := os.WriteFile(doomed, []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "doomed.ts")
	run("commit", "--quiet", "-m", "baseline")

	if err := os.Remove(doomed); err != nil {
		t.Fatal(err)
	}

	scope, err := changedFilesScope(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scope.includes(doomed) {
		t.Fatalf("a deleted file is in the format scope: %v", scope.FileNames)
	}
}

// Git quotes paths with unusual characters, and the quotes must not become part of the path. A file
// whose name survives as `"weird name.ts"` never matches anything, so it is never formatted and
// never reported as skipped.
func TestGitQuotedPathsAreUnquoted(t *testing.T) {
	for _, testCase := range []struct {
		raw  string
		want string
	}{
		{`ordinary.ts`, `ordinary.ts`},
		{`"with space.ts"`, `with space.ts`},
		{`"with\"quote.ts"`, `with"quote.ts`},
		{`"with\\backslash.ts"`, `with\backslash.ts`},
		{`"unterminated.ts`, `"unterminated.ts`},
	} {
		if got := unquoteGitPath(testCase.raw); got != testCase.want {
			t.Fatalf("unquoteGitPath(%q) = %q, want %q", testCase.raw, got, testCase.want)
		}
	}
}

// The scope description must distinguish the three cases a reader needs to tell apart, since it is
// the only thing in the output that says what was formatted and why.
func TestScopeDescriptionsAreDistinct(t *testing.T) {
	whole := wholeTreeScope().Description
	narrow := formatScope{Description: "12 changed files (working tree, staged, and untracked)"}.Description
	failed := formatScope{Description: "nothing (could not determine what changed: git exploded)"}.Description

	if whole == narrow || narrow == failed || whole == failed {
		t.Fatalf("two scopes describe themselves identically: %q %q %q", whole, narrow, failed)
	}
	if !strings.Contains(whole, "--format-all") {
		t.Fatalf("the whole-tree description does not name the flag that produced it: %q", whole)
	}
}

// The unquoting must happen where git's output is parsed, not merely exist as a function.
//
// TestGitQuotedPathsAreUnquoted calls unquoteGitPath directly, so it passes whether or not
// gitChangedFiles ever calls it. That is a test of a fragment read as a test of the behavior, and a
// mutant that deleted the call site survived it. This runs the real path: a file whose name needs
// quoting, through git, into the scope.
func TestAQuotedPathReachesTheScopeUnquoted(t *testing.T) {
	directory := t.TempDir()

	run := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Skipf("git is unavailable or refused (%v): %s", err, output)
		}
	}

	run("init", "--quiet")
	run("config", "user.email", "fixture@example.com")
	run("config", "user.name", "fixture")

	// A space forces git to quote the path in porcelain output.
	spaced := filepath.Join(directory, "with space.ts")
	if err := os.WriteFile(spaced, []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	scope, err := changedFilesScope(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !scope.includes(spaced) {
		t.Fatalf("a quoted path did not reach the scope unquoted: %v", scope.FileNames)
	}
	for _, name := range scope.FileNames {
		if strings.Contains(name, `"`) {
			t.Fatalf("a path kept git's quoting: %q", name)
		}
	}
}

// The whole-tree fast path must actually bypass the filter, not merely produce the same answer.
//
// TestAWholeTreeScopeDoesNotFilter passes either way, because a scope with Everything set also makes
// includes return true, so removing the bypass changes nothing it can see. Two guards and one
// assertion between them. This one watches whether the filter was consulted at all, by giving the
// whole-tree scope an index that would exclude the file if anything looked at it.
func TestAWholeTreeScopeBypassesTheFilterEntirely(t *testing.T) {
	inner := func(_ string, text string) (string, error) { return text + "!", nil }

	// Everything is set, and the index deliberately does not contain the file. If the returned
	// transform consults the index at all, this skips.
	scope := wholeTreeScope()
	scope.index = map[string]struct{}{"/some/other/file.ts": {}}

	formatted, err := scopedTransform(inner, scope)("/not/in/the/index.ts", "x")
	if err != nil {
		t.Fatalf("a whole-tree scope consulted its index: %v", err)
	}
	if formatted != "x!" {
		t.Fatalf("the inner transform did not run: %q", formatted)
	}
}

// A git call that fails must return an error, never an empty set.
//
// This is the most dangerous of the three gaps. An empty set from a failed subprocess is
// indistinguishable from a genuinely clean tree, so a broken git would silently format nothing
// while every phase reported success. The caller turns the error into a stated skip reason; it can
// only do that if the error arrives.
func TestAFailedGitCallIsAnErrorNotAnEmptyScope(t *testing.T) {
	// A directory that is not a repository and has no repository above it. t.TempDir is under the
	// system temp directory, which is not inside this checkout.
	outside := t.TempDir()

	scope, err := changedFilesScope(outside)
	if err == nil {
		t.Fatalf("a non-repository reported a scope of %d files instead of an error", len(scope.FileNames))
	}
	if !strings.Contains(err.Error(), "asking git what changed") {
		t.Fatalf("the error does not say what failed: %v", err)
	}
}

// An untracked directory must not enter the scope as a single entry.
//
// This fixture exists because the bug shipped. Git collapses an untracked directory to one porcelain
// line ending in a slash, so `code-quality/` arrived as one "changed file" that matches no real
// path, and every file under it left the scope with nothing saying so. Measured on the ahra tree at
// 01:54: 11 reported entries, three of them directories, hiding 1,958 formattable files. The
// coverage line called them "outside the format scope", which was true and structurally wrong.
//
// Caught by @system_cohere_lint doing arithmetic on the output rather than by any assertion here:
// 11 in scope against a reported split of 3,406 outside and 1 blocked does not reconcile, and the
// ten missing files were the contents of collapsed directories.
func TestAnUntrackedDirectoryDoesNotEnterTheScopeWhole(t *testing.T) {
	directory := t.TempDir()

	run := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Skipf("git is unavailable or refused (%v): %s", err, output)
		}
	}

	run("init", "--quiet")
	run("config", "user.email", "fixture@example.com")
	run("config", "user.name", "fixture")

	seed := filepath.Join(directory, "seed.ts")
	if err := os.WriteFile(seed, []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "seed.ts")
	run("commit", "--quiet", "-m", "baseline")

	// A wholly untracked directory with files at two depths, which is what git collapses.
	nested := filepath.Join(directory, "fresh", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	shallow := filepath.Join(directory, "fresh", "one.ts")
	deep := filepath.Join(nested, "two.ts")
	for _, name := range []string{shallow, deep} {
		if err := os.WriteFile(name, []byte("const b = 1;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	scope, err := changedFilesScope(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both real files must be in scope, at any depth.
	for _, want := range []string{shallow, deep} {
		if !scope.includes(want) {
			t.Fatalf("a file inside an untracked directory is not in scope: %v", scope.FileNames)
		}
	}

	// And no entry may be a directory. A directory in the scope matches nothing and hides everything
	// beneath it.
	for _, name := range scope.FileNames {
		if strings.HasSuffix(name, "/") {
			t.Fatalf("a directory entered the scope: %q", name)
		}
		info, statError := os.Stat(name)
		if statError == nil && info.IsDir() {
			t.Fatalf("a directory entered the scope: %q", name)
		}
	}
}

// The trailing-slash guard is tested directly, because the flag above it makes the guard
// unobservable through behavior.
//
// `--untracked-files=all` already stops git from emitting a directory entry, so deleting the guard
// changes nothing any end-to-end fixture can see. That is exactly the redundancy trap that let a
// deleted fast path survive earlier tonight: two guards where one is load-bearing means neither can
// be shown to work.
//
// The guard is kept rather than deleted, because unlike that case the two are not equivalent. The
// flag is an argument to a subprocess whose behavior is git's to change, and porcelain v1 is a
// stable format precisely so tools can rely on parsing it rather than on invocation flags. So the
// guard stays and is proven here against the input it exists for, rather than proven through a path
// that cannot produce that input.
func TestPorcelainDirectoryEntriesAreRefused(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		porcelain string
		want      []string
	}{
		{
			name:      "a collapsed untracked directory is dropped",
			porcelain: "?? fresh/\n M real.ts\n",
			want:      []string{"real.ts"},
		},
		{
			name:      "a quoted directory is dropped after unquoting",
			porcelain: "?? \"with space/\"\n M real.ts\n",
			want:      []string{"real.ts"},
		},
		{
			name:      "files at depth survive",
			porcelain: "?? fresh/deeper/two.ts\n?? fresh/one.ts\n",
			want:      []string{"fresh/deeper/two.ts", "fresh/one.ts"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := parsePorcelain(testCase.porcelain)
			if len(got) != len(testCase.want) {
				t.Fatalf("expected %v, got %v", testCase.want, got)
			}
			for index := range testCase.want {
				if got[index] != testCase.want[index] {
					t.Fatalf("expected %v, got %v", testCase.want, got)
				}
			}
		})
	}
}

// The scope must state both counts: what git reported and how many of those the program contains.
//
// The two differ because git knows nothing about the tsconfig, so a changed file can be excluded and
// never reach the format phase. Reporting only the git number left a reader doing arithmetic that
// does not work out: 72 changed files producing 3 formatter candidates, with the output explaining
// neither. That is the same shape as the summary-line defect from earlier tonight, where every
// number was right and the composition was misleading.
func TestTheScopeStatesBothCounts(t *testing.T) {
	scope := formatScope{
		FileNames: []string{"/repo/a.ts", "/repo/b.ts", "/repo/excluded.ts"},
		index: map[string]struct{}{
			"/repo/a.ts": {}, "/repo/b.ts": {}, "/repo/excluded.ts": {},
		},
		Description: "3 changed files (working tree, staged, and untracked)",
	}

	narrowed := scope.narrowTo(map[string]struct{}{
		"/repo/a.ts": {}, "/repo/b.ts": {}, "/repo/never-changed.ts": {},
	})

	want := "3 changed files (working tree, staged, and untracked), 2 of them in the program"
	if narrowed.Description != want {
		t.Fatalf("the scope description does not state both counts:\n  want %q\n  got  %q", want, narrowed.Description)
	}

	// Narrowing describes; it must not silently shrink what is in scope, or the two numbers would
	// describe the same population and the distinction would be lost.
	if len(narrowed.FileNames) != 3 {
		t.Fatalf("narrowing changed the scope rather than describing it: %v", narrowed.FileNames)
	}
}

// A whole-tree scope has no git count to reconcile, so narrowing must leave it alone.
func TestNarrowingAWholeTreeScopeChangesNothing(t *testing.T) {
	before := wholeTreeScope()
	after := before.narrowTo(map[string]struct{}{"/repo/a.ts": {}})

	if after.Description != before.Description {
		t.Fatalf("a whole-tree scope was re-described: %q", after.Description)
	}
	if !after.Everything {
		t.Fatalf("narrowing turned a whole-tree scope into a narrow one")
	}
}

// An empty scope must name the directory it asked about.
//
// This is the one answer the resolver cannot distinguish from a wrong question. Git exits zero from
// any directory inside a repository, so a resolver pointed at the wrong tree reports a clean one
// rather than failing, and "0 changed files" reads identically either way.
//
// The rule from `#kjbk9b2`: when the answer you are hoping for is empty, and the command cannot
// tell empty-because-nothing from empty-because-wrong-question, that is the trap and it needs an
// existence check beside it. Naming the directory is that check, on the one line where the
// distinction matters.
//
// Not hypothetical. Auditing for this, I ran a probe from the wrong working directory and read its
// empty output as a result before noticing the binary had failed on a missing tsconfig.
func TestAnEmptyScopeNamesWhereItLooked(t *testing.T) {
	directory := t.TempDir()

	run := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Skipf("git is unavailable or refused (%v): %s", err, output)
		}
	}

	run("init", "--quiet")
	run("config", "user.email", "fixture@example.com")
	run("config", "user.name", "fixture")

	seed := filepath.Join(directory, "seed.ts")
	if err := os.WriteFile(seed, []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "seed.ts")
	run("commit", "--quiet", "-m", "baseline")

	clean, err := changedFilesScope(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(clean.FileNames) != 0 {
		t.Fatalf("the fixture tree is not clean: %v", clean.FileNames)
	}
	if !strings.Contains(clean.Description, directory) {
		t.Fatalf("an empty scope did not say where it looked: %q", clean.Description)
	}

	// And a non-empty scope must not carry the directory, since the count already proves it found
	// the right tree and the path would be noise on every ordinary run.
	if err := os.WriteFile(filepath.Join(directory, "changed.ts"), []byte("const b = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := changedFilesScope(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(changed.Description, directory) {
		t.Fatalf("a non-empty scope carries the directory as noise: %q", changed.Description)
	}
}

// The empty-scope naming must survive narrowing, which is where it died.
//
// `changedFilesScope` names the directory when nothing changed, and `narrowTo` rebuilt the
// description unconditionally two calls later, discarding it. The guard was present, its fixture
// passed, and the behavior was gone: the fixture called `changedFilesScope` directly and never the
// composition the pipeline actually runs.
//
// That is the same defect the guard exists to catch, one layer up. A check that verifies a fragment
// gets read as a check on the behavior, and the gap between them is where this lived. Caught by
// running the real binary and reading its output, not by any assertion.
func TestTheEmptyScopeNamingSurvivesNarrowing(t *testing.T) {
	empty := formatScope{
		FileNames:   nil,
		index:       map[string]struct{}{},
		Description: "0 changed files in /some/where (working tree, staged, and untracked)",
	}

	narrowed := empty.narrowTo(map[string]struct{}{"/repo/a.ts": {}})

	if !strings.Contains(narrowed.Description, "/some/where") {
		t.Fatalf("narrowing discarded the directory an empty scope had named: %q", narrowed.Description)
	}
}

// And the whole path end to end, since the two halves passing separately is what let the defect
// through.
func TestAnEmptyScopeNamesWhereItLookedThroughTheWholePath(t *testing.T) {
	directory := t.TempDir()

	run := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Skipf("git is unavailable or refused (%v): %s", err, output)
		}
	}

	run("init", "--quiet")
	run("config", "user.email", "fixture@example.com")
	run("config", "user.name", "fixture")

	seed := filepath.Join(directory, "seed.ts")
	if err := os.WriteFile(seed, []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "seed.ts")
	run("commit", "--quiet", "-m", "baseline")

	scope, err := changedFilesScope(directory)
	if err != nil {
		t.Fatal(err)
	}

	// Exactly what the pipeline does: resolve, then narrow to the program's files.
	final := scope.narrowTo(map[string]struct{}{seed: {}})

	if !strings.Contains(final.Description, directory) {
		t.Fatalf("the description a run would print does not say where it looked: %q", final.Description)
	}
}

// enumerationOf builds an Enumeration the way the real walk would report one.
func enumerationOf(root string, files []string, walked int, declined map[string]int, nested []string, ignored map[string]int) prettier.Enumeration {
	return prettier.Enumeration{
		Root:               root,
		Walked:             walked,
		Files:              files,
		DeclinedExtensions: declined,
		NestedRepositories: nested,
		IgnoredByLayer:     ignored,
	}
}

// A whole-tree scope must become the enumeration, not the type graph.
//
// This is the ruling's core: the format phase's universe is what the walk found and the engine
// handles. Intersecting with the type graph is what made css, markdown, json and yaml invisible,
// because a tsconfig enumerates TypeScript by construction.
func TestAWholeTreeScopeBecomesTheEnumeration(t *testing.T) {
	files := []string{"/repo/a.ts", "/repo/b.css", "/repo/c.md", "/repo/d.json", "/repo/e.yaml"}
	enumeration := enumerationOf("/repo", files, 12, map[string]int{".rb": 3, "(none)": 4}, nil, nil)

	scope := wholeTreeScope().narrowToEnumeration(enumeration)

	// Every language the engine handles is in scope, which is the thing that was broken.
	for _, fileName := range files {
		if !scope.includes(fileName) {
			t.Fatalf("%s was enumerated and handled but is not in scope: %v", fileName, scope.FileNames)
		}
	}
	if scope.Everything {
		t.Fatalf("the scope stayed whole-tree instead of becoming the enumeration")
	}

	// The file set is the enumeration's, exactly. Falling through to the narrow branch would keep
	// whatever the whole-tree scope had, which is nothing, and every assertion above would still pass
	// because a whole-tree scope's includes returns true for everything it is asked about.
	if len(scope.FileNames) != len(files) {
		t.Fatalf("the scope did not take the enumeration's file set: %v", scope.FileNames)
	}

	// And a file the walk did not find must not be in scope. This is the assertion that catches a
	// whole-tree scope that ignored the enumeration: it would answer true here.
	if scope.includes("/repo/never-walked.ts") {
		t.Fatalf("a file the enumeration never found is in scope, so the enumeration was ignored")
	}
}

// A file the engine cannot handle must be a named zero, not an absence.
//
// This is the requirement the ruling attached and the one place in this pipeline where a file could
// previously go missing without a word: a `.json` in a TypeScript project was never a candidate, so
// the coverage line could not even report it as skipped.
func TestDeclinedExtensionsAreNamedRatherThanOmitted(t *testing.T) {
	enumeration := enumerationOf("/repo", []string{"/repo/a.ts"}, 61, map[string]int{".rb": 3, ".txt": 57, "(none)": 1}, nil, nil)

	description := wholeTreeScope().narrowToEnumeration(enumeration).Description

	for _, want := range []string{"3 .rb", "57 .txt", "1 (none)"} {
		if !strings.Contains(description, want) {
			t.Fatalf("a declined extension is missing from the coverage line: want %q in %q", want, description)
		}
	}
	if !strings.Contains(description, "declined") {
		t.Fatalf("declines are not labelled as declines: %q", description)
	}
}

// The description must say what it walked, so a small number is distinguishable from a wrong root.
func TestTheEnumerationDescriptionNamesTheRootAndTheCounts(t *testing.T) {
	enumeration := enumerationOf("/repo/deep", []string{"/repo/deep/a.ts"}, 900, nil, nil, nil)

	description := wholeTreeScope().narrowToEnumeration(enumeration).Description

	for _, want := range []string{"/repo/deep", "900 files", "1 the formatter handles"} {
		if !strings.Contains(description, want) {
			t.Fatalf("want %q in %q", want, description)
		}
	}
}

// Ignore layers and nested repositories are reported when they removed something.
//
// Nested repositories are named rather than counted because "we skipped a repo" is a fact somebody
// may want to argue with, and a number gives them nothing to argue with. Tonight a whole-tree run
// wrote into a submodule; four nested repositories exist on this tree and three had not been
// mentioned by anyone.
func TestIgnoreLayersAndNestedRepositoriesAreReported(t *testing.T) {
	enumeration := enumerationOf(
		"/repo", []string{"/repo/a.ts"}, 500, nil,
		[]string{"libraries/structure", "projects/ahraos-macos"},
		map[string]int{".gitignore": 400, "PrettierIgnoreDefaults": 28, ".prettierignore": 0},
	)

	description := wholeTreeScope().narrowToEnumeration(enumeration).Description

	for _, want := range []string{"400 by .gitignore", "28 by PrettierIgnoreDefaults", "libraries/structure", "projects/ahraos-macos"} {
		if !strings.Contains(description, want) {
			t.Fatalf("want %q in %q", want, description)
		}
	}
	// A layer that removed nothing is not printed. Printing a zero every run trains the reader to
	// stop looking at the line.
	if strings.Contains(description, "0 by .prettierignore") {
		t.Fatalf("a layer that removed nothing was printed: %q", description)
	}
}

// A changed-files scope keeps its own set and reports how much of it the formatter will see.
func TestANarrowScopeReportsHowMuchIsFormattable(t *testing.T) {
	scope := formatScope{
		FileNames: []string{"/repo/a.ts", "/repo/b.rb", "/repo/c.css"},
		index: map[string]struct{}{
			"/repo/a.ts": {}, "/repo/b.rb": {}, "/repo/c.css": {},
		},
		Description: "3 changed files (working tree, staged, and untracked)",
	}
	// The engine handles the ts and the css; the rb was declined during the walk.
	enumeration := enumerationOf("/repo", []string{"/repo/a.ts", "/repo/c.css"}, 40, map[string]int{".rb": 1}, nil, nil)

	narrowed := scope.narrowToEnumeration(enumeration)

	if !strings.Contains(narrowed.Description, "3 changed files") {
		t.Fatalf("the changed count was lost: %q", narrowed.Description)
	}
	if !strings.Contains(narrowed.Description, "2 the formatter handles") {
		t.Fatalf("the formattable count is wrong or missing: %q", narrowed.Description)
	}
	// The scope keeps all three, so the two numbers describe two populations rather than collapsing.
	if len(narrowed.FileNames) != 3 {
		t.Fatalf("narrowing shrank the scope instead of describing it: %v", narrowed.FileNames)
	}
}

// An empty scope keeps the description that names where it looked, exactly as narrowTo does.
func TestAnEmptyScopeSurvivesEnumerationNarrowing(t *testing.T) {
	empty := formatScope{
		index:       map[string]struct{}{},
		Description: "0 changed files in /some/where (working tree, staged, and untracked)",
	}

	narrowed := empty.narrowToEnumeration(enumerationOf("/repo", []string{"/repo/a.ts"}, 10, nil, nil, nil))

	if !strings.Contains(narrowed.Description, "/some/where") {
		t.Fatalf("enumeration narrowing discarded the directory an empty scope had named: %q", narrowed.Description)
	}
}

// A modified submodule is not a file, and the trailing-slash guard cannot tell.
//
// git reports a submodule whose checked-out commit moved as ` M libraries/structure`: two status
// characters, a space, a path, and no trailing slash, because a gitlink is one index entry rather
// than a directory listing. Every shape-based guard reads that as a file. The mode in the index is
// the only place the difference is stated, which is why the exclusion is asked of git rather than
// inferred from the status line.
func TestParseSubmoduleStageFindsGitlinks(t *testing.T) {
	const stage = "100644 a1c38b42a33df6d3e63d862883e5438950d7be41 0\tapp/Probe.tsx\n" +
		"160000 a1c38b42a33df6d3e63d862883e5438950d7be41 0\tlibraries/structure\n" +
		"100644 b2d49c53b44ef7e4f74e973994f6549061e8cf52 0\tREADME.md\n"

	submodules := parseSubmoduleStage(stage)

	if _, found := submodules["libraries/structure"]; !found {
		t.Error("the gitlink was not recognized as a submodule, so it would be read as a file")
	}
	if len(submodules) != 1 {
		t.Errorf("expected exactly the one gitlink, got %d: %v", len(submodules), submodules)
	}
}

// The other direction: a tree with no submodules must not have anything removed from its scope.
//
// Without this, an exclusion that returned every path would pass the test above and silently empty
// the format scope, which is the failure this whole layer exists to prevent.
func TestParseSubmoduleStageIgnoresOrdinaryFiles(t *testing.T) {
	const stage = "100644 a1c38b42a33df6d3e63d862883e5438950d7be41 0\tapp/Probe.tsx\n" +
		"100755 b2d49c53b44ef7e4f74e973994f6549061e8cf52 0\tscripts/run.sh\n" +
		"120000 c3e5ad64c55f08f5085fa84aa5f765a172f9d063 0\tlink.ts\n"

	if submodules := parseSubmoduleStage(stage); len(submodules) != 0 {
		t.Errorf("a tree with no gitlinks reported %d submodules: %v", len(submodules), submodules)
	}
}
