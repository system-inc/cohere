package load

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cgo_testing"
)

// stalePatterns are what the tests list of the stale workspace: a's packages, which import dep.
var stalePatterns = []string{"example.com/a/..."}

// Every go command a load runs in a workspace, the lister's build list and build context, the listing go list answers
// when the lister doubts, the cgo tool's translation, and a whole load under the host's environment and under an
// extra build's, leaves a stale go.work.sum byte for byte as it was, and runs beside a side copy of the go.work, never
// in the tree (#34r3v1y). Each without the guard rewrites the file, so the fixture is stale for each.
func TestEveryGoCommandOfALoadLeavesAStaleGoWorkSumAsItWas(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name  string
		files []map[string]string
		extra []string
		cgo   bool
		run   func(test *testing.T, goEnvironment *GoEnvironment, workspace string)
	}{
		{name: "the lister's build list and build context", run: func(test *testing.T, goEnvironment *GoEnvironment,
			workspace string) {
			if _, doubt := readGoState(goEnvironment, nil); doubt != "" {
				test.Fatal(doubt)
			}
		}},
		{name: "the listing go list answers", run: func(test *testing.T, goEnvironment *GoEnvironment,
			workspace string) {
			if _, listError := listWithGoList(goEnvironment.guard, workspace, goEnvironment.fullEnvironment,
				stalePatterns, nil); listError != nil {
				test.Fatal(listError)
			}
		}},
		{name: "the cgo tool's translation", files: []map[string]string{staleCgoFiles},
			extra: []string{"CGO_ENABLED=1"}, cgo: true, run: func(test *testing.T, goEnvironment *GoEnvironment,
				workspace string) {
				listed := &listing{byPath: map[string]*listedPackage{}}
				listed.add([]*listedPackage{{ImportPath: "example.com/a/native", CgoFiles: []string{"native.go"},
					Directory: filepath.Join(workspace, "a", "native")}})
				if _, compiledError := listCompiled(goEnvironment.guard, workspace,
					translatedEnvironment(goEnvironment), listed); compiledError != nil {
					test.Fatal(compiledError)
				}
			}},
		{name: "a whole load", run: loadUnderTest},
		{name: "a whole load under an extra build's environment", extra: []string{"GOOS=windows", "GOARCH=amd64"},
			run: loadUnderTest},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			if each.cgo {
				cgo_testing.RequireTranslation(test)
			}
			for _, guarded := range []bool{true, false} {
				workspace, environment := writeStaleWorkspace(test, append([]map[string]string{staleWorkspaceFiles},
					each.files...)...)
				environment = append(environment, each.extra...)
				before := readSums(test, workspace)
				goEnvironment := goEnvironmentOf(test, workspace, environment)
				if !guarded {
					unguarded := *goEnvironment
					unguarded.guard = nil
					goEnvironment = &unguarded
				}
				each.run(test, goEnvironment, workspace)
				after := readSums(test, workspace)
				if guarded && !bytes.Equal(after, before) {
					test.Errorf("go.work.sum was rewritten under the guard:\n%s\nwant it as it was:\n%s", after, before)
				}
				if guarded && goEnvironment.guard.unguardedSum() != "" {
					test.Errorf("a command ran beside %s with no side copy", goEnvironment.guard.unguardedSum())
				}
				if !guarded && bytes.Equal(after, before) {
					test.Errorf("without the guard go.work.sum was left as it was, so the fixture proves nothing")
				}
			}
		})
	}
}

// loadUnderTest loads the stale workspace's patterns under goEnvironment, as the engine's load does.
func loadUnderTest(test *testing.T, goEnvironment *GoEnvironment, workspace string) {
	test.Helper()
	if _, loadError := PackagesCached(workspace, stalePatterns, goEnvironment.environment, nil,
		Caching{GoEnvironment: goEnvironment}); loadError != nil {
		test.Fatal(loadError)
	}
}

