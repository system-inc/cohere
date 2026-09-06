// The run-scoped design system: built once per program, shared read-only across the worker pool.
//
// This is the seam between `internal/tailwind`, which knows how to build a design system and
// nothing about when, and the rules, which need one on every file and must not each build their
// own. No rule reads it yet — swapping the rules over is #4q5dsn3 and #3r6cxrb. What lands here is
// the seam and the proof that it is built once.
//
// # The pattern, and the failure history it carries
//
// Copied deliberately from `internal/rules/structure/boundary_no_project_theme_value.go`, whose own
// comment records why each part is shaped the way it is:
//
//   - **Keyed on the program pointer, not on nothing.** That rule originally memoized in a
//     module-level variable with no key, which is correct for a process that lints once and exits
//     and wrong for anything that lints twice: the second tree reads the first's data. Pointer
//     identity gives a miss on a new program for free. `TestDesignSystemCacheKeyIsTheProgram`
//     mutates the key away and asserts the bug reappears, so the key is load-bearing rather than
//     decorative.
//   - **A package variable rather than `rule.FileCache`.** That cache is per-file by construction,
//     its own comment says sharing an entry across files would be a bug, and it carries no mutex.
//     It cannot live at registration either, since the program is built after rules register.
//   - **The mutex sits at the only access point and the key is immutable for the run**, so this is
//     safe under the parallel walk by construction rather than by discipline.
//
// # Why the build-once property is a test
//
// That same rule measured 2,329ms of setup against 0.54ms of listening across 1,862 files, 64.5% of
// all rule time for a rule that reported nothing, because the scan was redone per file. Nothing
// failed; it was only slow, and the comment still said "once per run".
//
// The design system build is the same shape. Measured on both corpus repositories: 0.68ms per build
// on ~/Projects/ahra, over a 9-file `@import` graph resolving 744 theme entries and 37 `@utility`
// blocks, and 0.64ms on www-connected-app for 748 entries. Per-file across 1,862 files that is 1.27
// seconds, and across the 3,416-file tree the rule catalog is measured on, about 2.3 seconds —
// against a lint phase that currently runs in 0.19s. The tool would get twelve times slower and
// every test would stay green.
//
// So `TestDesignSystemIsBuiltOncePerProgram` walks a real program with a rule that asks for the
// design system on every file, and asserts the process-wide build counter moved by exactly one.
// A counter is the only form of that claim that cannot decay.
//
// # Failing safe
//
// A design system that will not build is no opinion, never a clean tree. The load result carries
// its error and `DesignSystemForProgram` hands it back on every call, so a rule that cannot get one
// must decline and say so. Returning a zero-valued system instead would make every rule report
// nothing on a repository whose CSS moved, which is confident green over unchecked work and the
// exact failure cohere exists to remove.
package tailwind

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/system-inc/cohere/internal/rule"
	tailwindengine "github.com/system-inc/cohere/internal/tailwind"
)

// DesignSystemResult is one program's design system, or the reason there is none.
//
// Both are cached, and caching the failure is the point rather than an oversight. A repository with
// no Tailwind entry point would otherwise pay a full filesystem search on every one of its files to
// rediscover that it has none, which is the per-file cost this whole file exists to refuse, in the
// case where there is not even a design system to show for it.
type DesignSystemResult struct {
	// System is the loaded design system, nil when Err is set.
	System *tailwindengine.LoadedDesignSystem
	// Table is this design system's descriptor table, nil when Err is set.
	//
	// Built here rather than by each rule, and that is the same claim the whole file makes about the
	// design system: a rule computing a sort key needs a table on every class in the repository, and
	// a table built per rule or per file is the per-file rebuild this file exists to refuse, wearing
	// a different name. It costs a few map copies over the framework's roots plus one PropertySort
	// per static `@utility` block, on a path the build counter already guards.
	Table *tailwindengine.Table
	// Err is why no design system could be built, nil when System is set.
	//
	// A rule holding this must decline rather than report zero findings. See the failing-safe note
	// in the file comment.
	Err error
	// EntryPoint is the stylesheet the load was attempted from, empty when none was found.
	EntryPoint string
}

