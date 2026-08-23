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

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
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
	fixOnly := flag.Bool("fix", false, "fix and format only, running no other phase")
	noFix := flag.Bool("no-fix", false, "mutate nothing: report what would change without writing a byte")
	formatAll := flag.Bool("format-all", false, "format every file rather than only the ones that changed")
	// A binary that implements no rule and a binary whose rule found nothing produce the same empty
	// finding list, and the differential harness cannot tell them apart from the outside. This is how
	// it asks.
	listRules := flag.Bool("rules", false, "print the rules this binary implements, one per line, and exit")
	// Off by default until the engine is shown to agree with the existing gate across the real
	// corpus. Reformatting the tree away from what the gate produces is worse than not formatting,
	// so enabling is a separate decision from wiring.
	format := flag.Bool("format", false, "run the formatter over the candidate files")
	maxFixPasses := flag.Int("fix-passes", fix.DefaultMaxPasses, "how many times a file may be re-linted while fixes keep landing")
	showTiming := flag.Bool("timing", false, "report what each rule cost, most expensive first")
	explainFile := flag.String("explain", "", "report what every rule did on one file, and why it did or did not run")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *listRules {
		// Sorted so two binaries can be diffed directly. The registry's own order is the order rules
		// were added, which is meaningful to a reader and useless to `diff`.
		names := make([]string, 0, len(registry.All()))
		for _, registered := range registry.All() {
			names = append(names, registered.Name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Println(name)
		}
		return nil
	}

	if *showVersion {
		// The full provenance rather than a bare number, because the number alone does not identify
		// what ran: the pinned typescript-go commit is most of the code in this binary and moves
		// independently of the version. A bug report that names all of it is reproducible.
		fmt.Println(release.Current())
		return nil
	}

	// A bare `verify` runs the whole pipeline. The phase flags isolate one phase for someone
	// debugging it; they are not the default path, and nothing about running the tool with no
	// arguments should be a partial check.
	anyPhaseNamed := *typesOnly || *lintOnly || *fixOnly
	runFix := *fixOnly || !anyPhaseNamed
	runTypes := *typesOnly || !anyPhaseNamed
	runLint := *lintOnly || !anyPhaseNamed

	// `--no-fix` mutates nothing, which is what continuous integration needs and what anyone asking
	// "what would this change" needs. It wins over `--fix` rather than erroring, because the safe
	// reading of a contradictory pair is the one that does not write to disk.
	mutate := runFix && !*noFix
	if *noFix && *fixOnly {
		// Naming both is a contradiction the user should hear about rather than have silently
		// resolved, since one of the two was certainly not what they meant.
		return fmt.Errorf("--fix and --no-fix contradict each other: --fix runs only the mutating phase, --no-fix mutates nothing")
	}

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
	report := &pipelineReport{}

	// Phase 2: fix and format. Mutation runs before anything reports, so every phase downstream sees
	// the repaired tree rather than findings a fixer would have silently repaired.
	//
	// The lint config is loaded here because fixing needs to know which rules apply to which files,
	// and loading it once serves both this phase and lint below.
	var lintConfig *config.Config
	if runFix || runLint {
		// A config that cannot be read is a hard failure and never a permissive default. Linting
		// everything with nothing configured produces output indistinguishable from a clean run, and
		// that exact confusion is what this tool exists to make impossible.
		loaded, err := config.Load(resolveLintConfigPath(*lintConfigFileName, *directory))
		if err != nil {
			return fmt.Errorf("loading the lint config: %w", err)
		}
		lintConfig = loaded
		graph.LintConfig = lintConfig
		graph.RuleOptions = registry.Options()
	}

	switch {
	case !runFix:
		report.record(phaseFix, outcomeSkipped, 0, "not requested")
	case !mutate:
		report.record(phaseFix, outcomeSkipped, 0, "--no-fix")
	default:
		// Formatting is scoped to changed files by default, and the scope is resolved before the phase
		// runs so its description can be reported whether or not anything was formatted.
		//
		// Measured: the formatter is 83 to 100ms per file with no warm-up, so the whole tree is 4.7 to
		// 5.7 minutes against a lint phase of 392ms. That is not a tuning problem, it is a different
		// tool, and a gate nobody waits for is a gate that does not exist. Real churn here is one file
		// per commit and 35 across five, so changed-files puts the common case in the tens of
		// milliseconds.
		scope := wholeTreeScope()
		if !*formatAll {
			resolved, scopeError := changedFilesScope(graph.Config.GetCurrentDirectory())
			if scopeError != nil {
				// Falling back to the whole tree would turn a failed subprocess into a five-minute
				// surprise, so the scope becomes empty and says why. Fixing still runs; only formatting
				// is withheld, and the reason reaches the coverage line.
				resolved = formatScope{Description: fmt.Sprintf("nothing (could not determine what changed: %v)", scopeError)}
			}
			scope = resolved
		}

		// Built before the fix phase rather than inside it, so a formatter that cannot load its
		// bundles stops the run here with a reason rather than degrading into the nil that means
		// nobody asked for one.
		formatter, err := configuredFormatter(*format)
		if err != nil {
			return err
		}

		// The format phase gets its own universe, and this is where the two narrowings part.
		//
		// A proposed fix comes from a rule that ran over the program, so its candidate must be in the
		// program. A format candidate comes from the disk, and intersecting it with the type graph is
		// what made css, markdown, json and yaml invisible: a tsconfig enumerates TypeScript by
		// construction. Formatting is the only phase whose subject is not the program.
		//
		// The walk is lazy, and the reason is not cost. An enumeration on a run with nothing changed
		// produces a true number whose only effect is to make a no-op run look like work, which is the
		// same shape as a count claiming files were reformatted that nobody touched.
		switch {
		case formatter == nil:
			// No formatter means nothing to enumerate for. The scope keeps its type-graph narrowing so
			// the fix phase's own reporting is unchanged.
			inProgram := make(map[string]struct{}, len(projectFiles))
			for _, sourceFile := range projectFiles {
				inProgram[sourceFile.FileName()] = struct{}{}
			}
			scope = scope.narrowTo(inProgram)

		case !scope.Everything && len(scope.FileNames) == 0:
			// Nothing changed, so the walk cannot affect the outcome. The scope already names where it
			// looked, which is the honest thing to print here.

		default:
			enumeration, enumerateError := formatter.Enumerate(
				graph.Config.GetCurrentDirectory(),
				resolveStructureIgnorePath(*directory),
			)
			if enumerateError != nil {
				// A failed walk withholds formatting and says why, rather than falling back to a
				// universe that would format the wrong set. Fixing still runs.
				scope = formatScope{Description: fmt.Sprintf("nothing (could not enumerate the tree: %v)", enumerateError)}
			} else {
				scope = scope.narrowToEnumeration(enumeration)
			}
		}

		fixStart := time.Now()
		fixSummary, err := applyProposedFixes(
			ctx, graph, projectFiles, registry.All(),
			scopedTransform(formatTransform(formatter), scope),
			scope.formatCandidates(),
			*maxFixPasses,
		)
		fixDuration := time.Since(fixStart)
		if err != nil {
			// The bail condition here is a failure to produce valid output, never a finding. A fixer
			// that cannot write a parseable file means the edit was malformed and everything after it
			// is meaningless, so the pipeline stops and says which phases never ran.
			report.record(phaseFix, outcomeRan, fixDuration, "")
			report.markRemainingNotReached(phaseFix, err.Error())
			report.Write(os.Stdout)
			return fmt.Errorf("fix: %w", err)
		}
		fmt.Println(fixSummary)

		// The scope is stated on every run rather than inferred from a file count. Working-tree
		// changes, staged changes, and a base-branch diff are three different answers to "what
		// changed", and a reader cannot tell which one they got from a number alone.
		fmt.Printf("format scope: %s\n", scope.Description)
		report.record(phaseFix, outcomeRan, fixDuration, "")

		// Files were rewritten, so the graph built from the old bytes no longer describes the tree.
		// Every phase after this must read the new text or it reports findings against source that no
		// longer exists — which is the same stale-read corruption the edit engine refuses internally,
		// one level up.
		if fixSummary.FilesChanged > 0 && (runTypes || runLint) {
			rebuiltGraph, rebuildDuration, err := rebuildGraph(*configFileName, *directory, *singleThreaded, lintConfig)
			if err != nil {
				report.markRemainingNotReached(phaseFix, fmt.Sprintf("the graph could not be rebuilt after fixing: %v", err))
				report.Write(os.Stdout)
				return fmt.Errorf("rebuilding the type graph after fixing: %w", err)
			}
			graph = rebuiltGraph
			projectFiles = graph.ProjectFiles()
			fmt.Printf(
				"graph rebuilt in %s — %d files changed, so every later phase reads the new text\n",
				round(rebuildDuration), fixSummary.FilesChanged,
			)
		}
	}

	// Phase 3: types. This is the phase that bails alone and loudly.
	if !runTypes {
		report.record(phaseTypes, outcomeSkipped, 0, "not requested")
	} else {
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
		report.record(phaseTypes, outcomeRan, typesDuration, "")

		// Types gate lint. Rule findings against code whose semantics are wrong are noise the reader
		// has to re-read after fixing the real problem, so one type error alone at the top beats one
		// type error buried under a hundred style findings in a file that does not compile.
		//
		// Bailing here is only honest because the phase line says lint did not run. Without it, a
		// bailed run and a clean lint print the same absence of findings.
		if len(typeDiagnostics) > 0 && runLint {
			report.markRemainingNotReached(
				phaseTypes,
				fmt.Sprintf("%d type diagnostics — lint findings against wrong semantics are noise", len(typeDiagnostics)),
			)
			report.Write(os.Stdout)
			os.Exit(1)
		}
	}

	if !runLint {
		report.record(phaseLint, outcomeSkipped, 0, "not requested")
	}

	if runLint {
		rules := registry.All()

		// The config was loaded above, before the fix phase, because fixing needs to know which rules
		// apply to which files. Only the timing switch is per-phase.
		graph.CollectTimings = *showTiming

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

		if *showTiming {
			printTimings(os.Stdout, result.Timings, lintDuration)
		}

		if *explainFile != "" {
			// Explained after the run rather than instead of it, so the reader sees the whole-tree
			// verdict and the single-file account together. Asking why one file behaved the way it
			// did is usually a question about a run that already happened.
			subject := findExplainSubject(projectFiles, *explainFile)
			if subject == nil {
				// Naming the file that was not found rather than explaining nothing, because an
				// empty explanation reads as a file with nothing to say.
				fmt.Printf("\nexplain: %s is not in the program, so there is nothing to explain\n", *explainFile)
			} else {
				explanation, err := graph.Explain(ctx, subject, rules)
				if err != nil {
					return fmt.Errorf("explaining %s: %w", *explainFile, err)
				}
				printExplanation(os.Stdout, explanation)
			}
		}

		report.record(phaseLint, outcomeRan, lintDuration, "")
	}

	// The phase line prints on every run, success included. A run that checked nothing must not be
	// able to print like a run that checked everything and found it clean, and a phase summary that
	// only appeared on failure would reintroduce exactly that ambiguity for the successful case.
	report.Write(os.Stdout)

	if findings > 0 {
		os.Exit(1)
	}
	return nil
}

