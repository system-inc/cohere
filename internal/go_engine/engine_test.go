package go_engine

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	goformat "go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/printf"

	"github.com/system-inc/cohere/internal/go_engine/cgo_testing"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/concurrency_require_escape_hatch_reason"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_ambiguous_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_any"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_boolean_outcome"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_console"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_dash_punctuation"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_else_return"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_empty_function"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_grouped_var"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_label"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_lone_block"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_lonely_if"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_file"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_parameter_reassignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_property_alias"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_redundant_import_alias"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_screaming_snake_case"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_shouting"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_stuttering_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_undefined_initializer"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_uppercase_initialism"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_use_before_define"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_useless_concatenation"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_utils_folder"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_warning_comment"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_default_last"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_matching_file_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_octal_prefix"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_one_type_per_file"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_operator_assignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_caller_data_mutation"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_identical_branches"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_process_exit_after_output"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_write_only_collection"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_response_status_check"
	"github.com/system-inc/cohere/internal/go_engine/rules/security_no_interpolated_shell_command"
	"github.com/system-inc/cohere/internal/go_engine/rules/security_no_interpolated_sql_string"
	"github.com/system-inc/cohere/internal/go_engine/settings"
)

var update = flag.Bool("update", false, "rewrite each case's expected.jsonl and expected.stderr from the engine")

// fixtureOnlyFiles are a case directory's files that describe the case rather than belong to its project.
var fixtureOnlyFiles = []string{"expected.jsonl", "expected.stderr", "gitlinks"}

// panickingRule is a planted rule that panics on every package, so the crash path has a rule to catch.
var panickingRule = rule.Rule{
	Name:   rule.Namespace + "correctness-always-panics",
	Origin: rule.OriginHouse,
	Analyzer: &analysis.Analyzer{
		Name: "alwayspanics",
		Doc:  "alwayspanics panics on every package, so the engine's crash path has a rule to catch.",
		Run: func(*analysis.Pass) (any, error) {
			// Invariant: this rule exists to panic; the engine must turn it into a crash entry.
			panic("planted")
		},
	},
}

// everyFileRule is a planted house rule that reports at every file's package clause, so a run shows which files
// a house rule's findings are written for and which the driver withholds as generated or copied.
var everyFileRule = rule.Rule{
	Name:   rule.Namespace + "consistency-report-every-file",
	Origin: rule.OriginHouse,
	Analyzer: &analysis.Analyzer{
		Name: "everyfile",
		Doc:  "everyfile reports every file, so a test can see which of its findings are written.",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				pass.Reportf(file.Package, "a file")
			}
			return nil, nil
		},
	},
}

// generatedFileRules are house rules that leave generated files to the driver, listed here because the registry does
// not run them yet. The generated-rules-withheld case runs them over a generated file that breaks each one, so a rule
// that skips such a file itself drops out of the lint record's withheld count and fails the case.
var generatedFileRules = []rule.Rule{
	concurrency_require_escape_hatch_reason.Rule,
	consistency_no_abbreviated_identifier.Rule,
	consistency_no_ambiguous_identifier.Rule,
	consistency_no_any.Rule,
	consistency_no_boolean_outcome.Rule,
	consistency_no_console.Rule,
	consistency_no_dash_punctuation.Rule,
	consistency_no_else_return.Rule,
	consistency_no_empty_function.Rule,
	consistency_no_grouped_var.Rule,
	consistency_no_label.Rule,
	consistency_no_lone_block.Rule,
	consistency_no_lonely_if.Rule,
	// The case sets this rule's limit, so it runs on an analyzer of its own: an option set on the registry's
	// analyzer would outlive this case and reach every case running beside it.
	withOwnAnalyzer(consistency_no_long_file.Rule, consistency_no_long_file.NewAnalyzer()),
	consistency_no_long_line.Rule,
	consistency_no_parameter_reassignment.Rule,
	consistency_no_property_alias.Rule,
	consistency_no_redundant_import_alias.Rule,
	consistency_no_screaming_snake_case.Rule,
	consistency_no_shouting.Rule,
	consistency_no_stuttering_name.Rule,
	consistency_no_undefined_initializer.Rule,
	consistency_no_uppercase_initialism.Rule,
	consistency_no_use_before_define.Rule,
	consistency_no_useless_concatenation.Rule,
	consistency_no_utils_folder.Rule,
	consistency_no_warning_comment.Rule,
	consistency_require_default_last.Rule,
	consistency_require_matching_file_name.Rule,
	consistency_require_octal_prefix.Rule,
	consistency_require_one_type_per_file.Rule,
	consistency_require_operator_assignment.Rule,
	correctness_no_caller_data_mutation.Rule,
	correctness_no_identical_branches.Rule,
	correctness_no_process_exit_after_output.Rule,
	correctness_no_write_only_collection.Rule,
	correctness_require_response_status_check.Rule,
	security_no_interpolated_shell_command.Rule,
	security_no_interpolated_sql_string.Rule,
}

