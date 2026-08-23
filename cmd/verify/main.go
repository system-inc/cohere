// Command verify type-checks, lints, fixes, and formats a TypeScript codebase in one process,
// over one AST, against one type graph.
//
// See the domain body at `ahra tasks show system_verify` for why this exists and what it replaces.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/scanner"
	"github.com/system-inc/verify/internal/config"
	"github.com/system-inc/verify/internal/fix"
	"github.com/system-inc/verify/internal/program"
	"github.com/system-inc/verify/internal/registry"
	"github.com/system-inc/verify/internal/release"
	"github.com/system-inc/verify/internal/rule"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configFileName := flag.String("tsconfig", "tsconfig.json", "the tsconfig that defines the program")
	directory := flag.String("directory", "", "the working directory paths resolve against (default: the process's own)")
	typesOnly := flag.Bool("types", false, "build the graph and report TypeScript's own diagnostics, running no rules")
	lintOnly := flag.Bool("lint", false, "run the rules, reporting no type diagnostics")
	lintConfigFileName := flag.String("lint-config", ".oxlintrc.json", "the config that says which rules apply to which files")
	singleThreaded := flag.Bool("single-threaded", false, "use one checker instead of several")
	applyFixes := flag.Bool("fix", false, "apply the repairs rules propose, rewriting files in place")
	maxFixPasses := flag.Int("fix-passes", fix.DefaultMaxPasses, "how many times a file may be re-linted while fixes keep landing")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		// The full provenance rather than a bare number, because the number alone does not identify
		// what ran: the pinned typescript-go commit is most of the code in this binary and moves
		// independently of the version. A bug report that names all of it is reproducible.
		fmt.Println(release.Current())
		return nil
	}

	// Neither flag means both phases, which is what a bare `verify` should do.
	runTypes := *typesOnly || !*lintOnly
	runLint := *lintOnly || !*typesOnly

	ctx := context.Background()

	buildStart := time.Now()
	graph, err := program.Build(program.Options{
		ConfigFileName:   *configFileName,
		CurrentDirectory: *directory,
		SingleThreaded:   *singleThreaded,
	})
	if err != nil {
		// A program that fails to build is a loud failure and never an empty result. An empty file list
		// is indistinguishable from a clean tree, and that confusion is what let the gate this replaces
		// print green over zero files for days.
		return fmt.Errorf("building the type graph: %w", err)
	}
	buildDuration := time.Since(buildStart)

	projectFiles := graph.ProjectFiles()

	fmt.Printf(
		"graph built in %s — %d files in the program, %d of them ours\n",
		round(buildDuration), len(graph.SourceFiles()), len(projectFiles),
	)

	findings := 0

	if runTypes {
		typesStart := time.Now()
		typeDiagnostics := collectTypeDiagnostics(ctx, graph, projectFiles)
		typesDuration := time.Since(typesStart)

		for _, diagnostic := range typeDiagnostics {
			printCompilerDiagnostic(diagnostic)
		}
		findings += len(typeDiagnostics)

		fmt.Printf(
			"types: %d diagnostics over %d files in %s\n",
			len(typeDiagnostics), len(projectFiles), round(typesDuration),
		)
	}

	if runLint {
		rules := registry.All()

		// The lint config decides which rules apply to which files, and it is loaded rather than
		// assumed. Without it every rule is all-or-nothing across the tree, which is how verify came
		// to report 336 findings inside a generated directory the gate correctly scopes off.
		//
		// A config that cannot be read is a hard failure and never a permissive default. Linting
		// everything with nothing configured produces output indistinguishable from a clean run, and
		// that exact confusion is what this tool exists to make impossible.
		lintConfig, err := config.Load(resolveLintConfigPath(*lintConfigFileName, *directory))
		if err != nil {
			return fmt.Errorf("loading the lint config: %w", err)
		}
		graph.LintConfig = lintConfig
		graph.RuleOptions = registry.Options()

		lintStart := time.Now()
		result, err := graph.Walk(ctx, projectFiles, rules)
		if err != nil {
			return fmt.Errorf("running rules: %w", err)
		}
		lintDuration := time.Since(lintStart)

		for _, diagnostic := range result.Diagnostics {
			printRuleDiagnostic(diagnostic)
		}
		findings += len(result.Diagnostics)

		// Coverage prints unconditionally, alongside the verdict rather than behind a flag. A run that
		// checked nothing must not be able to look like a run that found nothing, and the only way to
		// guarantee that is to make the population as visible as the findings.
		fmt.Printf(
			"lint: %d findings — %d rules over %d files, %d nodes visited, in %s\n",
			len(result.Diagnostics), result.Coverage.RulesRun, result.Coverage.FilesWalked,
			result.Coverage.NodesVisited, round(lintDuration),
		)
		printRuleCoverage(rules, result.Coverage)
		printSuppressionCoverage(result.Coverage)
		printConfigCoverage(result.Coverage)

		// Fixing runs after reporting rather than before it, so the findings a reader sees are the
		// ones that were actually there when the run started. Fixing first and then reporting would
		// print a shorter list than the tool found, which is the same class of lie as a coverage
		// number that omits what it skipped.
		if *applyFixes {
			fixSummary, err := applyProposedFixes(ctx, graph, projectFiles, rules, *maxFixPasses)
			if err != nil {
				return fmt.Errorf("applying fixes: %w", err)
			}
			fmt.Println(fixSummary)

			// The findings that were repaired are no longer reasons to fail. Anything left is, which
			// is why the count is reduced by what landed rather than reset.
			findings -= fixSummary.FixesApplied
			if findings < 0 {
				findings = 0
			}
		}
	}

	if findings > 0 {
		os.Exit(1)
	}
	return nil
}

