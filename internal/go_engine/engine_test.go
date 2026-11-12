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
	"sync"
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

// fixtureOnlyFiles are a case directory's files that describe the case rather than belong to its project. A
// platform's own expected stream, `expected.<GOOS>.jsonl` (expectedStreamPath), is one too, by platformStreamPattern.
var fixtureOnlyFiles = []string{"expected.jsonl", "expected.stderr", "gitlinks"}

// platformStreamPattern is the name of a platform's own expected stream.
var platformStreamPattern = regexp.MustCompile(`^expected\.[a-z0-9]+\.jsonl$`)

// expectedStreamPath is the expected stream of the case named name on this machine's platform: `expected.<GOOS>.jsonl`
// when the case has one, and expected.jsonl otherwise. A case keeps one per platform only where its stream honestly
// differs by platform, each with its reason in platformStreamReasons, which TestEveryPlatformStreamDiffersAndSaysWhy
// holds every such file to.
func expectedStreamPath(name string) string {
	platform := filepath.Join("testdata", name, "expected."+runtime.GOOS+".jsonl")
	if _, statError := os.Stat(platform); statError == nil {
		return platform
	}
	return filepath.Join("testdata", name, "expected.jsonl")
}

// platformStreamReasons is why each platform's own expected stream differs from its case's expected.jsonl, by its
// path under testdata: the platform fact the stream records. A platform stream is an escape from a fixture's one
// expectation, so each must say what on that platform makes the engine's honest record differ (#gj6rcxs).
var platformStreamReasons = map[string]string{
	"generated-rules-withheld/expected.linux.jsonl": "The generated file calls net/http, the one package the " +
		"response-status rule reads, and on linux net compiles cgo_unix.go, a dependency's cgo the types record " +
		"names as withheld (1 file, 24 declarations), where darwin's net compiles none (#dxn6y2z).",
}

// platformStreamProblems is what is wrong with the platform streams under testdata against reasons: a stream with no
// reason, or a blank one; a stream whose case has no expected.jsonl; a stream byte for byte its case's expected.jsonl,
// which needs no keeping; and a reason for a stream that no longer exists.
func platformStreamProblems(testdata string, reasons map[string]string) ([]string, error) {
	problems := []string{}
	found := map[string]bool{}
	cases, readError := os.ReadDir(testdata)
	if readError != nil {
		return nil, readError
	}
	for _, each := range cases {
		if !each.IsDir() {
			continue
		}
		files, filesError := os.ReadDir(filepath.Join(testdata, each.Name()))
		if filesError != nil {
			return nil, filesError
		}
		for _, file := range files {
			if !platformStreamPattern.MatchString(file.Name()) {
				continue
			}
			relative := each.Name() + "/" + file.Name()
			found[relative] = true
			if strings.TrimSpace(reasons[relative]) == "" {
				problems = append(problems, relative+": no reason in platformStreamReasons")
			}
			platform, platformError := os.ReadFile(filepath.Join(testdata, each.Name(), file.Name()))
			if platformError != nil {
				return nil, platformError
			}
			shared, sharedError := os.ReadFile(filepath.Join(testdata, each.Name(), "expected.jsonl"))
			switch {
			case sharedError != nil:
				problems = append(problems, relative+": its case has no expected.jsonl")
			case bytes.Equal(platform, shared):
				problems = append(problems, relative+": the same as expected.jsonl, so delete it")
			}
		}
	}
	for relative := range reasons {
		if !found[relative] {
			problems = append(problems, relative+": a reason for a stream that does not exist")
		}
	}
	slices.Sort(problems)
	return problems, nil
}

// Every platform stream in testdata differs from its case's expected.jsonl and has a reason, and every reason has its
// stream.
func TestEveryPlatformStreamDiffersAndSaysWhy(test *testing.T) {
	test.Parallel()
	problems, problemsError := platformStreamProblems("testdata", platformStreamReasons)
	if problemsError != nil {
		test.Fatal(problemsError)
	}
	for _, problem := range problems {
		test.Error(problem)
	}
}