// sideWorkspaceFiles is a workspace whose go list answers name paths: a requires other, which its go.mod replaces with
// a directory beside it, and beside, which the go.work replaces with one of its own, each spelled relative to where
// it is written, so go prints each as it spells it from the go.work's directory.
var sideWorkspaceFiles = map[string]string{
	"go.work": "go 1.21\n\n// The module the tests list.\nuse ./a\n\nreplace example.com/beside => ./beside\n",
	"a/go.mod": "module example.com/a\n\ngo 1.21\n\nrequire (\n\texample.com/other v0.0.0\n" +
		"\texample.com/beside v0.0.0\n)\n\nreplace example.com/other => ../other\n",
	"a/a.go": "package a\n\nimport (\n\t\"example.com/beside\"\n\t\"example.com/other\"\n)\n\n" +
		"// Value is both of theirs.\nconst Value = other.Value + beside.Value\n",
	"other/go.mod":     "module example.com/other\n\ngo 1.21\n",
	"other/other.go":   "package other\n\n// Value is one.\nconst Value = 1\n",
	"beside/go.mod":    "module example.com/beside\n\ngo 1.21\n",
	"beside/beside.go": "package beside\n\n// Value is two.\nconst Value = 2\n",
}

// A go command run beside the side copy of a go.work answers what it answers in the tree, byte for byte, on its output
// and its standard error, and so does a whole load, its diagnostics, its error, and every module its packages name,
// for a workspace whose answers name paths go spells from the go.work's directory, and for each way go list reports
// an error that names one: a replacement directory that is missing, from a go.mod or from the go.work, a godebug go
// does not know, a module used twice, a use of a directory with no module, an import nothing provides, and a go.mod
// that does not parse. Every command ran beside the copy, none in the tree.
func TestASideCopyAnswersAsTheTreeDoes(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name    string
		changed map[string]string
	}{
		{name: "relative replacements"},
		{name: "a go.mod's replacement directory that is missing", changed: map[string]string{
			"a/go.mod": "module example.com/a\n\ngo 1.21\n\nrequire (\n\texample.com/other v0.0.0\n" +
				"\texample.com/beside v0.0.0\n)\n\nreplace example.com/other => ../gone\n",
		}},
		{name: "the go.work's replacement directory that is missing", changed: map[string]string{
			"go.work": "go 1.21\n\nuse ./a\n\nreplace example.com/beside => ./gone\n",
		}},
		{name: "a godebug go does not know", changed: map[string]string{
			"go.work": "go 1.21\n\ngodebug unknownsetting=1\n\nuse ./a\n\nreplace example.com/beside => ./beside\n",
		}},
		{name: "a module used twice", changed: map[string]string{
			"go.work": "go 1.21\n\nuse ./a\nuse a\n\nreplace example.com/beside => ./beside\n",
		}},
		{name: "a use of a directory with no module", changed: map[string]string{
			"go.work": "go 1.21\n\nuse (\n\t./a\n\t./nothing\n)\n\nreplace example.com/beside => ./beside\n",
		}},
		{name: "an import nothing provides", changed: map[string]string{
			"a/b/b.go": "package b\n\nimport \"example.com/nowhere/thing\"\n\n// Value is thing's.\n" +
				"const Value = thing.Value\n",
		}},
		{name: "a go.mod that does not parse", changed: map[string]string{
			"go.work":  "go 1.21\n\nuse (\n\t./a\n\t./b\n)\n\nreplace example.com/beside => ./beside\n",
			"b/go.mod": "module example.com/b\n\nthis is no directive\n",
			"b/b.go":   "package b\n",
		}},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			workspace, environment := writeStaleWorkspace(test, sideWorkspaceFiles, each.changed)
			goEnvironment := goEnvironmentOf(test, workspace, environment)
			for _, arguments := range [][]string{
				{"list", "-m", "-json", "all"},
				{"list", "-e", "-deps", "-json=" + listedFields, "example.com/a/..."},
			} {
				aside, standardErrorAside, errorAside := goEnvironment.guard.output(goCommand(goEnvironment, arguments))
				tree, standardErrorTree, errorTree := runCommand(goCommand(goEnvironment, arguments))
				name := strings.Join(arguments[:2], " ")
				compareAnswers(test, name+"'s output", aside, tree)
				compareAnswers(test, name+"'s standard error", standardErrorAside, standardErrorTree)
				compareAnswers(test, name+"'s end", []byte(fmt.Sprint(errorAside)), []byte(fmt.Sprint(errorTree)))
			}
			loadedAside, errorAside := loadAnswer(test, workspace, goEnvironment)
			if unguarded := goEnvironment.guard.unguardedSum(); unguarded != "" {
				test.Errorf("a command ran beside %s with no side copy", unguarded)
			}
			unguarded := *goEnvironment
			unguarded.guard = nil
			loadedTree, errorTree := loadAnswer(test, workspace, &unguarded)
			compareAnswers(test, "the load", loadedAside, loadedTree)
			compareAnswers(test, "the load's error", errorAside, errorTree)
		})
	}
}