// copiedFileRules are a planted house rule that reports every file and vet's printf, so the copied-rules-withheld
// case shows the driver withholding and counting a house finding in a copied file while vet's finding there reports.
var copiedFileRules = []rule.Rule{everyFileRule, {
	Name:         rule.Namespace + printf.Analyzer.Name,
	Origin:       rule.OriginVet,
	UpstreamName: printf.Analyzer.Name,
	TypeAware:    true,
	Analyzer:     printf.Analyzer,
}}

// limitRule is a planted rule with an option, which reports the value it read, so a run shows the option the
// settings gave it reached its analyzer.
var limitRule = rule.Rule{
	Name:     rule.Namespace + "correctness-report-limit",
	Origin:   rule.OriginHouse,
	Analyzer: newLimitAnalyzer(),
}

func newLimitAnalyzer() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{Name: "reportlimit", Doc: "reportlimit reports the limit it was given, once."}
	limit := analyzer.Flags.Int("limit", 0, "the limit to report")
	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		pass.Reportf(pass.Files[0].Package, "the limit is %d", *limit)
		return nil, nil
	}
	return analyzer
}

// unconfiguredRule is a planted rule that reports nothing without its one option, which no setting gives it.
var unconfiguredRule = rule.Rule{
	Name:         rule.Namespace + "correctness-require-package-option",
	Origin:       rule.OriginHouse,
	Analyzer:     newUnconfiguredAnalyzer(),
	ConfiguredBy: []string{"package"},
}

func newUnconfiguredAnalyzer() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name: "packageoption", Doc: "packageoption reports nothing until it names a package.",
	}
	analyzer.Flags.String("package", "", "the package to look for")
	analyzer.Run = func(*analysis.Pass) (any, error) { return nil, nil }
	return analyzer
}

// endToEndCase is one run of the engine over a copy of a fixture, compared with the case's expected stream.
type endToEndCase struct {
	// name is the case, and the directory under testdata holding its expected files.
	name string

	// fixture is the directory under testdata copied as the project, the case's own when empty.
	fixture string

	// extra follows `--contract 3 --root <root>`, with {root} replaced by the copy's directory.
	extra []string

	// arguments, when set, is the whole command line instead, with {root} replaced the same way.
	arguments []string

	exitCode int

	// needsCgo marks a case whose project has cgo of its own, which the engine loads through go/packages and the cgo
	// tool: it skips, named, where the toolchain has no cgo and under a runner's stand-in go, as Loom's, which
	// refuses to compile (see cgo_testing).
	needsCgo bool

	// rules replaces the registry when set.
	rules []rule.Rule

	// prepare changes the copy before the run.
	prepare func(root string) error

	// verify reads the copy after the run, for what the stream cannot show: what was written to the project.
	verify func(test *testing.T, root string)
}