// The check finds each kind of problem it names, on streams planted in a testdata of its own: a stream with no
// reason, one identical to its case's expected.jsonl, one whose case has none, and a reason with no stream. A stream
// that differs and has its reason is no problem.
func TestPlatformStreamProblemsFindsEachKind(test *testing.T) {
	test.Parallel()
	testdata := test.TempDir()
	write := func(relative string, contents string) {
		test.Helper()
		path := filepath.Join(testdata, filepath.FromSlash(relative))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	write("honest/expected.jsonl", "shared\n")
	write("honest/expected.linux.jsonl", "linux\n")
	write("reasonless/expected.jsonl", "shared\n")
	write("reasonless/expected.windows.jsonl", "windows\n")
	write("identical/expected.jsonl", "shared\n")
	write("identical/expected.linux.jsonl", "shared\n")
	write("orphaned/expected.linux.jsonl", "linux\n")
	reasons := map[string]string{
		"honest/expected.linux.jsonl":    "a fact of linux",
		"identical/expected.linux.jsonl": "a fact of linux",
		"orphaned/expected.linux.jsonl":  "a fact of linux",
		"gone/expected.linux.jsonl":      "a fact of linux",
	}
	problems, problemsError := platformStreamProblems(testdata, reasons)
	if problemsError != nil {
		test.Fatal(problemsError)
	}
	want := []string{
		"gone/expected.linux.jsonl: a reason for a stream that does not exist",
		"identical/expected.linux.jsonl: the same as expected.jsonl, so delete it",
		"orphaned/expected.linux.jsonl: its case has no expected.jsonl",
		"reasonless/expected.windows.jsonl: no reason in platformStreamReasons",
	}
	if !slices.Equal(problems, want) {
		test.Errorf("problems:\n%s\nwant:\n%s", strings.Join(problems, "\n"), strings.Join(want, "\n"))
	}
}

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

	// needsCgo marks a case whose project has cgo of its own, which the engine has go list -compiled's cgo tool
	// translate: it skips, named, where the toolchain has no cgo and under a runner's stand-in go, as Loom's, which
	// refuses to compile (see cgo_testing).
	needsCgo bool

	// rules replaces the registry when set.
	rules []rule.Rule

	// prepare changes the copy before the run.
	prepare func(root string) error

	// verify reads the copy after the run, for what the stream cannot show: what was written to the project.
	verify func(test *testing.T, root string)
}

// platformsWindowsFile is the rename-platforms fixture's file only windows builds.
const platformsWindowsFile = "internal/limits/limits_windows.go"