// goCommand is go with arguments as the load runs it in goEnvironment's workspace.
func goCommand(goEnvironment *GoEnvironment, arguments []string) *exec.Cmd {
	command := exec.Command("go", arguments...)
	command.Dir = goEnvironment.workspace
	command.Env = append(slices.Clone(goEnvironment.fullEnvironment), "PWD="+goEnvironment.workspace)
	return command
}

// loadAnswer is what a load of the side workspace's patterns under goEnvironment says, as JSON: its diagnostics and,
// by package, every module field the load reads, the replacement's and the error's among them; and its error.
func loadAnswer(test *testing.T, workspace string, goEnvironment *GoEnvironment) ([]byte, []byte) {
	test.Helper()
	loaded, loadError := PackagesCached(workspace, []string{"example.com/a/..."}, goEnvironment.environment, nil,
		Caching{GoEnvironment: goEnvironment})
	if loadError != nil {
		return nil, []byte(loadError.Error())
	}
	modules := map[string]*packages.Module{}
	packages.Visit(loaded.Roots, nil, func(visited *packages.Package) { modules[visited.ID] = visited.Module })
	answer, encodeError := json.MarshalIndent(map[string]any{
		"diagnostics": loaded.Diagnostics,
		"modules":     modules,
		"packages":    slices.Sorted(maps.Keys(modules)),
	}, "", "\t")
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	return answer, nil
}

// compareAnswers fails the test unless the answer beside the side copy is the tree's, byte for byte.
func compareAnswers(test *testing.T, name string, aside []byte, tree []byte) {
	test.Helper()
	if !bytes.Equal(aside, tree) {
		test.Errorf("%s beside the side copy differs from the tree's:\n%s\nwant:\n%s", name, aside, tree)
	}
}

// A go command that cannot run beside a side copy of the go.work runs in the tree, as it always did, and the load
// says go list may have rewritten the go.work.sum: when the copy cannot be made, which a test makes it by having it
// made where nothing can be, and beside a vendor directory, where go prints the go.work's path in two spellings and
// no one spelling of the copy's maps back. Either way the stale file was rewritten, which is what the gap names.
func TestAGoCommandThatCannotRunAsideSaysItMayRewriteTheSum(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name    string
		files   map[string]string
		prepare func(test *testing.T, goEnvironment *GoEnvironment)
	}{
		{name: "the side copy cannot be made", prepare: func(test *testing.T, goEnvironment *GoEnvironment) {
			goEnvironment.guard.sideRoot = filepath.Join(test.TempDir(), "missing")
		}},
		{name: "a vendor directory beside the go.work", files: map[string]string{"vendor/README": "vendored\n"}},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			workspace, environment := writeStaleWorkspace(test, staleWorkspaceFiles, each.files)
			goEnvironment := goEnvironmentOf(test, workspace, environment)
			if each.prepare != nil {
				each.prepare(test, goEnvironment)
			}
			loaded, loadError := PackagesCached(workspace, stalePatterns, environment, nil,
				Caching{GoEnvironment: goEnvironment})
			if loadError != nil {
				test.Fatal(loadError)
			}
			sums := filepath.Join(workspace, "go.work.sum")
			if !sameFile(loaded.UnguardedWorkspaceSum, sums) || loaded.UnguardedWorkspaceSum == "" {
				test.Errorf("the load says %q may have been rewritten, want %s", loaded.UnguardedWorkspaceSum, sums)
			}
			if bytes.Equal(readSums(test, workspace), []byte(staleWorkspaceFiles["go.work.sum"])) {
				test.Errorf("go.work.sum is as it was, so nothing ran in the tree and the gap names nothing")
			}
		})
	}
}