var endToEndCases = []endToEndCase{
	{name: "clean", exitCode: 0},
	{
		name: "unformatted-no-fix", fixture: "unformatted", extra: []string{"--no-fix"}, exitCode: 1,
		verify: unchanged("unformatted.go", "unformatted"),
	},
	{name: "unformatted-rewritten", fixture: "unformatted", exitCode: 0, verify: formatted("unformatted.go")},
	{name: "type-error", exitCode: 1},
	{name: "vet-finding", exitCode: 1},
	{name: "workspace", exitCode: 0, verify: unchanged("third_party/lib/lib.go", "workspace")},
	{name: "cgo", exitCode: 0, needsCgo: true},
	{name: "build-constrained", exitCode: 1, verify: unchanged("generated.go", "build-constrained")},
	{name: "generated-house-rule", fixture: "build-constrained", exitCode: 1, rules: []rule.Rule{everyFileRule}},
	{
		name: "generated-rules-withheld", exitCode: 0, rules: generatedFileRules,
		verify: unchanged("generated.go", "generated-rules-withheld"),
	},
	{
		name: "copied-rules-withheld", exitCode: 1, rules: copiedFileRules,
		verify: unchanged("copied.go", "copied-rules-withheld"),
	},
	{name: "options", exitCode: 1, rules: []rule.Rule{limitRule, unconfiguredRule}},
	{name: "option-unknown", exitCode: 2, rules: []rule.Rule{limitRule}},
	{name: "panicking-rule", fixture: "clean", exitCode: 1, rules: []rule.Rule{panickingRule}},
	{name: "suppression", exitCode: 1},
	{name: "configured", exitCode: 1},
	{name: "lint-only", fixture: "clean", extra: []string{"--lint"}, exitCode: 0},
	{name: "lint-only-type-error", fixture: "type-error", extra: []string{"--lint"}, exitCode: 1},
	{
		name: "format-only-no-fix", fixture: "unformatted", extra: []string{"--format-only", "--no-fix"},
		exitCode: 1, verify: unchanged("unformatted.go", "unformatted"),
	},
	{
		name: "scoped", extra: []string{"{root}/inside"}, exitCode: 0,
		verify: unchanged("outside/outside.go", "scoped"),
	},
	{
		name: "unreadable", fixture: "clean", exitCode: 1,
		prepare: func(root string) error { return os.Chmod(filepath.Join(root, "clean.go"), 0) },
	},
	{name: "rules", fixture: "clean", extra: []string{"--rules"}, exitCode: 0},
	{name: "rules-enabled", fixture: "configured", extra: []string{"--rules-enabled"}, exitCode: 0},
	{name: "version", fixture: "clean", extra: []string{"--version"}, exitCode: 0},
	{
		name: "contract-mismatch", fixture: "clean", arguments: []string{"--contract", "2", "--root", "{root}"},
		exitCode: 2,
	},
	{name: "refused-flag", fixture: "clean", extra: []string{"--unused"}, exitCode: 2},
	{name: "refused-combination", fixture: "clean", extra: []string{"--format-only", "--lint"}, exitCode: 2},
	{name: "scope-outside-root", fixture: "clean", extra: []string{"{root}/.."}, exitCode: 2},
}

func TestEndToEnd(test *testing.T) {
	test.Parallel()
	for _, endToEnd := range endToEndCases {
		test.Run(endToEnd.name, func(test *testing.T) {
			test.Parallel()
			runCase(test, endToEnd)
		})
	}
}

// runCase copies the case's fixture, runs the engine over the copy, and compares what it wrote, normalized, with
// the case's expected files, or rewrites them under -update.
func runCase(test *testing.T, endToEnd endToEndCase) {
	test.Helper()
	if endToEnd.needsCgo {
		cgo_testing.RequireTranslation(test)
	}
	fixture := cmp.Or(endToEnd.fixture, endToEnd.name)
	root := copyFixture(test, filepath.Join("testdata", fixture))
	registered := endToEnd.rules
	if registered == nil {
		registered = rules.All()
	}
	if endToEnd.prepare != nil {
		if prepareError := endToEnd.prepare(root); prepareError != nil {
			test.Fatalf("preparing the copy: %v", prepareError)
		}
	}
	refuseSharedOptions(test, root, registered)

	// Cloned, since the table is shared by every subtest and {root} is each one's own.
	arguments := slices.Clone(endToEnd.arguments)
	if arguments == nil {
		arguments = append([]string{"--contract", fmt.Sprint(contract.Version), "--root", "{root}"}, endToEnd.extra...)
	}
	for index, argument := range arguments {
		arguments[index] = strings.ReplaceAll(argument, "{root}", root)
	}

	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	exitCode := run(arguments, &standardOutput, &standardError, registered)
	stream := normalize(standardOutput.String(), root)
	prose := strings.ReplaceAll(standardError.String(), root, "/project")

	checkStream(test, stream, exitCode)
	if exitCode != endToEnd.exitCode {
		test.Errorf("exit %d, want %d; stderr:\n%s", exitCode, endToEnd.exitCode, prose)
	}
	compareExpected(test, filepath.Join("testdata", endToEnd.name, "expected.jsonl"), stream)
	compareExpected(test, filepath.Join("testdata", endToEnd.name, "expected.stderr"), prose)
	if endToEnd.verify != nil {
		endToEnd.verify(test, root)
	}
}