// ErrNoTailwindEntryPoint is returned when a program's project holds no Tailwind stylesheet.
//
// A distinct error rather than a generic one because the two states a caller must tell apart are
// "this project does not use Tailwind" and "this project uses Tailwind and its CSS would not
// parse". The first is silence with nothing wrong; the second is a rule that must decline loudly.
var ErrNoTailwindEntryPoint = errors.New("no Tailwind entry point found in this project")

// designSystemCache holds the design system for one program, so the build happens once per run.
//
// Keyed on the program pointer rather than on nothing, which is what makes this safe to reuse
// across trees: a second program is a miss for free. A package variable rather than
// `rule.FileCache`, which is per-file by construction and carries no mutex. The mutex is at the
// only access point and the key is immutable for the run, so the parallel walk is safe by
// construction. Every one of those three choices has the failure that motivated it recorded in the
// file comment above.
var designSystemCache struct {
	sync.Mutex
	program *compiler.Program
	result  DesignSystemResult
}

// DesignSystemForProgram returns this run's design system, building it at most once.
//
// Every rule that calls this must declare `ReadsProgram: true`. The program reaches every file in
// the run and the stylesheet graph reaches files the program does not contain at all, so a findings
// cache keyed on the linted file alone is stale whenever `theme.css` changes and the `.tsx` file
// does not: zero findings, forever, indistinguishable from a clean tree.
//
// A nil program is a hard miss rather than a shared entry. The harnesses that build a Context by
// hand leave Program nil, and letting them share one cache slot would mean two unrelated fixtures
// reading each other's design system, which is the same bug the pointer key exists to prevent.
func DesignSystemForProgram(ctx rule.Context) DesignSystemResult {
	if ctx.Program == nil {
		return DesignSystemResult{Err: fmt.Errorf("no program: a design system cannot be located without one")}
	}

	designSystemCache.Lock()
	defer designSystemCache.Unlock()

	if designSystemCache.program == ctx.Program {
		return designSystemCache.result
	}

	result := loadDesignSystemForProgram(ctx.Program)
	designSystemCache.program = ctx.Program
	designSystemCache.result = result
	return result
}

// loadDesignSystemForProgram finds the project's stylesheet and builds a design system from it.
//
// Call `DesignSystemForProgram` rather than this: an uncached call re-walks the `@import` graph, and
// the rules that will read it run on every file in the tree.
func loadDesignSystemForProgram(program *compiler.Program) DesignSystemResult {
	projectRoot := projectRootOf(program)
	if projectRoot == "" {
		return DesignSystemResult{Err: fmt.Errorf("could not determine the project root from the program")}
	}

	entryPoint := findTailwindEntryPoint(projectRoot)
	if entryPoint == "" {
		return DesignSystemResult{Err: fmt.Errorf("%w: looked under %s", ErrNoTailwindEntryPoint, projectRoot)}
	}

	packageRoot := findTailwindPackageRoot(filepath.Dir(entryPoint))
	if packageRoot == "" {
		return DesignSystemResult{
			EntryPoint: entryPoint,
			Err: fmt.Errorf(
				"%s: no installed tailwindcss package found, so `@import \"tailwindcss\"` cannot resolve",
				entryPoint,
			),
		}
	}

	system, err := tailwindengine.LoadDesignSystem(tailwindengine.LoadOptions{
		EntryPoint:          entryPoint,
		TailwindPackageRoot: packageRoot,
	})
	if err != nil {
		return DesignSystemResult{EntryPoint: entryPoint, Err: err}
	}
	// The descriptor table is built here, on the counted path, rather than lazily on first use. A
	// second build path would not be visible to TestDesignSystemIsBuiltOncePerProgram, and an
	// invisible build path is exactly how the per-file rebuild came back the last time.
	return DesignSystemResult{System: system, Table: tailwindengine.NewTable(system), EntryPoint: entryPoint}
}