// collectTypeDiagnostics gathers the compiler's own findings for our files.
//
// The program is checked as a whole and then filtered down, rather than asked file by file. That
// ordering is load-bearing for speed and not at all for correctness: see Graph.AllDiagnostics. It
// does mean the compiler also checks the 6,500 third-party declarations, which is unavoidable — they
// have to be checked for our files to mean anything — and their findings are dropped here because
// nobody working in this repo can act on them.
func collectTypeDiagnostics(ctx context.Context, graph *program.Graph, files []*ast.SourceFile) []*ast.Diagnostic {
	ours := make(map[*ast.SourceFile]struct{}, len(files))
	for _, sourceFile := range files {
		ours[sourceFile] = struct{}{}
	}

	diagnostics := graph.ConfigDiagnostics(ctx)
	for _, diagnostic := range graph.AllDiagnostics(ctx) {
		// A diagnostic with no file is about the program rather than about any one file, so it is ours
		// by default: dropping it would hide exactly the configuration errors that matter most.
		if sourceFile := diagnostic.File(); sourceFile != nil {
			if _, isOurs := ours[sourceFile]; !isOurs {
				continue
			}
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	return diagnostics
}

// printCompilerDiagnostic prints one of TypeScript's own findings, in the shape tsc uses so an
// editor's problem matcher recognizes it.
func printCompilerDiagnostic(diagnostic *ast.Diagnostic) {
	sourceFile := diagnostic.File()
	if sourceFile == nil {
		fmt.Printf("error TS%d: %s\n", diagnostic.Code(), diagnostic.Message())
		return
	}

	line, character := scanner.GetLineAndCharacterOfPosition(sourceFile, diagnostic.Loc().Pos())
	fmt.Printf(
		"%s:%d:%d - error TS%d: %s\n",
		sourceFile.FileName(), line+1, character+1, diagnostic.Code(), diagnostic.Message(),
	)
}

// printRuleDiagnostic prints one rule finding in the same shape, with the rule name where the error
// code goes — a reader should not have to learn two formats.
func printRuleDiagnostic(diagnostic rule.Diagnostic) {
	sourceFile := diagnostic.SourceFile
	if sourceFile == nil {
		fmt.Printf("error %s: %s\n", diagnostic.RuleName, diagnostic.Message.Description)
		return
	}

	line, character := scanner.GetLineAndCharacterOfPosition(sourceFile, diagnostic.Range.Pos())
	fmt.Printf(
		"%s:%d:%d - %s [%s/%s]\n",
		sourceFile.FileName(), line+1, character+1,
		diagnostic.Message.Description, diagnostic.RuleName, diagnostic.Message.Id,
	)
}

// printRuleCoverage names any rule that never listened to a single file.
//
// A rule that declines every file is indistinguishable, by finding count alone, from a rule that ran
// and found nothing — and four rules in the gate this replaces were silently dead for months
// underneath exactly that ambiguity. Naming them is the cheapest possible guard.
func printRuleCoverage(rules []rule.Rule, coverage program.Coverage) {
	silent := []string{}
	for _, subject := range rules {
		if coverage.RulesListening[subject.Name] == 0 {
			silent = append(silent, subject.Name)
		}
	}
	if len(silent) == 0 {
		return
	}

	sort.Strings(silent)
	for _, name := range silent {
		fmt.Printf("  note: rule %s listened to no files — it ran, but it never looked\n", name)
	}
}

// printSuppressionCoverage says what the run chose not to tell you.
//
// This is the coverage discipline pointed the other way. The line above stops a run that checked
// nothing from printing the same green as a run that found nothing. This stops a run that withheld
// forty findings from printing the same green as a run that had none to withhold. A suppression is
// a decision someone made, and a decision that leaves no trace in the output is indistinguishable
// from the tool being blind — which is the exact confusion that let seven real findings look like
// agreement between two linters.
//
// The reasonless count is printed rather than enforced, deliberately. Measured across the codebase
// verify gates, 281 of the 306 suppressions naming one of our own rules state no reason, so
// requiring one today turns working code red for no defect. Printing it every run puts the number
// in front of us, which is what lets the convention be tightened later from evidence rather than
// from a guess.
func printSuppressionCoverage(coverage program.Coverage) {
	if coverage.Suppressed == 0 && coverage.UnusedSuppressions == 0 {
		return
	}

	fmt.Printf(
		"  suppressed: %d findings silenced by a disable comment, %d of them without a stated reason\n",
		coverage.Suppressed, coverage.SuppressedWithoutReason,
	)

	if coverage.UnusedSuppressions > 0 {
		// An unused suppression is a rule scoped off a file that no longer needs it, and it is how a
		// codebase accumulates permanent exemptions nobody chose. It is a note rather than a finding
		// because a filtered run makes every directive for an unselected rule look unused, and a
		// number that is wrong under a common flag should not fail a build.
		fmt.Printf(
			"  note: %d disable comments silenced nothing — they may be scoping off a rule that no longer fires\n",
			coverage.UnusedSuppressions,
		)
	}
}

// resolveLintConfigPath finds the lint config relative to the directory paths resolve against.
//
// A relative config path resolved against the process's own working directory rather than the
// project's would quietly find nothing, and "nothing" here means every rule runs everywhere.
func resolveLintConfigPath(configPath string, directory string) string {
	if filepath.IsAbs(configPath) || directory == "" {
		return configPath
	}
	return filepath.Join(directory, configPath)
}

// printConfigCoverage says what the configuration excluded.
//
// A file skipped by an ignorePattern and a file with no findings produce identical output
// otherwise, and a rule scoped off across a directory looks exactly like a rule with nothing to
// report. Both are decisions someone made, and a decision that leaves no trace in the output is
// indistinguishable from the tool never having looked.
func printConfigCoverage(coverage program.Coverage) {
	if coverage.FilesIgnored > 0 {
		fmt.Printf("  config: %d files excluded by ignorePatterns\n", coverage.FilesIgnored)
	}

	scopedOff := make([]string, 0, len(coverage.RulesScopedOff))
	for name := range coverage.RulesScopedOff {
		scopedOff = append(scopedOff, name)
	}
	sort.Strings(scopedOff)
	for _, name := range scopedOff {
		fmt.Printf("  config: rule %s scoped off for %d files by the config\n", name, coverage.RulesScopedOff[name])
	}

	// Reported separately from scoped-off on purpose. "Someone turned this rule off" and "nobody has
	// said whether this rule should run" are different facts, and a rule newly added to the registry
	// is a decision waiting to be made rather than one already made. Collapsing them would describe
	// a brand-new rule as though it had been deliberately excluded.
	unconfigured := make([]string, 0, len(coverage.RulesUnconfigured))
	for name := range coverage.RulesUnconfigured {
		unconfigured = append(unconfigured, name)
	}
	sort.Strings(unconfigured)
	for _, name := range unconfigured {
		fmt.Printf("  config: rule %s is not in the config, so it ran on no files — nobody has said whether it should\n", name)
	}
}

// round trims a duration to milliseconds, which is the resolution any of these numbers means
// anything at.
func round(duration time.Duration) time.Duration {
	return duration.Round(time.Millisecond)
}