// withOwnAnalyzer is registered with analyzer in place of the one it was registered with, for a case that sets one
// of its options.
func withOwnAnalyzer(registered rule.Rule, analyzer *analysis.Analyzer) rule.Rule {
	registered.Analyzer = analyzer
	return registered
}

// refuseSharedOptions fails a case whose settings set an option on an analyzer the registry holds. The engine sets
// options on the analyzer's flags, which outlive a run: in its own process that is one run, but here every case runs
// in one process, in parallel, so the value would reach whichever case runs that rule next, and a golden written
// then records the leak. Such a case registers the rule with an analyzer of its own (withOwnAnalyzer).
func refuseSharedOptions(test *testing.T, root string, registered []rule.Rule) {
	test.Helper()
	ruleSettings, loadError := settings.Load(root, "", knownRuleNames(registered))
	if loadError != nil {
		// Ignored: a case whose settings are refused is testing that refusal, and the engine configures nothing
		// before it, so no option can be set on any analyzer.
		return
	}
	shared := map[*analysis.Analyzer]bool{}
	for _, registryRule := range rules.All() {
		shared[registryRule.Analyzer] = true
	}
	for _, registeredRule := range registered {
		setting, configured := ruleSettings.Rules[registeredRule.Name]
		if configured && setting.Options != nil && shared[registeredRule.Analyzer] {
			test.Fatalf("the settings set options on %s's registry analyzer, which every case shares; register it "+
				"with withOwnAnalyzer", registeredRule.Name)
		}
	}
}

// compareExpected compares what the run wrote with an expected file, line for line, or rewrites the file under
// -update.
func compareExpected(test *testing.T, path string, actual string) {
	test.Helper()
	if *update {
		if directoryError := os.MkdirAll(filepath.Dir(path), 0o755); directoryError != nil {
			test.Fatalf("updating %s: %v", path, directoryError)
		}
		if writeError := os.WriteFile(path, []byte(actual), 0o644); writeError != nil {
			test.Fatalf("updating %s: %v", path, writeError)
		}
		return
	}
	expected, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatalf("reading %s (run with -update to write it): %v", path, readError)
	}
	expectedLines := strings.Split(string(expected), "\n")
	actualLines := strings.Split(actual, "\n")
	for index := range max(len(expectedLines), len(actualLines)) {
		expectedLine, actualLine := lineAt(expectedLines, index), lineAt(actualLines, index)
		if expectedLine != actualLine {
			test.Errorf("%s line %d:\nwant %s\ngot  %s", path, index+1, expectedLine, actualLine)
		}
	}
}

func lineAt(lines []string, index int) string {
	if index < len(lines) {
		return lines[index]
	}
	return "(no line)"
}

// The parts of a stream that vary from run to run and machine to machine.
var (
	elapsedPattern = regexp.MustCompile(`"elapsedMilliseconds":\d+`)
	packagesLoaded = regexp.MustCompile(`(holds its export data|restoring analysis only): \d+ packages`)
	// The packages the findings cache missed, wrote and could not store, and the facts it dropped, count the
	// toolchain's own packages, which a Go release changes, as it changes the packages loaded. Hit stays, since a
	// fresh copy hits nothing.
	cachePackagesPattern = regexp.MustCompile(`"(missed|written|unstorable|factsDropped|factsSkipped)":\d+`)
	panicFramesPattern   = regexp.MustCompile(`, at [^"]*"`)
	hostTarget           = " on " + runtime.GOOS + "/" + runtime.GOARCH
	normalizedProvenance = contract.ProvenanceRecord{
		Contract: contract.Version, Engine: engineName, Version: "test", Commit: "test", Toolchain: "test",
	}
)

