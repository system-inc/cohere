package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// syntaxInheritFixture is a module of one package whose shared.go and other.go every build compiles and whose
// platform_windows.go only windows's does, each holding a literal the planted probe reports.
var syntaxInheritFixture = filepath.Join("testdata", "syntax-inherit")

// plantedProbeName is the planted probe's rule name.
const plantedProbeName = rule.Namespace + "correctness-probe-planted"

// inheritedCounts is a build record's counts of what it inherited, which a run inheriting writes and one that walks
// every file does not.
var inheritedCounts = regexp.MustCompile(`,"inherited(Files|Findings)":[0-9]+`)

// enteredFiles counts the files the planted probe's listener entered over a run, every build's, by base name.
type enteredFiles struct {
	lock    sync.Mutex
	entered map[string]int
}

// plantedProbe is a house rule marked syntax-only, on a syntax dispatcher of its own so tests running at once never
// share its counts, whose listener reports each string literal holding "planted", with a fix writing "grown" in its
// place, and counts into entered each file it enters.
func plantedProbe(entered *enteredFiles) rule.Rule {
	listener := &rule.Listener{
		FileLocal:  true,
		SyntaxOnly: true,
		FileEnter: func(context *rule.Context, file *ast.File) {
			entered.lock.Lock()
			defer entered.lock.Unlock()
			entered.entered[filepath.Base(context.FileSet.File(file.FileStart).Name())]++
		},
		Nodes: []ast.Node{(*ast.BasicLit)(nil)},
		Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
			literal, isLiteral := node.(*ast.BasicLit)
			if !isLiteral || literal.Kind != token.STRING || !strings.Contains(literal.Value, "planted") {
				return
			}
			context.Report(analysis.Diagnostic{Pos: literal.Pos(), End: literal.End(), Category: "plantedLiteral",
				Message: "a planted literal", SuggestedFixes: []analysis.SuggestedFix{{
					Message: "grow it",
					TextEdits: []analysis.TextEdit{
						{Pos: literal.Pos(), End: literal.End(), NewText: []byte(`"grown"`)},
					},
				}}})
		},
	}
	analyzer := dispatch.NewSyntax().RuleAnalyzer(plantedProbeName, "probe_planted", "reports each planted literal",
		listener)
	return rule.Rule{Name: plantedProbeName, Origin: rule.OriginHouse, Analyzer: analyzer, Listener: listener}
}

// runInheriting runs the engine over a copy of the syntax-inherit fixture with the registry's rules and the planted
// probe, under `--no-cache --other-builds` and extra, inheriting or not, and returns the pipeline, its stream,
// normalized, the files the probe entered over every build, and the copy's root.
func runInheriting(test *testing.T, inherits bool, extra ...string) (*pipeline, string, map[string]int, string) {
	test.Helper()
	entered := &enteredFiles{entered: map[string]int{}}
	root := copyFixture(test, syntaxInheritFixture)
	engine, stream, _ := runOverRegistered(test, root, exitFindings, append(rules.All(), plantedProbe(entered)),
		func(engine *pipeline) { engine.keepsInheritanceOff = !inherits },
		append([]string{"--no-cache", "--other-builds"}, extra...)...)
	entered.lock.Lock()
	defer entered.lock.Unlock()
	return engine, normalize(stream, root), maps.Clone(entered.entered), root
}

// buildFindings are run's own findings of the planted probe, before the merge, sorted, each with its suggestions, and
// with root, the run's copy of the fixture, written as /project.
func buildFindings(run buildRun, root string) []string {
	found := []string{}
	for _, finding := range run.result.Findings {
		if finding.Rule == plantedProbeName {
			found = append(found, strings.ReplaceAll(fmt.Sprintf("%+v", finding), root, "/project"))
		}
	}
	slices.Sort(found)
	return found
}

// plantedFindings are the stream's findings of the planted probe, as written.
func plantedFindings(stream string) []string {
	found := []string{}
	for line := range strings.SplitSeq(stream, "\n") {
		if strings.HasPrefix(line, `{"kind":"finding"`) && strings.Contains(line, `"rule":"`+plantedProbeName+`"`) {
			found = append(found, line)
		}
	}
	return found
}

