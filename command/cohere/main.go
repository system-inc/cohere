// Command cohere type-checks, lints, fixes, and formats a TypeScript codebase in one process,
// over one AST, against one type graph.
//
// See the domain body at `ahra tasks show system_cohere` for why this exists and what it replaces.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/locale"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/configuration"
	"github.com/system-inc/cohere/internal/fix"
	"github.com/system-inc/cohere/internal/program"
	"github.com/system-inc/cohere/internal/registry"
	"github.com/system-inc/cohere/internal/release"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/unused_code_report"
)

// processStart is stamped before anything else runs, so the phase line can say how much of the run
// its own numbers explain. A package-level variable rather than a parameter because `run` already
// takes none, and the value has exactly one writer, at initialization.
var processStart = time.Now()

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "cohere: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// A subcommand is matched before flag.Parse, because the flag package stops at the first
	// argument it does not recognize and would hand `rename` back as a positional while
	// swallowing the flags meant for it. The dispatcher forwards unknown arguments untouched, so the
	// verb arrives here intact.
	//
	// This is a verb rather than a flag on the pipeline for the reason set out on runRenameVerb: a
	// rename is imperative where every other write here is reactive, and it must not inherit the
	// default-on mutation that `--fix` correctly has.
	if isRenameVerb(os.Args[1:]) {
		return runRenameVerb(os.Args[2:])
	}

	configFileName := flag.String("tsconfig", "tsconfig.json", "the tsconfig that defines the program")
	directory := flag.String("directory", "", "the working directory paths resolve against (default: the process's own)")
	typesOnly := flag.Bool("types", false, "build the graph and report TypeScript's own diagnostics, running no rules")
	lintOnly := flag.Bool("lint", false, "run the rules, reporting no type diagnostics")
	lintConfigFileName := flag.String("lint-config", "CohereSettings.json", "the config that says which rules apply to which files")
	singleThreaded := flag.Bool("single-threaded", false, "use one checker instead of several")
	fixOnly := flag.Bool("fix", false, "fix and format only, running no other phase")
	noFix := flag.Bool("no-fix", false, "mutate nothing: report what would change without writing a byte")
	formatAll := flag.Bool("format-all", false, "format every file rather than only the ones that changed")
	// A binary that implements no rule and a binary whose rule found nothing produce the same empty
	// finding list, and the differential harness cannot tell them apart from the outside. This is how
	// it asks.
	listRules := flag.Bool("rules", false, "print the rules this binary implements, one per line, and exit")
	// `-rules` answers what the binary CAN run; this answers what it WILL. The two differ by every
	// rule the config never names, and that gap is invisible from the outside: a rule the config
	// cannot resolve passes its own fixtures and lints nothing, which reads exactly like a rule that
	// found nothing wrong.
	//
	// Reconstructing the answer by hand is the alternative, and it is worse than it looks. Doing it
	// against this config meant walking a nested rules block, then the overrides, then deciding
	// whether a scoped `off` counts, and the hand-derived set was wrong on 29 of 356 rules when
	// finally checked against the real resolver. ESLint has had `--print-config` for this reason.
	listRulesEnabled := flag.Bool("rules-enabled", false,
		"print the rules the lint config actually resolves, with severity, and exit")
	// Off by default until the engine is shown to agree with the existing gate across the real
	// corpus. Reformatting the tree away from what the gate produces is worse than not formatting,
	// so enabling is a separate decision from wiring.
	format := flag.Bool("format", false, "run the formatter over the candidate files")
	maxFixPasses := flag.Int("fix-passes", fix.DefaultMaxPasses, "how many times a file may be re-linted while fixes keep landing")
	showTiming := flag.Bool("timing", false, "report what each rule cost, most expensive first")
	explainFile := flag.String("explain", "", "report what every rule did on one file, and why it did or did not run")
	unusedReport := flag.Bool("unused", false, "report code that was written and never used: unreferenced exports, and statements nothing can reach")
	unusedAll := flag.Bool("unused-all", false, "with --unused, list the findings already marked cohere-keep rather than only counting them")
	// Opt-in on purpose. The closure's claim is strictly stronger than the flat one and it fails
	// differently: a wrong root mis-reports one file in the flat view and cascades here, going dark
	// across everything that was alive only through it. Keeping both means the two numbers can be
	// read against each other before the bigger one is trusted.
	unusedDeep := flag.Bool("unused-deep", false, "with --unused, also compute the transitive closure and group the dead code into islands")
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
		// The provenance travels with the list rather than waiting behind `-version`.
		//
		// A development build implements whatever was on disk when it was compiled, which in a shared
		// worktree includes another author's uncommitted work. Three of us read a count off this
		// surface on one night and reported it as the repository's, and `-version` had been saying so
		// the whole time. A disclosure that has to be sought is a disclosure that will be skipped,
		// especially when the number it qualifies looks plausible, so the qualification has to travel
		// with the number a reader carries away rather than sitting one command to the side.
		//
		// It goes to stderr so the rule list on stdout stays diffable between two binaries.
		if provenance := release.Current(); provenance.IsDevelopment() {
			fmt.Fprintf(
				os.Stderr,
				"note: %d rules from a local build, so this is whatever was on disk when it was compiled, not necessarily what is committed\n",
				len(names),
			)
		}
		return nil
	}

	if *listRulesEnabled {
		lintConfig, err := configuration.Load(*lintConfigFileName)
		if err != nil {
			return fmt.Errorf("reading %s: %w", *lintConfigFileName, err)
		}

		// Resolution is per file, because an override can turn a rule off for one path and leave it
		// on everywhere else, so there is no single answer for the whole tree. The probe path names
		// which file the answer is about, and it defaults to a plain TypeScript source at the root
		// rather than to nothing: a caller who forgets to pass one gets the ordinary case rather
		// than an error, and a caller who wants the generated-file answer asks for it by name.
		probePath := "index.ts"
		if arguments := flag.Args(); len(arguments) > 0 {
			probePath = arguments[0]
		}
		resolved := lintConfig.Resolve(probePath)
		if resolved.Ignored {
			fmt.Fprintf(os.Stderr, "note: %s is excluded by ignore pattern %q, so no rule applies to it\n",
				probePath, resolved.IgnoredBy)
			return nil
		}

		// Only rules the binary actually implements. A config key naming a rule this build does not
		// have resolves to a severity and still runs nothing, and printing it here would report a
		// rule as enabled that cannot fire. That gap is real rather than hypothetical: this config
		// carries several keys spelled for an older name, and they are exactly the rules whose
		// standing decision silently stopped applying.
		implemented := make(map[string]bool, len(registry.All()))
		for _, registered := range registry.All() {
			implemented[registered.Name] = true
		}

		type enabledRule struct {
			name     string
			severity string
		}
		var enabled []enabledRule
		unimplemented := 0
		for name, setting := range resolved.Rules {
			if setting.Severity == configuration.SeverityOff {
				continue
			}
			if !implemented[name] {
				unimplemented++
				continue
			}
			enabled = append(enabled, enabledRule{name: name, severity: setting.Severity.String()})
		}
		sort.Slice(enabled, func(first, second int) bool { return enabled[first].name < enabled[second].name })
		for _, rule := range enabled {
			fmt.Printf("%s\t%s\n", rule.name, rule.severity)
		}

		fmt.Fprintf(os.Stderr, "note: %d rules enabled for %s, of %d implemented by this binary\n",
			len(enabled), probePath, len(implemented))
		if unimplemented > 0 {
			fmt.Fprintf(os.Stderr,
				"note: %d config keys name a rule this binary does not implement, so their setting applies to nothing\n",
				unimplemented)
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

	// A bare `cohere` runs the whole pipeline. The phase flags isolate one phase for someone
	// debugging it; they are not the default path, and nothing about running the tool with no
	// arguments should be a partial check.
	anyPhaseNamed := *typesOnly || *lintOnly || *fixOnly
	runFix := *fixOnly || !anyPhaseNamed
	runTypes := *typesOnly || !anyPhaseNamed
	runLint := *lintOnly || !anyPhaseNamed

	// `--unused` is the one phase a bare `cohere` does not run, and it is deliberately absent from
	// `anyPhaseNamed` above rather than folded into it.
	//
	// Two separate things follow from that, and both are the point. It does not run unless named, so
	// the gate people run constantly stays the correctness gate and nothing else. And naming it does
	// not narrow the run the way `--lint` does — `cohere --unused` still fixes, type-checks, and
	// lints, because unused leans on the type graph being sound and a narrowing flag would encourage
	// running it against a tree whose symbols do not resolve.
	//
	// The reason it is opt-in rather than merely quiet: this is a report someone asks for, not part
	// of the correctness gate. An export kept for an external consumer is unused and correct, so
	// there is no answer here a build can enforce, and a phase that cannot fail honestly should not
	// be in the path of a phase that can.
	runUnused := *unusedReport || *unusedAll || *unusedDeep

	// Carried to every bail site so an opt-in phase that was genuinely cut off reads differently
	// from one nobody asked for. Without it a bail prints "unused did not run (types bailed)" on
	// every ordinary run, which manufactures a gap out of a run where nothing was withheld.
	unusedRequest := requestedPhases{phaseUnused: runUnused}

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
	report := &pipelineReport{graph: buildDuration, processStart: processStart}

	// Phase 2: fix and format. Mutation runs before anything reports, so every phase downstream sees
	// the repaired tree rather than findings a fixer would have silently repaired.
	//
	// The lint config is loaded here because fixing needs to know which rules apply to which files,
	// and loading it once serves both this phase and lint below.
	var lintConfig *configuration.Config

	// The fix phase's walk, kept when it is still valid for the lint phase to reuse.
	//
	// Nil means lint must walk for itself: either the fix phase did not run, or it rewrote a file and
	// the graph was rebuilt underneath these diagnostics. Both cases are the same instruction, which
	// is why one nil check covers them.
	var reusableWalk *program.Result

	if runFix || runLint {
		// A config that cannot be read is a hard failure and never a permissive default. Linting
		// everything with nothing configured produces output indistinguishable from a clean run, and
		// that exact confusion is what this tool exists to make impossible.
		loaded, err := configuration.Load(resolveLintConfigPath(*lintConfigFileName, *directory))
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
		fixSummary, fixWalk, err := applyProposedFixes(
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
			report.markRemainingNotReachedFor(phaseFix, err.Error(), unusedRequest)
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
				report.markRemainingNotReachedFor(phaseFix, fmt.Sprintf("the graph could not be rebuilt after fixing: %v", err), unusedRequest)
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

		// The fix phase's walk is reusable by lint only when nothing was rewritten.
		//
		// The condition is the same one the rebuild above turns on, and deliberately so: if any file
		// changed, the graph those diagnostics came from has been replaced and they describe bytes
		// that no longer exist. That is the stale-read the rebuild exists to prevent, and reusing the
		// walk across it would reintroduce it one level up while looking like an optimisation.
		//
		// When nothing changed, the graph is provably the same object the walk ran against, so the
		// result is exactly what a second walk would produce. Measured on the ahra tree: a run costs
		// about 0.9s fixed plus 1.1s per walk, so this removes roughly a third of a default run.
		if fixSummary.FilesChanged == 0 {
			reusableWalk = &fixWalk
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
			report.markRemainingNotReachedFor(
				phaseTypes,
				fmt.Sprintf("%d type diagnostics — lint findings against wrong semantics are noise", len(typeDiagnostics)),
				unusedRequest,
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

		// Reuse the fix phase's walk when it is still valid, rather than walking the same graph with
		// the same rules a second time.
		//
		// Two walks over one graph was the largest single cost in a default run: measured on the ahra
		// tree, roughly 0.9s fixed plus 1.1s per walk, and the fix phase's own comment described its
		// walk as cheap on the assumption that a reporting walk had already happened. It had not.
		// Fix is phase 2 and this runs after phase 3, so the fix walk was the first and this was the
		// repeat.
		//
		// Validity is decided at the fix phase, not here, because only it knows whether a file was
		// rewritten. See where reusableWalk is set.
		//
		// A run with --timing still walks here, because per-rule timings are collected during the
		// walk and the fix phase ran before CollectTimings was set. Reusing that walk would print an
		// empty timing table, which is a report that looks like a measurement.
		var result program.Result
		reusedWalk := reusableWalk != nil && !*showTiming
		if reusedWalk {
			result = *reusableWalk
		} else {
			walked, err := graph.Walk(ctx, projectFiles, rules)
			if err != nil {
				return fmt.Errorf("running rules: %w", err)
			}
			result = walked
		}
		lintDuration := time.Since(lintStart)

		// How the walk was paid for, rather than a duration that would read as this phase's cost.
		//
		// A reused walk timed here measures the pointer copy, so it prints as `in 0s`: a coverage line
		// stating that 212 rules visited 2 million nodes for free. The counts are real and the
		// duration is the lie, which is the harder kind to spot because everything around it checks
		// out. Naming the phase that did the walking keeps the line checkable.
		lintWalkCost := fmt.Sprintf("in %s", round(lintDuration))
		if reusedWalk {
			lintWalkCost = "walked by the fix phase (nothing was rewritten, so its findings still hold)"
		}

		for _, diagnostic := range result.Diagnostics {
			printRuleDiagnostic(diagnostic)
		}
		findings += len(result.Diagnostics)

		// Coverage prints unconditionally, alongside the verdict rather than behind a flag. A run that
		// checked nothing must not be able to look like a run that found nothing, and the only way to
		// guarantee that is to make the population as visible as the findings.
		fmt.Printf(
			"lint: %d findings — %d rules over %d files, %d nodes visited, %s\n",
			len(result.Diagnostics), result.Coverage.RulesRun, result.Coverage.FilesWalked,
			result.Coverage.NodesVisited, lintWalkCost,
		)
		printParityCoverage(rules, lintConfig)
		printRuleCoverage(rules, result.Coverage)
		printCrashCoverage(result.Coverage)
		printSuppressionCoverage(result.Coverage)
		printConfigCoverage(result.Coverage)
		printOrphanedConfigKeys(rules, lintConfig)

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

		if reusedWalk {
			// Zero elapsed on purpose: the walk's cost belongs to the phase that performed it, and
			// recording it here too would bill one walk to two rows.
			report.record(phaseLint, outcomeReused, 0, "the fix phase's walk (nothing was rewritten)")
		} else {
			report.record(phaseLint, outcomeRan, lintDuration, "")
		}
	}

	// Phase 5: unused_code_report. A report, run only when asked for, and never a reason to fail a build.
	if !runUnused {
		report.record(phaseUnused, outcomeSkipped, 0, "not requested — this is a report, ask for it with --unused")
	} else {
		unusedStart := time.Now()

		// The honest statement about what the findings are worth. The reference analysis is keyed on
		// symbol identity, so unresolved symbols read as "nothing references this" and a broken build
		// produces a longer report rather than an error. Saying so beside the findings is the whole
		// difference between a report a reader can calibrate and one that quietly misleads.
		//
		// It is a warning rather than a refusal because the reachability half needs no types at all
		// and is fully trustworthy either way, so refusing to run would withhold good findings over a
		// caveat that only applies to the other half.
		if !runTypes {
			fmt.Printf(
				"\nunused: types did not run, so the reference half of this report is UNRELIABLE — " +
					"unresolved symbols look exactly like unreferenced ones. The unreachable half " +
					"below needs no types and is unaffected.\n",
			)
		}

		unusedResult, err := unused_code_report.Run(ctx, graph, projectFiles, *unusedDeep)
		if err != nil {
			return fmt.Errorf("running the unused report: %w", err)
		}
		unusedDuration := time.Since(unusedStart)

		unused_code_report.Write(os.Stdout, unusedResult, *unusedAll)

		// Deliberately NOT added to `findings`. The exit code is the gate's verdict, and this phase
		// is not part of the gate: an export held for an external consumer is unused and correct, so
		// failing a build over it would make the report something people route around rather than
		// read.
		report.record(phaseUnused, outcomeRan, unusedDuration, "")
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
	lintConfig *configuration.Config,
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
//
// When the tsconfig is incremental, the check runs warm: files whose contents have not changed
// since the last run are not re-checked. Measured on the ahra tree, 1149ms cold against 380ms
// warm, with the finding set identical in both directions. A tsconfig without `incremental`
// falls back to the full pass, which is also what happens on the first run, after a toolchain
// version change, and any time the build info cannot be read.
//
// The build info is written even when this run reports findings, and that is deliberate: it
// records which files were checked, not whether they passed. A file that type-checked and
// produced a diagnostic was still checked. Discarding that on failure would make the slow path
// the one a person hits while iterating on an error, which is exactly the loop where the pause
// costs most.
func collectTypeDiagnostics(ctx context.Context, graph *program.Graph, files []*ast.SourceFile) []*ast.Diagnostic {
	ours := make(map[*ast.SourceFile]struct{}, len(files))
	for _, sourceFile := range files {
		ours[sourceFile] = struct{}{}
	}

	// One session across check-then-write. The build info has to be emitted from the same
	// incremental program that did the checking: that program's snapshot is what records which
	// files were checked, and emitting from a second one writes a build info that skips nothing
	// while looking correct. See program.IncrementalSession.
	var checked []*ast.Diagnostic
	if session := graph.NewIncrementalSession(); session != nil {
		checked = session.Diagnostics(ctx)
		if writeDiagnostics := session.Write(ctx); len(writeDiagnostics) > 0 {
			// A failed build-info write must not pass silently. The next run would be cold while
			// this one reported success, and the symptom is a saving that quietly never appears.
			for _, diagnostic := range writeDiagnostics {
				fmt.Fprintf(os.Stderr, "cohere: writing the incremental cache: %s\n",
					diagnostic.MessageKey())
			}
		}
	} else {
		checked = graph.AllDiagnostics(ctx)
	}

	diagnostics := graph.ConfigDiagnostics(ctx)
	for _, diagnostic := range checked {
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
		fmt.Printf("error TS%d: %s\n", diagnostic.Code(), diagnosticMessage(diagnostic))
		return
	}

	line, character := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, diagnostic.Loc().Pos())
	fmt.Printf(
		"%s:%d:%d - error TS%d: %s\n",
		sourceFile.FileName(), line+1, character+1, diagnostic.Code(), diagnosticMessage(diagnostic),
	)
}

// diagnosticMessage renders a compiler diagnostic's text.
//
// `MessageText()` is the wrong accessor and returns the empty string for every diagnostic the
// checker produces. A compiler diagnostic carries a message template and its arguments separately,
// and `messageText` is only populated for external diagnostics that arrive pre-localized. So a real
// type error printed as `error TS2322: ` with nothing after the colon, which is worse than not
// printing it: the reader is told a file is broken and not told how.
//
// `Localize` is what the compiler's own diagnostic writer uses. It renders the template with its
// arguments, and falls back to `messageText` for the external case, so it is correct for both rather
// than for the one that happened to be tested.
//
// The zero Locale selects the built-in English text, which is what this tool prints everywhere else.
func diagnosticMessage(diagnostic *ast.Diagnostic) string {
	return diagnostic.Localize(locale.Locale{})
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
	writeRuleCoverage(os.Stdout, rules, coverage)
}

// writeRuleCoverage is printRuleCoverage against an arbitrary writer, so the note can be tested.
//
// This note has caught two dead rules tonight and had no fixture of its own, which is the same
// shape as the rules it guards: a check nobody has proven can fail.
func writeRuleCoverage(out io.Writer, rules []rule.Rule, coverage program.Coverage) {
	silent := []string{}
	watchedAndQuiet := 0
	for _, subject := range rules {
		if coverage.RulesListening[subject.Name] == 0 {
			silent = append(silent, subject.Name)
			continue
		}
		if coverage.RulesReporting[subject.Name] == 0 {
			watchedAndQuiet++
		}
	}

	// A rule that listened to thousands of files and reported nothing is the third case, and it was
	// invisible until now. The two above are about wiring: nothing offered it files, or it declined
	// the ones it got. This one looked at real code and had nothing to say, which is either a clean
	// tree or a rule that cannot see.
	//
	// Counted rather than named. On this tree it is 69 of 79 rules, and printing 69 names every run
	// would bury the two lines above it that a reader must act on. The count is the honest summary:
	// most of what cohere checked was checked against code that already satisfies it, and a reader
	// deciding whether a zero means anything needs to know how much of the zero is this.
	//
	// Two false positives shipped past a full fixture pair tonight and were caught only by running
	// against the tree. That check was a habit rather than a line of output, and a habit is not a
	// guard. This is the smallest version of it that survives being forgotten.
	if watchedAndQuiet > 0 {
		fmt.Fprintf(out,
			"  note: %d rules watched files and reported nothing — a clean tree and a rule that cannot see look identical here\n",
			watchedAndQuiet,
		)
	}

	if len(silent) == 0 {
		return
	}

	sort.Strings(silent)
	for _, name := range silent {
		// Two opposite defects wore one sentence until now. A rule offered files that declined every
		// one of them is configured and satisfied: nothing in the tree matches it, which is the
		// result a passing rule produces. A rule offered nothing was never wired, and its silence
		// says nothing about the tree at all.
		//
		// Both printed as "listened to no files", so a satisfied rule read exactly like a dead one.
		// The pair was separable only because a second line about missing config happened to print
		// for one of them, and that line does not always appear. A guard that works by accident of
		// which sentence prints is not a guard.
		if coverage.RulesOffered[name] == 0 {
			fmt.Fprintf(out, "  note: rule %s was offered no files — nothing wired it, so its silence says nothing about the tree\n", name)
			continue
		}
		// "Registered no listener" rather than "declined", because those are not the same thing and
		// five rules here prove it. A rule whose whole job is answered from the file itself does all
		// its work in Run and returns nil: network-require-hook-request-suffix inspects every hook
		// declaration and reports before returning, and next-require-page-default-export is the same
		// shape. Both were described by the old sentence as having "ran and looked, and nothing
		// matched", which is true of the walk and false of the rule.
		//
		// The distinction matters because the two readings send a reader to different places. "Nothing
		// matched" says look at the tree. "Registered no listener" says look at what the rule does,
		// which for these five is where the answer is.
		fmt.Fprintf(out, "  note: rule %s registered no listener on any of the %d files it was offered — either nothing matched, or it answered eagerly and had nothing to report\n",
			name, coverage.RulesOffered[name])
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
// cohere gates, 281 of the 306 suppressions naming one of our own rules state no reason, so
// requiring one today turns working code red for no defect. Printing it every run puts the number
// in front of us, which is what lets the convention be tightened later from evidence rather than
// from a guess.
// printCrashCoverage names the files a rule panicked on.
//
// Printed before findings rather than after, and named rather than counted. A crashed file produced
// no findings, and a run that lost a file to a panic must not read as a run that found nothing in it.
// That is the same rule the ignored, scoped-off and declined lines already follow.
func printCrashCoverage(coverage program.Coverage) {
	for _, crash := range coverage.FilesCrashed {
		fmt.Printf("  crashed: %s could not be linted, so nothing in it was checked: %v\n",
			crash.FileName, crash.Cause)
	}
}

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
		//
		// The two halves are printed apart because they ask for opposite actions. A directive that
		// silenced nothing while its rule ran is dead scaffolding, and deleting it is the right
		// response. One naming only rules this run did not run silenced nothing because nothing
		// looked, and deleting it would strip a suppression the gate still needs. Measured during
		// the migration at 63 of 171 rules ported: 82 of 100 were the second kind, so the single
		// number was telling a reader to delete comments that are load-bearing today.
		dead := coverage.UnusedSuppressions - coverage.UnusedSuppressionsForUnrunRules
		fmt.Printf(
			"  note: %d disable comments silenced nothing while their rule ran — they may be scoping off a rule that no longer fires\n",
			dead,
		)
		if coverage.UnusedSuppressionsForUnrunRules > 0 {
			fmt.Printf(
				"  note: %d more name only rules cohere has not ported yet, so nothing looked and they are not dead\n",
				coverage.UnusedSuppressionsForUnrunRules,
			)
		}
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

// printOrphanedConfigKeys says which config entries name a rule that does not exist.
//
// This is the inverse of the unconfigured line above, and it is the failure the rename of every
// rule to its upstream spelling introduced. The resolver matches a config key exactly, or trims a
// prefix off it on a `/` boundary, so a key that is LONGER than the rule name resolves and a key
// that is SHORTER never can. `typescript/no-base-to-string` cannot reach a rule registered as
// `@typescript-eslint/no-base-to-string`: there is no prefix to trim, only one to add.
//
// Left silent, this reads as the opposite of what happened. The rule reports "nobody has said
// whether it should run" while someone did say, in the config, in a key sitting three lines above
// one that works. A deliberate `off` that stops applying is worse than one that was never written,
// because the config still shows the decision and the run no longer honors it.
//
// Keys naming a rule cohere has not ported yet are the expected case and not an error: a decision
// recorded ahead of the rule is how this migration is supposed to work. They are counted and named
// separately so the two are never confused.
func printOrphanedConfigKeys(rules []rule.Rule, config *configuration.Config) {
	if config == nil {
		return
	}

	registered := make(map[string]bool, len(rules))
	for _, registeredRule := range rules {
		registered[registeredRule.Name] = true
	}

	// A key resolves if some registered rule matches it exactly, or ends with it on a `/` boundary.
	// This mirrors settingFor rather than reimplementing it loosely, because a check that disagreed
	// with the resolver would report keys that work and miss keys that do not.
	resolves := func(key string) bool {
		for name := range registered {
			if name == key || strings.HasSuffix(name, "/"+key) {
				return true
			}
		}
		return false
	}

	orphaned := make([]string, 0)
	for key := range config.Rules {
		if !resolves(key) {
			orphaned = append(orphaned, key)
		}
	}
	sort.Strings(orphaned)

	for _, key := range orphaned {
		setting := config.Rules[key]
		fmt.Printf(
			"  config: key %q matches no registered rule, so its %s never applies — either the rule is not ported yet, or the key is spelled for an older name\n",
			key, setting.Severity,
		)
	}
}

// round trims a duration to milliseconds, which is the resolution any of these numbers means
// anything at.
func round(duration time.Duration) time.Duration {
	return duration.Round(time.Millisecond)
}

// printParityCoverage says how many of the rules the config asks for this binary can actually run.
//
// The lint line above reports how many rules ran, which is what this binary contains. It says nothing
// about how many were wanted, and while a port is in progress those are different numbers. A reader
// seeing a rule count has no way to learn whether the config asked for more, and the whole argument
// of this tool is that a run which checked less than it appears to must say so.
//
// The gap this closes was once wide enough to quote, and quoting it here is what made this comment
// wrong within weeks: the figures drift with every rule that lands, while the reason they matter
// does not. The live numbers belong in the line this function prints, which is derived, and not in
// a comment, which is remembered.
//
// This is the same omission the differential harness carried until `7b590f6`, in the line a reader
// trusts most, and it is worth fixing in both places rather than only in the instrument that gets
// read during a migration review.
//
// Names are compared on the `/` boundary the config resolver uses, for the reason stated there:
// plain suffix matching would let a config entry for `no-enum` claim `consistency-no-enum`, and the
// count would read better than the truth.
//
// **The rules block is not the whole config**, and the first version of this function read only that
// and reported 166. Forty rules are enforced by the `plugins` declarations and named in no rules
// block, so a denominator taken from the block alone understates by exactly the rules nobody wrote
// down. Those forty are held in `configuration.PluginDefaultRules`, which is where that knowledge
// lives now: this function once also merged a captured inventory of what the replaced tools
// enforced, and that catalog was deleted once cohere passed it, since a denominator that can only
// be met asserts nothing after it has been.
func printParityCoverage(rules []rule.Rule, lintConfig *configuration.Config) {
	if lintConfig == nil {
		return
	}

	implemented := make(map[string]bool, len(rules))
	for _, registered := range rules {
		implemented[registered.Name] = true
	}

	wanted := map[string]bool{}
	for name, setting := range lintConfig.Rules {
		// A rule turned off ran over no files regardless, so it is not something this run failed to
		// check. The `delete` this once carried was for a merged catalog that could have seeded the
		// map before this loop; with the config as the only source, skipping is the whole of it.
		if setting.Severity == configuration.SeverityOff {
			continue
		}
		wanted[name] = true
	}

	missing := 0
	for name := range wanted {
		if !implementsConfiguredRule(name, implemented) {
			missing++
		}
	}

	if missing == 0 {
		return
	}

	fmt.Printf(
		"  parity: %d of %d rules the config asks for, so %d were not checked by anything here\n",
		len(wanted)-missing, len(wanted), missing,
	)
}

// implementsConfiguredRule reports whether a config entry names a rule this binary contains.
func implementsConfiguredRule(configured string, implemented map[string]bool) bool {
	if implemented[configured] {
		return true
	}
	if slash := strings.LastIndexByte(configured, '/'); slash >= 0 {
		return implemented[configured[slash+1:]]
	}
	return false
}
