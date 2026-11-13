package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"

	"github.com/system-inc/cohere/internal/streams"
)

// The unit-isolation step (#h350vqr): a landing that touches a rule's package, its code or its fixtures, runs that
// rule's fixtures under unit isolation on the Mac, after the module's tests. A rule that starts reading another
// declaration's body does it by editing its code while its declared inputs stay the same, so the declaration alone
// cannot say when to look. Home's run is left alone: its script takes no variable of the land's, and widening its
// contract for this is not the gate's to do. A landing that touches no rule directory runs only the isolation core
// the module's tests already hold, the test's own fixture and the pin, about a second.
//
// A change to a shared helper isolates every rule that imports it, and a dispatcher every rule imports moves any
// rule's findings, so a landing that changes one isolates nearly every rule's fixtures, about 140 s on the Mac. That
// is the price of the trigger reading code rather than declarations, and it is right: such a change can move any rule.
//
// A rule's declaration can widen too: a line of the inventory's table, ruleInputsFile, every rule's declaration's one
// home (#qr19hvc), new at the head, or flipped to PerUnit or FileSyntax, claims the rule now judges per unit, so the
// landing isolates that rule as well (widenedRules). A flip to Composes claims less, and needs nothing.

// rulesDirectory is where the Go engine's rules live, relative to the module's root, each in a directory of its own;
// the directory itself holds the registry, registryFile, and the table of every rule's inputs, ruleInputsFile.
const (
	rulesDirectory = "internal/go_engine/rules"
	ruleInputsFile = rulesDirectory + "/rule_inputs.txt"
	registryFile   = rulesDirectory + "/registry.go"
)

// unitIsolationTest is the isolation test, which the step runs by name in unitIsolationPackage. A -run pattern that
// names no test runs none and passes, so TestTheGateRunsTheIsolationTestThatExists holds the file to this name.
const (
	unitIsolationTest    = "TestEveryIsolatedRuleFindsTheSameInTheUnitsAVariantKeeps"
	unitIsolationPackage = "internal/go_engine/check"
	unitIsolationFile    = unitIsolationPackage + "/unit_isolation_test.go"
)