// projectRootOf is the directory the program's tsconfig sits in.
//
// `ConfigFilePath` rather than `GetCurrentDirectory`, because the working directory is where cohere
// was invoked from and the project root is where its config lives, and the two differ whenever
// anyone runs the linter from a parent directory. Falls back to the current directory when the
// program was built without a config file, which is what the in-memory test harnesses do.
func projectRootOf(program *compiler.Program) string {
	if options := program.Options(); options != nil && options.ConfigFilePath != "" {
		return filepath.Dir(options.ConfigFilePath)
	}
	return program.GetCurrentDirectory()
}

// tailwindEntryPointCandidates are the paths a project's root stylesheet is looked for at,
// in order.
//
// A fixed list rather than a search, and that is the decision rather than a shortcut. A glob for
// `**/*.css` on a repository this size walks `node_modules` and every build output, which costs
// more than the design system it is trying to find and can pick a stylesheet nobody imports. Both
// repositories in this port's corpus put theirs at the first path; the rest are the conventional
// Tailwind 4 locations.
//
// A project that puts its stylesheet somewhere else gets ErrNoTailwindEntryPoint, which is a rule
// declining loudly rather than a rule reporting a clean tree. That is the right failure: adding a
// path here is a one-line change, and a wrong-stylesheet guess is a whole run of wrong answers.
var tailwindEntryPointCandidates = []string{
	filepath.Join("app", "_theme", "styles", "theme.css"),
	filepath.Join("app", "globals.css"),
	filepath.Join("src", "app", "globals.css"),
	filepath.Join("styles", "globals.css"),
	filepath.Join("src", "styles", "globals.css"),
	filepath.Join("app", "theme.css"),
}

// findTailwindEntryPoint returns the project's root stylesheet, or empty when there is none.
func findTailwindEntryPoint(projectRoot string) string {
	for _, candidate := range tailwindEntryPointCandidates {
		path := filepath.Join(projectRoot, candidate)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// findTailwindPackageRoot walks up from a directory looking for an installed `tailwindcss`.
//
// Upward from the stylesheet rather than from the project root, because that is how Node resolves
// and because a monorepo hoists: this repository's stylesheet is several directories below the
// `node_modules` that holds its Tailwind. The walk stops at the filesystem root.
func findTailwindPackageRoot(start string) string {
	directory := start
	for {
		candidate := filepath.Join(directory, "node_modules", "tailwindcss")
		if info, err := os.Stat(filepath.Join(candidate, "index.css")); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

// DesignSystemDeclineMessage is what a rule says when it has no design system to work from.
//
// A shared message rather than one per rule, because every rule that reads the design system
// declines for exactly the same reasons and a reader hitting it in two rules should not have to
// decide whether they mean the same thing. It names the entry point when one was found, since "the
// CSS would not parse" and "there is no CSS" are different problems and the fix differs.
func DesignSystemDeclineMessage(ruleName string, result DesignSystemResult) string {
	var where string
	if result.EntryPoint != "" {
		where = " from " + result.EntryPoint
	}
	return fmt.Sprintf(
		"%s could not build this project's Tailwind design system%s, so it is reporting nothing rather "+
			"than reporting a clean tree: %v",
		ruleName, where, result.Err,
	)
}

// resetDesignSystemCacheForTest clears the cache so a test can measure a cold build.
//
// Test-only and named so, because a caller in the shipped path that reset this would reintroduce
// the per-file rebuild this file exists to prevent, and it would do it silently. Not in a _test.go
// file only because the tests that need it live in this package and the one that asserts the cache
// key needs to reach the key itself.
func resetDesignSystemCacheForTest() {
	designSystemCache.Lock()
	defer designSystemCache.Unlock()
	designSystemCache.program = nil
	designSystemCache.result = DesignSystemResult{}
}

// stylesheetsUnder reports the design system's stylesheet graph as project-relative paths.
//
// For reporting and for tests. Absolute paths in a test failure message name a machine rather than
// a repository, and a failure a reader cannot place is one they cannot act on.
func stylesheetsUnder(root string, system *tailwindengine.LoadedDesignSystem) []string {
	if system == nil {
		return nil
	}
	relative := make([]string, 0, len(system.Stylesheets))
	for _, path := range system.Stylesheets {
		if trimmed, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(trimmed, "..") {
			relative = append(relative, trimmed)
			continue
		}
		relative = append(relative, path)
	}
	return relative
}