var endToEndCases = []endToEndCase{
	{name: "clean", exitCode: 0},
	{
		name: "unformatted-no-fix", fixture: "unformatted", extra: []string{"--no-fix"}, exitCode: 1,
		verify: unchanged("unformatted.go", "unformatted"),
	},
	{name: "unformatted-rewritten", fixture: "unformatted", exitCode: 0, verify: formatted("unformatted.go")},
	{name: "type-error", exitCode: 1},
	// The first load runs beside gofmt's check under `--no-fix` (loadsBesideFix), and the stream reads as it did when
	// the load came after: gofmt's finding and the fix record first, then the type error, and lint not reached.
	{
		name: "unformatted-type-error-no-fix", fixture: "unformatted-type-error", extra: []string{"--no-fix"},
		exitCode: 1, verify: unchanged("unformatted.go", "unformatted-type-error"),
	},
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
	// The duplicates report (#jfvhbj7): one computation written three times, one copy kept by a suppression with its
	// reason, and a suppression that keeps nothing, listed and never a finding. The record follows the unused phase's.
	{name: "duplicates", extra: []string{"--lint", "--duplicates"}, exitCode: 0},
	// A load with a type error never reaches lint, so the report says it did not run, and why.
	{name: "duplicates-type-error", fixture: "type-error", extra: []string{"--duplicates"}, exitCode: 1},
	// A file only windows builds and one only the cohere_extra tag builds: `--other-builds` checks both beside the
	// host's build and writes the merge of every build's findings, the windows file's else after a return among them,
	// and the lint record names each build (#72n9mq0).
	{name: "other-builds", extra: []string{"--other-builds"}, exitCode: 1},
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
	// The fix phase wraps the four code lines a wrap can bring within the limit, the Example's call among them, and
	// Greeting's chain everywhere but between its two joined literals, which useless-concatenation still reports, and
	// leaves as they were written the two it cannot (the string with a comment after it, which no break touches, and
	// the return of a long string and a name, which a break after its + would leave long) and every comment: a long
	// doc comment, a `// Serial:` marker and an Example's `// Output:` line, which a line-based reader parses. The lint
	// phase reports those five and the join alone, and a second run changes nothing.
	{
		name: "long-lines-rewritten", fixture: "long-lines", exitCode: 1,
		verify: every(wrapped("long_lines.go", "long-lines-rewritten"),
			golden("long_lines_test.go", "long-lines-rewritten"),
			keepsLine("long_lines.go", "long-lines", `	return "with thanks from the whole depot`),
			keepsLine("long_lines.go", "long-lines", "// Describe says where a delivery goes"),
			keepsLine("long_lines.go", "long-lines", "\t// Serial: "),
			keepsLine("long_lines_test.go", "long-lines", "\t// Ada Lovelace sends a parcel")),
	},
	// A file one wrap short of the long-file limit the settings give is left unwrapped, its long line a finding and
	// the note saying the wrap would take it past the limit, and the same file with room for the wrap is wrapped.
	{
		name: "long-lines-crowded", exitCode: 1, prepare: limitLongFiles(14), rules: ownLongFileAnalyzer(),
		verify: unchanged("crowded.go", "long-lines-crowded"),
	},
	{
		name: "long-lines-room", fixture: "long-lines-crowded", exitCode: 0, prepare: limitLongFiles(15),
		rules: ownLongFileAnalyzer(), verify: golden("crowded.go", "long-lines-room"),
	},
	// A writing run over a package that does not type-check writes gofmt's output and wraps nothing, saying the
	// long-line fix was held, and the same package once it type-checks is wrapped.
	{
		name: "long-lines-type-error", exitCode: 1, verify: wrapped("held.go", "long-lines-type-error"),
	},
	{
		name: "long-lines-type-error-fixed", fixture: "long-lines-type-error", exitCode: 0, prepare: fixTypeError,
		verify: wrapped("held.go", "long-lines-type-error-fixed"),
	},
	{
		name: "long-lines-no-fix", fixture: "long-lines", extra: []string{"--no-fix"}, exitCode: 1,
		verify: unchanged("long_lines.go", "long-lines"),
	},
	{
		// Greeting's join of two literals is still useless-concatenation's finding, so the run exits 1.
		name: "long-lines-rule-off", fixture: "long-lines", exitCode: 1, prepare: turnOffLongLines,
		verify: unchanged("long_lines.go", "long-lines"),
	},
	// The fix phase renames what the naming rules ask, at every reference: maxLen in two rounds to maximumLength,
	// ParseURL across its package, both test packages and the command, FetchURL with its implementation, and maxRows in
	// report.go, a file the long-line wrap writes first, whose one changedFiles entry carries both rules, and cfg and
	// modeID under their //go:embed and //go:generate directives, which come out as written, beside a //go:linkname
	// whose name the rule never flags. It leaves msg, a type switch's symbol, which no object declares, refused once
	// though three rounds read its finding, and OwnerID, which json reads by name with no tag, UserID, which reflect
	// reads with FieldByName, TitleHTML, which a template calls as .TitleHTML, LinkURL and PageID, which a template
	// parsed from an embedded page.html calls and reads, RunHTTP, which reflect's MethodByName may call by a computed
	// name, pageHTML and rowCSV, whose type names %T and reflect.TypeOf read, cfgName, which a generated file reads,
	// groupID, whose new name is taken, readHTML, which a directive keeps, and ToHTML, exported from an importable
	// package, each still a finding, and MarshalJSON, which json.Marshaler names, no finding at all. A second run
	// changes nothing.
	{name: "rename-rewritten", fixture: "rename", exitCode: 1, verify: renamedTree("rename", "rename-rewritten")},
	{
		name: "rename-no-fix", fixture: "rename", extra: []string{"--no-fix"}, exitCode: 1,
		verify: unchangedTree("rename"),
	},
	// `--no-format` leaves formatting out and still applies the rules' fixes: every rename of rename-rewritten is made
	// and each renamed file is printed by gofmt, but the long-line wrap, which is formatting, does not run, so
	// report.go's long call stays one line and a finding while its maxRows is renamed.
	{
		name: "rename-no-format", fixture: "rename", extra: []string{"--no-format"}, exitCode: 1,
		verify: renamedTreeUnder("rename", "rename-no-format", "--no-format"),
	},
	// A rename's files are written as one set: with the command's directory read-only, the rename of ParseURL, which
	// reaches it, can't be written whole, so no rename is written, each is refused as unwritable, and every finding
	// stands. The long-line wrap, which wrote report.go before the rename, is its own write and stands too.
	{
		name: "rename-unwritable", fixture: "rename", exitCode: 1, prepare: lockDirectory("command/report"),
		verify: renamedNothing("rename", "rename-unwritable", "command/report", "internal/store/report.go"),
	},
	// With files held to 12 lines, the wrap of report.go's long line would take it past the long-file limit, so both
	// wraps of it are declined and noted: the one after the load, and the one over the rename of maxRows, which
	// lengthens the line. The file holds gofmt's output of the rename, and the line stays a finding.
	{
		name: "rename-wrap-declined", fixture: "rename", exitCode: 1, prepare: limitLongFiles(12),
		rules: ownLongFileAnalyzer(), verify: renamedTree("rename", "rename-wrap-declined"),
	},
	// maxBytes is read by a file only windows builds and one only freebsd builds: it is renamed in all three, and
	// the rewrite is proven under each platform with the engine's own load for it as well as the host's.
	{
		name: "rename-platforms-rewritten", fixture: "rename-platforms", exitCode: 0,
		verify: renamedTree("rename-platforms", "rename-platforms-rewritten"),
	},
	// The same tree with a type error in the file only windows builds: nothing can prove the rename under windows,
	// so it is declined, naming the platform, and every file but the one planted reads as its fixture.
	{
		name: "rename-platforms-broken", fixture: "rename-platforms", exitCode: 1, prepare: breakUnderWindows,
		verify: unchangedBut("rename-platforms", platformsWindowsFile),
	},
	// Every load runs with cgo off, so a cgo file only windows builds is type-checked by none, and no rename may edit
	// it unproven: maxReads, which such a file reads, and maxBytes, which such a file declares as a twin, keep their
	// names, each refused with why, and every file reads as its fixture.
	{
		name: "rename-cgo", exitCode: 1, verify: unchangedTree("rename-cgo"),
	},
	// A file only windows builds declares maxBytes's new name as a local where a use of maxBytes stands, so the rename
	// would capture that use, and windows would still type-check it. The rename is refused, naming the file and the
	// platform, the finding stands, and every file reads as its fixture.
	{
		name: "rename-capture", exitCode: 1, verify: unchangedTree("rename-capture"),
	},
	// With renameExports set on the initialism rule, ToHTML is renamed too, in the library and the command.
	{
		name: "rename-exports", fixture: "rename", exitCode: 1, prepare: renameExports, rules: renamingOwnAnalyzers(),
		verify: renamedTree("rename", "rename-exports"),
	},
	// A local error written once by a single call is renamed for that call at every reference: Load's err becomes
	// readError, in its definition, its check and its return. Both's err, written by two reads, keeps the advice, and
	// is still a finding. A second run changes nothing.
	{
		name: "rename-errors-rewritten", fixture: "rename-errors", exitCode: 1,
		verify: renamedTree("rename-errors", "rename-errors-rewritten"),
	},
	// A context.Context parameter is renamed for its caller at every use: Run's ctx becomes callerContext in its
	// signature, both checks and the call it hands it to. Detach's ctx, written by two calls, keeps the advice, and is
	// still a finding. A second run changes nothing.
	{
		name: "rename-contexts-rewritten", fixture: "rename-contexts", exitCode: 1,
		verify: renamedTree("rename-contexts", "rename-contexts-rewritten"),
	},
}