// windowsRecord is the stream's lint record's build record of windows, read by its place among the builds, the host's
// first, decoded and as written.
func windowsRecord(test *testing.T, engine *pipeline, stream string) (contract.BuildRecord, string) {
	test.Helper()
	at := slices.IndexFunc(engine.buildRuns, func(run buildRun) bool { return run.build.OperatingSystem == "windows" })
	for line := range strings.SplitSeq(stream, "\n") {
		if !strings.HasPrefix(line, `{"kind":"lint"`) {
			continue
		}
		var record struct {
			Builds []json.RawMessage `json:"builds"`
		}
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil || at < 0 ||
			at >= len(record.Builds) {
			test.Fatalf("reading windows's build record at %d of %s: %v", at, line, decodeError)
		}
		var build contract.BuildRecord
		if decodeError := json.Unmarshal(record.Builds[at], &build); decodeError != nil {
			test.Fatalf("decoding windows's build record %s: %v", record.Builds[at], decodeError)
		}
		return build, string(record.Builds[at])
	}
	test.Fatalf("the run wrote no lint record:\n%s", stream)
	return contract.BuildRecord{}, ""
}

// A syntax-only rule in a build beside the host's takes the host's findings on each file the host checked rather than
// walk the file again (#y6q1x2j): over a package windows checks from source because of its windows file, the planted
// probe enters shared.go and other.go once over the whole run, in the host's build, and platform_windows.go once, in
// windows's, which counts two files and two findings inherited. Windows's own findings, before the merge, are the ones
// it finds walking every file, positions, ends, messages and suggestions alike, and the stream is the one a run walking
// every file in every build writes, but for those counts. The merge alone could not tell: the host's findings on the
// files windows inherits stand there whatever windows says.
func TestASyntaxOnlyRuleInheritsTheHostsFindingsOnTheFilesTheHostChecked(test *testing.T) {
	test.Parallel()
	engine, on, enteredOn, onRoot := runInheriting(test, true)
	walking, off, enteredOff, offRoot := runInheriting(test, false)
	if inheritedCounts.ReplaceAllString(on, "") != off {
		test.Errorf("inheriting changed the stream:\ninheriting\n%s\nwalking every file\n%s", on, off)
	}
	inherited, walked := buildFindings(windowsRun(test, engine), onRoot), buildFindings(windowsRun(test, walking),
		offRoot)
	if len(walked) != 3 || !slices.Equal(inherited, walked) {
		test.Errorf("windows found, inheriting\n%s\nwalking every file\n%s\nwant three, alike",
			strings.Join(inherited, "\n"), strings.Join(walked, "\n"))
	}
	if found := plantedFindings(on); len(found) != 3 {
		test.Errorf("the planted probe found %d literals, want one in each of the three files:\n%s", len(found),
			strings.Join(found, "\n"))
	}
	windows := windowsRun(test, engine)
	if windows.inheritedFiles != 2 || windows.inheritedFindings != 2 {
		test.Errorf("windows inherited %d files and %d findings, want shared.go's and other.go's, one each",
			windows.inheritedFiles, windows.inheritedFindings)
	}
	if record, _ := windowsRecord(test, engine, on); record.InheritedFiles != 2 || record.InheritedFindings != 2 {
		test.Errorf("windows's build record counts %d files and %d findings inherited, want 2 and 2",
			record.InheritedFiles, record.InheritedFindings)
	}
	expectedOn := map[string]int{"shared.go": 1, "other.go": 1, "platform_windows.go": 1}
	expectedOff := map[string]int{"shared.go": 2, "other.go": 2, "platform_windows.go": 1}
	if !maps.Equal(enteredOn, expectedOn) || !maps.Equal(enteredOff, expectedOff) {
		test.Errorf("the probe entered %v inheriting and %v walking every file, want %v and %v", enteredOn,
			enteredOff, expectedOn, expectedOff)
	}
}