// rebuildGraph reconstructs the type graph after files on disk have changed.
//
// The graph is a snapshot of the bytes as they were when it was built. Once the fix phase rewrites
// a file, every later phase reading that graph is reading source that no longer exists — the same
// stale-read corruption the edit engine refuses internally, one level up and with a wider blast
// radius, because a type diagnostic against deleted text points at a line nobody can find.
//
// It costs a full rebuild, which is why it only happens when something actually changed.
func rebuildGraph(
	configFileName string,
	directory string,
	singleThreaded bool,
	lintConfig *config.Config,
) (*program.Graph, time.Duration, error) {
	start := time.Now()
	rebuilt, err := program.Build(program.Options{
		ConfigFileName:   configFileName,
		CurrentDirectory: directory,
		SingleThreaded:   singleThreaded,
	})
	if err != nil {
		return nil, time.Since(start), err
	}
	rebuilt.LintConfig = lintConfig
	rebuilt.RuleOptions = registry.Options()
	return rebuilt, time.Since(start), nil
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
		fmt.Printf("error TS%d: %s\n", diagnostic.Code(), diagnostic.MessageText())
		return
	}

	line, character := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, diagnostic.Loc().Pos())
	fmt.Printf(
		"%s:%d:%d - error TS%d: %s\n",
		sourceFile.FileName(), line+1, character+1, diagnostic.Code(), diagnostic.MessageText(),
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

	line, character := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, diagnostic.Range.Pos())
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
