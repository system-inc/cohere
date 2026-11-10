package dispatch

import (
	"fmt"
	"go/ast"
	"go/token"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// requireSplitting fails a test that could not split, rather than letting it pass over one run: a split takes two
// shards, at most one per core, so on one core nothing would prove a split.
func requireSplitting(test *testing.T) {
	test.Helper()
	if runtime.GOMAXPROCS(0) < 2 {
		test.Fatalf("GOMAXPROCS is %d, and a package splits into at most one shard per core", runtime.GOMAXPROCS(0))
	}
}

// heardFiles is what a syntax listener heard over a package, from every run of it at once.
type heardFiles struct {
	mutex    sync.Mutex
	entered  []string
	ifs      int
	contexts map[*rule.Context]bool
}

// listen is a listener that records each file it enters, counts each if, reports each if, and notes the context of
// each call, marked fileLocal or not.
func (heard *heardFiles) listen(fileLocal bool) *rule.Listener {
	return &rule.Listener{
		FileLocal: fileLocal,
		FileEnter: func(context *rule.Context, file *ast.File) {
			heard.mutex.Lock()
			defer heard.mutex.Unlock()
			heard.entered = append(heard.entered, context.Fset.Position(file.Pos()).Filename)
			heard.contexts[context] = true
		},
		Nodes: []ast.Node{(*ast.IfStmt)(nil)},
		Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
			heard.mutex.Lock()
			heard.ifs++
			heard.mutex.Unlock()
			context.Reportf(node.Pos(), "an if")
		},
	}
}

// A package large enough to split hands each of its files to exactly one run of a listener marked FileLocal, several
// runs over the package at once, and the listener reports what it reports unsplit: every file entered once, every if
// heard once, the same findings. A listener left unmarked beside it hears every file in one run. The split is forced on
// a two-file fixture by a shard of one byte.
func TestAFileLocalListenerHearsEachFileOnceAcrossShards(test *testing.T) {
	test.Parallel()
	requireSplitting(test)
	run := func(shardBytes int) (*heardFiles, *heardFiles, []string) {
		dispatcher := NewSyntax()
		dispatcher.shardBytes = shardBytes
		local := &heardFiles{contexts: map[*rule.Context]bool{}}
		whole := &heardFiles{contexts: map[*rule.Context]bool{}}
		localAnalyzer := dispatcher.RuleAnalyzer(rule.Namespace+"local", "local", "hears some files",
			local.listen(true))
		wholeAnalyzer := dispatcher.RuleAnalyzer(rule.Namespace+"whole", "whole", "hears every file",
			whole.listen(false))
		findings := []string{}
		for _, action := range analyzeFixture(test, "walked", localAnalyzer, wholeAnalyzer).Roots {
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			for _, diagnostic := range action.Diagnostics {
				findings = append(findings, action.Analyzer.Name+" "+
					action.Package.Fset.Position(diagnostic.Pos).String())
			}
		}
		slices.Sort(findings)
		return local, whole, findings
	}
	local, whole, splitFindings := run(1)
	_, _, wholeFindings := run(defaultShardBytes)

	if len(local.contexts) < 2 {
		test.Fatalf("the file-local listener ran in %d context, so the package did not split", len(local.contexts))
	}
	if len(whole.contexts) != 1 {
		test.Errorf("the unmarked listener ran in %d contexts, want the one run over every file", len(whole.contexts))
	}
	slices.Sort(local.entered)
	slices.Sort(whole.entered)
	if len(whole.entered) != 2 || !slices.Equal(local.entered, whole.entered) || local.ifs != whole.ifs {
		test.Errorf("the file-local listener entered %v and heard %d ifs, the unmarked one %v and %d, want each file "+
			"once and every if once in both", local.entered, local.ifs, whole.entered, whole.ifs)
	}
	if len(wholeFindings) == 0 || !slices.Equal(splitFindings, wholeFindings) {
		test.Errorf("split, the findings were\n%s\nand unsplit\n%s", strings.Join(splitFindings, "\n"),
			strings.Join(wholeFindings, "\n"))
	}
}