// A fixing run writes nothing of an inherited finding's fix and offers it once (#y6q1x2j): the engine applies no
// rule's suggestion but a naming rule's rename, which it takes from the host's own findings before any other build
// runs, so the run without `--no-fix` leaves every file of the fixture as it was, its fix record applying nothing, and
// the merge holds the host's finding on shared.go and windows's inherited one as one finding, its one suggestion's one
// edit over the literal's bytes, exactly as a run walking every file writes it. Windows's own inherited finding offers
// that one edit too.
func TestAFixingRunOffersAnInheritedFixOnce(test *testing.T) {
	test.Parallel()
	engine, on, _, root := runInheriting(test, true)
	_, off, _, _ := runInheriting(test, false)
	windows := windowsRun(test, engine)
	if windows.inheritedFindings == 0 {
		test.Fatal("windows inherited nothing, so the run shows nothing of an inherited fix")
	}
	windowsEdit := "Suggestions:[{Message:grow it Edits:[{File:/project/shared.go Start:315 End:333 Text:\"grown\"}]}]"
	ownShared := slices.DeleteFunc(buildFindings(windows, root), func(found string) bool {
		return !strings.Contains(found, "File:/project/shared.go Line:")
	})
	if len(ownShared) != 1 || !strings.Contains(ownShared[0], windowsEdit) {
		test.Errorf("windows's own findings on shared.go are\n%s\nwant one offering %s", strings.Join(ownShared, "\n"),
			windowsEdit)
	}
	for _, name := range []string{"go.mod", "shared.go", "other.go", "platform_windows.go"} {
		written, readError := os.ReadFile(filepath.Join(root, name))
		original, originalError := os.ReadFile(filepath.Join(syntaxInheritFixture, name))
		if readError != nil || originalError != nil || !bytes.Equal(written, original) {
			test.Errorf("the run wrote %s (%v, %v):\n%s", name, readError, originalError, written)
		}
	}
	for line := range strings.SplitSeq(on, "\n") {
		if strings.HasPrefix(line, `{"kind":"fix"`) && (!strings.Contains(line, `"filesRewritten":0`) ||
			!strings.Contains(line, `"fixesApplied":0`)) {
			test.Errorf("the fix record says the run wrote something: %s", line)
		}
	}
	shared := []string{}
	for _, found := range plantedFindings(on) {
		if strings.Contains(found, `"file":"/project/shared.go"`) {
			shared = append(shared, found)
		}
	}
	// The literal is `"planted greeting"`, 18 bytes from offset 315 of shared.go.
	offered := `"suggestions":[{"message":"grow it","fixes":[{"start":315,"end":333,"text":"\"grown\""}]}]`
	if len(shared) != 1 || !strings.Contains(shared[0], offered) {
		test.Errorf("the stream holds shared.go's planted finding %d times, want once offering %s:\n%s", len(shared),
			offered, strings.Join(shared, "\n"))
	}
	if !slices.Equal(plantedFindings(on), plantedFindings(off)) {
		test.Errorf("inheriting offered\n%s\nwhere walking every file offered\n%s",
			strings.Join(plantedFindings(on), "\n"), strings.Join(plantedFindings(off), "\n"))
	}
}

// A timed run keeps nothing of the host's for its builds, so under `--timing` with extra builds windows inherits
// nothing (#y6q1x2j): its record counts no file inherited, and the probe enters shared.go in both builds.
func TestATimedRunWithExtraBuildsInheritsNothing(test *testing.T) {
	test.Parallel()
	engine, stream, entered, _ := runInheriting(test, true, "--timing")
	windows := windowsRun(test, engine)
	if windows.inheritedFiles != 0 || windows.inheritedFindings != 0 {
		test.Errorf("the timed windows build inherited %d files and %d findings, want none", windows.inheritedFiles,
			windows.inheritedFindings)
	}
	if _, written := windowsRecord(test, engine, stream); strings.Contains(written, `"inherited`) {
		test.Errorf("the timed windows build's record counts something inherited, want the counts left out: %s",
			written)
	}
	if entered["shared.go"] != 2 {
		test.Errorf("the timed run's probe entered shared.go %d times, want once in each build", entered["shared.go"])
	}
}

// syntaxInheritGeneratedFixture is a module of one package whose shared.go ends in a redundant return a line directive
// places in gen_notwindows.go, a generated file every build but windows compiles, and whose platform_windows.go only
// windows compiles.
var syntaxInheritGeneratedFixture = filepath.Join("testdata", "syntax-inherit-generated")