// normalize makes a stream comparable across runs: the copy's root reads /project, every elapsed time 0, the
// provenance fixed, the standard library's package count (which moves with the toolchain) N, a panic's frames
// (which move with this file) left out, and the host's target `host`.
func normalize(stream string, root string) string {
	lines := strings.SplitAfter(stream, "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, `{"kind":"provenance"`) {
			var provenance bytes.Buffer
			if writeError := contract.NewWriter(&provenance).Write(normalizedProvenance); writeError == nil {
				lines[index] = provenance.String()
			}
		}
	}
	normalized := strings.Join(lines, "")
	normalized = strings.ReplaceAll(normalized, root, "/project")
	normalized = elapsedPattern.ReplaceAllString(normalized, `"elapsedMilliseconds":0`)
	normalized = packagesLoaded.ReplaceAllString(normalized, "$1: N packages")
	normalized = cachePackagesPattern.ReplaceAllString(normalized, `"$1":0`)
	normalized = panicFramesPattern.ReplaceAllString(normalized, `, at <frames>"`)
	return strings.ReplaceAll(normalized, hostTarget, " on host")
}

// copyFixture copies a fixture into a fresh directory named project, in a git repository of its own with the
// fixture's gitlinks in its index, and returns the directory with every symbolic link above it resolved, so the
// paths the engine writes are the ones the copy is replaced by.
func copyFixture(test *testing.T, fixture string) string {
	test.Helper()
	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	root := filepath.Join(base, "project")
	walkError := filepath.WalkDir(fixture, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		relative, relativeError := filepath.Rel(fixture, path)
		if relativeError != nil {
			return relativeError
		}
		target := filepath.Join(root, projectName(relative))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if relative == entry.Name() && slices.Contains(fixtureOnlyFiles, entry.Name()) {
			return nil
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		return os.WriteFile(target, contents, 0o644)
	})
	if walkError != nil {
		test.Fatalf("copying %s: %v", fixture, walkError)
	}
	runGit(test, root, "init", "-q")
	gitlinks, readError := os.ReadFile(filepath.Join(fixture, "gitlinks"))
	if readError != nil && !errors.Is(readError, fs.ErrNotExist) {
		test.Fatalf("reading %s's gitlinks: %v", fixture, readError)
	}
	for _, gitlink := range strings.Fields(string(gitlinks)) {
		runGit(test, root, "update-index", "--add", "--cacheinfo",
			"160000,0123456789abcdef0123456789abcdef01234567,"+gitlink)
	}
	return root
}

func runGit(test *testing.T, directory string, arguments ...string) {
	test.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	if output, runError := command.CombinedOutput(); runError != nil {
		test.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), runError, output)
	}
}

// unformattedFixtureSuffix ends the name of a fixture that must stay unformatted to test the fix phase. Such a
// file lives on disk as `<name>.go.fixture` and becomes `<name>.go` only in a case's temporary copy, because
// internal/guard's TestEveryGoFileCohereOwnsIsGofmtClean holds every .go file cohere owns to gofmt, testdata
// included, as the release's own `gofmt -l ./command ./internal` does.
const unformattedFixtureSuffix = ".go.fixture"

// projectName is the name a fixture file takes in a case's copy: a `.go.fixture` becomes the `.go` it stands for,
// and every other file keeps its name.
func projectName(relative string) string {
	if stem, isFixture := strings.CutSuffix(relative, unformattedFixtureSuffix); isFixture {
		return stem + ".go"
	}
	return relative
}

// unchanged is a verify that a file of the copy reads as it does in its fixture: the run wrote nothing to it.
// relative names the file as the copy has it, so a `.go.fixture` on disk is read under its own name.
func unchanged(relative string, fixture string) func(test *testing.T, root string) {
	return func(test *testing.T, root string) {
		test.Helper()
		source := filepath.Join("testdata", fixture, filepath.FromSlash(relative))
		if stem, isGo := strings.CutSuffix(source, ".go"); isGo {
			if _, statError := os.Stat(stem + unformattedFixtureSuffix); statError == nil {
				source = stem + unformattedFixtureSuffix
			}
		}
		original, originalError := os.ReadFile(source)
		after, afterError := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if originalError != nil || afterError != nil {
			test.Fatalf("reading %s: %v, %v", relative, originalError, afterError)
		}
		if !bytes.Equal(original, after) {
			test.Errorf("%s was written, and this run must leave it as it was", relative)
		}
	}
}