// isolatedRuleDirectories are the rule directories under rulesDirectory whose fixtures a landing changing changed,
// paths relative to the worktree's root with slashes, must isolate, sorted: each rule directory holding a changed
// file, a fixture's among them, and each rule directory that imports, at any depth under rulesDirectory, a package
// there holding one, as a shared helper several rules import is. A rule directory is one below rulesDirectory with a
// testdata directory, which its fixtures are in; rulesDirectory's own testdata is the registry's. A changed file
// outside rulesDirectory, or in it directly, as the registry is, isolates no rule here: a declaration it widens is
// widenedRules', and a change elsewhere reaches every rule alike.
func isolatedRuleDirectories(worktree string, changed []string) ([]string, error) {
	goMod, readError := os.ReadFile(filepath.Join(worktree, "go.mod"))
	if readError != nil {
		return nil, fmt.Errorf("reading the module's go.mod: %w", readError)
	}
	rulesImport := modfile.ModulePath(goMod) + "/" + rulesDirectory + "/"
	// importers is, by package directory under rulesDirectory, the package directories there that import it.
	importers := map[string][]string{}
	packages := map[string]bool{}
	ruleDirectories := map[string]bool{}
	walkError := filepath.WalkDir(filepath.Join(worktree, filepath.FromSlash(rulesDirectory)),
		func(walked string, entry fs.DirEntry, walkError error) error {
			if walkError != nil {
				return walkError
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					relative, relativeError := filepath.Rel(worktree, filepath.Dir(walked))
					if relativeError != nil {
						return relativeError
					}
					// The registry imports every rule, so a rules directory counted as a rule would have any rule's
					// change isolate every fixture.
					if owner := filepath.ToSlash(relative); owner != rulesDirectory {
						ruleDirectories[owner] = true
					}
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(walked, ".go") || strings.HasSuffix(walked, "_test.go") {
				return nil
			}
			relative, relativeError := filepath.Rel(worktree, filepath.Dir(walked))
			if relativeError != nil {
				return relativeError
			}
			importer := filepath.ToSlash(relative)
			packages[importer] = true
			parsed, parseError := parser.ParseFile(token.NewFileSet(), walked, nil, parser.ImportsOnly)
			if parseError != nil {
				return fmt.Errorf("reading the imports of %s: %w", walked, parseError)
			}
			for _, imported := range parsed.Imports {
				importPath, unquoteError := strconv.Unquote(imported.Path.Value)
				if unquoteError != nil || !strings.HasPrefix(importPath, rulesImport) {
					continue
				}
				dependency := rulesDirectory + "/" + strings.TrimPrefix(importPath, rulesImport)
				if !slices.Contains(importers[dependency], importer) {
					importers[dependency] = append(importers[dependency], importer)
				}
			}
			return nil
		})
	if walkError != nil {
		return nil, fmt.Errorf("reading the rules' packages: %w", walkError)
	}
	affected := map[string]bool{}
	var reach func(directory string)
	reach = func(directory string) {
		if affected[directory] {
			return
		}
		affected[directory] = true
		for _, importer := range importers[directory] {
			reach(importer)
		}
	}
	for _, file := range changed {
		if !strings.HasPrefix(file, rulesDirectory+"/") {
			continue
		}
		directory := path.Dir(file)
		// A fixture's file belongs to the rule whose testdata holds it, however deep in it.
		if before, _, inFixture := strings.Cut(file, "/testdata/"); inFixture {
			directory = before
		}
		for directory != rulesDirectory && !packages[directory] && !ruleDirectories[directory] {
			directory = path.Dir(directory)
		}
		if directory != rulesDirectory {
			reach(directory)
		}
	}
	var isolated []string
	for directory := range affected {
		if ruleDirectories[directory] {
			isolated = append(isolated, directory)
		}
	}
	slices.Sort(isolated)
	return isolated, nil
}

// changedAgainstMain are the files commit, a landing's merge, changes against its first parent, the main it lands
// on, relative to the worktree's root with slashes.
func changedAgainstMain(worktree string, commit string) ([]string, error) {
	listing, diffError := gitOutput(worktree, "diff", "--name-only", commit+"^1", commit)
	if diffError != nil {
		return nil, fmt.Errorf("listing what %s changes: %w", shortCommit(commit), diffError)
	}
	if listing == "" {
		return nil, nil
	}
	return strings.Split(listing, "\n"), nil
}

// landingRuleDirectories are the rule directories the landing commit, a merge onto main, must isolate the fixtures
// of: those its changed files reach (isolatedRuleDirectories), and, when it changes the inventory's table, those of
// the rules whose declaration it widens (widenedRules), each once, sorted.
func landingRuleDirectories(worktree string, commit string) ([]string, error) {
	changed, changedError := changedAgainstMain(worktree, commit)
	if changedError != nil {
		return nil, changedError
	}
	directories, directoriesError := isolatedRuleDirectories(worktree, changed)
	if directoriesError != nil {
		return nil, directoriesError
	}
	if !slices.Contains(changed, ruleInputsFile) {
		return directories, nil
	}
	// Ignored: main without the table reads as an empty one, every line new, as a tree before the table had none.
	before, _ := gitOutput(worktree, "show", commit+"^1:"+ruleInputsFile)
	after, afterError := gitOutput(worktree, "show", commit+":"+ruleInputsFile)
	if afterError != nil {
		return nil, fmt.Errorf("reading %s at %s: %w", ruleInputsFile, shortCommit(commit), afterError)
	}
	for _, name := range widenedRules(before, after) {
		directory, directoryError := ruleDirectoryOf(worktree, name)
		if directoryError != nil {
			return nil, directoryError
		}
		if !slices.Contains(directories, directory) {
			directories = append(directories, directory)
		}
	}
	slices.Sort(directories)
	return directories, nil
}

// ruleInputs are the inventory table's lines, each rule's inputs by its name; a line that is a comment, blank or not
// a name and inputs is left out, which the registry's own test refuses.
func ruleInputs(table string) map[string]string {
	inputs := map[string]string{}
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && !strings.HasPrefix(fields[0], "#") {
			inputs[fields[0]] = fields[1]
		}
	}
	return inputs
}

// widenedRules are the rules, by name, whose line of the inventory's table is new in after, or whose inputs became
// PerUnit or FileSyntax there from before's, sorted: each claims now what isolation holds a rule to.
func widenedRules(before string, after string) []string {
	was, now := ruleInputs(before), ruleInputs(after)
	var widened []string
	for name, inputs := range now {
		previous, wasListed := was[name]
		if !wasListed || previous != inputs && (inputs == "PerUnit" || inputs == "FileSyntax") {
			widened = append(widened, name)
		}
	}
	slices.Sort(widened)
	return widened
}