// renameExports writes settings beside the copy's go.mod that set renameExports on the initialism rule.
func renameExports(root string) error {
	settingsText := `{"go": {"rules": {"` + consistency_no_uppercase_initialism.Name +
		`": ["error", {"renameExports": true}]}}}` + "\n"
	return os.WriteFile(filepath.Join(root, "CohereSettings.json"), []byte(settingsText), 0o644)
}

// renamingOwnAnalyzers is the registry with each naming rule on an analyzer of its own, so a case that sets
// renameExports sets it on no analyzer another case runs.
func renamingOwnAnalyzers() []rule.Rule {
	registered := rules.All()
	for index, each := range registered {
		switch each.Name {
		case consistency_no_abbreviated_identifier.Name:
			registered[index] = withOwnAnalyzer(each, consistency_no_abbreviated_identifier.NewAnalyzer())
		case consistency_no_uppercase_initialism.Name:
			registered[index] = withOwnAnalyzer(each, consistency_no_uppercase_initialism.NewAnalyzer())
		}
	}
	return registered
}

// goFilesOf is every .go file below root, relative and slash-separated, sorted.
func goFilesOf(test *testing.T, root string) []string {
	test.Helper()
	var files []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
			return pathError
		}
		relative, relativeError := filepath.Rel(root, path)
		files = append(files, filepath.ToSlash(relative))
		return relativeError
	})
	if walkError != nil {
		test.Fatalf("listing %s: %v", root, walkError)
	}
	slices.Sort(files)
	return files
}