// formatted is a verify that the run left a file of the copy formatted the way gofmt formats it.
func formatted(relative string) func(test *testing.T, root string) {
	return func(test *testing.T, root string) {
		test.Helper()
		after, readError := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if readError != nil {
			test.Fatalf("reading %s: %v", relative, readError)
		}
		gofmted, formatError := goformat.Source(after)
		if formatError != nil || !bytes.Equal(gofmted, after) {
			test.Errorf("%s is not formatted after a run that rewrites (%v)", relative, formatError)
		}
	}
}

// checkStream holds a stream to what the front door refuses (command/cohere/swift_records.go), restated here
// because the renderer is package main and no test outside it can feed it a stream: exit 2 leaves stdout empty;
// provenance comes first and speaks this contract; a checking stream has every phase once and in order, the
// unreadable records before the first, counts that agree with the finding records, and a summary that calls
// nothing complete its own records show a gap in, with an exit code that follows from it.
func checkStream(test *testing.T, stream string, exitCode int) {
	test.Helper()
	if exitCode == exitRefused {
		if stream != "" {
			test.Errorf("exit 2 left a stream:\n%s", stream)
		}
		return
	}
	lines := strings.Split(strings.TrimSuffix(stream, "\n"), "\n")
	var phases []string
	findings, findingsInPhase, filesRewritten, changedFiles := 0, 0, 0, 0
	gap, summarized := false, false
	for index, line := range lines {
		var record struct {
			Kind               string            `json:"kind"`
			Contract           int               `json:"contract"`
			Name               string            `json:"name"`
			Outcome            string            `json:"outcome"`
			Detail             string            `json:"detail"`
			Diagnostics        int               `json:"diagnostics"`
			Findings           int               `json:"findings"`
			FilesRewritten     int               `json:"filesRewritten"`
			ChangedFiles       []json.RawMessage `json:"changedFiles"`
			FilesWithoutRecord []string          `json:"filesWithoutRecord"`
			Crashes            []json.RawMessage `json:"crashes"`
			Complete           bool              `json:"complete"`
			ExitCode           int               `json:"exitCode"`
		}
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatalf("line %d is not a record: %v: %s", index+1, decodeError, line)
		}
		if (index == 0) != (record.Kind == "provenance") {
			test.Fatalf("line %d is %q, and provenance is first and only first", index+1, record.Kind)
		}
		switch record.Kind {
		case "provenance":
			if record.Contract != contract.Version {
				test.Errorf("provenance speaks contract %d", record.Contract)
			}
		case "unreadable":
			gap = true
			if len(phases) > 0 {
				test.Errorf("an unreadable record after phase %s", phases[len(phases)-1])
			}
		case "finding":
			findings++
			findingsInPhase++
		case "fix":
			filesRewritten, changedFiles = record.FilesRewritten, len(record.ChangedFiles)
		case "types":
			gap = gap || len(record.FilesWithoutRecord) > 0
			if record.Diagnostics != findingsInPhase {
				test.Errorf("the types record counts %d and %d findings came with it", record.Diagnostics,
					findingsInPhase)
			}
		case "lint":
			gap = gap || len(record.Crashes) > 0
			if record.Findings != findingsInPhase {
				test.Errorf("the lint record counts %d and %d findings came with it", record.Findings, findingsInPhase)
			}
		case "phase":
			phases = append(phases, record.Name)
			findingsInPhase = 0
			gap = gap || record.Outcome == "notReached" ||
				(record.Outcome == "skipped" && record.Name != "fix" && record.Detail != detailNotRequested)
		case "summary":
			summarized = true
			if !slices.Equal(phases, []string{"fix", "types", "lint", "unused"}) {
				test.Errorf("phases %v, want fix, types, lint and unused once each, in order", phases)
			}
			if record.Findings != findings {
				test.Errorf("the summary counts %d findings and %d came", record.Findings, findings)
			}
			if gap && record.Complete {
				test.Errorf("the summary calls complete a run whose records show a gap")
			}
			wantExit := exitClean
			if findings > 0 || !record.Complete {
				wantExit = exitFindings
			}
			if record.ExitCode != wantExit || exitCode != wantExit {
				test.Errorf("the summary says exit %d and the engine exited %d, want %d", record.ExitCode, exitCode,
					wantExit)
			}
		}
	}
	if filesRewritten != changedFiles {
		test.Errorf("the fix record counts %d files rewritten and lists %d", filesRewritten, changedFiles)
	}
	if len(phases) > 0 && !summarized {
		test.Errorf("a checking stream with no summary")
	}
}