// A rule whose findings pass FilterGenerated is not syntax-only (#y6q1x2j): staticcheck withholds a finding whose
// display file, the one a line directive names, is generated among the package's files as the build compiled them, so
// another file decides. The host compiles gen_notwindows.go and withholds S1023's finding on shared.go; windows does
// not compile it and reports the finding. Windows's own findings of S1023 are the same inheriting or not, one on
// shared.go: a mark on S1023 would have windows inherit the host's none.
func TestAFindingAnotherFileWithholdsIsNotInherited(test *testing.T) {
	test.Parallel()
	redundantReturn := rule.Namespace + "S1023"
	windowsFindings := func(inherits bool) []string {
		root := copyFixture(test, syntaxInheritGeneratedFixture)
		engine, _, _ := runOverRegistered(test, root, exitFindings, rules.All(),
			func(engine *pipeline) { engine.keepsInheritanceOff = !inherits },
			"--no-cache", "--no-fix", "--other-builds")
		found := []string{}
		for _, finding := range windowsRun(test, engine).result.Findings {
			if finding.Rule == redundantReturn {
				found = append(found, fmt.Sprintf("%s:%d:%d %s", filepath.Base(finding.File), finding.Line,
					finding.Column, finding.Message))
			}
		}
		slices.Sort(found)
		return found
	}
	inheriting, walking := windowsFindings(true), windowsFindings(false)
	expected := []string{"shared.go:8:2 redundant return statement"}
	if !slices.Equal(walking, expected) || !slices.Equal(inheriting, walking) {
		test.Errorf("windows found of S1023, inheriting\n%s\nwalking every file\n%s\nwant %q both ways",
			strings.Join(inheriting, "\n"), strings.Join(walking, "\n"), expected)
	}
}

// A run judging each build's dependence restores nothing from the host's run, so under `--build-dependence` windows
// inherits nothing (#y6q1x2j): its record counts no file inherited, and the probe enters shared.go in both builds.
func TestABuildDependenceRunInheritsNothing(test *testing.T) {
	test.Parallel()
	engine, stream, entered, _ := runInheriting(test, true, "--build-dependence")
	requireInheritedNothing(test, engine, stream, entered, 2)
}

// A run that renamed reads no findings cache, so its builds restore nothing from the host's run and windows inherits
// nothing (#y6q1x2j): with an uppercase initialism the naming rule renames, the run renames it, windows's record counts
// no file inherited, and the probe enters shared.go in windows's build as well as twice in the host's, once in the pass
// the rename was planned from and once over what it wrote. The host of a renamed run keeps nothing for its builds
// too, so the run inherits nothing on either count.
func TestARenamedRunInheritsNothing(test *testing.T) {
	test.Parallel()
	entered := &enteredFiles{entered: map[string]int{}}
	root := copyFixture(test, syntaxInheritFixture)
	// Unexported, since a run renames no exported name of an importable package.
	renamed := "package inherit\n\n// parseURL is a name the naming rule renames.\nfunc parseURL() string {\n" +
		"\treturn \"planted address\"\n}\n"
	if writeError := os.WriteFile(filepath.Join(root, "renamed.go"), []byte(renamed), 0o600); writeError != nil {
		test.Fatal(writeError)
	}
	engine, stream, _ := runOverRegistered(test, root, exitFindings, append(rules.All(), plantedProbe(entered)), nil,
		"--no-cache", "--other-builds")
	if !engine.renamed {
		test.Fatalf("the run renamed nothing, so it shows nothing of a renamed run:\n%s", normalize(stream, root))
	}
	entered.lock.Lock()
	defer entered.lock.Unlock()
	requireInheritedNothing(test, engine, normalize(stream, root), entered.entered, 3)
}

// requireInheritedNothing fails test unless windows's build in engine's run inherited nothing: its run and its record
// in stream count nothing inherited, and the probe entered shared.go the times the run's passes over it, windows's
// among them, number.
func requireInheritedNothing(test *testing.T, engine *pipeline, stream string, entered map[string]int, passes int) {
	test.Helper()
	windows := windowsRun(test, engine)
	if windows.inheritedFiles != 0 || windows.inheritedFindings != 0 {
		test.Errorf("windows inherited %d files and %d findings, want none", windows.inheritedFiles,
			windows.inheritedFindings)
	}
	if _, written := windowsRecord(test, engine, stream); strings.Contains(written, `"inherited`) {
		test.Errorf("windows's record counts something inherited, want the counts left out: %s", written)
	}
	if entered["shared.go"] != passes {
		test.Errorf("the probe entered shared.go %d times, want %d, windows's among them", entered["shared.go"],
			passes)
	}
}