// renamedTree is a verify that every .go file of the copy reads as the case's golden, `<path>.golden` beside its
// expected files under the file's own path, and that a second run, which reads the renamed tree and the same rules,
// rewrites nothing. Under -update the goldens are rewritten from the copy.
func renamedTree(fixture string, caseName string) func(test *testing.T, root string) {
	return renamedTreeUnder(fixture, caseName)
}

// renamedTreeUnder is renamedTree for a case run with extra arguments, which its second run is given too, so the
// second run asks what the first did.
func renamedTreeUnder(fixture string, caseName string, extra ...string) func(test *testing.T, root string) {
	return func(test *testing.T, root string) {
		test.Helper()
		files := goFilesOf(test, root)
		if !slices.Equal(files, goFilesOf(test, filepath.Join("testdata", fixture))) {
			test.Fatalf("the run left the files %q, and the fixture has others", files)
		}
		after := map[string][]byte{}
		for _, relative := range files {
			contents, readError := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
			if readError != nil {
				test.Fatalf("reading %s: %v", relative, readError)
			}
			after[relative] = contents
			compareExpected(test, filepath.Join("testdata", caseName, filepath.FromSlash(relative)+".golden"),
				string(contents))
		}
		registered := rules.All()
		switch caseName {
		case "rename-exports":
			registered = renamingOwnAnalyzers()
		case "rename-wrap-declined":
			registered = ownLongFileAnalyzer()
		}
		var standardOutput bytes.Buffer
		var standardError bytes.Buffer
		arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root}, extra...)
		run(arguments, &standardOutput, &standardError, registered, nil)
		for line := range strings.Lines(standardOutput.String()) {
			var record contract.FixRecord
			if strings.HasPrefix(line, `{"kind":"fix"`) && json.Unmarshal([]byte(line), &record) == nil &&
				record.FilesRewritten != 0 {
				test.Errorf("a second run rewrote %d files: %s", record.FilesRewritten, line)
			}
		}
		for _, relative := range files {
			again, againError := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
			if againError != nil || !bytes.Equal(again, after[relative]) {
				test.Errorf("a second run changed %s (%v)", relative, againError)
			}
		}
	}
}

// breakUnderWindows plants a type error in the rename-platforms copy's file only windows builds, which the host's
// load never reads.
func breakUnderWindows(root string) error {
	broken := "package limits\n\n// extra is what windows adds to a read, mistyped.\nfunc extra() int {\n" +
		"\treturn maxBytes + \"half\"\n}\n"
	return os.WriteFile(filepath.Join(root, filepath.FromSlash(platformsWindowsFile)), []byte(broken), 0o644)
}

// unchangedBut is a verify that every .go file of the copy but planted, which the case's prepare wrote, reads as it
// does in its fixture.
func unchangedBut(fixture string, planted string) func(test *testing.T, root string) {
	return func(test *testing.T, root string) {
		test.Helper()
		for _, relative := range goFilesOf(test, filepath.Join("testdata", fixture)) {
			if relative != planted {
				unchanged(relative, fixture)(test, root)
			}
		}
	}
}

// lockDirectory makes a directory of the copy read-only, so no file can be created in it, nor one renamed over a file
// there.
func lockDirectory(relative string) func(root string) error {
	return func(root string) error {
		return os.Chmod(filepath.Join(root, filepath.FromSlash(relative)), 0o555)
	}
}