// A side copy's directory holds only what the guard put there, and is gone once the command has run, a failed one
// too: every file removed by name, and then the directory.
func TestASideCopyLeavesNothingBehind(test *testing.T) {
	test.Parallel()
	workspace, environment := writeStaleWorkspace(test, sideWorkspaceFiles)
	goEnvironment := goEnvironmentOf(test, workspace, environment)
	goEnvironment.guard.sideRoot = test.TempDir()
	for _, arguments := range [][]string{{"list", "-m", "-json", "all"}, {"list", "-e", "example.com/nowhere"},
		{"no-such-command"}} {
		// Ignored: a command that fails is one of the cases, and what it printed is not what is judged.
		_, _, _ = goEnvironment.guard.output(goCommand(goEnvironment, arguments))
		left, readError := os.ReadDir(goEnvironment.guard.sideRoot)
		if readError != nil {
			test.Fatal(readError)
		}
		if len(left) > 0 {
			test.Errorf("go %s left %s behind", strings.Join(arguments, " "), left[0].Name())
		}
	}
	if unguarded := goEnvironment.guard.unguardedSum(); unguarded != "" {
		test.Errorf("a command ran beside %s with no side copy", unguarded)
	}
}

// spellBack writes a side spelling as the tree's only where it stands as a whole path: at the start and the end of
// what go printed, between quotes, after a space and before a colon or a parenthesis, and never where it begins or
// ends a longer path.
func TestSpellBackSpellsOnlyWholePaths(test *testing.T) {
	test.Parallel()
	pairs := []spellingPair{{side: "../side/go.work", tree: "go.work"}, {side: "../../x/other", tree: "./other"}}
	for printed, want := range map[string]string{
		"../side/go.work:3: unknown godebug":            "go.work:3: unknown godebug",
		`"Path": "../../x/other"`:                       `"Path": "./other"`,
		"(replaced by ../../x/other)":                   "(replaced by ./other)",
		"replaced by ../../x/other":                     "replaced by ./other",
		"../../x/other2 and a/../../x/other":            "../../x/other2 and a/../../x/other",
		"../side/go.work.sum is not the work file":      "../side/go.work.sum is not the work file",
		"nothing of the side copy is named here at all": "nothing of the side copy is named here at all",
	} {
		if spelled := string(spellBack([]byte(printed), pairs)); spelled != want {
			test.Errorf("%q spelled back is %q, want %q", printed, spelled, want)
		}
	}
}