// A listener that gathers across a package's files stays right in a package that splits, since unmarked it hears every
// file in one run: it counts the files it has entered in its State and reports at the last file's exit how many it
// saw, which judged over some of the files would say fewer, or, marked FileLocal, crash on State.
func TestACrossFileListenerStaysWholeInASplitPackage(test *testing.T) {
	test.Parallel()
	requireSplitting(test)
	type seen struct{ files int }
	dispatcher := NewSyntax()
	dispatcher.shardBytes = 1
	counting := dispatcher.RuleAnalyzer(rule.Namespace+"counting", "counting", "counts the files it hears",
		&rule.Listener{
			FileEnter: func(context *rule.Context, _ *ast.File) {
				rule.State[seen](context).files++
			},
			FileExit: func(context *rule.Context, file *ast.File) {
				if state := rule.State[seen](context); state.files == len(context.Files) {
					context.Reportf(file.Pos(), "saw %d of %d files", state.files, len(context.Files))
				}
			},
		})
	// Beside a file-local listener, so the package splits.
	local := &heardFiles{contexts: map[*rule.Context]bool{}}
	splitting := dispatcher.RuleAnalyzer(rule.Namespace+"local", "local", "hears some files", local.listen(true))
	messages := []string{}
	for _, action := range analyzeFixture(test, "walked", counting, splitting).Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		if action.Analyzer == counting {
			for _, diagnostic := range action.Diagnostics {
				messages = append(messages, diagnostic.Message)
			}
		}
	}
	if len(local.contexts) < 2 {
		test.Fatalf("the file-local listener ran in %d context, so the package did not split", len(local.contexts))
	}
	if !slices.Equal(messages, []string{"saw 2 of 2 files"}) {
		test.Errorf("the gathering listener reported %q, want one finding saying it saw both files", messages)
	}
}

// heardFunctions is what an own-walk listener heard over a package, from every run of it at once.
type heardFunctions struct {
	mutex     sync.Mutex
	functions []string
	contexts  map[*rule.Context]bool
}

// A package large enough to split hands each source function to exactly one run of an own-walk listener marked
// FileLocal, the run of the shard holding the function's file, and every function to a listener left unmarked, in one
// run. The fixture declares a function in each of its two files, one with an anonymous function inside it.
func TestAFileLocalOwnListenerHearsEachFunctionOnceAcrossShards(test *testing.T) {
	test.Parallel()
	requireSplitting(test)
	dispatcher := NewIntermediateRepresentation()
	dispatcher.shardBytes = 1
	listen := func(heard *heardFunctions, fileLocal bool) *rule.Listener {
		own := &rule.IntermediateRepresentationListener{
			Function: func(context *rule.Context, function *intermediate_representation.Function) {
				heard.mutex.Lock()
				defer heard.mutex.Unlock()
				heard.functions = append(heard.functions, function.String())
				heard.contexts[context] = true
			},
		}
		return &rule.Listener{FileLocal: fileLocal, IntermediateRepresentation: own}
	}
	local := &heardFunctions{contexts: map[*rule.Context]bool{}}
	whole := &heardFunctions{contexts: map[*rule.Context]bool{}}
	analyzers := []*analysis.Analyzer{
		dispatcher.RuleAnalyzer(rule.Namespace+"local", "local", "hears some functions", listen(local, true)),
		dispatcher.RuleAnalyzer(rule.Namespace+"whole", "whole", "hears every function", listen(whole, false)),
	}
	graph, analyzeError := checker.Analyze(analyzers, loadFixture(test, "walked"), &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}
	if len(local.contexts) < 2 || len(whole.contexts) != 1 {
		test.Fatalf("the file-local listener ran in %d contexts and the unmarked one in %d, want a split and one run",
			len(local.contexts), len(whole.contexts))
	}
	slices.Sort(local.functions)
	slices.Sort(whole.functions)
	if len(whole.functions) < 2 || !slices.Equal(local.functions, whole.functions) {
		test.Errorf("the file-local listener heard %v and the unmarked one %v, want every function once in both",
			local.functions, whole.functions)
	}
}

// A package splits into one shard per shardBytes of its source, at most one per core, and not at all below two, and
// its files are cut into runs of consecutive files of about as much source each, every file in one shard.
func TestAPackageSplitsByItsSourceIntoConsecutiveFiles(test *testing.T) {
	test.Parallel()
	requireSplitting(test)
	fileSet := token.NewFileSet()
	pass := &analysis.Pass{Fset: fileSet}
	for _, size := range []int{100, 100, 100, 100} {
		tokenFile := fileSet.AddFile("", -1, size)
		pass.Files = append(pass.Files, &ast.File{Package: token.Pos(tokenFile.Base())})
	}
	dispatcher := NewSyntax()
	for _, testCase := range []struct {
		shardBytes int
		want       int
	}{
		{shardBytes: 1000, want: 1},
		{shardBytes: 300, want: 1},
		{shardBytes: 200, want: 2},
		{shardBytes: 1, want: runtime.GOMAXPROCS(0)},
	} {
		dispatcher.shardBytes = testCase.shardBytes
		if shards := dispatcher.shardsOf(pass); shards != testCase.want {
			test.Errorf("at %d bytes a shard, 400 bytes of source split into %d shards, want %d",
				testCase.shardBytes, shards, testCase.want)
		}
	}
	shardOf := shardOfFile(pass, 2)
	got := []int{}
	for _, file := range pass.Files {
		got = append(got, shardOf[fileSet.File(file.Pos())])
	}
	if !slices.Equal(got, []int{0, 0, 1, 1}) {
		test.Errorf("four equal files cut into two shards as %v, want [0 0 1 1]", got)
	}
}