// ruleDirectoryOf is the directory of the rule named name, as the registry runs it: a house rule's, named for it, as
// cohere-go/consistency-no-else-return is consistency_no_else_return; a port's, the package of the analyzer the
// registry's ports table holds under its upstream name; or the one rule directory whose code spells the upstream name,
// as nilness, unusedwrite and U1000 do outside the table. A rule none of them finds is an error, so a widened rule is
// never let through unisolated.
func ruleDirectoryOf(worktree string, name string) (string, error) {
	upstream := strings.TrimPrefix(name, "cohere-go/")
	house := rulesDirectory + "/" + strings.ReplaceAll(upstream, "-", "_")
	if information, statError := os.Stat(filepath.Join(worktree, filepath.FromSlash(house), "testdata")); statError ==
		nil && information.IsDir() {
		return house, nil
	}
	registryPath := filepath.Join(worktree, filepath.FromSlash(registryFile))
	registry, parseError := parser.ParseFile(token.NewFileSet(), registryPath, nil, 0)
	if parseError != nil {
		return "", fmt.Errorf("reading the registry for %s: %w", name, parseError)
	}
	imported := map[string]string{}
	for _, specification := range registry.Imports {
		importPath, unquoteError := strconv.Unquote(specification.Path.Value)
		if unquoteError != nil {
			return "", fmt.Errorf("reading the registry's import %s: %w", specification.Path.Value, unquoteError)
		}
		local := path.Base(importPath)
		if specification.Name != nil {
			local = specification.Name.Name
		}
		imported[local] = importPath
	}
	found := ""
	ast.Inspect(registry, func(node ast.Node) bool {
		entry, isEntry := node.(*ast.KeyValueExpr)
		if !isEntry || found != "" {
			return found == ""
		}
		if key, isKey := entry.Key.(*ast.BasicLit); !isKey || key.Value != strconv.Quote(upstream) {
			return true
		}
		ast.Inspect(entry.Value, func(inner ast.Node) bool {
			if selector, isSelector := inner.(*ast.SelectorExpr); isSelector && found == "" {
				if owner, isOwner := selector.X.(*ast.Ident); isOwner && imported[owner.Name] != "" {
					found = imported[owner.Name]
				}
			}
			return found == ""
		})
		return false
	})
	if _, within, isRule := strings.Cut(found, "/"+rulesDirectory+"/"); isRule {
		return rulesDirectory + "/" + within, nil
	}
	spelled, spelledError := directoriesSpelling(worktree, upstream)
	if spelledError != nil {
		return "", spelledError
	}
	if len(spelled) == 1 {
		return spelled[0], nil
	}
	return "", fmt.Errorf("the landing widens %s's inputs, and no one rule directory is its (%d spell its name), "+
		"so it cannot be isolated", name, len(spelled))
}

// directoriesSpelling are the rule directories whose code, its tests left out, holds upstream as a string literal.
func directoriesSpelling(worktree string, upstream string) ([]string, error) {
	quoted := []byte(strconv.Quote(upstream))
	var spelled []string
	walkError := filepath.WalkDir(filepath.Join(worktree, filepath.FromSlash(rulesDirectory)),
		func(walked string, entry fs.DirEntry, walkError error) error {
			if walkError != nil {
				return walkError
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(walked, ".go") || strings.HasSuffix(walked, "_test.go") ||
				filepath.Dir(walked) == filepath.Join(worktree, filepath.FromSlash(rulesDirectory)) {
				return nil
			}
			contents, readError := os.ReadFile(walked)
			if readError != nil {
				return readError
			}
			relative, relativeError := filepath.Rel(worktree, filepath.Dir(walked))
			if relativeError != nil {
				return relativeError
			}
			if directory := filepath.ToSlash(relative); bytes.Contains(contents, quoted) &&
				!slices.Contains(spelled, directory) {
				spelled = append(spelled, directory)
			}
			return nil
		})
	return spelled, walkError
}

// checkUnitIsolation runs the isolation test over the fixtures of the rule directories commit changes against its
// first parent, the main it lands on, through COHERE_UNIT_ISOLATION=rules and COHERE_UNIT_ISOLATION_RULES. The
// test's failures name the rule and the unit. It runs nothing when commit touches no rule directory, or the tree has no
// isolation test, as a repository other than cohere has none. The boolean reports whether it ran, and the string what
// the land line says of it.
func checkUnitIsolation(worktree string, commit string, environment func() []string) (int, bool, string) {
	if _, statError := os.Stat(filepath.Join(worktree, filepath.FromSlash(unitIsolationFile))); statError != nil {
		return 0, false, ""
	}
	directories, directoriesError := landingRuleDirectories(worktree, commit)
	if directoriesError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", directoriesError)
		return 1, false, ""
	}
	if len(directories) == 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: no rule directory changed, so no rule's fixtures to isolate\n")
		return 0, false, ""
	}
	started := time.Now()
	fmt.Fprintf(streams.Errors, "cohere-dev: isolating the fixtures of %d rule directories: %s\n", len(directories),
		strings.Join(directories, ", "))
	// Cached as any test is, which is sound: the test reads both variables through os.Getenv and every fixture through
	// the os package, which go test's cache keys on, so another set of directories, or a changed fixture, misses it.
	check := exec.Command("go", "test", "-run=^"+unitIsolationTest+"$", "./"+unitIsolationPackage)
	check.Dir = worktree
	check.Env = append(environment(), "COHERE_UNIT_ISOLATION=rules",
		"COHERE_UNIT_ISOLATION_RULES="+strings.Join(directories, ","))
	check.Stdout, check.Stderr = os.Stdout, os.Stderr
	// The command writes to the terminal itself, so what this run said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	if runError := check.Run(); runError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: a changed rule finds otherwise in a unit a variant keeps, so "+
			"nothing landed; the failures above name the rule and the unit\n")
		return exitCodeOf(runError), true, ""
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: unit isolation passed (%s)\n", time.Since(started).Round(time.Second))
	return 0, true, fmt.Sprintf("unit isolation of %d rule directories", len(directories))
}