// renamedNothing is a verify, after a run whose rename could not be written, that the locked directory is writable
// again, so the copy can be removed, that wrapped, the file the long-line wrap writes, reads as the case's golden, and
// that every other .go file reads as its fixture.
func renamedNothing(fixture string, caseName string, locked string, wrapped string) func(test *testing.T, root string) {
	return func(test *testing.T, root string) {
		test.Helper()
		if chmodError := os.Chmod(filepath.Join(root, filepath.FromSlash(locked)), 0o755); chmodError != nil {
			test.Fatalf("unlocking %s: %v", locked, chmodError)
		}
		contents, readError := os.ReadFile(filepath.Join(root, filepath.FromSlash(wrapped)))
		if readError != nil {
			test.Fatalf("reading %s: %v", wrapped, readError)
		}
		golden := filepath.Join("testdata", caseName, filepath.FromSlash(wrapped)+".golden")
		compareExpected(test, golden, string(contents))
		for _, relative := range goFilesOf(test, filepath.Join("testdata", fixture)) {
			if relative != wrapped {
				unchanged(relative, fixture)(test, root)
			}
		}
	}
}

// unchangedTree is a verify that every .go file of the copy reads as it does in its fixture.
func unchangedTree(fixture string) func(test *testing.T, root string) {
	return func(test *testing.T, root string) {
		test.Helper()
		for _, relative := range goFilesOf(test, filepath.Join("testdata", fixture)) {
			unchanged(relative, fixture)(test, root)
		}
	}
}

// every is a verify that runs each of verifies in order.
func every(verifies ...func(test *testing.T, root string)) func(test *testing.T, root string) {
	return func(test *testing.T, root string) {
		test.Helper()
		for _, verify := range verifies {
			verify(test, root)
		}
	}
}

// keepsLine is a verify that the one line of a fixture file starting with prefix is in the copy's file after the run,
// byte for byte and whole: a long line no break cures is left as it was written, not broken and left long, and so is
// every comment line, which the wrap never touches.
func keepsLine(relative string, fixture string, prefix string) func(test *testing.T, root string) {
	return func(test *testing.T, root string) {
		test.Helper()
		original, originalError := os.ReadFile(filepath.Join("testdata", fixture, filepath.FromSlash(relative)))
		after, afterError := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if originalError != nil || afterError != nil {
			test.Fatalf("reading %s: %v, %v", relative, originalError, afterError)
		}
		var kept []string
		for line := range strings.Lines(string(original)) {
			if strings.HasPrefix(line, prefix) {
				kept = append(kept, line)
			}
		}
		if len(kept) != 1 {
			test.Fatalf("want one line of %s starting %q, found %d", relative, prefix, len(kept))
		}
		if !slices.Contains(slices.Collect(strings.Lines(string(after))), kept[0]) {
			test.Errorf("the run changed a line it must leave as it was written:\n%s\nfile now:\n%s", kept[0], after)
		}
	}
}

// turnOffLongLines writes settings beside the copy's go.mod that turn the long-line rule off.
func turnOffLongLines(root string) error {
	settingsText := `{"go": {"rules": {"` + consistency_no_long_line.Name + `": "off"}}}` + "\n"
	return os.WriteFile(filepath.Join(root, "CohereSettings.json"), []byte(settingsText), 0o644)
}

// limitLongFiles is a prepare that writes settings beside the copy's go.mod holding files to maximum lines.
func limitLongFiles(maximum int) func(root string) error {
	return func(root string) error {
		settingsText := fmt.Sprintf(`{"go": {"rules": {"%s": ["error", {"max": %d}]}}}`+"\n",
			consistency_no_long_file.Name, maximum)
		return os.WriteFile(filepath.Join(root, "CohereSettings.json"), []byte(settingsText), 0o644)
	}
}

// ownLongFileAnalyzer is the registry with the long-file rule on an analyzer of its own, for a case whose settings
// set its limit, since an option set on the registry's analyzer would reach every case running beside it.
func ownLongFileAnalyzer() []rule.Rule {
	registered := rules.All()
	for index := range registered {
		if registered[index].Name == consistency_no_long_file.Name {
			registered[index] = withOwnAnalyzer(registered[index], consistency_no_long_file.NewAnalyzer())
		}
	}
	return registered
}

// fixTypeError is a prepare that gives the long-lines-type-error copy's Count the number it is declared to hold, so
// its package type-checks.
func fixTypeError(root string) error {
	fixed := "package heldwrap\n\n// Count is a number, given one.\nvar Count int = 1\n"
	return os.WriteFile(filepath.Join(root, "broken.go"), []byte(fixed), 0o644)
}

// golden is a verify that a file of the copy reads as the case's golden, `<name>.golden` beside its expected files.
// Under -update the golden is rewritten from the copy.
func golden(relative string, caseName string) func(test *testing.T, root string) {
	return func(test *testing.T, root string) {
		test.Helper()
		after, readError := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if readError != nil {
			test.Fatalf("reading %s: %v", relative, readError)
		}
		compareExpected(test, filepath.Join("testdata", caseName, filepath.Base(relative)+".golden"), string(after))
	}
}