// A go command run beside the side copy answers what it answers in the tree from whatever directory it runs in, as
// `PWD` names it or not: every path go shortens, and every module root it joins to the go.work's directory, follows
// its own working directory, which is the one the side copy is made and spelled back for. So a workspace reached
// through a symbolic link, with `PWD` the link, without `PWD`, or with a `PWD` that names another directory, answers
// as the tree does: the module listing, its directories and its replacements, and the error naming go.work's line of
// a godebug go does not know.
func TestASideCopyAnswersAsTheTreeDoesFromEveryWorkingDirectory(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		name    string
		changed map[string]string
		want    string
	}{
		{name: "relative replacements", want: `"Path": "./beside"`},
		{name: "a godebug go does not know", want: "go.work:3", changed: map[string]string{
			"go.work": "go 1.21\n\ngodebug unknownsetting=1\n\nuse ./a\n\nreplace example.com/beside => ./beside\n",
		}},
	} {
		workspace, environment := writeStaleWorkspace(test, sideWorkspaceFiles, each.changed)
		link := filepath.Join(test.TempDir(), "link")
		if linkError := os.Symlink(workspace, link); linkError != nil {
			test.Fatal(linkError)
		}
		goEnvironment := goEnvironmentOf(test, workspace, environment)
		for name, working := range map[string]struct {
			directory string
			given     []string
		}{
			"the workspace":                         {directory: workspace, given: []string{"PWD=" + workspace}},
			"a link to it, named by PWD":            {directory: link, given: []string{"PWD=" + link}},
			"a link to it, with no PWD":             {directory: link},
			"the workspace, with PWD another place": {directory: workspace, given: []string{"PWD=" + test.TempDir()}},
		} {
			label := each.name + " from " + name
			command := func() *exec.Cmd {
				listing := exec.Command("go", "list", "-m", "-json", "all")
				listing.Dir = working.directory
				listing.Env = append(slices.DeleteFunc(slices.Clone(goEnvironment.fullEnvironment),
					func(variable string) bool { return strings.HasPrefix(variable, "PWD=") }), working.given...)
				return listing
			}
			aside, standardErrorAside, errorAside := goEnvironment.guard.output(command())
			tree, standardErrorTree, errorTree := runCommand(command())
			compareAnswers(test, label+"'s output", aside, tree)
			compareAnswers(test, label+"'s standard error", standardErrorAside, standardErrorTree)
			compareAnswers(test, label+"'s end", []byte(fmt.Sprint(errorAside)), []byte(fmt.Sprint(errorTree)))
			if !bytes.Contains(append(tree, standardErrorTree...), []byte(each.want)) {
				test.Errorf("%s printed no %s, so nothing was spelled back", label, each.want)
			}
		}
		if unguarded := goEnvironment.guard.unguardedSum(); unguarded != "" {
			test.Errorf("a command ran beside %s with no side copy", unguarded)
		}
	}
}

// A command that prints, beside the side copy, a spelling of the copy's place that cannot be spelled back runs again
// in the tree, so its answer is go's own and never one that names the copy, and the load says go list may have
// rewritten the go.work.sum, which on a stale tree it did. A test has go print the copy's own directory, as nothing go
// prints today does.
func TestAnAnswerBesideTheCopyThatCannotBeSpelledBackRunsInTheTree(test *testing.T) {
	test.Parallel()
	workspace, environment := writeStaleWorkspace(test, staleWorkspaceFiles)
	goEnvironment := goEnvironmentOf(test, workspace, environment)
	goEnvironment.guard.printedAside = func(sideDirectory string) []byte { return []byte(sideDirectory) }
	loaded, loadError := PackagesCached(workspace, stalePatterns, environment, nil,
		Caching{GoEnvironment: goEnvironment})
	if loadError != nil {
		test.Fatalf("the load failed, want the answer the tree's run gave: %v", loadError)
	}
	sums := filepath.Join(workspace, "go.work.sum")
	if !sameFile(loaded.UnguardedWorkspaceSum, sums) || loaded.UnguardedWorkspaceSum == "" {
		test.Errorf("the load says %q may have been rewritten, want %s", loaded.UnguardedWorkspaceSum, sums)
	}
	if bytes.Equal(readSums(test, workspace), []byte(staleWorkspaceFiles["go.work.sum"])) {
		test.Errorf("go.work.sum is as it was, so nothing ran again in the tree")
	}
	if len(loaded.Roots) == 0 || loaded.Roots[0].Module == nil || loaded.Roots[0].Module.Path != "example.com/a" {
		test.Errorf("the load holds no example.com/a, so its answer was not go's in the tree: %v", loaded.Roots)
	}
}