// A file-local listener that gathers over one file in FileState and judges it at FileExit reports the same split as
// unsplit, and what a walk of each file alone says: the ifs of each file, counted in a slot that file alone fills. A
// slot shared across files would carry one file's count into the next wherever a run hears two.
func TestAFileLocalListenerReadingFileStateJudgesEachFileAlone(test *testing.T) {
	test.Parallel()
	requireSplitting(test)
	type counted struct{ ifs int }
	run := func(shardBytes int) []string {
		dispatcher := NewSyntax()
		dispatcher.shardBytes = shardBytes
		counting := dispatcher.RuleAnalyzer(rule.Namespace+"counting", "counting", "counts each file's ifs",
			&rule.Listener{
				FileLocal: true,
				Nodes:     []ast.Node{(*ast.IfStmt)(nil)},
				Node: func(context *rule.Context, _ ast.Node, _ []ast.Node) {
					rule.FileState[counted](context).ifs++
				},
				FileExit: func(context *rule.Context, file *ast.File) {
					context.Reportf(file.Pos(), "%d ifs", rule.FileState[counted](context).ifs)
				},
			})
		messages := []string{}
		for _, action := range analyzeFixture(test, "walked", counting).Roots {
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			for _, diagnostic := range action.Diagnostics {
				messages = append(messages, action.Package.Fset.Position(diagnostic.Pos).Filename+": "+
					diagnostic.Message)
			}
		}
		slices.Sort(messages)
		return messages
	}
	split, whole := run(1), run(defaultShardBytes)

	probe, files := filesProbe()
	expected := []string{}
	for _, action := range analyzeFixture(test, "walked", probe).Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}
	for _, file := range files.files {
		ifs := 0
		ast.Inspect(file, func(node ast.Node) bool {
			if _, isIf := node.(*ast.IfStmt); isIf {
				ifs++
			}
			return true
		})
		expected = append(expected, fmt.Sprintf("%s: %d ifs", files.fileSet.Position(file.Pos()).Filename, ifs))
	}
	slices.Sort(expected)
	if len(expected) != 2 || !slices.Equal(whole, expected) || !slices.Equal(split, expected) {
		test.Errorf("unsplit the listener said\n%s\nsplit\n%s\nwhere each file alone has\n%s",
			strings.Join(whole, "\n"), strings.Join(split, "\n"), strings.Join(expected, "\n"))
	}
}

// TestTheDefaultShardSplitsWhatEndsACohereRun pins defaultShardBytes by the packages it was set for (#3ddm7fm), each
// by its size in source bytes at cohere's root: internal/lint/rules/core's test variant splits, and so does
// internal/lint/rules/typescript's, which a 4 MiB shard left whole at the end of the cold run; react's test variant and
// command/cohere stay whole, since splitting them bought no wall.
func TestTheDefaultShardSplitsWhatEndsACohereRun(test *testing.T) {
	test.Parallel()
	requireSplitting(test)
	dispatcher := NewSyntax()
	for _, testCase := range []struct {
		name  string
		bytes int
		split bool
	}{
		{name: "internal/lint/rules/core's test variant", bytes: 14_259_553, split: true},
		{name: "internal/lint/rules/typescript's test variant", bytes: 5_775_349, split: true},
		{name: "internal/lint/rules/react's test variant", bytes: 5_182_045, split: false},
		{name: "command/cohere", bytes: 2_098_452, split: false},
	} {
		fileSet := token.NewFileSet()
		tokenFile := fileSet.AddFile("", -1, testCase.bytes)
		pass := &analysis.Pass{Fset: fileSet, Files: []*ast.File{{Package: token.Pos(tokenFile.Base())}}}
		if split := dispatcher.shardsOf(pass) > 1; split != testCase.split {
			test.Errorf("%s, %d bytes, splits %v at the default shard of %d bytes, want %v", testCase.name,
				testCase.bytes, split, defaultShardBytes, testCase.split)
		}
	}
}