// wrapped is a verify that a file of the copy reads as the case's golden (see golden), and that a second run over the
// copy rewrites nothing, since a wrap the fix phase keeps is gofmt's and its own fixed point.
func wrapped(relative string, caseName string) func(test *testing.T, root string) {
	return func(test *testing.T, root string) {
		test.Helper()
		golden(relative, caseName)(test, root)
		after, readError := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if readError != nil {
			test.Fatalf("reading %s: %v", relative, readError)
		}
		var standardOutput bytes.Buffer
		var standardError bytes.Buffer
		run([]string{"--contract", fmt.Sprint(contract.Version), "--root", root}, &standardOutput, &standardError,
			rules.All(), nil)
		for line := range strings.Lines(standardOutput.String()) {
			var record contract.FixRecord
			if strings.HasPrefix(line, `{"kind":"fix"`) && json.Unmarshal([]byte(line), &record) == nil &&
				record.FilesRewritten != 0 {
				test.Errorf("a second run rewrote %d files: %s", record.FilesRewritten, line)
			}
		}
		again, againError := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if againError != nil || !bytes.Equal(again, after) {
			test.Errorf("a second run changed %s (%v)", relative, againError)
		}
	}
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
	exitCode := run(arguments, &standardOutput, &standardError, registered, nil)
	stream := normalize(standardOutput.String(), root)
	prose := strings.ReplaceAll(standardError.String(), root, "/project")

	checkStream(test, stream, exitCode)
	if exitCode != endToEnd.exitCode {
		test.Errorf("exit %d, want %d; stderr:\n%s", exitCode, endToEnd.exitCode, prose)
	}
	compareExpected(test, expectedStreamPath(endToEnd.name), stream)
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
	ruleSettings, loadError := settings.Load(root, "", knownRuleNames(registered), nil)
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
	// The packages the findings cache missed, wrote and could not store, the facts it dropped, and the waves it
	// analyzed them in, count the toolchain's own packages, which a Go release changes, as it changes the packages
	// loaded. Hit stays, since a fresh copy hits nothing.
	// The milliseconds the cgo tool's load took vary like any elapsed time.
	cgoMillisecondsPattern = regexp.MustCompile(`("cgoTranslation":\{"packages":\d+,"milliseconds":)\d+`)
	cachePackagesPattern   = regexp.MustCompile(`"(missed|written|unstorable|factsDropped|factsSkipped|waves)":\d+`)
	panicFramesPattern     = regexp.MustCompile(`, at [^"]*"`)
	buildTargetPattern     = regexp.MustCompile(`"target":"[^"]*"`)
	// buildTimingPattern is a build record's times and, under `--timing`, its heap holdings, each written only when not
	// 0, so one that took under a millisecond on one run and more on the next would appear and vanish: each is
	// normalized to 0, which the record writes by leaving it out.
	buildTimingPattern = regexp.MustCompile(
		`,"((load|lint|decide|keep|wait|lintWait)Milliseconds|heldAt(LoadEnd|LintPeak)Bytes)":\d+`)
	// lintWaitedPattern is whether an extra build's lint waited for its weight at the gate, which turns on how the
	// builds side by side fall in time and never reaches the verdict, so it is left out as the times are.
	lintWaitedPattern     = regexp.MustCompile(`,"lintWaited":true`)
	checkedByBuildPattern = regexp.MustCompile(`"reason":"checked by the [^"]* build"`)
	// buildsAtOncePattern is how many extra builds ran side by side, the host's live heap, the first build's holding
	// and the peak factor that was decided by, and the run's pool's size, one slot per core, which the machine decides
	// and the verdict never reads, so they are left out as the times are.
	buildsAtOncePattern = regexp.MustCompile(
		`,"buildsAtOnce":\d+(,"buildsWidthLiveBytes":\d+,"buildsWidthBuildBytes":\d+,"buildsWidthPeakFactor":[\d.]+)?` +
			`(,"buildsPoolSize":\d+)?`)
	hostTarget           = " on " + runtime.GOOS + "/" + runtime.GOARCH
	normalizedProvenance = contract.ProvenanceRecord{
		Contract: contract.Version, Engine: engineName, Version: "test", Commit: "test", Toolchain: "test",
	}
)

// toolchainRoot is the `GOROOT` go env names for this process, asked once, or "" when go env can't say, in which
// case no path is normalized for it and a stream naming the toolchain's files compares unnormalized.
var toolchainRoot = sync.OnceValue(func() string {
	output, runError := exec.Command("go", "env", "GOROOT").Output()
	if runError != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
})

// normalize makes a stream comparable across runs: the copy's root reads /project, every elapsed time 0, the
// provenance fixed, the inputs held to the copy's own paths (the toolchain's, the module cache's and the temporary
// directory's move with the machine), the standard library's package count (which moves with the toolchain) N, a
// panic's frames (which move with this file) left out, the host's target `host`, and each build's target in the lint
// record `target`, as in the reason the project record gives a file another build checks, since every build is made
// on the host's `GOOS` or `GOARCH` and its cgo.
func normalize(stream string, root string) string {
	lines := strings.SplitAfter(stream, "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, `{"kind":"provenance"`) {
			var provenance bytes.Buffer
			if writeError := contract.NewWriter(&provenance).Write(normalizedProvenance); writeError == nil {
				lines[index] = provenance.String()
			}
		}
		if strings.HasPrefix(line, `{"kind":"inputs"`) {
			lines[index] = inputsInside(line, root)
		}
	}
	normalized := strings.Join(lines, "")
	normalized = strings.ReplaceAll(normalized, root, "/project")
	// The toolchain's own files, as a dependency's cgo the record names as withheld, sit where the machine keeps Go.
	if goroot := toolchainRoot(); goroot != "" {
		normalized = strings.ReplaceAll(normalized, goroot+string(filepath.Separator), "/goroot/")
	}
	normalized = elapsedPattern.ReplaceAllString(normalized, `"elapsedMilliseconds":0`)
	normalized = packagesLoaded.ReplaceAllString(normalized, "$1: N packages")
	normalized = cachePackagesPattern.ReplaceAllString(normalized, `"$1":0`)
	normalized = cgoMillisecondsPattern.ReplaceAllString(normalized, "${1}0")
	normalized = panicFramesPattern.ReplaceAllString(normalized, `, at <frames>"`)
	normalized = buildTargetPattern.ReplaceAllString(normalized, `"target":"target"`)
	normalized = buildTimingPattern.ReplaceAllString(normalized, "")
	normalized = lintWaitedPattern.ReplaceAllString(normalized, "")
	normalized = buildsAtOncePattern.ReplaceAllString(normalized, "")
	normalized = checkedByBuildPattern.ReplaceAllString(normalized, `"reason":"checked by the target build"`)
	return strings.ReplaceAll(normalized, hostTarget, " on host")
}

// inputsInside is an inputs record's line with only the paths at or below root kept, or the line as it was when it
// does not decode, so the comparison shows it.
func inputsInside(line string, root string) string {
	var record contract.InputsRecord
	if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
		return line
	}
	keep := func(paths []string) []string {
		kept := []string{}
		for _, path := range paths {
			if insideScope(path, []string{root}) {
				kept = append(kept, path)
			}
		}
		return kept
	}
	record = contract.InputsRecord{
		Files: keep(record.Files), Directories: keep(record.Directories), Absent: keep(record.Absent),
	}
	var written bytes.Buffer
	writer := contract.NewWriter(&written)
	if writeError := writer.Write(normalizedProvenance); writeError != nil {
		return line
	}
	written.Reset()
	if writeError := writer.Write(record); writeError != nil {
		return line
	}
	return written.String()
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
		if relative == entry.Name() &&
			(slices.Contains(fixtureOnlyFiles, entry.Name()) || platformStreamPattern.MatchString(entry.Name())) {
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
	inputsLine := -1
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
			NotWritten         []json.RawMessage `json:"notWritten"`
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
			gap = gap || len(record.NotWritten) > 0
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
		case "inputs":
			if inputsLine >= 0 {
				test.Errorf("line %d is a second inputs record", index+1)
			}
			inputsLine = index
			if len(phases) != 4 {
				test.Errorf("line %d is the inputs record after phases %v, and it follows every phase", index+1, phases)
			}
		case "summary":
			summarized = true
			if inputsLine >= 0 && inputsLine != index-1 {
				test.Errorf("the inputs record is on line %d, and it comes just before the summary", inputsLine+1)
			}
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
